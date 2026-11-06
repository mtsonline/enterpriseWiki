package config

import (
	"net"
	"os"
	"regexp"
	"strings"
)

// EnvTrustedProxies overrides server.trusted_proxies: IP addresses and CIDR
// networks separated by commas or spaces (e.g. "172.30.80.250" for the proxy
// container of docker-compose.yml, "127.0.0.1,::1" for a local nginx).
const EnvTrustedProxies = "EWIKI_TRUSTED_PROXIES"

// TrustedProxiesFromEnv returns the proxies of EWIKI_TRUSTED_PROXIES (nil if
// unset); invalid entries are skipped.
func TrustedProxiesFromEnv() ([]string, bool) {
	v, ok := os.LookupEnv(EnvTrustedProxies)
	if !ok || strings.TrimSpace(v) == "" {
		return nil, false
	}
	out := []string{}
	for _, f := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == ';' || r == '\n' || r == '\t' }) {
		if net.ParseIP(f) != nil {
			out = append(out, f)
		} else if _, _, err := net.ParseCIDR(f); err == nil {
			out = append(out, f)
		}
	}
	return out, true
}

// EnvExtraTLSHosts lists further host names (comma separated) the bundled
// proxy may get certificates for: the office servers of docker-compose.yml
// (EWIKI_COLLABORA_HOST, EWIKI_ONLYOFFICE_HOST).
const EnvExtraTLSHosts = "EWIKI_EXTRA_TLS_HOSTS"

// ExtraTLSHosts returns the valid host names of EWIKI_EXTRA_TLS_HOSTS (lower case).
func ExtraTLSHosts() []string {
	var out []string
	for _, f := range strings.FieldsFunc(os.Getenv(EnvExtraTLSHosts), func(r rune) bool { return r == ',' || r == ' ' }) {
		f = strings.ToLower(f)
		if hostRe.MatchString(f) && len(f) <= 253 {
			out = append(out, f)
		}
	}
	return out
}

var hostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// officeEnv are defaults of office plugin settings from the environment
// (docker-compose.yml profiles "collabora" and "onlyoffice"). They apply
// only while the setting is empty, so the administration can change them.
var officeEnv = []struct{ env, plugin, key string }{
	{"EWIKI_COLLABORA_URL", "office.collabora", "url"},
	{"EWIKI_ONLYOFFICE_URL", "office.onlyoffice", "url"},
	{"EWIKI_ONLYOFFICE_INTERNAL_URL", "office.onlyoffice", "internal_url"},
	{"EWIKI_ONLYOFFICE_JWT_SECRET", "office.onlyoffice", "jwt_secret"},
}

// applyEnv applies settings that the environment overrides or presets (not
// written to the config file).
func applyEnv(c *Config) {
	if p, ok := TrustedProxiesFromEnv(); ok {
		c.Server.TrustedProxies = p
	}
	for _, o := range officeEnv {
		v := strings.TrimSpace(os.Getenv(o.env))
		if v == "" {
			continue
		}
		if c.Settings == nil {
			c.Settings = map[string]map[string]any{}
		}
		if c.Settings[o.plugin] == nil {
			c.Settings[o.plugin] = map[string]any{}
		}
		if cur, _ := c.Settings[o.plugin][o.key].(string); cur == "" {
			c.Settings[o.plugin][o.key] = v
		}
	}
	// default viewer (e.g. office.collabora) while none is chosen
	if v := strings.TrimSpace(os.Getenv("EWIKI_MEDIA_VIEWER")); v != "" && c.Media.Viewer == "" && pluginIDRe.MatchString(v) {
		c.Media.Viewer = v
	}
}
