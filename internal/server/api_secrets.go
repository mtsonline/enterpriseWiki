package server

import (
	"net/http"
	"os"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	"github.com/mtsonline/enterprisewiki/internal/audit"
	"github.com/mtsonline/enterprisewiki/internal/identity"
	"github.com/mtsonline/enterprisewiki/internal/secrets"
)

// routesSecrets: how secrets in the database are stored (secrets.mode) and
// bringing all of them to the current mode. Only administrators of the
// instance (the key file belongs to the instance).
func (s *Server) routesSecrets() {
	instanceAdmin := func(r *http.Request) error {
		if err := requireAdmin(r); err != nil {
			return err
		}
		p := identity.From(r.Context())
		if !p.Admin() || !s.limitsEditable(r) {
			return apperr.Forbidden("only administrators of the instance manage the secrets")
		}
		return nil
	}
	// GET /admin/secrets: mode, key file and the values by kind
	s.handle("GET /api/v1/admin/secrets", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := instanceAdmin(r); err != nil {
			return nil, err
		}
		box := s.app.Instance.Secrets
		st, err := secrets.Count(r.Context(), s.app.DB)
		if err != nil {
			return nil, err
		}
		c := s.app.Config.Get()
		env := c.Secrets.PasswordEnv
		if env == "" {
			env = "EWIKI_SECRETS_PASSWORD"
		}
		return map[string]any{"mode": box.Mode(), "key_loaded": box.HasKey(), "password": os.Getenv(env) != "", "password_env": env,
			"values": st, "rewrap_needed": (box.Mode() == secrets.ModePlain && st.AES+st.PQ > 0) ||
				(box.Mode() == secrets.ModeAES && st.Plain+st.PQ > 0) || (box.Mode() == secrets.ModePQ && st.Plain+st.AES > 0)}, nil
	})
	// POST /admin/secrets/rewrap: stores all values in the current mode
	s.handle("POST /api/v1/admin/secrets/rewrap", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if err := instanceAdmin(r); err != nil {
			return nil, err
		}
		n, err := secrets.Rewrap(r.Context(), s.app.DB, s.app.Instance.Secrets)
		if err != nil {
			return nil, err
		}
		if err := audit.Log(r.Context(), s.app.DB, "settings.secrets_rewrap", "settings", 0, nil, map[string]any{"mode": s.app.Instance.Secrets.Mode(), "changed": n}); err != nil {
			return nil, err
		}
		return map[string]int{"changed": n}, nil
	})
}
