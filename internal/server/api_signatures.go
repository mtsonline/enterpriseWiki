package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
	h "github.com/mtsonline/enterprisewiki/internal/httpx"
	"github.com/mtsonline/enterprisewiki/internal/signature"
)

// routesSignatures: signature methods, evidence, verification and export.
func (s *Server) routesSignatures() {
	// GET /api/v1/signature-methods: the active methods, those the caller can use first.
	s.handle("GET /api/v1/signature-methods", func(w http.ResponseWriter, r *http.Request) (any, error) {
		p, err := requireUser(r)
		if err != nil {
			return nil, err
		}
		return s.app.Signatures.Methods(r.Context(), p.UserID, r.URL.Query().Get("lang")), nil
	})
	// GET /api/v1/signature-keys: the public seal keys of the wiki (to compare
	// with an export).
	s.handle("GET /api/v1/signature-keys", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if _, err := requireUser(r); err != nil {
			return nil, err
		}
		return s.app.Signatures.Keys(r.Context())
	})
	get := func(r *http.Request) (*signature.Evidence, error) {
		if _, err := requireUser(r); err != nil {
			return nil, err
		}
		id, err := h.PathID(r, "id")
		if err != nil {
			return nil, err
		}
		return s.app.Signatures.Get(r.Context(), id)
	}
	// GET /api/v1/signatures/{id}: the evidence (signer, administrators and
	// who may see what was signed).
	s.handle("GET /api/v1/signatures/{id}", func(w http.ResponseWriter, r *http.Request) (any, error) {
		return get(r)
	})
	// GET /api/v1/signatures/{id}/verify: record, method proof and seal.
	s.handle("GET /api/v1/signatures/{id}/verify", func(w http.ResponseWriter, r *http.Request) (any, error) {
		e, err := get(r)
		if err != nil {
			return nil, err
		}
		x, err := s.app.Signatures.ExportOf(r.Context(), e)
		if err != nil {
			return nil, err
		}
		keys, err := s.app.Signatures.Keys(r.Context())
		if err != nil {
			return nil, err
		}
		checks := signature.Verify(x, keys)
		return map[string]any{"valid": signature.Valid(checks), "checks": checks, "signature": e}, nil
	})
	// GET /api/v1/signatures/{id}/export: the evidence with its public key
	// as a JSON file (verify elsewhere: ewiki signature verify FILE).
	s.handle("GET /api/v1/signatures/{id}/export", func(w http.ResponseWriter, r *http.Request) (any, error) {
		e, err := get(r)
		if err != nil {
			return nil, err
		}
		x, err := s.app.Signatures.ExportOf(r.Context(), e)
		if err != nil {
			return nil, err
		}
		b, err := json.MarshalIndent(x, "", "  ")
		if err != nil {
			return nil, err
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="signature-%d.json"`, e.ID))
		_, _ = w.Write(b)
		return h.Handled{}, nil
	})
	// POST /api/v1/signatures/verify: verify an exported signature (JSON
	// body) against the keys of this wiki.
	s.handle("POST /api/v1/signatures/verify", func(w http.ResponseWriter, r *http.Request) (any, error) {
		if _, err := requireUser(r); err != nil {
			return nil, err
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		var x signature.Export
		if err := json.Unmarshal(b, &x); err != nil || x.Method == "" {
			return nil, apperr.BadRequest("not an exported signature")
		}
		keys, err := s.app.Signatures.Keys(r.Context())
		if err != nil {
			return nil, err
		}
		checks := signature.Verify(&x, keys)
		return map[string]any{"valid": signature.Valid(checks), "checks": checks}, nil
	})
}
