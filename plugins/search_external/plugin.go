// Package search_external connects an external full text engine
// (OpenSearch/Elasticsearch or Meilisearch).
//
// PostgreSQL stays the source of truth: a background job follows the
// search_changes log and mirrors search_docs into the engine. Searches go to
// the engine once the index is complete; results are filtered by the core
// (trash, tags, permissions). If the engine fails, the built-in search answers.
package search_external

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"github.com/mtsonline/enterprisewiki/internal/perm"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/content"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"

	_ "embed"
)

//go:embed plugin.json
var manifest []byte

//go:embed migrations/*.sql
var migrations embed.FS

//go:embed locales/*.json
var locales embed.FS

// MaxText is the maximum text length of one document sent to the engine.
const MaxText = 100000

// Plugin is the external search plugin.
type Plugin struct {
	host *sdk.Host
	mu   sync.Mutex // one sync at a time in this process
}

func init() { sdk.Register(&Plugin{}) }

// Manifest implements sdk.Plugin.
func (p *Plugin) Manifest() sdk.Manifest { return sdk.ParseManifest(manifest) }

// Migrations implements sdk.MigrationsPlugin.
func (p *Plugin) Migrations() fs.FS { s, _ := fs.Sub(migrations, "migrations"); return s }

// Locales implements sdk.LocalesPlugin.
func (p *Plugin) Locales() fs.FS { s, _ := fs.Sub(locales, "locales"); return s }

func str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return strings.TrimSpace(v)
}

// settings reads plugin_settings."search.external".
func (p *Plugin) settings() Settings {
	m := p.host.Settings()
	s := Settings{Engine: str(m, "engine"), URL: str(m, "url"), Index: str(m, "index"), APIKey: str(m, "api_key"),
		Username: str(m, "username"), Password: str(m, "password")}
	if s.Index == "" {
		s.Index = "ewiki"
	}
	return s
}

// State is the synchronization state.
type State struct {
	Signature string     `json:"-"`
	LastSeq   int64      `json:"last_seq"`
	Ready     bool       `json:"ready"`
	LastSync  *time.Time `json:"last_sync"`
	LastError string     `json:"last_error"`
	Indexed   int64      `json:"indexed"`
}

func (p *Plugin) state(ctx context.Context) (*State, error) {
	var s State
	err := p.host.DB.QueryRow(ctx, `SELECT signature, last_seq, ready, last_sync, last_error, indexed FROM plugin_search_state WHERE id=1`).
		Scan(&s.Signature, &s.LastSeq, &s.Ready, &s.LastSync, &s.LastError, &s.Indexed)
	return &s, err
}

// ---------------------------------------------------------------- backend for the core

type backend struct{ p *Plugin }

func (b backend) Name() string { return b.p.settings().Engine }

func (b backend) Ready(ctx context.Context) bool {
	s := b.p.settings()
	if !s.Configured() {
		return false
	}
	st, err := b.p.state(ctx)
	return err == nil && st.Ready && st.Signature == s.Signature()
}

func (b backend) Search(ctx context.Context, q content.SearchQuery) ([]content.BackendHit, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	hits, err := NewEngine(b.p.settings()).Search(ctx, q.Q, q.Tags, q.Limit, q.Offset)
	if err != nil {
		return nil, err
	}
	out := make([]content.BackendHit, len(hits))
	for i, x := range hits {
		out[i] = content.BackendHit{ElementID: x.ElementID, Lang: x.Lang, Title: x.Title, Snippet: x.Snippet, Score: x.Score}
	}
	return out, nil
}

// ---------------------------------------------------------------- synchronization

// docs loads the current documents of elements (nil ids = all, paged by after).
func (p *Plugin) docs(ctx context.Context, ids []int64, after int64, limit int) ([]Doc, error) {
	rows, err := p.host.DB.Query(ctx, `SELECT d.element_id, d.category_id, d.lang, d.title, left(d.body_text, $4), e.tags
		FROM search_docs d JOIN elements e ON e.id=d.element_id
		WHERE e.deleted_at IS NULL AND ($1::bigint[] IS NULL OR d.element_id = ANY($1)) AND d.element_id > $2
			-- strictly confidential content is not copied into an external index
			AND NOT EXISTS (SELECT 1 FROM categories cc JOIN categories cx ON cx.id = d.category_id WHERE cc.classification >= 3 AND cx.path <@ cc.path)
		ORDER BY d.element_id, d.lang LIMIT $3`, ids, after, limit, MaxText)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Doc, error) {
		var d Doc
		err := r.Scan(&d.ElementID, &d.CategoryID, &d.Lang, &d.Title, &d.Text, &d.Tags)
		d.ID = fmt.Sprintf("e%d_%s", d.ElementID, d.Lang)
		if d.Tags == nil {
			d.Tags = []string{}
		}
		return d, err
	})
}

// rebuild creates the index from scratch.
func (p *Plugin) rebuild(ctx context.Context, e Engine, sig string) error {
	var watermark int64
	if err := p.host.DB.QueryRow(ctx, `SELECT COALESCE(max(seq), 0) FROM search_changes`).Scan(&watermark); err != nil {
		return err
	}
	if _, err := p.host.DB.Exec(ctx, `UPDATE plugin_search_state SET ready=false, signature=$1, indexed=0 WHERE id=1`, sig); err != nil {
		return err
	}
	if err := e.Reset(ctx); err != nil {
		return fmt.Errorf("create index: %w", err)
	}
	var after, n int64
	for {
		// whole elements per batch: fetch ids first
		rows, err := p.host.DB.Query(ctx, `SELECT DISTINCT element_id FROM search_docs WHERE element_id > $1 ORDER BY element_id LIMIT 200`, after)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			break
		}
		docs, err := p.docs(ctx, ids, 0, 100000)
		if err != nil {
			return err
		}
		if err := e.Upsert(ctx, docs); err != nil {
			return fmt.Errorf("index documents: %w", err)
		}
		n += int64(len(docs))
		after = ids[len(ids)-1]
	}
	_, err := p.host.DB.Exec(ctx, `UPDATE plugin_search_state SET ready=true, last_seq=$1, last_sync=now(), last_error='', indexed=$2 WHERE id=1`, watermark, n)
	p.host.Log.Info("search index rebuilt", "engine", e.Name(), "documents", n)
	return err
}

// Sync brings the external index up to date (called by the worker).
func (p *Plugin) Sync(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.settings()
	if !s.Configured() {
		return nil
	}
	err := p.sync(ctx, s)
	msg := ""
	if err != nil {
		msg = err.Error()
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
	}
	if _, uerr := p.host.DB.Exec(context.WithoutCancel(ctx), `UPDATE plugin_search_state SET last_error=$1 WHERE id=1`, msg); uerr != nil && err == nil {
		err = uerr
	}
	return err
}

func (p *Plugin) sync(ctx context.Context, s Settings) error {
	e := NewEngine(s)
	st, err := p.state(ctx)
	if err != nil {
		return err
	}
	var minSeq *int64
	if err := p.host.DB.QueryRow(ctx, `SELECT min(seq) FROM search_changes`).Scan(&minSeq); err != nil {
		return err
	}
	// rebuild: new engine/index, not complete, or changes were pruned meanwhile
	if !st.Ready || st.Signature != s.Signature() || (minSeq != nil && *minSeq > st.LastSeq+1 && st.LastSeq > 0) {
		return p.rebuild(ctx, e, s.Signature())
	}
	for round := 0; round < 20; round++ {
		rows, err := p.host.DB.Query(ctx, `SELECT seq, element_id FROM search_changes WHERE seq > $1 ORDER BY seq LIMIT 2000`, st.LastSeq)
		if err != nil {
			return err
		}
		var maxSeq int64
		set := map[int64]bool{}
		for rows.Next() {
			var seq, id int64
			if err := rows.Scan(&seq, &id); err != nil {
				rows.Close()
				return err
			}
			maxSeq, set[id] = seq, true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(set) == 0 {
			_, err := p.host.DB.Exec(ctx, `UPDATE plugin_search_state SET last_sync=now() WHERE id=1`)
			return err
		}
		ids := make([]int64, 0, len(set))
		for id := range set {
			ids = append(ids, id)
		}
		if err := e.DeleteElements(ctx, ids); err != nil {
			return fmt.Errorf("delete documents: %w", err)
		}
		docs, err := p.docs(ctx, ids, 0, 100000)
		if err != nil {
			return err
		}
		if err := e.Upsert(ctx, docs); err != nil {
			return fmt.Errorf("index documents: %w", err)
		}
		st.LastSeq = maxSeq
		if _, err := p.host.DB.Exec(ctx, `UPDATE plugin_search_state SET last_seq=$1, last_sync=now() WHERE id=1`, maxSeq); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- setup and API

// Setup registers the backend, the worker and the admin API.
func (p *Plugin) Setup(host *sdk.Host) error {
	p.host = host
	host.RegisterSearchBackend(backend{p})
	host.RegisterWorker("sync", 10*time.Second, p.Sync)

	// GET /status: configuration, synchronization state and pending changes.
	host.Handle("GET", "/status", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "search.external.manage"); err != nil {
			return nil, err
		}
		ctx := r.Context()
		s := p.settings()
		st, err := p.state(ctx)
		if err != nil {
			return nil, err
		}
		var pending, docs int64
		if err := host.DB.QueryRow(ctx, `SELECT count(*) FROM search_changes WHERE seq > $1`, st.LastSeq).Scan(&pending); err != nil {
			return nil, err
		}
		if err := host.DB.QueryRow(ctx, `SELECT count(*) FROM search_docs d JOIN elements e ON e.id=d.element_id WHERE e.deleted_at IS NULL`).Scan(&docs); err != nil {
			return nil, err
		}
		out := map[string]any{"configured": s.Configured(), "engine": s.Engine, "url": s.URL, "index": s.Index, "state": st,
			"active": s.Configured() && st.Ready && st.Signature == s.Signature(), "pending_changes": pending, "documents": docs}
		if s.Configured() {
			cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if n, err := NewEngine(s).Count(cctx); err == nil {
				out["engine_documents"] = n
			} else {
				out["engine_error"] = err.Error()
			}
		}
		return out, nil
	})
	// POST /test checks the connection.
	host.Handle("POST", "/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "search.external.manage"); err != nil {
			return nil, err
		}
		s := p.settings()
		if !s.Configured() {
			return nil, apperr.BadRequest("choose an engine and enter its URL first")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if err := NewEngine(s).Health(ctx); err != nil {
			return nil, apperr.BadRequest("connection failed: %s", err.Error())
		}
		return map[string]string{"status": "ok"}, nil
	})
	// POST /reindex rebuilds the index (in the background).
	host.Handle("POST", "/reindex", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := perm.Require(r.Context(), "search.external.manage"); err != nil {
			return nil, err
		}
		if !p.settings().Configured() {
			return nil, apperr.BadRequest("the search engine is not configured")
		}
		if _, err := host.DB.Exec(r.Context(), `UPDATE plugin_search_state SET ready=false WHERE id=1`); err != nil {
			return nil, err
		}
		_ = audit.Log(r.Context(), host.DB, "search.reindex", "settings", 0, nil, nil)
		go func() {
			if err := host.Workers.RunOnce(context.WithoutCancel(r.Context()), p.Manifest().ID+".sync"); err != nil && !errors.Is(err, context.Canceled) {
				host.Log.Error("search rebuild failed", "err", err)
			}
		}()
		return h.NoContent{}, nil
	})
	return nil
}
