// Package webhook sends HTTP notifications about events (pages changed,
// event registrations …) to Microsoft Teams, Slack or any JSON endpoint.
// Deliveries are queued in the database right after the event and sent by
// a worker with retries, so a slow or unreachable receiver never blocks the
// wiki and nothing is lost on restarts.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/netguard"
)

// Events lists the event names a webhook can subscribe to.
var Events = []string{
	"element.create", "element.update", "element.delete", "element.move", "element.restore", "element.rollback",
	"category.create", "category.update", "category.delete", "category.move", "category.restore",
	"event.registration", "event.registration_cancelled", "event.occurrence_changed",
}

// Formats of the request body.
var Formats = []string{"json", "slack", "teams"}

// retry delays after failed attempts (then the delivery fails)
var backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// Hook is a configured webhook.
type Hook struct {
	ID         int64      `json:"id"`
	Name       string     `json:"name"`
	URL        string     `json:"url"`
	Format     string     `json:"format"`
	HasSecret  bool       `json:"has_secret"`
	Events     []string   `json:"events"`
	CategoryID *int64     `json:"category_id"`
	Enabled    bool       `json:"enabled"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	LastStatus int        `json:"last_status"`
	LastError  string     `json:"last_error"`
	LastAt     *time.Time `json:"last_at"`
	Pending    int        `json:"pending"`
	Failed     int        `json:"failed"`
}

// Input creates or changes a webhook. Secret: nil keeps the current one,
// "" removes it, "generate" creates a random one (returned once).
type Input struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Format     string   `json:"format"`
	Secret     *string  `json:"secret"`
	Events     []string `json:"events"`
	CategoryID *int64   `json:"category_id"`
	Enabled    *bool    `json:"enabled"`
}

// Object is what an event is about.
type Object struct {
	Type  string `json:"type"` // element | category | event
	ID    int64  `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Event is one notification.
type Event struct {
	Name       string         `json:"event"`
	OccurredAt time.Time      `json:"occurred_at"`
	Site       string         `json:"site"`
	Actor      string         `json:"actor,omitempty"`
	Object     Object         `json:"object"`
	Data       map[string]any `json:"data,omitempty"`
	CategoryID int64          `json:"-"` // for the category filter (0 = none)
}

// Delivery is one queued or sent request.
type Delivery struct {
	ID          int64           `json:"id"`
	Event       string          `json:"event"`
	Status      string          `json:"status"`
	Attempts    int             `json:"attempts"`
	NextAt      time.Time       `json:"next_at"`
	LastStatus  int             `json:"last_status"`
	LastError   string          `json:"last_error"`
	CreatedAt   time.Time       `json:"created_at"`
	DeliveredAt *time.Time      `json:"delivered_at"`
	Payload     json.RawMessage `json:"payload"`
}

// Service manages webhooks.
type Service struct {
	pool *pgxpool.Pool
	cfg  *config.Store
	log  *slog.Logger
	// T translates texts for Teams and Slack messages (default language).
	T func(ctx context.Context, lang, key string, params map[string]string) string
	// Kick asks the worker to deliver soon (set by the app).
	Kick   func()
	client func() *http.Client
}

// New creates the service.
func New(pool *pgxpool.Pool, cfg *config.Store, log *slog.Logger) *Service {
	return &Service{pool: pool, cfg: cfg, log: log, Kick: func() {}}
}

// SetClient replaces the HTTP client (tests).
func (s *Service) SetClient(c func() *http.Client) { s.client = c }

func (s *Service) httpClient(u string) *http.Client {
	if s.client != nil {
		return s.client()
	}
	return netguard.Client([]string{u}, s.cfg.Get().Webhooks.AllowPrivate, 15*time.Second)
}

const hookCols = `h.id, h.name, h.url, h.format, h.secret <> '', h.events, h.category_id, h.enabled, h.created_at, h.updated_at, h.last_status, h.last_error, h.last_at,
	(SELECT count(*) FROM webhook_deliveries d WHERE d.webhook_id=h.id AND d.status='pending')::int,
	(SELECT count(*) FROM webhook_deliveries d WHERE d.webhook_id=h.id AND d.status='failed')::int`

// List returns all webhooks.
func (s *Service) List(ctx context.Context) ([]Hook, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+hookCols+` FROM webhooks h ORDER BY h.name, h.id`)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Hook])
	if list == nil {
		list = []Hook{}
	}
	return list, err
}

// Get returns one webhook.
func (s *Service) Get(ctx context.Context, id int64) (*Hook, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+hookCols+` FROM webhooks h WHERE h.id=$1`, id)
	if err != nil {
		return nil, err
	}
	h, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[Hook])
	if err != nil {
		return nil, apperr.NotFound("webhook")
	}
	return &h, nil
}

func (in *Input) check() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
		return apperr.BadRequest("a name (max. 100 characters) is required")
	}
	u, err := url.Parse(strings.TrimSpace(in.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || len(in.URL) > 2000 {
		return apperr.BadRequest("the URL must be an http(s) address")
	}
	in.URL = u.String()
	if in.Format == "" {
		in.Format = "json"
	}
	if !contains(Formats, in.Format) {
		return apperr.BadRequest("format must be json, slack or teams")
	}
	clean := []string{}
	for _, e := range in.Events {
		if !contains(Events, e) {
			return apperr.BadRequest("unknown event %q", e)
		}
		if !contains(clean, e) {
			clean = append(clean, e)
		}
	}
	in.Events = clean
	if in.CategoryID != nil && *in.CategoryID == 0 {
		in.CategoryID = nil
	}
	return nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// Created is the result of Create/Update; Secret is set once when generated.
type Created struct {
	Hook
	Secret string `json:"secret,omitempty"`
}

func secretValue(in *string) (string, string) {
	if in == nil {
		return "", ""
	}
	if *in == "generate" {
		b := make([]byte, 24)
		_, _ = rand.Read(b)
		v := hex.EncodeToString(b)
		return v, v
	}
	return strings.TrimSpace(*in), ""
}

// Create adds a webhook.
func (s *Service) Create(ctx context.Context, in Input) (*Created, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	secret, shown := secretValue(in.Secret)
	enabled := in.Enabled == nil || *in.Enabled
	var id int64
	if err := s.pool.QueryRow(ctx, `INSERT INTO webhooks(name, url, format, secret, events, category_id, enabled) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		in.Name, in.URL, in.Format, secret, in.Events, in.CategoryID, enabled).Scan(&id); err != nil {
		return nil, err
	}
	h, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Created{Hook: *h, Secret: shown}, nil
}

// Update changes a webhook.
func (s *Service) Update(ctx context.Context, id int64, in Input) (*Created, error) {
	if err := in.check(); err != nil {
		return nil, err
	}
	enabled := in.Enabled == nil || *in.Enabled
	secret, shown := secretValue(in.Secret)
	tag, err := s.pool.Exec(ctx, `UPDATE webhooks SET name=$2, url=$3, format=$4, events=$5, category_id=$6, enabled=$7, updated_at=now(),
		secret = CASE WHEN $8 THEN $9 ELSE secret END WHERE id=$1`, id, in.Name, in.URL, in.Format, in.Events, in.CategoryID, enabled, in.Secret != nil, secret)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, apperr.NotFound("webhook")
	}
	h, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return &Created{Hook: *h, Secret: shown}, nil
}

// Delete removes a webhook with its deliveries.
func (s *Service) Delete(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM webhooks WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("webhook")
	}
	return nil
}

// Deliveries lists the latest deliveries of a webhook.
func (s *Service) Deliveries(ctx context.Context, id int64, limit int) ([]Delivery, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT id, event, status, attempts, next_at, last_status, last_error, created_at, delivered_at, payload
		FROM webhook_deliveries WHERE webhook_id=$1 ORDER BY created_at DESC, id DESC LIMIT $2`, id, limit)
	if err != nil {
		return nil, err
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Delivery])
	if list == nil {
		list = []Delivery{}
	}
	return list, err
}

// Redeliver queues a delivery again (e.g. after it failed).
func (s *Service) Redeliver(ctx context.Context, hookID, deliveryID int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE webhook_deliveries SET status='pending', next_at=now(), attempts=0 WHERE id=$1 AND webhook_id=$2`, deliveryID, hookID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound("delivery")
	}
	s.Kick()
	return nil
}

// Test queues a test event for one webhook.
func (s *Service) Test(ctx context.Context, id int64) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	c := s.cfg.Get()
	ev := Event{Name: "test", OccurredAt: time.Now().UTC(), Site: c.UI.SiteName, Actor: actorName(ctx),
		Object: Object{Type: "site", Title: c.UI.SiteName, URL: strings.TrimSuffix(c.Server.BaseURL, "/") + "/"}}
	b, _ := json.Marshal(ev)
	if _, err := s.pool.Exec(ctx, `INSERT INTO webhook_deliveries(webhook_id, event, payload) VALUES ($1, 'test', $2)`, id, b); err != nil {
		return err
	}
	s.Kick()
	return nil
}

func actorName(ctx context.Context) string {
	p := identity.From(ctx)
	if p == nil || p.UserID == 0 {
		return ""
	}
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.Username
}

// Emit queues an event for all matching webhooks (errors are logged).
func (s *Service) Emit(ctx context.Context, ev Event) {
	ctx = context.WithoutCancel(ctx)
	c := s.cfg.Get()
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	ev.Site = c.UI.SiteName
	if ev.Actor == "" {
		ev.Actor = actorName(ctx)
	}
	if ev.Object.URL != "" && strings.HasPrefix(ev.Object.URL, "/") {
		ev.Object.URL = strings.TrimSuffix(c.Server.BaseURL, "/") + ev.Object.URL
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO webhook_deliveries(webhook_id, event, payload)
		SELECT h.id, $1, $2 FROM webhooks h WHERE h.enabled AND (cardinality(h.events) = 0 OR $1 = ANY(h.events))
			AND (h.category_id IS NULL OR EXISTS (SELECT 1 FROM categories a, categories c WHERE c.id = $3 AND a.id = h.category_id AND a.path @> c.path))`, ev.Name, b, ev.CategoryID)
	if err != nil {
		s.log.Error("queuing webhooks failed", "event", ev.Name, "err", err)
		return
	}
	if tag.RowsAffected() > 0 {
		s.Kick()
	}
}

type due struct {
	id, hook            int64
	event               string
	payload             []byte
	attempts            int
	url, format, secret string
}

// Deliver sends due deliveries (worker). It returns the number sent.
func (s *Service) Deliver(ctx context.Context) (int, error) {
	sent := 0
	for round := 0; round < 10; round++ {
		var list []due
		err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `SELECT d.id, d.webhook_id, d.event, d.payload, d.attempts, h.url, h.format, h.secret
				FROM webhook_deliveries d JOIN webhooks h ON h.id = d.webhook_id
				WHERE d.status='pending' AND d.next_at <= now() AND h.enabled ORDER BY d.next_at, d.id LIMIT 20 FOR UPDATE OF d SKIP LOCKED`)
			if err != nil {
				return err
			}
			list, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (due, error) {
				var d due
				return d, r.Scan(&d.id, &d.hook, &d.event, &d.payload, &d.attempts, &d.url, &d.format, &d.secret)
			})
			if err != nil || len(list) == 0 {
				return err
			}
			// claim: a crash during sending retries after the first back-off
			for _, d := range list {
				if _, err := tx.Exec(ctx, `UPDATE webhook_deliveries SET next_at = now() + interval '1 minute' WHERE id=$1`, d.id); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil || len(list) == 0 {
			return sent, err
		}
		for _, d := range list {
			status, errText := s.send(ctx, d)
			ok := status >= 200 && status < 300
			attempts := d.attempts + 1
			st, next := "pending", time.Now()
			switch {
			case ok:
				st = "delivered"
				sent++
			case attempts > len(backoff):
				st = "failed"
			default:
				next = next.Add(backoff[attempts-1])
			}
			if _, err := s.pool.Exec(ctx, `UPDATE webhook_deliveries SET status=$2, attempts=$3, next_at=$4, last_status=$5, last_error=$6,
				delivered_at = CASE WHEN $2 = 'delivered' THEN now() END WHERE id=$1`, d.id, st, attempts, next, status, errText); err != nil {
				return sent, err
			}
			if _, err := s.pool.Exec(ctx, `UPDATE webhooks SET last_status=$2, last_error=$3, last_at=now() WHERE id=$1`, d.hook, status, errText); err != nil {
				return sent, err
			}
			if !ok {
				s.log.Warn("webhook delivery failed", "webhook", d.hook, "delivery", d.id, "status", status, "err", errText, "attempt", attempts)
			}
		}
	}
	return sent, nil
}

// Cleanup removes old deliveries.
func (s *Service) Cleanup(ctx context.Context, days int) error {
	if days <= 0 {
		days = 30
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM webhook_deliveries WHERE status <> 'pending' AND created_at < now() - make_interval(days => $1)`, days)
	return err
}

func (s *Service) send(ctx context.Context, d due) (int, string) {
	var ev Event
	if err := json.Unmarshal(d.payload, &ev); err != nil {
		return 0, "invalid payload"
	}
	body, err := s.Body(ctx, d.format, ev, d.payload)
	if err != nil {
		return 0, err.Error()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
	if err != nil {
		return 0, err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "EnterpriseWiki-Webhook/1")
	req.Header.Set("X-Ewiki-Event", d.event)
	req.Header.Set("X-Ewiki-Delivery", fmt.Sprint(d.id))
	if d.secret != "" {
		m := hmac.New(sha256.New, []byte(d.secret))
		m.Write(body)
		req.Header.Set("X-Ewiki-Signature", "sha256="+hex.EncodeToString(m.Sum(nil)))
	}
	res, err := s.httpClient(d.url).Do(req)
	if err != nil {
		msg := err.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return 0, msg
	}
	defer res.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(res.Body, 300))
	if res.StatusCode >= 300 {
		return res.StatusCode, strings.TrimSpace(string(snippet))
	}
	return res.StatusCode, ""
}

// Body renders the request body of an event in a format.
func (s *Service) Body(ctx context.Context, format string, ev Event, raw []byte) ([]byte, error) {
	if format == "json" {
		if raw != nil {
			return raw, nil
		}
		return json.Marshal(ev)
	}
	lang := s.cfg.Get().I18n.DefaultLanguage
	params := map[string]string{"title": ev.Object.Title, "actor": ev.Actor, "site": ev.Site}
	for k, v := range ev.Data {
		params[k] = fmt.Sprint(v)
	}
	if params["actor"] == "" {
		params["actor"] = s.tr(ctx, lang, "webhook.someone", nil)
	}
	text := s.tr(ctx, lang, "webhook.text."+ev.Name, params)
	open := s.tr(ctx, lang, "webhook.open", nil)
	switch format {
	case "slack":
		msg := text
		if ev.Object.URL != "" {
			msg += fmt.Sprintf("\n<%s|%s>", ev.Object.URL, slackEscape(open))
		}
		return json.Marshal(map[string]any{"text": slackEscape(ev.Site) + ": " + msg, "unfurl_links": false})
	case "teams":
		// Adaptive Card for Teams workflows ("When a Teams webhook request is received")
		card := map[string]any{
			"$schema": "http://adaptivecards.io/schemas/adaptive-card.json", "type": "AdaptiveCard", "version": "1.4",
			"body": []map[string]any{
				{"type": "TextBlock", "text": ev.Site, "isSubtle": true, "size": "Small", "wrap": true},
				{"type": "TextBlock", "text": ev.Object.Title, "weight": "Bolder", "size": "Medium", "wrap": true},
				{"type": "TextBlock", "text": text, "wrap": true},
			},
		}
		if ev.Object.URL != "" {
			card["actions"] = []map[string]any{{"type": "Action.OpenUrl", "title": open, "url": ev.Object.URL}}
		}
		return json.Marshal(map[string]any{"type": "message", "attachments": []map[string]any{{
			"contentType": "application/vnd.microsoft.card.adaptive", "contentUrl": nil, "content": card}}})
	}
	return nil, fmt.Errorf("unknown format %q", format)
}

func (s *Service) tr(ctx context.Context, lang, key string, params map[string]string) string {
	if s.T == nil {
		return key
	}
	return s.T(ctx, lang, key, params)
}

func slackEscape(v string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(v)
}
