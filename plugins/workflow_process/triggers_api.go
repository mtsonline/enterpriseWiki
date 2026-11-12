package workflow_process

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtsonline/enterprisewiki/internal/acl"
	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// mayChange reports whether the caller may change the workflow (its
// creator with the create permission, or a workflow administrator).
func (pl *Plugin) mayChange(ctx context.Context, element int64) (int64, error) {
	cat, err := pl.host.Content.ElementCategory(ctx, element, acl.View)
	if err != nil {
		return 0, err
	}
	var created *int64
	var typ string
	if err := pl.host.DB.QueryRow(ctx, `SELECT created_by, type FROM elements WHERE id=$1`, element).Scan(&created, &typ); err != nil || typ != "workflow.process" {
		return 0, apperr.NotFound("workflow")
	}
	p := identity.From(ctx)
	own := p != nil && created != nil && *created == p.ID()
	if (own && (pl.host.Can(ctx, cat, permCreate) || pl.host.Can(ctx, cat, permAdmin))) || pl.host.Can(ctx, cat, permAdmin) {
		return cat, nil
	}
	return 0, apperr.Forbidden("only the creator of the workflow or a workflow administrator may do this")
}

// TriggerStatus describes a saved trigger for its editor.
type TriggerStatus struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Name      string     `json:"name,omitempty"`
	Active    bool       `json:"active"`
	HasSecret bool       `json:"has_secret,omitempty"`
	URL       string     `json:"url,omitempty"`
	Next      *time.Time `json:"next,omitempty"`
	LastAt    string     `json:"last_at,omitempty"`
	LastError string     `json:"last_error,omitempty"`
	Starts24h int        `json:"starts_24h"`
}

func (pl *Plugin) triggerRoutes(host *sdk.Host) {
	// POST /hooks/{id}/{trigger}: incoming webhook (signed; see receiveHook)
	host.Handle("POST", "/hooks/{id}/{trigger}", pl.receiveHook)
	host.Signed("POST", "/hooks/{id}/{trigger}")

	// GET /elements/{id}/triggers: the saved triggers with their status
	host.Handle("GET", "/elements/{id}/triggers", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		if _, err := pl.mayChange(ctx, id); err != nil {
			return nil, err
		}
		def, _, _, err := pl.definitionBy(ctx, id, "")
		if err != nil {
			return nil, err
		}
		active := map[string]map[string]any{}
		rows, err := host.DB.Query(ctx, `SELECT trigger_id, spec FROM plugin_workflow_triggers WHERE element_id=$1`, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var tid string
			var raw json.RawMessage
			if err := rows.Scan(&tid, &raw); err != nil {
				rows.Close()
				return nil, err
			}
			m := map[string]any{}
			_ = json.Unmarshal(raw, &m)
			active[tid] = m
		}
		rows.Close()
		secrets := map[string]bool{}
		srows, err := host.DB.Query(ctx, `SELECT trigger_id FROM plugin_workflow_hook_secrets WHERE element_id=$1`, id)
		if err != nil {
			return nil, err
		}
		names, err := pgx.CollectRows(srows, pgx.RowTo[string])
		if err != nil {
			return nil, err
		}
		for _, n := range names {
			secrets[n] = true
		}
		base := strings.TrimSuffix(host.Config.Get().Server.BaseURL, "/")
		out := []TriggerStatus{}
		for _, t := range def.Triggers {
			st := TriggerStatus{ID: t.ID, Type: t.Type, Name: t.Name}
			if m, ok := active[t.ID]; ok {
				st.Active = true
				st.LastAt, _ = m["_last_at"].(string)
				st.LastError, _ = m["_last_error"].(string)
			}
			if t.Type == TrigWebhook {
				st.HasSecret = secrets[t.ID]
				st.URL = fmt.Sprintf("%s/api/v1/x/workflow.process/hooks/%d/%s", base, id, t.ID)
			}
			if t.Type == TrigSchedule {
				if c, err := ParseCron(t.Cron); err == nil {
					if n, ok := c.Next(time.Now()); ok {
						st.Next = &n
					}
				}
			}
			if err := host.DB.QueryRow(ctx, `SELECT count(*) FROM plugin_workflow_fired WHERE element_id=$1 AND trigger_id=$2 AND at > now() - interval '1 day'`,
				id, t.ID).Scan(&st.Starts24h); err != nil {
				return nil, err
			}
			out = append(out, st)
		}
		return map[string]any{"triggers": out}, nil
	})

	// POST /elements/{id}/triggers/{trigger}/secret: a new secret for an
	// incoming webhook (shown once; the old one stops working)
	host.Handle("POST", "/elements/{id}/triggers/{trigger}/secret", func(w http.ResponseWriter, r *http.Request) (any, error) {
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		ctx := r.Context()
		if _, err := pl.mayChange(ctx, id); err != nil {
			return nil, err
		}
		def, _, _, err := pl.definitionBy(ctx, id, "")
		if err != nil {
			return nil, err
		}
		t := def.Trigger(r.PathValue("trigger"))
		if t == nil || t.Type != TrigWebhook {
			return nil, apperr.NotFound("webhook trigger (save the workflow first)")
		}
		secret, err := pl.newHookSecret(ctx, id, t.ID)
		if err != nil {
			return nil, err
		}
		base := strings.TrimSuffix(host.Config.Get().Server.BaseURL, "/")
		return map[string]string{"secret": secret, "url": fmt.Sprintf("%s/api/v1/x/workflow.process/hooks/%d/%s", base, id, t.ID)}, nil
	})
}
