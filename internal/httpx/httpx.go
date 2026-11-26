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
	"sync/atomic"

	"github.com/mtsonline/enterprisewiki/internal/apperr"
)

// Handler returns a value that is encoded as JSON, or an error.
type Handler func(w http.ResponseWriter, r *http.Request) (any, error)

// NoContent can be returned by a Handler to send 204.
type NoContent struct{}

// Created wraps a value to send 201.
type Created struct{ V any }

// Accepted wraps a value to send 202: the work goes on in the background.
type Accepted struct{ V any }

// Handled is returned by handlers that wrote the response themselves
// (files, XML, redirects).
type Handled struct{}

// logger is used for internal errors. Opening an instance sets it while
// requests of another app of the process may read it: atomic.
var logger atomic.Pointer[slog.Logger]

// SetLogger sets the logger for internal errors.
func SetLogger(l *slog.Logger) { logger.Store(l) }

func logErr() *slog.Logger {
	if l := logger.Load(); l != nil {
		return l
	}
	return slog.Default()
}

// JSON adapts a Handler to http.HandlerFunc.
//
// A handler that wrote the response itself (a file, CSS, a redirect)
// should return Handled; if it returns nil or NoContent instead, nothing
// more is written (a second status would be ignored by net/http with the
// warning "superfluous response.WriteHeader call"). An error after the
// response was started is only logged: the client already has its answer.
func JSON(h Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tw := &trackWriter{ResponseWriter: w}
		v, err := h(tw, r)
		if tw.wrote {
			if err != nil {
				logErr().Error("request failed after the answer started", "method", r.Method, "path", r.URL.Path, "err", err)
			}
			return
		}
		if err != nil {
			WriteError(w, r, err)
			return
		}
		switch x := v.(type) {
		case Handled:
		case NoContent:
			w.WriteHeader(http.StatusNoContent)
		case Created:
			Write(w, http.StatusCreated, x.V)
		case Accepted:
			Write(w, http.StatusAccepted, x.V)
		case nil:
			w.WriteHeader(http.StatusNoContent)
		default:
			Write(w, http.StatusOK, v)
		}
	}
}

// trackWriter notes whether a handler started the response.
type trackWriter struct {
	http.ResponseWriter
	wrote bool
}

func (t *trackWriter) WriteHeader(s int) {
	t.wrote = true
	t.ResponseWriter.WriteHeader(s)
}

func (t *trackWriter) Write(b []byte) (int, error) {
	t.wrote = true
	return t.ResponseWriter.Write(b)
}

// Flush passes on to the underlying writer (streamed answers).
func (t *trackWriter) Flush() {
	t.wrote = true
	if f, ok := t.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (t *trackWriter) Unwrap() http.ResponseWriter { return t.ResponseWriter }

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
	status, code, _ := apperr.Status(err)
	if status >= 500 {
		logErr().Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	// in the language the client asks for (the interface sends its own)
	body := map[string]string{"code": code, "message": apperr.Localized(err, apperr.RequestLanguage(r))}
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Detail != "" {
		body["detail"] = ae.Detail
	}
	Write(w, status, map[string]any{"error": body})
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
			return apperr.TooLarge("request body too large")
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

var signed sync.Map // route pattern -> true

// RegisterSigned declares that a route ("METHOD /path" as registered)
// receives messages of other systems that the handler authenticates by a
// signature of its own (e.g. incoming webhooks): sessions and API keys never
// count there, so a browser's cookie cannot be used for it and the CSRF
// header is not needed. The handler must check the signature itself.
func RegisterSigned(pattern string) { signed.Store(pattern, true) }

// Signed reports whether a route is declared with RegisterSigned.
func Signed(pattern string) bool {
	_, ok := signed.Load(pattern)
	return ok
}
