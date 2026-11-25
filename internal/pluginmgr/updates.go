package pluginmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
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
	// Security: a release between the installed and the latest version has
	// a section "## Security" or "## Sicherheit" in its notes (the rule of
	// the release notes, see CONTRIBUTING.md); SecurityNotes holds these
	// sections, newest first.
	Security      bool   `json:"security"`
	SecurityNotes string `json:"security_notes,omitempty"`
	Error         string `json:"error,omitempty"`
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

// ghGet returns a function that reads a path of the GitHub API for the
// repository ownerRepo (with its access data or the general token).
func (m *Manager) ghGet(ctx context.Context, ownerRepo string) func(path string, v any) (int, error) {
	plugins := m.cfg.Get().Plugins
	cfg := plugins.Updates
	base := strings.TrimSuffix(cfg.APIURL, "/")
	gitHost := "github.com"
	if base == "" {
		base = "https://api.github.com"
	} else if u, err := neturl.Parse(base); err == nil && u.Host != "" {
		gitHost = u.Host // GitHub Enterprise: the API lives on the same host
	}
	// access data of the repository, else the general token
	token := cfg.Token
	if cred, ok := credentialFor(plugins.Credentials, "https://"+gitHost+"/"+ownerRepo); ok {
		token = cred.Token
	}
	return func(path string, v any) (int, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", base+path, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "enterprise-wiki")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
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
}

// latestRelease asks the GitHub API for the latest release (falls back to
// the newest tag if the repository has no releases).
func (m *Manager) latestRelease(ctx context.Context, ownerRepo string) (tag, url, notes string, err error) {
	get := m.ghGet(ctx, ownerRepo)
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
func (m *Manager) CheckUpdates(ctx context.Context, coreVersion string, plugins []coreplugin.Manifest, force bool) (*UpdateReport, error) {
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
			it.Tag, it.URL, it.Notes = tag, url, clip(notes, 2000)
			it.Latest = strings.TrimPrefix(tag, "v")
			it.Available = CompareVersions(tag, it.Current) > 0
			if it.Available {
				it.SecurityNotes = m.securityNotes(ctx, or, it.Current, tag, notes)
				it.Security = it.SecurityNotes != ""
			}
		}(&items[i])
	}
	wg.Wait()
	m.updates.report = &UpdateReport{CheckedAt: time.Now().UTC(), Items: items}
	return m.updates.report, nil
}

// securityNotes collects the security sections of all releases after the
// installed version up to the latest one (one more request to GitHub, only
// when an update is available). Without the list of releases (only tags)
// it uses the notes of the latest release.
func (m *Manager) securityNotes(ctx context.Context, ownerRepo, current, latest, latestNotes string) string {
	var rels []struct {
		TagName    string `json:"tag_name"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	var parts []versionedSection
	if code, err := m.ghGet(ctx, ownerRepo)("/repos/"+ownerRepo+"/releases?per_page=100", &rels); err != nil || code != 200 {
		rels = nil
	}
	seen := false
	for _, r := range rels {
		if r.Draft || r.Prerelease || r.TagName == "" || CompareVersions(r.TagName, current) <= 0 || CompareVersions(r.TagName, latest) > 0 {
			continue
		}
		seen = seen || r.TagName == latest
		if sec, ok := SecuritySection(r.Body); ok {
			parts = append(parts, versionedSection{r.TagName, sec})
		}
	}
	if !seen {
		if sec, ok := SecuritySection(latestNotes); ok {
			parts = append(parts, versionedSection{latest, sec})
		}
	}
	// newest first
	slices.SortStableFunc(parts, func(a, b versionedSection) int { return CompareVersions(b.tag, a.tag) })
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = p.tag + ":"
		if p.text != "" {
			out[i] += "\n" + p.text
		}
	}
	return clip(strings.Join(out, "\n\n"), 4000)
}

// versionedSection is the security section of one release.
type versionedSection struct{ tag, text string }

// clip shortens s to at most n bytes (valid UTF-8, with "…").
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "") + "…"
}

// SecuritySection finds the section "Security" or "Sicherheit" (a Markdown
// heading of any level, case ignored, optional colon) in release notes and
// returns its text up to the next heading of the same or a higher level.
// ok reports whether the heading exists (the text may be empty).
func SecuritySection(notes string) (text string, ok bool) {
	lines := strings.Split(strings.ReplaceAll(notes, "\r\n", "\n"), "\n")
	level := 0
	var out []string
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		n := 0
		for n < len(t) && n < 6 && t[n] == '#' {
			n++
		}
		isHeading := n > 0 && (len(t) == n || t[n] == ' ')
		if level > 0 {
			if isHeading && n <= level {
				break
			}
			out = append(out, ln)
			continue
		}
		if isHeading {
			title := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(strings.Trim(t[n:], "# ")), ":"))
			if title == "security" || title == "sicherheit" {
				level, ok = n, true
			}
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n")), ok
}

// LastUpdateReport returns the cached report (nil if never checked).
func (m *Manager) LastUpdateReport() *UpdateReport {
	m.updates.mu.Lock()
	defer m.updates.mu.Unlock()
	return m.updates.report
}

// UpdatePlugin installs the latest release of a plugin (from the last check)
// and starts a rebuild.
func (m *Manager) UpdatePlugin(ctx context.Context, id string, opt InstallOptions) (*InstallResult, error) {
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
		res, err := m.Install(ctx, it.Repo, it.Tag, opt)
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

// describeSuffix matches what git describe appends to a tag: the number of
// commits after it, the commit and optionally "-dirty" (v1.2.0-5-gabc1234).
var describeSuffix = regexp.MustCompile(`-(\d+)-g[0-9a-f]{4,40}(-dirty)?$`)

// parseVersion parses "v1.2.3", "1.2", "1.2.3-rc1" into numbers (pre-release
// suffixes rank below the release). A version of git describe
// ("v1.2.3-5-gabc1234") ranks after its tag and before the next version.
func parseVersion(v string) ([5]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	after := 0
	if m := describeSuffix.FindStringSubmatchIndex(v); m != nil {
		n, err := strconv.Atoi(v[m[2]:m[3]])
		if err != nil {
			return [5]int{}, false
		}
		after, v = n, v[:m[0]]
	}
	pre := 1 << 30
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		if v[i] == '-' {
			pre = 0
		}
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return [5]int{}, false
	}
	var out [5]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [5]int{}, false
		}
		out[i] = n
	}
	out[3], out[4] = pre, after
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
	for i := 0; i < 5; i++ {
		if va[i] != vb[i] {
			if va[i] < vb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
