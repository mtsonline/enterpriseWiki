// Package httpx contains small helpers shared by the API and plugins.
package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// Handler returns a value that is encoded as JSON, or an error.
type Handler func(w http.ResponseWriter, r *http.Request) (any, error)

// NoContent can be returned by a Handler to send 204.
type NoContent struct{}

// Created wraps a value to send 201.
type Created struct{ V any }

// Logger is used for internal errors.
var Logger = slog.Default()

// JSON adapts a Handler to http.HandlerFunc.
func JSON(h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := h(w, r)
		if err != nil {
			WriteError(w, r, err)
			return
		}
		switch x := v.(type) {
		case NoContent:
			w.WriteHeader(http.StatusNoContent)
		case Created:
			Write(w, http.StatusCreated, x.V)
		case nil:
			w.WriteHeader(http.StatusNoContent)
		default:
			Write(w, http.StatusOK, v)
		}
	}
}

// Write writes v as JSON with status.
func Write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

// WriteError writes an error response {"error": {...}}.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg := apperr.Status(err)
	if status >= 500 {
		Logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	Write(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}

// Decode reads a JSON body into v (unknown fields are rejected).
func Decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return apperr.BadRequest("request body is empty")
		}
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return apperr.BadRequest("request body too large")
		}
		return apperr.BadRequest("invalid JSON: %v", err)
	}
	return nil
}

// PathID parses a numeric path value.
func PathID(r *http.Request, name string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || v < 0 {
		return 0, apperr.BadRequest("invalid %s", name)
	}
	return v, nil
}

// QueryInt parses an integer query parameter with a default.
func QueryInt(r *http.Request, name string, def int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	return v
}

// QueryList splits a comma separated query parameter.
func QueryList(r *http.Request, name string) []string {
	var out []string
	for _, v := range r.URL.Query()[name] {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

var (
	routesMu sync.Mutex
	routes   = map[string]bool{}
)

// RecordRoute remembers a registered API route ("METHOD /path") so tests can
// verify that every route is covered by the frontend.
func RecordRoute(pattern string) {
	routesMu.Lock()
	routes[pattern] = true
	routesMu.Unlock()
}

// Routes returns all recorded routes, sorted.
func Routes() []string {
	routesMu.Lock()
	defer routesMu.Unlock()
	out := make([]string, 0, len(routes))
	for r := range routes {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}
