package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// countWriter counts the status lines a handler chain writes.
type countWriter struct {
	*httptest.ResponseRecorder
	headers int
}

func (c *countWriter) WriteHeader(s int) {
	c.headers++
	c.ResponseRecorder.WriteHeader(s)
}

// TestJSONWrittenOnce: a handler that writes the answer itself and returns
// nil (or an error) gets no second status line (net/http would log
// "superfluous response.WriteHeader call").
func TestJSONWrittenOnce(t *testing.T) {
	for name, h := range map[string]Handler{
		"body, nil": func(w http.ResponseWriter, r *http.Request) (any, error) {
			_, _ = w.Write([]byte("body{}"))
			return nil, nil
		},
		"status, NoContent": func(w http.ResponseWriter, r *http.Request) (any, error) {
			w.WriteHeader(http.StatusNoContent)
			return NoContent{}, nil
		},
		"body, error": func(w http.ResponseWriter, r *http.Request) (any, error) {
			w.WriteHeader(http.StatusOK)
			return nil, errors.New("late")
		},
	} {
		cw := &countWriter{ResponseRecorder: httptest.NewRecorder()}
		JSON(h)(cw, httptest.NewRequest("GET", "/", nil))
		if cw.headers > 1 {
			t.Errorf("%s: %d status lines", name, cw.headers)
		}
	}
	// nothing written: the wrapper answers as before
	cw := &countWriter{ResponseRecorder: httptest.NewRecorder()}
	JSON(func(w http.ResponseWriter, r *http.Request) (any, error) { return map[string]int{"a": 1}, nil })(cw, httptest.NewRequest("GET", "/", nil))
	if cw.Code != 200 || cw.Body.String() != "{\"a\":1}\n" {
		t.Fatalf("value: %d %q", cw.Code, cw.Body.String())
	}
	cw = &countWriter{ResponseRecorder: httptest.NewRecorder()}
	JSON(func(w http.ResponseWriter, r *http.Request) (any, error) { return nil, nil })(cw, httptest.NewRequest("GET", "/", nil))
	if cw.Code != 204 {
		t.Fatalf("nil: %d", cw.Code)
	}
}
