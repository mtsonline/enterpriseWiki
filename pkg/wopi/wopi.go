// Package wopi implements the client side helpers office plugins share:
// reading the WOPI discovery of an office server (Collabora Online,
// OnlyOffice Docs, Microsoft 365 for the web) and verifying the proof keys
// it signs its requests with.
package wopi

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Action is one entry of the discovery (an app action for an extension).
type Action struct {
	App    string // app name (e.g. "writer", "Word" or a MIME type)
	Name   string // view, edit, embedview, ...
	Ext    string // extension without dot ("" for MIME based entries)
	URLSrc string
}

// ProofKey is an RSA public key of the office server.
type ProofKey struct {
	Current *rsa.PublicKey
	Old     *rsa.PublicKey
}

// Discovery is a parsed WOPI discovery document.
type Discovery struct {
	Actions []Action
	Proof   ProofKey
	Fetched time.Time
}

type xmlDiscovery struct {
	Zones []struct {
		Name string `xml:"name,attr"`
		Apps []struct {
			Name    string `xml:"name,attr"`
			Actions []struct {
				Name   string `xml:"name,attr"`
				Ext    string `xml:"ext,attr"`
				URLSrc string `xml:"urlsrc,attr"`
			} `xml:"action"`
		} `xml:"app"`
	} `xml:"net-zone"`
	Proof struct {
		Modulus     string `xml:"modulus,attr"`
		Exponent    string `xml:"exponent,attr"`
		OldModulus  string `xml:"oldmodulus,attr"`
		OldExponent string `xml:"oldexponent,attr"`
	} `xml:"proof-key"`
}

// Parse reads a discovery document. zone selects the net zone (e.g.
// "external-https"); "" takes the first zone that has actions.
func Parse(data []byte, zone string) (*Discovery, error) {
	var x xmlDiscovery
	if err := xml.Unmarshal(data, &x); err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}
	d := &Discovery{Fetched: time.Now()}
	for _, z := range x.Zones {
		if zone != "" && z.Name != zone {
			continue
		}
		for _, a := range z.Apps {
			for _, ac := range a.Actions {
				if ac.URLSrc == "" || ac.Name == "" {
					continue
				}
				d.Actions = append(d.Actions, Action{App: a.Name, Name: ac.Name, Ext: strings.ToLower(ac.Ext), URLSrc: ac.URLSrc})
			}
		}
		if len(d.Actions) > 0 {
			break
		}
	}
	if len(d.Actions) == 0 {
		return nil, errors.New("discovery: no actions")
	}
	var err error
	if x.Proof.Modulus != "" {
		if d.Proof.Current, err = rsaKey(x.Proof.Modulus, x.Proof.Exponent); err != nil {
			return nil, err
		}
	}
	if x.Proof.OldModulus != "" {
		if d.Proof.Old, err = rsaKey(x.Proof.OldModulus, x.Proof.OldExponent); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func rsaKey(mod, exp string) (*rsa.PublicKey, error) {
	m, err := base64.StdEncoding.DecodeString(mod)
	if err != nil {
		return nil, fmt.Errorf("proof key modulus: %w", err)
	}
	e, err := base64.StdEncoding.DecodeString(exp)
	if err != nil || len(e) == 0 || len(e) > 4 {
		return nil, errors.New("proof key exponent invalid")
	}
	n := new(big.Int).SetBytes(m)
	if n.BitLen() < 2048 {
		return nil, errors.New("proof key too short")
	}
	ev := 0
	for _, b := range e {
		ev = ev<<8 | int(b)
	}
	return &rsa.PublicKey{N: n, E: ev}, nil
}

// Find returns the action for a file: by extension, else by MIME type
// (Collabora lists apps by MIME type). names are tried in order.
func (d *Discovery) Find(ext, mime string, names ...string) *Action {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	for _, n := range names {
		for i := range d.Actions {
			a := &d.Actions[i]
			if a.Name == n && ext != "" && a.Ext == ext {
				return a
			}
		}
		for i := range d.Actions {
			a := &d.Actions[i]
			if a.Name == n && a.Ext == "" && mime != "" && a.App == mime {
				return a
			}
		}
	}
	return nil
}

var placeholder = regexp.MustCompile(`<([A-Za-z_]+)=([A-Z_]+)&?>`)

// ActionURL builds the URL to open: placeholders of urlsrc are filled
// (language) or removed, WOPISrc is appended.
func ActionURL(urlsrc, wopiSrc, lang string) (string, error) {
	s := placeholder.ReplaceAllStringFunc(urlsrc, func(m string) string {
		p := placeholder.FindStringSubmatch(m)
		switch p[2] {
		case "UI_LLCC", "DC_LLCC":
			if lang != "" {
				return p[1] + "=" + url.QueryEscape(lang) + "&"
			}
		}
		return ""
	})
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errors.New("invalid action URL in discovery")
	}
	q := u.Query()
	q.Set("WOPISrc", wopiSrc)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Origin returns scheme://host of a URL.
func Origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// ---------------------------------------------------------------- client with cache

// Client fetches and caches the discovery of one office server.
type Client struct {
	// URL of the discovery document (e.g. https://office.example.com/hosting/discovery).
	URL  string
	Zone string
	HTTP *http.Client
	// TTL of the cache (default 12h).
	TTL time.Duration

	mu        sync.Mutex
	d         *Discovery
	cached    atomic.Pointer[Discovery]
	lastTry   time.Time
	lastError error
}

const maxDiscovery = 4 << 20

// Discovery returns the cached discovery, fetching it when it is older
// than TTL (a stale copy is kept when the server is unreachable).
func (c *Client) Discovery(ctx context.Context) (*Discovery, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ttl := c.TTL
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	if c.d != nil && time.Since(c.d.Fetched) < ttl {
		return c.d, nil
	}
	if time.Since(c.lastTry) < 30*time.Second && c.lastError != nil {
		if c.d != nil {
			return c.d, nil
		}
		return nil, c.lastError
	}
	return c.fetchLocked(ctx)
}

// Refresh fetches the discovery again (at most every 5 minutes; e.g. after
// a proof key mismatch when the server rotated its keys).
func (c *Client) Refresh(ctx context.Context) (*Discovery, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.d != nil && time.Since(c.lastTry) < 5*time.Minute {
		return c.d, nil
	}
	return c.fetchLocked(ctx)
}

func (c *Client) fetchLocked(ctx context.Context) (*Discovery, error) {
	c.lastTry = time.Now()
	d, err := c.fetch(ctx)
	c.lastError = err
	if err != nil {
		if c.d != nil {
			return c.d, nil
		}
		return nil, err
	}
	c.d = d
	c.cached.Store(d)
	return d, nil
}

// Cached returns the last fetched discovery without blocking (nil = none yet).
func (c *Client) Cached() *Discovery { return c.cached.Load() }

func (c *Client) fetch(ctx context.Context) (*Discovery, error) {
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, errors.New("invalid discovery URL")
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req) // #nosec G704 -- URL from the administrator's plugin settings
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("discovery: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDiscovery))
	if err != nil {
		return nil, err
	}
	return Parse(data, c.Zone)
}

// ---------------------------------------------------------------- proof keys

// MaxProofAge is how old the timestamp of a signed request may be.
const MaxProofAge = 20 * time.Minute

// Ticks converts a time to .NET ticks (100 ns since 0001-01-01 UTC), the
// unit of X-WOPI-TimeStamp.
func Ticks(t time.Time) int64 {
	const epochTicks = 621355968000000000
	return epochTicks + t.UnixNano()/100
}

// ProofData is the signed byte string of a WOPI request.
func ProofData(token, fullURL string, timestamp int64) []byte {
	tb, ub := []byte(token), []byte(strings.ToUpper(fullURL))
	out := make([]byte, 0, 4+len(tb)+4+len(ub)+4+8)
	out = binary.BigEndian.AppendUint32(out, uint32(len(tb))) // #nosec G115 -- token length is bounded
	out = append(out, tb...)
	out = binary.BigEndian.AppendUint32(out, uint32(len(ub))) // #nosec G115 -- URL length is bounded
	out = append(out, ub...)
	out = binary.BigEndian.AppendUint32(out, 8)
	out = binary.BigEndian.AppendUint64(out, uint64(timestamp)) // #nosec G115 -- ticks are positive
	return out
}

// ErrProof is returned when a request is not signed by the office server.
var ErrProof = errors.New("wopi: invalid proof")

// Verify checks the X-WOPI-Proof headers of r. fullURL is the public URL
// the office server called (scheme, host, path and query).
func (k ProofKey) Verify(r *http.Request, token, fullURL string, now time.Time) error {
	if k.Current == nil {
		return errors.New("wopi: the office server publishes no proof key")
	}
	if len(token) > 4096 || len(fullURL) > 8192 {
		return ErrProof
	}
	var ts int64
	if _, err := fmt.Sscan(r.Header.Get("X-WOPI-TimeStamp"), &ts); err != nil || ts <= 0 {
		return ErrProof
	}
	// compare in ticks (no overflow for absurd values)
	nowT := Ticks(now)
	if ts < nowT-int64(MaxProofAge/100) || ts > nowT+int64(5*time.Minute/100) {
		return ErrProof
	}
	digest := sha256.Sum256(ProofData(token, fullURL, ts))
	check := func(key *rsa.PublicKey, header string) bool {
		if key == nil {
			return false
		}
		sig, err := base64.StdEncoding.DecodeString(r.Header.Get(header))
		if err != nil || len(sig) == 0 {
			return false
		}
		return rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig) == nil
	}
	if check(k.Current, "X-WOPI-Proof") || check(k.Current, "X-WOPI-ProofOld") || check(k.Old, "X-WOPI-Proof") {
		return nil
	}
	return ErrProof
}
