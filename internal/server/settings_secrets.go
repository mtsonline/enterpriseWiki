package server

import (
	"fmt"

	"net/url"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/auth"
	"github.com/mtsonline/enterprisewiki/internal/config"
)

const secretMask = "********"

// maskConfig hides secrets of a configuration copy before it leaves the server.
func maskConfig(c *config.Config) {
	c.Database.URL = maskURL(c.Database.URL)
	maskSecrets(c.Settings)
	if p := c.Mail.SMTP.Password; p != "" && !config.HasEnvRef(p) {
		c.Mail.SMTP.Password = secretMask
	}
	if p := c.Mail.Bounce.Password; p != "" && !config.HasEnvRef(p) {
		c.Mail.Bounce.Password = secretMask
	}
}

// secretKey reports whether a settings key holds a secret.
func secretKey(k string) bool {
	lk := strings.ToLower(k)
	return strings.Contains(lk, "password") || strings.Contains(lk, "secret") || strings.HasSuffix(lk, "_key")
}

// maskSecrets hides plugin settings whose key looks secret (unless they are
// ${ENV} references, which are safe to show), also in nested maps and
// lists (e.g. the settings of provider instances).
func maskSecrets(settings map[string]map[string]any) {
	for _, m := range settings {
		maskMap(m)
	}
}

func maskMap(m map[string]any) {
	for k, v := range m {
		switch x := v.(type) {
		case string:
			if secretKey(k) && x != "" && !config.HasEnvRef(x) {
				m[k] = secretMask
			}
		case map[string]any:
			maskMap(x)
		case []any:
			for _, e := range x {
				if em, ok := e.(map[string]any); ok {
					maskMap(em)
				}
			}
		}
	}
}

// checkInstances validates the provider instances of plugin settings m:
// each needs a valid id, unique within the plugin.
func checkInstances(m map[string]any) error {
	v, ok := m[auth.InstanceKey]
	if !ok || v == nil {
		return nil
	}
	l, ok := v.([]any)
	if !ok {
		return apperr.BadRequest("%s must be a list", auth.InstanceKey)
	}
	seen := map[string]bool{}
	for _, e := range l {
		em, ok := e.(map[string]any)
		if !ok {
			return apperr.BadRequest("each instance must be a map")
		}
		id, _ := em["id"].(string)
		if !auth.ValidInstanceID(id) {
			return apperr.BadRequest("instance id %q: 1-24 lower-case letters, digits and hyphens", id)
		}
		if seen[id] {
			return apperr.BadRequest("instance id %q is used twice", id)
		}
		seen[id] = true
	}
	return nil
}

// errSecretAgain refuses a masked secret for a changed destination: the
// stored secret would go to the new address, which the person was never
// allowed to read it for (the mask hides it from them).
func errSecretAgain(where string) error {
	return apperr.BadRequest("%s: the address changed, so enter the secret again (the stored one is not sent to a new address)", where)
}

// destinationKey reports whether a settings key says where a secret goes.
func destinationKey(k string) bool {
	lk := strings.ToLower(k)
	if lk == "port" || strings.HasSuffix(lk, "_port") {
		return true
	}
	// where it goes and how it is protected on the way
	for _, w := range []string{"url", "host", "address", "server", "endpoint", "issuer", "domain", "tenant", "realm", "security", "skip_verify", "insecure", "tls"} {
		if strings.Contains(lk, w) {
			return true
		}
	}
	return false
}

// destinationChanged reports whether a destination key of the map differs
// from the stored one (a key that is new or removed counts as a change).
func destinationChanged(in, stored map[string]any) bool {
	for k, v := range in {
		if destinationKey(k) && !secretKey(k) && fmt.Sprint(v) != fmt.Sprint(stored[k]) {
			return true
		}
	}
	for k, v := range stored {
		if _, ok := in[k]; !ok && destinationKey(k) && !secretKey(k) && v != nil && fmt.Sprint(v) != "" {
			return true
		}
	}
	return false
}

// keepMasked puts the stored secret back wherever the client sent the mask
// it was shown – but only while the destination of the secret (address,
// host, port … of the same map or of a map above) is unchanged. Lists of
// maps are matched by their "id" (provider instances), so reordering or
// removing entries keeps the right secrets.
func keepMasked(in, stored map[string]any, where string) error {
	return keepMaskedBelow(in, stored, where, false)
}

func keepMaskedBelow(in, stored map[string]any, where string, moved bool) error {
	moved = moved || destinationChanged(in, stored)
	for k, v := range in {
		switch x := v.(type) {
		case string:
			if x == secretMask {
				old, ok := stored[k].(string)
				switch {
				case !ok || !secretKey(k):
					delete(in, k) // never store the mask itself
				case moved && old != "":
					return errSecretAgain(where + "." + k)
				default:
					in[k] = old
				}
			}
		case map[string]any:
			old, _ := stored[k].(map[string]any)
			if err := keepMaskedBelow(x, old, where+"."+k, moved); err != nil {
				return err
			}
		case []any:
			olds := map[string]map[string]any{}
			if ol, ok := stored[k].([]any); ok {
				for _, e := range ol {
					if em, ok := e.(map[string]any); ok {
						if id, ok := em["id"].(string); ok && id != "" {
							olds[id] = em
						}
					}
				}
			}
			for _, e := range x {
				if em, ok := e.(map[string]any); ok {
					id, _ := em["id"].(string)
					old := olds[id]
					// a new entry has no stored secret: the mask is dropped
					if err := keepMaskedBelow(em, old, where+"."+k+"."+id, moved && old != nil); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func maskURL(u string) string {
	if config.HasEnvRef(u) {
		return u
	}
	pu, err := url.Parse(u)
	if err != nil || pu.User == nil {
		return u
	}
	if _, has := pu.User.Password(); has {
		pu.User = url.UserPassword(pu.User.Username(), "xxxxx")
	}
	return pu.String()
}
