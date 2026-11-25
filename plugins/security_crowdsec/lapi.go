package security_crowdsec

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// lapi is a client of the CrowdSec Local API, signed in as a machine
// (watcher). Machines may read alerts and allowlists and delete decisions;
// they never see the API keys of bouncers. The wiki needs no access to the
// Docker socket or to the CrowdSec container.
type lapi struct {
	base     string // without trailing slash
	machine  string
	password string
	http     *http.Client

	mu     sync.Mutex
	token  string
	expire time.Time
}

// maxBody caps every answer of the Local API (alerts with their events can
// be large; the wiki asks only for few at a time).
const maxBody = 8 << 20

// errLAPI is an answer of the Local API that is not a success.
type errLAPI struct {
	Status int
	Msg    string
}

func (e *errLAPI) Error() string {
	if e.Msg == "" {
		return fmt.Sprintf("CrowdSec answered %d", e.Status)
	}
	return fmt.Sprintf("CrowdSec answered %d: %s", e.Status, e.Msg)
}

func newLAPI(base, machine, password string) (*lapi, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("the address of the Local API must be http(s)://host:port without user, query or fragment")
	}
	return &lapi{
		base: strings.TrimRight(u.String(), "/"), machine: machine, password: password,
		http: &http.Client{
			Timeout: 10 * time.Second,
			// a redirect would carry the password or the token elsewhere
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// login returns a valid token, signing in again shortly before it expires.
func (c *lapi) login(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && c.token != "" && time.Until(c.expire) > time.Minute {
		return c.token, nil
	}
	body, _ := json.Marshal(map[string]any{"machine_id": c.machine, "password": c.password, "scenarios": []string{}})
	var out struct {
		Token  string `json:"token"`
		Expire string `json:"expire"`
	}
	if err := c.do(ctx, http.MethodPost, "/v1/watchers/login", "", body, &out); err != nil {
		var le *errLAPI
		if errors.As(err, &le) && (le.Status == 401 || le.Status == 403) {
			return "", fmt.Errorf("the machine %q could not sign in to CrowdSec (name or password wrong, or the machine is not validated)", c.machine)
		}
		return "", err
	}
	if out.Token == "" {
		return "", errors.New("CrowdSec sent no token")
	}
	exp, err := time.Parse(time.RFC3339, out.Expire)
	if err != nil {
		exp = time.Now().Add(10 * time.Minute)
	}
	c.token, c.expire = out.Token, exp
	return c.token, nil
}

// call runs an authenticated request; after a 401 it signs in once more.
func (c *lapi) call(ctx context.Context, method, path string, out any) error {
	for i := 0; ; i++ {
		tok, err := c.login(ctx, i > 0)
		if err != nil {
			return err
		}
		err = c.do(ctx, method, path, tok, nil, out)
		var le *errLAPI
		if i == 0 && errors.As(err, &le) && le.Status == 401 {
			continue
		}
		return err
	}
}

func (c *lapi) do(ctx context.Context, method, path, token string, body []byte, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	// the Local API refuses agents without "name/version" (it reads the version)
	req.Header.Set("User-Agent", "enterprise-wiki/1.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("CrowdSec is not reachable: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil {
		return err
	}
	if len(data) > maxBody {
		return errors.New("the answer of CrowdSec is too large")
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var e struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(data, &e)
		return &errLAPI{Status: res.StatusCode, Msg: short(e.Message, 200)}
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("unexpected answer of CrowdSec: %w", err)
	}
	return nil
}

// health reports whether the Local API answers (no sign-in needed).
func (c *lapi) health(ctx context.Context) error {
	return c.do(ctx, http.MethodGet, "/health", "", nil, nil)
}

// ---------------------------------------------------------------- data of the Local API

type lapiDecision struct {
	ID        int64  `json:"id"`
	Origin    string `json:"origin"`
	Type      string `json:"type"`
	Scope     string `json:"scope"`
	Value     string `json:"value"`
	Duration  string `json:"duration"`
	Scenario  string `json:"scenario"`
	Simulated bool   `json:"simulated"`
}

type lapiAlert struct {
	ID          int64          `json:"id"`
	Scenario    string         `json:"scenario"`
	Message     string         `json:"message"`
	MachineID   string         `json:"machine_id"`
	EventsCount int            `json:"events_count"`
	CreatedAt   string         `json:"created_at"`
	Simulated   bool           `json:"simulated"`
	Decisions   []lapiDecision `json:"decisions"`
	Source      struct {
		Scope  string `json:"scope"`
		Value  string `json:"value"`
		IP     string `json:"ip"`
		Range  string `json:"range"`
		CN     string `json:"cn"`
		ASName string `json:"as_name"`
	} `json:"source"`
}

type lapiAllowlist struct {
	Name  string `json:"name"`
	Items []struct {
		Value       string `json:"value"`
		Description string `json:"description"`
		Expiration  string `json:"expiration"`
	} `json:"items"`
}

// alerts returns the latest alerts (at most limit); active: only those with
// a decision in force. Alerts of the community blocklist are left out.
func (c *lapi) alerts(ctx context.Context, limit int, active bool) ([]lapiAlert, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("include_capi", "false")
	if active {
		q.Set("has_active_decision", "true")
	}
	var out []lapiAlert
	err := c.call(ctx, http.MethodGet, "/v1/alerts?"+q.Encode(), &out)
	return out, err
}

// deleteDecision lifts one decision (a ban) by its id.
func (c *lapi) deleteDecision(ctx context.Context, id int64) (int, error) {
	var out struct {
		NbDeleted string `json:"nbDeleted"`
	}
	if err := c.call(ctx, http.MethodDelete, "/v1/decisions/"+strconv.FormatInt(id, 10), &out); err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(out.NbDeleted)
	return n, nil
}

// allowlists returns the allowlists of the Local API with their entries.
func (c *lapi) allowlists(ctx context.Context) ([]lapiAllowlist, error) {
	var out []lapiAllowlist
	err := c.call(ctx, http.MethodGet, "/v1/allowlists?with_content=true", &out)
	return out, err
}

// short cuts s to at most n bytes at a rune boundary.
func short(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }
