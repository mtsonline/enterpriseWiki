package pluginmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/pkg/sdk"
)

// DefaultCoreRepo is the repository of the core.
const DefaultCoreRepo = "github.com/mtsonline/enterpriseWiki"

// UpdateInfo is the result of an update check for the core or one plugin.
type UpdateInfo struct {
	ID        string `json:"id"` // plugin id or "core"
	Name      string `json:"name"`
	Repo      string `json:"repo"`
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Tag       string `json:"tag,omitempty"`
	URL       string `json:"url,omitempty"`
	Notes     string `json:"notes,omitempty"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

// UpdateReport is the result of CheckUpdates.
type UpdateReport struct {
	CheckedAt time.Time    `json:"checked_at"`
	Items     []UpdateInfo `json:"items"`
}

type updateCache struct {
	mu     sync.Mutex
	report *UpdateReport
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// githubRepo extracts owner/repo from github.com/owner/repo, an https URL or
// owner/repo.
func githubRepo(repo string) (string, bool) {
	r := strings.TrimSpace(repo)
	r = strings.TrimPrefix(r, "https://")
	r = strings.TrimPrefix(r, "http://")
	r = strings.TrimPrefix(r, "git@github.com:")
	r = strings.TrimPrefix(r, "github.com/")
	r = strings.TrimSuffix(strings.TrimSuffix(r, "/"), ".git")
	parts := strings.Split(r, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return r, true
}

// latestRelease asks the GitHub API for the latest release (falls back to
// the newest tag if the repository has no releases).
func (m *Manager) latestRelease(ctx context.Context, ownerRepo string) (tag, url, notes string, err error) {
	cfg := m.cfg.Get().Plugins.Updates
	base := strings.TrimSuffix(cfg.APIURL, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	get := func(path string, v any) (int, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", base+path, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "enterprise-wiki")
		if cfg.Token != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.Token)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return 0, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != 200 {
			return resp.StatusCode, nil
		}
		return 200, json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(v)
	}
	var rel struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
	}
	code, err := get("/repos/"+ownerRepo+"/releases/latest", &rel)
	if err != nil {
		return "", "", "", err
	}
	if code == 200 && rel.TagName != "" {
		if len(rel.Body) > 2000 {
			rel.Body = rel.Body[:2000] + "…"
		}
		return rel.TagName, rel.HTMLURL, rel.Body, nil
	}
	if code != 404 {
		return "", "", "", fmt.Errorf("GitHub API returned %d", code)
	}
	var tags []struct {
		Name string `json:"name"`
	}
	code, err = get("/repos/"+ownerRepo+"/tags?per_page=100", &tags)
	if err != nil {
		return "", "", "", err
	}
	if code != 200 {
		return "", "", "", fmt.Errorf("GitHub API returned %d", code)
	}
	best := ""
	for _, t := range tags {
		if _, ok := parseVersion(t.Name); ok && (best == "" || CompareVersions(t.Name, best) > 0) {
			best = t.Name
		}
	}
	if best == "" {
		return "", "", "", fmt.Errorf("no releases")
	}
	return best, "https://github.com/" + ownerRepo + "/releases/tag/" + best, "", nil
}

// CheckUpdates checks the core and all plugins that name a GitHub repository
// (manifest "repo" or the catalog entry). Results are cached for an hour
// unless force is set.
func (m *Manager) CheckUpdates(ctx context.Context, coreVersion string, plugins []sdk.Manifest, force bool) (*UpdateReport, error) {
	m.updates.mu.Lock()
	defer m.updates.mu.Unlock()
	if !force && m.updates.report != nil && time.Since(m.updates.report.CheckedAt) < time.Hour {
		return m.updates.report, nil
	}
	cfg := m.cfg.Get()
	coreRepo := cfg.Plugins.Updates.CoreRepo
	if coreRepo == "" {
		coreRepo = DefaultCoreRepo
	}
	items := []UpdateInfo{{ID: "core", Name: "Enterprise Wiki", Repo: coreRepo, Current: coreVersion}}
	catalog := map[string]string{}
	for _, c := range cfg.Plugins.Catalog {
		catalog[c.ID] = c.Repo
	}
	for _, p := range plugins {
		repo := p.Repo
		if repo == "" {
			repo = catalog[p.ID]
		}
		if repo == "" {
			continue
		}
		items = append(items, UpdateInfo{ID: p.ID, Name: p.Name, Repo: repo, Current: p.Version})
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range items {
		wg.Add(1)
		go func(it *UpdateInfo) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			or, ok := githubRepo(it.Repo)
			if !ok {
				it.Error = "not a GitHub repository"
				return
			}
			tag, url, notes, err := m.latestRelease(ctx, or)
			if err != nil {
				it.Error = err.Error()
				return
			}
			it.Tag, it.URL, it.Notes = tag, url, notes
			it.Latest = strings.TrimPrefix(tag, "v")
			it.Available = CompareVersions(tag, it.Current) > 0
		}(&items[i])
	}
	wg.Wait()
	m.updates.report = &UpdateReport{CheckedAt: time.Now().UTC(), Items: items}
	return m.updates.report, nil
}

// LastUpdateReport returns the cached report (nil if never checked).
func (m *Manager) LastUpdateReport() *UpdateReport {
	m.updates.mu.Lock()
	defer m.updates.mu.Unlock()
	return m.updates.report
}

// UpdatePlugin installs the latest release of a plugin (from the last check)
// and starts a rebuild.
func (m *Manager) UpdatePlugin(ctx context.Context, id string) (*InstallResult, error) {
	rep := m.LastUpdateReport()
	if rep == nil {
		return nil, apperr.BadRequest("check for updates first")
	}
	for _, it := range rep.Items {
		if it.ID != id || id == "core" {
			continue
		}
		if !it.Available {
			return nil, apperr.BadRequest("no update available for %s", id)
		}
		res, err := m.Install(ctx, it.Repo, it.Tag)
		if err != nil {
			return nil, err
		}
		if res.ID != id {
			return nil, apperr.BadRequest("repository %s contains plugin %s, not %s", it.Repo, res.ID, id)
		}
		if m.CanBuild() {
			if _, err := m.StartBuild(ctx, "update"); err != nil {
				return res, err
			}
			res.RebuildRequired = false
		}
		m.updates.mu.Lock()
		m.updates.report = nil
		m.updates.mu.Unlock()
		return res, nil
	}
	return nil, apperr.NotFound("update for " + id)
}

// parseVersion parses "v1.2.3", "1.2", "1.2.3-rc1" into numbers (pre-release
// suffixes rank below the release).
func parseVersion(v string) ([4]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	pre := 1 << 30
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		if v[i] == '-' {
			pre = 0
		}
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return [4]int{}, false
	}
	var out [4]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [4]int{}, false
		}
		out[i] = n
	}
	out[3] = pre
	return out, true
}

// CompareVersions compares two semantic versions (-1, 0, 1). Unparsable
// versions compare as lower.
func CompareVersions(a, b string) int {
	va, oka := parseVersion(a)
	vb, okb := parseVersion(b)
	switch {
	case !oka && !okb:
		return 0
	case !oka:
		return -1
	case !okb:
		return 1
	}
	for i := 0; i < 4; i++ {
		if va[i] != vb[i] {
			if va[i] < vb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
