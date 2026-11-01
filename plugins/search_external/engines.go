package search_external

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Doc is a document in the external index (one per element and language).
type Doc struct {
	ID         string   `json:"id"`
	ElementID  int64    `json:"element_id"`
	CategoryID int64    `json:"category_id"`
	Lang       string   `json:"lang"`
	Title      string   `json:"title"`
	Text       string   `json:"text"`
	Tags       []string `json:"tags"`
}

// Hit is a search result of an engine.
type Hit struct {
	ElementID int64
	Lang      string
	Title     string
	Snippet   string // matches between \x01 and \x02
	Score     float64
}

// Engine is an external search engine.
type Engine interface {
	Name() string
	Health(ctx context.Context) error
	// Reset creates an empty index with the right settings.
	Reset(ctx context.Context) error
	Upsert(ctx context.Context, docs []Doc) error
	DeleteElements(ctx context.Context, ids []int64) error
	Search(ctx context.Context, q string, tags []string, limit, offset int) ([]Hit, error)
	Count(ctx context.Context) (int64, error)
}

// Settings configures the engine (plugin_settings."search.external").
type Settings struct {
	Engine   string // opensearch | meilisearch
	URL      string
	Index    string
	APIKey   string
	Username string
	Password string
}

// Configured reports whether an engine is set up.
func (s Settings) Configured() bool {
	return s.URL != "" && (s.Engine == "opensearch" || s.Engine == "meilisearch")
}

// Signature identifies the index (a change triggers a rebuild).
func (s Settings) Signature() string {
	return s.Engine + "|" + strings.TrimSuffix(s.URL, "/") + "|" + s.Index
}

// NewEngine creates the configured engine.
func NewEngine(s Settings) Engine {
	c := &client{base: strings.TrimSuffix(s.URL, "/"), s: s, http: &http.Client{Timeout: 30 * time.Second}}
	if s.Engine == "meilisearch" {
		return &meili{c}
	}
	return &opensearch{c}
}

type client struct {
	base string
	s    Settings
	http *http.Client
}

type httpError struct {
	Status int
	Body   string
}

func (e *httpError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

// do sends a request; body may be []byte (sent as is) or a value (JSON).
func (c *client) do(ctx context.Context, method, path string, body any, contentType string, out any) error {
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		r = bytes.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			return err
		}
		r = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return err
	}
	if contentType == "" {
		contentType = "application/json"
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	if c.s.APIKey != "" {
		if c.s.Engine == "meilisearch" {
			req.Header.Set("Authorization", "Bearer "+c.s.APIKey)
		} else {
			req.Header.Set("Authorization", "ApiKey "+c.s.APIKey)
		}
	} else if c.s.Username != "" {
		req.SetBasicAuth(c.s.Username, c.s.Password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if resp.StatusCode >= 300 {
		msg := string(data)
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return &httpError{resp.StatusCode, msg}
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

func isStatus(err error, code int) bool {
	he, ok := err.(*httpError)
	return ok && he.Status == code
}

// ---------------------------------------------------------------- OpenSearch / Elasticsearch

type opensearch struct{ c *client }

func (o *opensearch) Name() string { return "opensearch" }

func (o *opensearch) Health(ctx context.Context) error {
	return o.c.do(ctx, "GET", "/", nil, "", nil)
}

func (o *opensearch) Reset(ctx context.Context) error {
	if err := o.c.do(ctx, "DELETE", "/"+o.c.s.Index, nil, "", nil); err != nil && !isStatus(err, 404) {
		return err
	}
	return o.c.do(ctx, "PUT", "/"+o.c.s.Index, map[string]any{
		"mappings": map[string]any{"properties": map[string]any{
			"element_id": map[string]string{"type": "long"}, "category_id": map[string]string{"type": "long"},
			"lang": map[string]string{"type": "keyword"}, "tags": map[string]string{"type": "keyword"},
			"title": map[string]string{"type": "text"}, "text": map[string]string{"type": "text"},
		}},
	}, "", nil)
}

func (o *opensearch) Upsert(ctx context.Context, docs []Doc) error {
	if len(docs) == 0 {
		return nil
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, d := range docs {
		_ = enc.Encode(map[string]any{"index": map[string]string{"_index": o.c.s.Index, "_id": d.ID}})
		_ = enc.Encode(d)
	}
	var res struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Error any `json:"error"`
		} `json:"items"`
	}
	if err := o.c.do(ctx, "POST", "/_bulk?refresh=wait_for", b.Bytes(), "application/x-ndjson", &res); err != nil {
		return err
	}
	if res.Errors {
		for _, it := range res.Items {
			for _, v := range it {
				if v.Error != nil {
					e, _ := json.Marshal(v.Error)
					return fmt.Errorf("bulk indexing failed: %s", e)
				}
			}
		}
	}
	return nil
}

func (o *opensearch) DeleteElements(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return o.c.do(ctx, "POST", "/"+o.c.s.Index+"/_delete_by_query?refresh=true&conflicts=proceed",
		map[string]any{"query": map[string]any{"terms": map[string]any{"element_id": ids}}}, "", nil)
}

func (o *opensearch) Search(ctx context.Context, q string, tags []string, limit, offset int) ([]Hit, error) {
	filter := []any{}
	for _, t := range tags {
		filter = append(filter, map[string]any{"term": map[string]string{"tags": t}})
	}
	body := map[string]any{
		"from": offset, "size": limit, "_source": []string{"element_id", "lang", "title"},
		"query": map[string]any{"bool": map[string]any{
			"must":   []any{map[string]any{"multi_match": map[string]any{"query": q, "fields": []string{"title^3", "tags^2", "text"}, "fuzziness": "AUTO"}}},
			"filter": filter,
		}},
		"highlight": map[string]any{"pre_tags": []string{"\x01"}, "post_tags": []string{"\x02"},
			"fields": map[string]any{"text": map[string]any{"fragment_size": 160, "number_of_fragments": 2}}},
	}
	var res struct {
		Hits struct {
			Hits []struct {
				Score     float64 `json:"_score"`
				Source    Doc     `json:"_source"`
				Highlight struct {
					Text []string `json:"text"`
				} `json:"highlight"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := o.c.do(ctx, "POST", "/"+o.c.s.Index+"/_search", body, "", &res); err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(res.Hits.Hits))
	for _, h := range res.Hits.Hits {
		out = append(out, Hit{ElementID: h.Source.ElementID, Lang: h.Source.Lang, Title: h.Source.Title,
			Snippet: strings.Join(h.Highlight.Text, " … "), Score: h.Score})
	}
	return out, nil
}

func (o *opensearch) Count(ctx context.Context) (int64, error) {
	var res struct {
		Count int64 `json:"count"`
	}
	err := o.c.do(ctx, "GET", "/"+o.c.s.Index+"/_count", nil, "", &res)
	return res.Count, err
}

// ---------------------------------------------------------------- Meilisearch

type meili struct{ c *client }

func (m *meili) Name() string { return "meilisearch" }

func (m *meili) Health(ctx context.Context) error {
	if err := m.c.do(ctx, "GET", "/health", nil, "", nil); err != nil {
		return err
	}
	// checks the key, too
	err := m.c.do(ctx, "GET", "/indexes/"+m.c.s.Index, nil, "", nil)
	if isStatus(err, 404) {
		return nil
	}
	return err
}

// wait waits for an asynchronous Meilisearch task.
func (m *meili) wait(ctx context.Context, task struct {
	TaskUID int64 `json:"taskUid"`
}) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		var t struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
				Code    string `json:"code"`
			} `json:"error"`
		}
		if err := m.c.do(ctx, "GET", "/tasks/"+strconv.FormatInt(task.TaskUID, 10), nil, "", &t); err != nil {
			return err
		}
		switch t.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			if t.Error != nil && t.Error.Code == "index_not_found" {
				return nil
			}
			if t.Error != nil {
				return fmt.Errorf("meilisearch task failed: %s", t.Error.Message)
			}
			return fmt.Errorf("meilisearch task %s", t.Status)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("meilisearch task %d did not finish", task.TaskUID)
}

type task = struct {
	TaskUID int64 `json:"taskUid"`
}

func (m *meili) Reset(ctx context.Context) error {
	idx := m.c.s.Index
	var t task
	if err := m.c.do(ctx, "DELETE", "/indexes/"+idx, nil, "", &t); err != nil && !isStatus(err, 404) {
		return err
	} else if err == nil {
		if err := m.wait(ctx, t); err != nil {
			return err
		}
	}
	if err := m.c.do(ctx, "POST", "/indexes", map[string]string{"uid": idx, "primaryKey": "id"}, "", &t); err != nil {
		return err
	}
	if err := m.wait(ctx, t); err != nil {
		return err
	}
	if err := m.c.do(ctx, "PATCH", "/indexes/"+idx+"/settings", map[string]any{
		"searchableAttributes": []string{"title", "tags", "text"},
		"filterableAttributes": []string{"element_id", "category_id", "lang", "tags"},
	}, "", &t); err != nil {
		return err
	}
	return m.wait(ctx, t)
}

func (m *meili) Upsert(ctx context.Context, docs []Doc) error {
	if len(docs) == 0 {
		return nil
	}
	var t task
	if err := m.c.do(ctx, "POST", "/indexes/"+m.c.s.Index+"/documents", docs, "", &t); err != nil {
		return err
	}
	return m.wait(ctx, t)
}

func (m *meili) DeleteElements(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	var t task
	if err := m.c.do(ctx, "POST", "/indexes/"+m.c.s.Index+"/documents/delete",
		map[string]string{"filter": "element_id IN [" + strings.Join(parts, ", ") + "]"}, "", &t); err != nil {
		return err
	}
	return m.wait(ctx, t)
}

func quoteFilter(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func (m *meili) Search(ctx context.Context, q string, tags []string, limit, offset int) ([]Hit, error) {
	filters := []string{}
	for _, t := range tags {
		filters = append(filters, "tags = "+quoteFilter(t))
	}
	body := map[string]any{
		"q": q, "limit": limit, "offset": offset, "attributesToRetrieve": []string{"element_id", "lang", "title"},
		"attributesToCrop": []string{"text"}, "cropLength": 30, "attributesToHighlight": []string{"text"},
		"highlightPreTag": "\x01", "highlightPostTag": "\x02", "showRankingScore": true,
	}
	if len(filters) > 0 {
		body["filter"] = strings.Join(filters, " AND ")
	}
	var res struct {
		Hits []struct {
			ElementID int64   `json:"element_id"`
			Lang      string  `json:"lang"`
			Title     string  `json:"title"`
			Score     float64 `json:"_rankingScore"`
			Formatted struct {
				Text string `json:"text"`
			} `json:"_formatted"`
		} `json:"hits"`
	}
	if err := m.c.do(ctx, "POST", "/indexes/"+m.c.s.Index+"/search", body, "", &res); err != nil {
		return nil, err
	}
	out := make([]Hit, 0, len(res.Hits))
	for _, h := range res.Hits {
		out = append(out, Hit{ElementID: h.ElementID, Lang: h.Lang, Title: h.Title, Snippet: h.Formatted.Text, Score: h.Score})
	}
	return out, nil
}

func (m *meili) Count(ctx context.Context) (int64, error) {
	var res struct {
		N int64 `json:"numberOfDocuments"`
	}
	err := m.c.do(ctx, "GET", "/indexes/"+m.c.s.Index+"/stats", nil, "", &res)
	return res.N, err
}
