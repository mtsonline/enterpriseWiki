// Package audit writes the immutable audit trail. Every mutation of users,
// permissions, categories, elements and settings is recorded with a before
// and after snapshot, which is also what category history and rollback use.
//
// Each entry records who (user, sign-in method, IP address, browser) did
// what (action "<area>.<verb>") to which object. Areas can be switched off
// in the settings except the security relevant ones (Always). Entries can be
// searched, exported and forwarded to other systems (forward.go).
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/db"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// Entry is one audit record.
type Entry struct {
	ID         int64           `json:"id"`
	At         time.Time       `json:"at"`
	UserID     *int64          `json:"user_id"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"`
	EntityType string          `json:"entity_type"`
	EntityID   *int64          `json:"entity_id"`
	Before     json.RawMessage `json:"before,omitempty"`
	After      json.RawMessage `json:"after,omitempty"`
	IP         string          `json:"ip,omitempty"`
	UserAgent  string          `json:"user_agent,omitempty"`
	Via        string          `json:"via,omitempty"` // session, apikey, ssh, cli, system
}

// Area returns the part of an action before the first dot ("user.update" → "user").
func Area(action string) string {
	a, _, _ := strings.Cut(action, ".")
	return a
}

// Always are areas that cannot be switched off.
var Always = []string{"auth", "user", "group", "role", "permissions", "acl", "apikey", "sshkey", "settings", "plugin", "system", "license", "audit", "webhook", "mail"}

// Filter decides whether an action is logged (set by the application from
// the configuration; nil = everything).
type Filter func(action string) bool

var filter atomic.Pointer[Filter]

// SetFilter installs the filter.
func SetFilter(f Filter) { filter.Store(&f) }

// Enabled reports whether an action is logged.
func Enabled(action string) bool {
	if slices.Contains(Always, Area(action)) {
		return true
	}
	if f := filter.Load(); f != nil && *f != nil {
		return (*f)(action)
	}
	return true
}

// request holds the client of an HTTP request.
type request struct{ ip, ua string }

type reqKey struct{}

// WithRequest stores the client's IP address and browser in ctx.
func WithRequest(ctx context.Context, ip, userAgent string) context.Context {
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	return context.WithValue(ctx, reqKey{}, request{ip, userAgent})
}

func toJSON(v any) any {
	if v == nil {
		return nil
	}
	if b, ok := v.(json.RawMessage); ok {
		if len(b) == 0 {
			return nil
		}
		return []byte(b)
	}
	b, err := json.Marshal(v)
	if err != nil || string(b) == "null" {
		return nil
	}
	return b
}

// Log appends an audit record using q (pass the transaction so the record is
// committed atomically with the change).
func Log(ctx context.Context, q db.Querier, action, entityType string, entityID int64, before, after any) error {
	if !Enabled(action) {
		return nil
	}
	p := identity.From(ctx)
	var uid *int64
	if p.ID() > 0 {
		id := p.ID()
		uid = &id
	}
	var eid *int64
	if entityID != 0 {
		eid = &entityID
	}
	req, _ := ctx.Value(reqKey{}).(request)
	via := "system"
	if p != nil && p.Via != "" {
		via = p.Via
	} else if p == nil && req.ip != "" {
		via = "guest"
	}
	_, err := q.Exec(ctx, `INSERT INTO audit_log(user_id, actor, action, entity_type, entity_id, before, after, ip, user_agent, via)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''),NULLIF($9,''),$10)`, uid, p.Handle(), action, entityType, eid, toJSON(before), toJSON(after), req.ip, req.ua, via)
	return err
}

// Search filters audit entries.
type Search struct {
	EntityType string    `json:"entity_type"`
	EntityID   int64     `json:"entity_id"`
	Area       string    `json:"area"`   // e.g. "user"
	Action     string    `json:"action"` // exact action or prefix with "*"
	Actor      string    `json:"actor"`  // part of the handle name@provider
	UserID     int64     `json:"user_id"`
	IP         string    `json:"ip"`
	Text       string    `json:"q"` // text in actor, action, object or the snapshots
	From, To   time.Time `json:"-"`
	Limit      int       `json:"limit"`
	Offset     int       `json:"offset"`
	AfterID    int64     `json:"-"` // forwarding: entries after this id, oldest first
}

const cols = `id, at, user_id, actor, action, entity_type, entity_id, before, after, COALESCE(ip, ''), COALESCE(user_agent, ''), COALESCE(via, '')`

func (s Search) where() (string, []any) {
	conds := []string{"true"}
	args := []any{}
	add := func(cond string, v any) {
		args = append(args, v)
		conds = append(conds, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	if s.EntityType != "" {
		add("entity_type = ?", s.EntityType)
	}
	if s.EntityID != 0 {
		add("entity_id = ?", s.EntityID)
	}
	if s.Area != "" {
		add("split_part(action, '.', 1) = ?", s.Area)
	}
	if s.Action != "" {
		if strings.HasSuffix(s.Action, "*") {
			add("action LIKE ?", strings.TrimSuffix(s.Action, "*")+"%")
		} else {
			add("action = ?", s.Action)
		}
	}
	if s.Actor != "" {
		add("actor ILIKE ?", "%"+s.Actor+"%")
	}
	if s.UserID != 0 {
		add("user_id = ?", s.UserID)
	}
	if s.IP != "" {
		add("ip = ?", s.IP)
	}
	if !s.From.IsZero() {
		add("at >= ?", s.From)
	}
	if !s.To.IsZero() {
		add("at < ?", s.To)
	}
	if s.AfterID > 0 {
		add("id > ?", s.AfterID)
	}
	if t := strings.TrimSpace(s.Text); t != "" {
		add("(actor ILIKE ? OR action ILIKE $X OR entity_type ILIKE $X OR before::text ILIKE $X OR after::text ILIKE $X OR COALESCE(ip,'') ILIKE $X)", "%"+t+"%")
		n := fmt.Sprintf("$%d", len(args))
		conds[len(conds)-1] = strings.ReplaceAll(conds[len(conds)-1], "$X", n)
	}
	return strings.Join(conds, " AND "), args
}

// Find returns entries (newest first; with AfterID oldest first) and the total.
func Find(ctx context.Context, q db.Querier, s Search) ([]Entry, int, error) {
	if s.Limit <= 0 || s.Limit > 1000 {
		s.Limit = 100
	}
	where, args := s.where()
	total := 0
	if s.AfterID == 0 {
		if err := q.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE `+where, args...).Scan(&total); err != nil {
			return nil, 0, err
		}
	}
	order := "at DESC, id DESC"
	if s.AfterID > 0 {
		order = "id"
	}
	args = append(args, s.Limit, s.Offset)
	rows, err := q.Query(ctx, fmt.Sprintf(`SELECT `+cols+` FROM audit_log WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d`, where, order, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.At, &e.UserID, &e.Actor, &e.Action, &e.EntityType, &e.EntityID, &e.Before, &e.After, &e.IP, &e.UserAgent, &e.Via); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// Query lists audit entries of an object, newest first.
func Query(ctx context.Context, q db.Querier, entityType string, entityID int64, limit, offset int) ([]Entry, error) {
	out, _, err := Find(ctx, q, Search{EntityType: entityType, EntityID: entityID, Limit: limit, Offset: offset})
	return out, err
}

// Areas returns the areas that occur in the log (for filters and settings).
func Areas(ctx context.Context, q db.Querier) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT split_part(action, '.', 1) FROM audit_log ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	for _, a := range append(slices.Clone(Always), "category", "element", "comment", "workflow", "event", "menu", "block", "shell", "search", "access") {
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	slices.Sort(out)
	return out, rows.Err()
}
