package audit

// Forwarding the audit log to other systems. A worker reads new entries
// (by id) for every enabled destination and sends them in batches; the
// position per destination is stored in audit_forward_state, so nothing is
// lost or sent twice when a destination is unreachable for a while (it is
// retried on the next run) or the server restarts. In a cluster the row
// lock makes sure that only one node sends to a destination at a time.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/version"
)

// ForwardTypes are the supported destination types.
var ForwardTypes = []string{"syslog", "webhook", "otlp", "file", "stdout"}

// ForwardStatus is the state of a destination.
type ForwardStatus struct {
	ID        string     `json:"id"`
	LastID    int64      `json:"last_id"`
	LastAt    *time.Time `json:"last_at"`
	LastError string     `json:"last_error"`
	Errors    int        `json:"errors"`
	Pending   int64      `json:"pending"`
}

// Forwarder sends the audit log to the configured destinations.
type Forwarder struct {
	Pool    *pgxpool.Pool
	Cfg     *config.Store
	DataDir string // file destinations are written below <DataDir>/logs
	Site    func() string
	client  *http.Client
	stdout  io.Writer
}

// ValidateForward checks a destination.
func ValidateForward(f config.AuditForward) error {
	if !slices.Contains(ForwardTypes, f.Type) {
		return fmt.Errorf("type must be one of %s", strings.Join(ForwardTypes, ", "))
	}
	switch f.Type {
	case "syslog":
		if _, _, err := net.SplitHostPort(f.Address); err != nil {
			return fmt.Errorf("syslog address must be host:port")
		}
		if f.Protocol != "" && f.Protocol != "udp" && f.Protocol != "tcp" && f.Protocol != "tls" {
			return fmt.Errorf("syslog protocol must be udp, tcp or tls")
		}
	case "webhook", "otlp":
		if !strings.HasPrefix(f.Address, "https://") && !strings.HasPrefix(f.Address, "http://") {
			return fmt.Errorf("address must be an http(s) URL")
		}
	case "file":
		if f.Address == "" || strings.ContainsAny(f.Address, `/\`) || strings.HasPrefix(f.Address, ".") {
			return fmt.Errorf("file name without directory, e.g. audit.jsonl (written to <data>/logs)")
		}
	}
	return nil
}

// Run sends pending entries to every enabled destination (worker).
func (f *Forwarder) Run(ctx context.Context) error {
	var errs []error
	for _, d := range f.Cfg.Get().Audit.Forward {
		if !d.Enabled {
			continue
		}
		if err := f.forward(ctx, d); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (f *Forwarder) forward(ctx context.Context, d config.AuditForward) error {
	return pgx.BeginFunc(ctx, f.Pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO audit_forward_state(id, last_id) VALUES ($1, (SELECT COALESCE(max(id), 0) FROM audit_log))
			ON CONFLICT (id) DO NOTHING`, d.ID); err != nil {
			return err
		}
		var last int64
		err := tx.QueryRow(ctx, `SELECT last_id FROM audit_forward_state WHERE id=$1 FOR UPDATE SKIP LOCKED`, d.ID).Scan(&last)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // another node is sending
		}
		if err != nil {
			return err
		}
		for range 20 { // at most 20 batches per run
			batch, _, err := Find(ctx, tx, Search{AfterID: last, Limit: 500})
			if err != nil || len(batch) == 0 {
				return err
			}
			newLast := batch[len(batch)-1].ID
			if len(d.Areas) > 0 {
				batch = slices.DeleteFunc(batch, func(e Entry) bool { return !slices.Contains(d.Areas, Area(e.Action)) })
			}
			if len(batch) > 0 {
				if err := f.send(ctx, d, batch); err != nil {
					_, _ = tx.Exec(ctx, `UPDATE audit_forward_state SET last_error=$2, errors=errors+1, error_at=now() WHERE id=$1`, d.ID, err.Error())
					return nil // keep the position, retry on the next run
				}
			}
			last = newLast
			if _, err := tx.Exec(ctx, `UPDATE audit_forward_state SET last_id=$2, last_at=now(), last_error='', errors=0 WHERE id=$1`, d.ID, last); err != nil {
				return err
			}
		}
		return nil
	})
}

// Test sends a test entry to a destination.
func (f *Forwarder) Test(ctx context.Context, d config.AuditForward, actor string) error {
	if err := ValidateForward(d); err != nil {
		return err
	}
	return f.send(ctx, d, []Entry{{ID: 0, At: time.Now().UTC(), Actor: actor, Action: "audit.test", EntityType: "audit", Via: "test"}})
}

// Status returns the state of all destinations.
func (f *Forwarder) Status(ctx context.Context) ([]ForwardStatus, error) {
	var max int64
	if err := f.Pool.QueryRow(ctx, `SELECT COALESCE(max(id), 0) FROM audit_log`).Scan(&max); err != nil {
		return nil, err
	}
	out := []ForwardStatus{}
	for _, d := range f.Cfg.Get().Audit.Forward {
		st := ForwardStatus{ID: d.ID, LastID: max}
		err := f.Pool.QueryRow(ctx, `SELECT last_id, last_at, last_error, errors FROM audit_forward_state WHERE id=$1`, d.ID).Scan(&st.LastID, &st.LastAt, &st.LastError, &st.Errors)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		st.Pending = max - st.LastID
		out = append(out, st)
	}
	return out, nil
}

// record is the JSON form of an entry for other systems.
func (f *Forwarder) record(e Entry) map[string]any {
	m := map[string]any{"id": e.ID, "time": e.At.UTC().Format(time.RFC3339Nano), "actor": e.Actor, "action": e.Action, "area": Area(e.Action),
		"entity_type": e.EntityType, "via": e.Via, "site": f.site()}
	if e.UserID != nil {
		m["user_id"] = *e.UserID
	}
	if e.EntityID != nil {
		m["entity_id"] = *e.EntityID
	}
	if e.IP != "" {
		m["ip"] = e.IP
	}
	if e.UserAgent != "" {
		m["user_agent"] = e.UserAgent
	}
	if len(e.Before) > 0 {
		m["before"] = e.Before
	}
	if len(e.After) > 0 {
		m["after"] = e.After
	}
	return m
}

func (f *Forwarder) site() string {
	if f.Site != nil {
		return f.Site()
	}
	return ""
}

func (f *Forwarder) send(ctx context.Context, d config.AuditForward, batch []Entry) error {
	switch d.Type {
	case "stdout":
		w := f.stdout
		if w == nil {
			w = os.Stdout
		}
		for _, e := range batch {
			rec := f.record(e)
			rec["log"] = "audit"
			b, _ := json.Marshal(rec)
			if _, err := fmt.Fprintln(w, string(b)); err != nil {
				return err
			}
		}
		return nil
	case "file":
		return f.sendFile(d, batch)
	case "syslog":
		return f.sendSyslog(ctx, d, batch)
	case "webhook":
		recs := make([]map[string]any, len(batch))
		for i, e := range batch {
			recs[i] = f.record(e)
		}
		body, _ := json.Marshal(map[string]any{"source": "enterprise-wiki", "site": f.site(), "entries": recs})
		return f.post(ctx, d, body, func(r *http.Request) {
			if d.Secret != "" {
				mac := hmac.New(sha256.New, []byte(d.Secret))
				mac.Write(body)
				r.Header.Set("X-Ewiki-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
			}
		})
	case "otlp":
		return f.post(ctx, d, f.otlp(batch), nil)
	}
	return fmt.Errorf("unknown type %q", d.Type)
}

func (f *Forwarder) post(ctx context.Context, d config.AuditForward, body []byte, sign func(*http.Request)) error {
	if f.client == nil {
		f.client = &http.Client{Timeout: 20 * time.Second}
	}
	url := d.Address
	if d.Type == "otlp" && !strings.HasSuffix(url, "/v1/logs") {
		url = strings.TrimRight(url, "/") + "/v1/logs"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "EnterpriseWiki/"+version.Version)
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}
	if sign != nil {
		sign(req)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// otlp encodes a batch as OpenTelemetry logs (OTLP/HTTP JSON).
func (f *Forwarder) otlp(batch []Entry) []byte {
	str := func(k, v string) map[string]any {
		return map[string]any{"key": k, "value": map[string]any{"stringValue": v}}
	}
	recs := make([]map[string]any, len(batch))
	for i, e := range batch {
		attrs := []map[string]any{str("ewiki.action", e.Action), str("ewiki.area", Area(e.Action)), str("enduser.id", e.Actor),
			str("ewiki.entity_type", e.EntityType), str("ewiki.via", e.Via)}
		if e.EntityID != nil {
			attrs = append(attrs, str("ewiki.entity_id", strconv.FormatInt(*e.EntityID, 10)))
		}
		if e.IP != "" {
			attrs = append(attrs, str("client.address", e.IP))
		}
		if e.UserAgent != "" {
			attrs = append(attrs, str("user_agent.original", e.UserAgent))
		}
		body, _ := json.Marshal(f.record(e))
		recs[i] = map[string]any{"timeUnixNano": strconv.FormatInt(e.At.UnixNano(), 10), "severityNumber": 9, "severityText": "INFO",
			"body": map[string]any{"stringValue": string(body)}, "attributes": attrs}
	}
	out, _ := json.Marshal(map[string]any{"resourceLogs": []any{map[string]any{
		"resource":  map[string]any{"attributes": []any{str("service.name", "enterprise-wiki"), str("service.version", version.Version), str("ewiki.site", f.site())}},
		"scopeLogs": []any{map[string]any{"scope": map[string]any{"name": "audit"}, "logRecords": recs}},
	}}})
	return out
}

func (f *Forwarder) sendFile(d config.AuditForward, batch []Entry) error {
	dir := filepath.Join(f.DataDir, "logs")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	path := filepath.Join(dir, filepath.Base(d.Address))
	if st, err := os.Stat(path); err == nil && st.Size() > 100<<20 { // rotate at 100 MB, keep 5
		for i := 4; i >= 1; i-- {
			_ = os.Rename(fmt.Sprintf("%s.%d", path, i), fmt.Sprintf("%s.%d", path, i+1))
		}
		_ = os.Rename(path, path+".1")
	}
	fh, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600) // #nosec G304 -- file destination set by an administrator
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, e := range batch {
		b, _ := json.Marshal(f.record(e))
		buf.Write(b)
		buf.WriteByte('\n')
	}
	_, err = fh.Write(buf.Bytes())
	if cerr := fh.Close(); err == nil {
		err = cerr // a failed close can mean lost entries
	}
	return err
}

// sendSyslog sends RFC 5424 messages (facility auth=10 / severity notice=5),
// with the entry as JSON message; tcp and tls use octet counting (RFC 6587).
func (f *Forwarder) sendSyslog(ctx context.Context, d config.AuditForward, batch []Entry) error {
	proto := d.Protocol
	if proto == "" {
		proto = "udp"
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	var err error
	if proto == "tls" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: &tls.Config{MinVersion: tls.VersionTLS12}}).DialContext(ctx, "tcp", d.Address)
	} else {
		conn, err = dialer.DialContext(ctx, proto, d.Address)
	}
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	host, _ := os.Hostname()
	if host == "" {
		host = "-"
	}
	for _, e := range batch {
		b, _ := json.Marshal(f.record(e))
		msg := fmt.Sprintf("<%d>1 %s %s enterprise-wiki - %s - %s", 10*8+5, e.At.UTC().Format(time.RFC3339Nano), host, sdID(e.Action), b)
		if proto != "udp" {
			msg = fmt.Sprintf("%d %s", len(msg), msg)
		}
		if _, err := conn.Write([]byte(msg)); err != nil {
			return err
		}
	}
	return nil
}

// sdID makes an action usable as syslog MSGID (printable ASCII, max 32).
func sdID(action string) string {
	b := []byte(action)
	for i, c := range b {
		if c < 33 || c > 126 {
			b[i] = '_'
		}
	}
	if len(b) > 32 {
		b = b[:32]
	}
	if len(b) == 0 {
		return "-"
	}
	return string(b)
}
