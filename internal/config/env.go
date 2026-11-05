package config

import (
	"net"
	"os"
	"strings"
)

// EnvTrustedProxies overrides server.trusted_proxies: IP addresses and CIDR
// networks separated by commas or spaces (e.g. "172.30.80.2" for the proxy
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

// applyEnv applies settings that the environment overrides (not written to
// the config file).
func applyEnv(c *Config) {
	if p, ok := TrustedProxiesFromEnv(); ok {
		c.Server.TrustedProxies = p
	}
}
