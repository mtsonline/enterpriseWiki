package pluginmgr

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/config"
	"github.com/mtsonline/enterprisewiki/internal/coreplugin"
)

// Origin levels of core plugins. They are decided by checks of the source,
// never by anything the plugin says about itself.
const (
	// LevelOfficial: shipped with the wiki (later also: signed by the project).
	LevelOfficial = "official"
	// LevelOrganisation: from a source the administrators entered as trusted.
	LevelOrganisation = "organisation"
	// LevelExternal: any other source; installing needs a confirmation.
	LevelExternal = "external"
	// LevelUnknown: a plugin folder without a record of its installation
	// (copied by hand); treated like an external one.
	LevelUnknown = "unknown"
)

// originFile records how a core plugin got into the plugin folder. A file of
// this name coming with the plugin's sources is never trusted: Install
// removes it and writes its own.
const originFile = ".ewiki-origin.json"

// Origin describes where an installed core plugin comes from.
type Origin struct {
	Level       string    `json:"level"`
	Source      string    `json:"source,omitempty"`  // normalised address or directory
	Trusted     string    `json:"trusted,omitempty"` // matching trusted source
	Ref         string    `json:"ref,omitempty"`
	Commit      string    `json:"commit,omitempty"`
	InstalledBy string    `json:"installed_by,omitempty"`
	InstalledAt time.Time `json:"installed_at,omitzero"`
	Accepted    bool      `json:"accepted,omitempty"` // external: the warning was confirmed
}

// SourceCheck is the result of checking a source before an installation.
type SourceCheck struct {
	Level   string `json:"level"`
	Source  string `json:"source"`
	Trusted string `json:"trusted,omitempty"`
	// Allowed is false when external core plugins are switched off.
	Allowed bool `json:"allowed"`
}

// InstallOptions are the decisions of the person installing.
type InstallOptions struct {
	// AcceptExternal confirms the warning for a source that is neither
	// official nor trusted. It covers exactly this installation.
	AcceptExternal bool
	// By names who installs (user handle or "cli"), for the record.
	By string
}

// ErrExternalNotAccepted is returned (status 400, code
// "external_not_accepted") when an external source was not confirmed.
var ErrExternalNotAccepted = &apperr.Error{Status: 400, Code: "external_not_accepted",
	Msg: "this source is neither official nor trusted: confirm the warning to install it (accept_external / --accept-external)"}

var scpLike = regexp.MustCompile(`^([A-Za-z0-9._-]+)@([A-Za-z0-9.-]+):(.+)$`)

// NormalizeSource brings an address or directory into one comparable form:
// git addresses as scheme://host/path (lower-case scheme and host, without
// user, ".git" and trailing slashes), directories as absolute paths with
// symbolic links resolved. Addresses with passwords, queries, fragments or
// ".." segments are refused.
func NormalizeSource(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("the source is empty")
	}
	if strings.HasPrefix(raw, "github.com/") || strings.HasPrefix(raw, "gitlab.com/") {
		raw = "https://" + raw
	}
	if m := scpLike.FindStringSubmatch(raw); m != nil && !strings.Contains(raw, "://") {
		raw = "ssh://" + m[1] + "@" + m[2] + "/" + strings.TrimPrefix(m[3], "/")
	}
	if !strings.Contains(raw, "://") {
		abs, err := filepath.Abs(raw)
		if err != nil {
			return "", err
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		return filepath.Clean(abs), nil
	}
	// what is checked must be exactly what git fetches: no encodings,
	// backslashes or control characters a server might read differently
	if strings.ContainsAny(raw, "%\\") || strings.ContainsFunc(raw, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", errors.New("the address must not contain %, \\ or control characters")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid address: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "https" && scheme != "ssh" {
		return "", errors.New("only https:// and ssh:// addresses are allowed")
	}
	if _, hasPW := u.User.Password(); hasPW {
		return "", errors.New("the address must not contain a password")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", errors.New("the address must not contain a query or fragment")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return "", errors.New("the address has no host")
	}
	if port := u.Port(); port != "" && !(scheme == "https" && port == "443") && !(scheme == "ssh" && port == "22") {
		host += ":" + port
	}
	segs := strings.Split(strings.TrimSuffix(strings.TrimPrefix(u.Path, "/"), "/"), "/")
	if slices.Contains(segs, "..") || slices.Contains(segs, ".") {
		return "", errors.New("the address must not contain . or .. segments")
	}
	if len(segs) > 1 && slices.Contains(segs, "") {
		return "", errors.New("the address must not contain empty path segments")
	}
	p := path.Clean("/" + strings.Join(segs, "/"))
	p = strings.TrimSuffix(strings.TrimSuffix(p, "/"), ".git")
	if p == "/" {
		p = ""
	}
	return scheme + "://" + host + p, nil
}

// prefixMatches reports whether the normalised source lies below the
// normalised prefix, at a path boundary.
func prefixMatches(source, prefix string) bool {
	if source == prefix {
		return true
	}
	sep := "/"
	if !strings.Contains(prefix, "://") {
		sep = string(filepath.Separator)
	}
	return strings.HasPrefix(source, strings.TrimSuffix(prefix, sep)+sep)
}

// CheckSource decides the level of a source (catalog ids are resolved
// first). Only sources from trusted entries are "organisation"; until
// signatures exist nothing installed from a source is "official".
func (m *Manager) CheckSource(source string) (*SourceCheck, error) {
	gitURL, local, _, err := m.resolve(source, "")
	if err != nil {
		return nil, err
	}
	raw := gitURL
	if raw == "" {
		raw = local
	}
	norm, err := NormalizeSource(raw)
	if err != nil {
		return nil, apperr.BadRequest("%s", err.Error())
	}
	cfg := m.cfg.Get().Plugins
	out := &SourceCheck{Level: LevelExternal, Source: norm, Allowed: cfg.ExternalAllowed()}
	for _, t := range cfg.TrustedSources {
		p, err := NormalizeSource(t.Prefix)
		if err != nil || strings.Contains(p, "://") != strings.Contains(norm, "://") {
			continue
		}
		if prefixMatches(norm, p) {
			out.Level, out.Trusted, out.Allowed = LevelOrganisation, p, true
			break
		}
	}
	return out, nil
}

// NormalizeTrusted checks a new trusted source: a normalised https/ssh
// address with at least one path segment (a whole host is too broad), or an
// absolute directory.
func NormalizeTrusted(prefix string) (string, error) {
	p := strings.TrimSpace(prefix)
	if !strings.Contains(p, "://") && !scpLike.MatchString(p) && !strings.HasPrefix(p, "github.com/") && !strings.HasPrefix(p, "gitlab.com/") && !filepath.IsAbs(p) {
		return "", errors.New("a trusted source is an https:// or ssh:// address or an absolute directory")
	}
	norm, err := NormalizeSource(p)
	if err != nil {
		return "", err
	}
	if strings.Contains(norm, "://") {
		if rest := norm[strings.Index(norm, "://")+3:]; !strings.Contains(rest, "/") {
			return "", errors.New("a trusted address needs a path (an organisation or folder), not only a host")
		}
	} else if norm == string(filepath.Separator) {
		return "", errors.New("the root directory cannot be a trusted source")
	}
	return norm, nil
}

// SetTrustedSources replaces the list of trusted sources (normalised, without
// duplicates).
func SetTrustedSources(c *config.Config, list []config.TrustedSource) error {
	seen := map[string]bool{}
	out := make([]config.TrustedSource, 0, len(list))
	for _, t := range list {
		p, err := NormalizeTrusted(t.Prefix)
		if err != nil {
			return apperr.BadRequest("%s: %s", t.Prefix, err.Error())
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, config.TrustedSource{Prefix: p, Note: strings.TrimSpace(t.Note)})
	}
	c.Plugins.TrustedSources = out
	return nil
}

// Origins reports the origin of every core plugin known on disk. Plugins of
// the image itself are official; plugins in the plugin folder carry the
// record their installation wrote, or count as unknown when they were
// copied there by hand.
func (m *Manager) Origins() map[string]Origin {
	out := map[string]Origin{}
	// compiled into this program: official only if the project ships it
	for _, p := range coreplugin.Registered() {
		id := p.Manifest().ID
		if slices.Contains(officialPlugins, id) {
			out[id] = Origin{Level: LevelOfficial}
		} else {
			out[id] = Origin{Level: LevelUnknown}
		}
	}
	// in the plugin folder (also installed ones not built yet): the record
	// written by Install decides; a wiki without plugin folder has none
	dir := m.PluginsDir()
	if _, err := os.Stat(dir); err == nil {
		for id, d := range pluginDirs(dir) {
			if _, err := ReadManifest(filepath.Join(dir, d)); err == nil {
				out[id] = m.originOf(id, d)
			}
		}
	}
	return out
}

func (m *Manager) originOf(id, dir string) Origin {
	b, err := os.ReadFile(filepath.Join(m.PluginsDir(), dir, originFile)) // #nosec G304 -- plugin folder of this wiki
	if err == nil {
		var o Origin
		if json.Unmarshal(b, &o) == nil && o.Level != "" {
			return o
		}
	}
	if !m.layout.Image() && slices.Contains(officialPlugins, id) {
		return Origin{Level: LevelOfficial} // part of the wiki's source tree
	}
	return Origin{Level: LevelUnknown}
}

func writeOrigin(dir string, o Origin) error {
	b, err := json.MarshalIndent(o, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, originFile), append(b, '\n'), 0o600) // #nosec G703 -- inside the plugin folder of this wiki
}
