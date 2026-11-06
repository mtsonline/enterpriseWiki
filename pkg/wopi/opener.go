package wopi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/media"
)

// Settings of a WOPI office program (from the plugin settings).
type Settings struct {
	// Discovery is the URL of the discovery document.
	Discovery string
	// Zone of the discovery ("" = first).
	Zone string
	// VerifyProof checks the proof key signature of every request.
	VerifyProof bool
	// ReadOnlyWithoutDownload: users without the right "download" may view
	// in the program (download, print, export and copy are switched off).
	ReadOnlyWithoutDownload bool
}

// Opener is a media.Opener for a WOPI office program.
type Opener struct {
	PluginID string
	Name     string
	// Restrict and Watermark: what the program enforces (see media.OpenerCaps).
	Restrict  bool
	Watermark bool
	// Settings returns the current settings.
	Settings func() Settings
	// HTTP client for the discovery (nil = default).
	HTTP *http.Client
	// Extend adds program specific CheckFileInfo fields (optional).
	Extend func(info map[string]any, t *media.Token, m *media.Medium)

	mu     sync.Mutex
	client *Client
}

var _ media.Opener = (*Opener)(nil)

// ID implements media.Opener.
func (o *Opener) ID() string { return o.PluginID }

// Label implements media.Opener.
func (o *Opener) Label() string { return o.Name }

// Caps implements media.Opener.
func (o *Opener) Caps() media.OpenerCaps {
	return media.OpenerCaps{Restrict: o.Restrict, Watermark: o.Watermark}
}

// Options implements media.OptionsProvider.
func (o *Opener) Options() media.OpenerOptions {
	return media.OpenerOptions{ReadOnlyWithoutDownload: o.Settings().ReadOnlyWithoutDownload}
}

// Client returns the discovery client for the current settings.
func (o *Opener) Client() *Client {
	s := o.Settings()
	o.mu.Lock()
	defer o.mu.Unlock()
	if s.Discovery == "" {
		return nil
	}
	if o.client == nil || o.client.URL != s.Discovery || o.client.Zone != s.Zone {
		o.client = &Client{URL: s.Discovery, Zone: s.Zone, HTTP: o.HTTP}
	}
	return o.client
}

func (o *Opener) discovery(ctx context.Context) *Discovery {
	c := o.Client()
	if c == nil {
		return nil
	}
	d, err := c.Discovery(ctx)
	if err != nil {
		return nil
	}
	return d
}

// Supports implements media.Opener.
func (o *Opener) Supports(ctx context.Context, name, mime string) (view, edit bool) {
	d := o.discovery(ctx)
	if d == nil {
		return false, false
	}
	ext := media.Ext(name)
	return d.Find(ext, mime, "view", "embedview") != nil, d.Find(ext, mime, "edit") != nil
}

// Launch implements media.Opener.
func (o *Opener) Launch(ctx context.Context, l media.Launch) (*media.Action, error) {
	d := o.discovery(ctx)
	if d == nil {
		return nil, errors.New("discovery not available")
	}
	names := []string{"view", "embedview"}
	if l.Mode == media.ModeEdit {
		names = []string{"edit"}
	}
	a := d.Find(media.Ext(l.Medium.Name), l.Medium.Mime, names...)
	if a == nil {
		return nil, errors.New("file type not supported")
	}
	src := l.BaseURL + media.WOPIPath + strconv.FormatInt(l.Medium.ID, 10)
	u, err := ActionURL(a.URLSrc, src, l.Lang)
	if err != nil {
		return nil, err
	}
	return &media.Action{Kind: "wopi", URL: u, Origin: Origin(u), Form: map[string]string{
		"access_token":     l.Token,
		"access_token_ttl": strconv.FormatInt(l.Expires.UnixMilli(), 10),
	}}, nil
}

// Origins implements media.Opener: the host of the discovery and of all
// actions of the cached discovery (never fetches: called for every page).
func (o *Opener) Origins() []string {
	c := o.Client()
	if c == nil {
		return nil
	}
	var out []string
	if origin := Origin(c.URL); origin != "" {
		out = append(out, origin)
	}
	if d := c.Cached(); d != nil {
		for _, a := range d.Actions {
			if origin := Origin(a.URLSrc); origin != "" && !slices.Contains(out, origin) {
				out = append(out, origin)
			}
		}
	}
	return out
}

// VerifyProof implements media.ProofVerifier.
func (o *Opener) VerifyProof(ctx context.Context, r *http.Request, token, fullURL string) error {
	if !o.Settings().VerifyProof {
		return nil
	}
	c := o.Client()
	if c == nil {
		return errors.New("not configured")
	}
	d, err := c.Discovery(ctx)
	if err != nil {
		return err
	}
	if err := d.Proof.Verify(r, token, fullURL, time.Now()); err == nil {
		return nil
	}
	// keys rotated? fetch the discovery again (rate limited)
	if d, err = c.Refresh(ctx); err != nil {
		return err
	}
	return d.Proof.Verify(r, token, fullURL, time.Now())
}

// Warm fetches the discovery in the background (so the page header knows
// all hosts of the program before the first file is opened). Office
// servers often start after the wiki: it retries for ten minutes.
func (o *Opener) Warm() {
	go func() {
		for i := 0; i < 40; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			c := o.Client()
			var err error
			if c != nil {
				_, err = c.fetchNow(ctx)
			}
			cancel()
			if c == nil || err == nil {
				return
			}
			time.Sleep(15 * time.Second)
		}
	}()
}

// ExtendFileInfo implements media.FileInfoExtender.
func (o *Opener) ExtendFileInfo(info map[string]any, t *media.Token, m *media.Medium) {
	if o.Extend != nil {
		o.Extend(info, t, m)
	}
}

// DiscoveryURL joins a server URL and the standard discovery path.
func DiscoveryURL(server string) string {
	server = strings.TrimSuffix(strings.TrimSpace(server), "/")
	if server == "" || strings.HasSuffix(server, "/hosting/discovery") {
		return server
	}
	return server + "/hosting/discovery"
}
