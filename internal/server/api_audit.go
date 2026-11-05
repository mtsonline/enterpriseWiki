package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/config"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/identity"
)

// auditSearch reads the filters of the audit log from the query string:
// q, area, action (prefix with *), actor, user_id, ip, entity_type,
// entity_id, from, to (date or RFC 3339), limit, offset.
func auditSearch(r *http.Request) audit.Search {
	q := r.URL.Query()
	id, _ := strconv.ParseInt(q.Get("entity_id"), 10, 64)
	uid, _ := strconv.ParseInt(q.Get("user_id"), 10, 64)
	parse := func(v string, end bool) time.Time {
		if v == "" {
			return time.Time{}
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
		if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
			if end {
				t = t.AddDate(0, 0, 1) // to: including the whole day
			}
			return t
		}
		return time.Time{}
	}
	return audit.Search{EntityType: q.Get("entity_type"), EntityID: id, Area: q.Get("area"), Action: q.Get("action"), Actor: q.Get("actor"),
		UserID: uid, IP: q.Get("ip"), Text: q.Get("q"), From: parse(q.Get("from"), false), To: parse(q.Get("to"), true),
		Limit: h.QueryInt(r, "limit", 100), Offset: h.QueryInt(r, "offset", 0)}
}

// auditSettings is the editable part of the audit configuration.
type auditSettings struct {
	Disabled  []string              `json:"disabled"`
	LogLogins bool                  `json:"log_logins"`
	LogDenied bool                  `json:"log_denied"`
	LogViews  bool                  `json:"log_views"`
	Forward   []config.AuditForward `json:"forward"`
}

func maskForward(fs []config.AuditForward) []config.AuditForward {
	out := make([]config.AuditForward, len(fs))
	for i, f := range fs {
		if f.Secret != "" {
			f.Secret = secretMask
		}
		hs := map[string]string{}
		for k, v := range f.Headers {
			hs[k] = secretMask
			if v == "" {
				hs[k] = ""
			}
		}
		f.Headers = hs
		out[i] = f
	}
	return out
}

// keepSecrets restores masked secrets and headers from the stored destinations.
func keepSecrets(in, stored []config.AuditForward) {
	for i := range in {
		f := &in[i]
		for _, o := range stored {
			if o.ID != f.ID {
				continue
			}
			if f.Secret == secretMask {
				f.Secret = o.Secret
			}
			for k, v := range f.Headers {
				if v == secretMask {
					f.Headers[k] = o.Headers[k]
				}
			}
		}
	}
}

func (s *Server) routesAudit() {
	// GET /api/v1/admin/audit/search – entries with total and the known areas
	s.handle("GET /api/v1/admin/audit/search", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		entries, total, err := audit.Find(r.Context(), s.app.DB, auditSearch(r))
		if err != nil {
			return nil, err
		}
		areas, err := audit.Areas(r.Context(), s.app.DB)
		return map[string]any{"entries": entries, "total": total, "areas": areas}, err
	})
	// GET /api/v1/admin/audit/export?format=csv|jsonl&… – the filtered entries as a file
	h.RecordRoute("GET /api/v1/admin/audit/export")
	s.app.Mux.HandleFunc("GET /api/v1/admin/audit/export", func(w http.ResponseWriter, r *http.Request) {
		if err := requireAdmin(r); err != nil {
			h.WriteError(w, r, err)
			return
		}
		f := auditSearch(r)
		format := r.URL.Query().Get("format")
		if format != "csv" && format != "jsonl" {
			format = "jsonl"
		}
		name := "audit-" + time.Now().Format("20060102-150405") + "." + format
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		var cw *csv.Writer
		if format == "csv" {
			w.Header().Set("Content-Type", "text/csv; charset=utf-8")
			_, _ = w.Write([]byte("\xef\xbb\xbf")) // BOM for spreadsheet programs
			cw = csv.NewWriter(w)
			_ = cw.Write([]string{"id", "time", "actor", "action", "entity_type", "entity_id", "ip", "via", "user_agent", "before", "after"})
		} else {
			w.Header().Set("Content-Type", "application/x-ndjson")
		}
		_ = audit.Log(r.Context(), s.app.DB, "audit.export", "audit", 0, nil, map[string]any{"format": format, "filter": r.URL.RawQuery})
		f.Offset = 0
		for page := 0; page < 1000; page++ { // max. 1 000 000 entries
			f.Limit = 1000
			entries, _, err := audit.Find(r.Context(), s.app.DB, f)
			if err != nil {
				return
			}
			for _, e := range entries {
				if cw != nil {
					eid := ""
					if e.EntityID != nil {
						eid = strconv.FormatInt(*e.EntityID, 10)
					}
					_ = cw.Write([]string{strconv.FormatInt(e.ID, 10), e.At.Format(time.RFC3339), e.Actor, e.Action, e.EntityType, eid, e.IP, e.Via, e.UserAgent, string(e.Before), string(e.After)})
				} else {
					b, _ := json.Marshal(e)
					_, _ = w.Write(append(b, '\n'))
				}
			}
			if cw != nil {
				cw.Flush()
			}
			if len(entries) < f.Limit {
				return
			}
			f.Offset += f.Limit
		}
	})
	s.handle("GET /api/v1/admin/audit/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		c := s.app.Config.Get().Audit
		status, err := s.app.AuditForward.Status(r.Context())
		if err != nil {
			return nil, err
		}
		areas, err := audit.Areas(r.Context(), s.app.DB)
		if err != nil {
			return nil, err
		}
		disabled := c.Disabled
		if disabled == nil {
			disabled = []string{}
		}
		return map[string]any{"settings": auditSettings{Disabled: disabled, LogLogins: c.Logins(), LogDenied: c.LogDenied, LogViews: c.LogViews, Forward: maskForward(c.Forward)},
			"status": status, "areas": areas, "always": audit.Always, "types": audit.ForwardTypes}, nil
	})
	s.handle("PUT /api/v1/admin/audit/settings", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in auditSettings
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		in.Disabled = slices.DeleteFunc(in.Disabled, func(a string) bool { return slices.Contains(audit.Always, a) || strings.TrimSpace(a) == "" })
		ids := map[string]bool{}
		for i := range in.Forward {
			f := &in.Forward[i]
			if f.ID == "" {
				f.ID = fmt.Sprintf("f%d", time.Now().UnixNano()%1e9+int64(i))
			}
			if ids[f.ID] {
				return nil, apperr.BadRequest("duplicate destination id %q", f.ID)
			}
			ids[f.ID] = true
			if strings.TrimSpace(f.Name) == "" {
				f.Name = f.Type
			}
			if err := audit.ValidateForward(*f); err != nil {
				return nil, apperr.BadRequest("%s: %s", f.Name, err.Error())
			}
		}
		before := s.app.Config.Get().Audit
		keepSecrets(in.Forward, before.Forward)
		logins := in.LogLogins
		err := s.app.Config.Update(func(c *config.Config) error {
			c.Audit.Disabled, c.Audit.LogLogins, c.Audit.LogDenied, c.Audit.LogViews, c.Audit.Forward = in.Disabled, &logins, in.LogDenied, in.LogViews, in.Forward
			return nil
		})
		if err != nil {
			return nil, err
		}
		after := in
		after.Forward = maskForward(in.Forward)
		beforeView := auditSettings{Disabled: before.Disabled, LogLogins: before.Logins(), LogDenied: before.LogDenied, LogViews: before.LogViews, Forward: maskForward(before.Forward)}
		_ = audit.Log(r.Context(), s.app.DB, "audit.settings", "audit", 0, beforeView, after)
		return h.NoContent{}, nil
	})
	// POST /api/v1/admin/audit/forward/test – sends a test entry to a destination (masked secrets: stored values)
	s.handle("POST /api/v1/admin/audit/forward/test", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := requireAdmin(r); err != nil {
			return nil, err
		}
		var in config.AuditForward
		if err := h.Decode(r, &in); err != nil {
			return nil, err
		}
		list := []config.AuditForward{in}
		keepSecrets(list, s.app.Config.Get().Audit.Forward)
		if err := s.app.AuditForward.Test(r.Context(), list[0], identity.From(r.Context()).Handle()); err != nil {
			return nil, apperr.BadRequest("test failed: %s", err.Error())
		}
		return map[string]string{"status": "sent"}, nil
	})
}
