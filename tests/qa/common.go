package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Options are the settings shared by all commands.
type Options struct {
	Base      string // base URL of the wiki under test
	AdminUser string
	AdminPass string
	Out       string // directory of the result files
	Users     int    // QA accounts (qa-u001 ...)
	SpoofIPs  bool   // one client address per virtual user (X-Forwarded-For; needs server.trusted_proxies)
	Force     bool   // allow targets that are not on this machine
	Routes    string // route list (docs/developer/api.md) for the sweep of the penetration test
	ServerPID string // process id of the wiki when it runs on this machine (memory, CPU)
}

// QAPassword is the password of the QA accounts of a run. It is derived
// from EWIKI_QA_USER_PASSWORD or a fixed test value: the accounts only
// exist on test instances and are removed by "cleanup".
func QAPassword() string {
	if v := os.Getenv("EWIKI_QA_USER_PASSWORD"); v != "" {
		return v
	}
	return "Qa-User-Passw0rd!2026"
}

var transport = &http.Transport{
	Proxy: nil, // never through a proxy: the target is a test instance
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	MaxIdleConns:          0,
	MaxIdleConnsPerHost:   20000,
	MaxConnsPerHost:       0,
	IdleConnTimeout:       90 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
	DisableCompression:    true,
}

// Client is one caller: a guest, a session (cookie) or an API key.
type Client struct {
	Base    string
	Name    string
	Cookie  string // value of ewiki_session
	Bearer  string
	IP      string // sent as X-Forwarded-For when set
	NoCSRF  bool   // omit X-Requested-With (to test the CSRF protection)
	Headers map[string]string
	hc      *http.Client
	rec     *Recorder
}

func newClient(base, name string) *Client {
	return &Client{Base: strings.TrimRight(base, "/"), Name: name, hc: &http.Client{
		Transport: transport, Timeout: 90 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Resp is an answer of the wiki.
type Resp struct {
	Status int
	Header http.Header
	Body   []byte
	Dur    time.Duration
	Err    error
}

// JSON decodes the body (errors leave v untouched).
func (r *Resp) JSON(v any) error { return json.Unmarshal(r.Body, v) }

// Text returns up to n bytes of the body for evidence.
func (r *Resp) Text(n int) string {
	if r.Err != nil {
		return "error: " + r.Err.Error()
	}
	b := r.Body
	if len(b) > n {
		b = b[:n]
	}
	return string(b)
}

// Do sends a request. body: nil, []byte / io.Reader (sent as is, see
// Headers for the content type) or anything else (JSON).
func (c *Client) Do(method, path string, body any) *Resp {
	return c.DoOp("", method, path, body)
}

// DoOp is Do with the name of the operation for the latency statistics.
func (c *Client) DoOp(op, method, path string, body any) *Resp {
	var rd io.Reader
	ctype := ""
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case string:
		rd = strings.NewReader(b)
	case io.Reader:
		rd = b
	default:
		j, _ := json.Marshal(b)
		rd, ctype = bytes.NewReader(j), "application/json"
	}
	u := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		u = c.Base + path
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return &Resp{Err: err}
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if c.Cookie != "" {
		req.Header.Set("Cookie", "ewiki_session="+c.Cookie)
	}
	if c.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+c.Bearer)
	}
	if !c.NoCSRF {
		req.Header.Set("X-Requested-With", "ewiki")
	}
	if c.IP != "" {
		req.Header.Set("X-Forwarded-For", c.IP)
	}
	req.Header.Set("User-Agent", "ewiki-qa/1.0")
	for k, v := range c.Headers {
		if strings.EqualFold(k, "Host") {
			req.Host = v
			continue
		}
		req.Header.Set(k, v)
	}
	start := time.Now()
	res, err := c.hc.Do(req)
	if err != nil {
		d := time.Since(start)
		if c.rec != nil {
			c.rec.Add(opName(op), d, 0)
		}
		return &Resp{Err: err, Dur: d}
	}
	defer func() { _ = res.Body.Close() }()
	b, rerr := io.ReadAll(io.LimitReader(res.Body, 64<<20))
	d := time.Since(start)
	st := res.StatusCode
	if rerr != nil {
		st = 0
	}
	if c.rec != nil {
		c.rec.Add(opName(op), d, st)
	}
	for _, ck := range res.Cookies() {
		if ck.Name == "ewiki_session" && c.Bearer == "" {
			c.Cookie = ck.Value
		}
	}
	return &Resp{Status: res.StatusCode, Header: res.Header, Body: b, Dur: d, Err: rerr}
}

func opName(op string) string {
	if op == "" {
		return "other"
	}
	return op
}

// Login signs in with a password.
func (c *Client) Login(user, pass string) *Resp {
	return c.DoOp("auth: login", "POST", "/api/v1/auth/login", map[string]string{"username": user, "password": pass})
}

// Upload sends a multipart upload (field "file" plus form fields).
func (c *Client) Upload(op, path string, fields map[string]string, filename string, data []byte) *Resp {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write(data)
	_ = mw.Close()
	old := c.Headers
	c.Headers = map[string]string{"Content-Type": mw.FormDataContentType()}
	for k, v := range old {
		c.Headers[k] = v
	}
	defer func() { c.Headers = old }()
	return c.DoOp(op, "POST", path, buf.Bytes())
}

// clone returns a client with the same identity and its own header set.
func (c *Client) clone() *Client {
	n := *c
	n.Headers = map[string]string{}
	for k, v := range c.Headers {
		n.Headers[k] = v
	}
	return &n
}

// ---- latency statistics

// Recorder collects the response times per operation.
type Recorder struct {
	mu  sync.Mutex
	ops map[string]*opStat
	off atomic.Bool // true: requests are not recorded (ramp-up of a load stage)
}

type opStat struct {
	dur    []float32 // milliseconds
	status map[int]int
}

func newRecorder() *Recorder { return &Recorder{ops: map[string]*opStat{}} }

// Add records one request (status 0: transport error or timeout).
func (r *Recorder) Add(op string, d time.Duration, status int) {
	if r.off.Load() {
		return
	}
	r.mu.Lock()
	s := r.ops[op]
	if s == nil {
		s = &opStat{status: map[int]int{}}
		r.ops[op] = s
	}
	s.dur = append(s.dur, float32(d.Microseconds())/1000)
	s.status[status]++
	r.mu.Unlock()
}

// OpResult is the summary of one operation.
type OpResult struct {
	Op       string         `json:"op"`
	Count    int            `json:"count"`
	Errors   int            `json:"errors"`   // transport errors, timeouts and 5xx
	Rejected int            `json:"rejected"` // 429
	Status   map[string]int `json:"status"`
	Min      float64        `json:"min_ms"`
	Mean     float64        `json:"mean_ms"`
	P50      float64        `json:"p50_ms"`
	P90      float64        `json:"p90_ms"`
	P95      float64        `json:"p95_ms"`
	P99      float64        `json:"p99_ms"`
	Max      float64        `json:"max_ms"`
}

func pct(sorted []float32, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(float64(len(sorted))*p+0.5) - 1
	if i < 0 {
		i = 0
	}
	if i >= len(sorted) {
		i = len(sorted) - 1
	}
	return round1(float64(sorted[i]))
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }

// Results returns the summaries sorted by operation, plus one of everything.
func (r *Recorder) Results() (ops []OpResult, total OpResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []float32
	total = OpResult{Op: "all", Status: map[string]int{}}
	for name, s := range r.ops {
		d := append([]float32(nil), s.dur...)
		sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
		o := summarize(name, d, s.status)
		ops = append(ops, o)
		all = append(all, d...)
		for k, v := range s.status {
			total.Status[fmt.Sprint(k)] += v
		}
		total.Errors += o.Errors
		total.Rejected += o.Rejected
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	t := summarize("all", all, nil)
	t.Status, t.Errors, t.Rejected = total.Status, total.Errors, total.Rejected
	sort.Slice(ops, func(i, j int) bool { return ops[i].Op < ops[j].Op })
	return ops, t
}

func summarize(name string, d []float32, status map[int]int) OpResult {
	o := OpResult{Op: name, Count: len(d), Status: map[string]int{}}
	var sum float64
	for _, v := range d {
		sum += float64(v)
	}
	if len(d) > 0 {
		o.Min, o.Max, o.Mean = round1(float64(d[0])), round1(float64(d[len(d)-1])), round1(sum/float64(len(d)))
	}
	o.P50, o.P90, o.P95, o.P99 = pct(d, 0.50), pct(d, 0.90), pct(d, 0.95), pct(d, 0.99)
	for k, v := range status {
		o.Status[fmt.Sprint(k)] = v
		if k == 0 || k >= 500 {
			o.Errors += v
		}
		if k == 429 {
			o.Rejected += v
		}
	}
	return o
}

// ---- helpers

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func writeJSON(dir, name string, v any) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), b, 0o600)
}

func readJSON(dir, name string, v any) error {
	b, err := os.ReadFile(filepath.Join(dir, name)) // #nosec G304 -- result file of this tool
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// vuIP returns a private client address for virtual user n.
func vuIP(n int) string { return fmt.Sprintf("10.%d.%d.%d", 16+(n>>16)&0x7f, (n>>8)&0xff, n&0xff) }

// localTarget reports whether base points to this machine.
func localTarget(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func waitReady(ctx context.Context, base string) error {
	c := newClient(base, "probe")
	for {
		if r := c.Do("GET", "/readyz", nil); r.Err == nil && r.Status == 200 {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s does not answer /readyz", base)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func htmlBody(src string) map[string]string {
	return map[string]string{"format": "html", "source": src}
}
func mdBody(src string) map[string]string {
	return map[string]string{"format": "markdown", "source": src}
}
