package feed

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/mtsonline/enterprisewiki/internal/content"
)

// BaseURL is the configured public URL or, if none is set, the URL the
// request came in on.
func BaseURL(configured string, r *http.Request) string {
	if configured != "" {
		return strings.TrimSuffix(configured, "/")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// SelfURL is the URL of the requested feed without the personal token.
func SelfURL(base string, r *http.Request) string {
	q := url.Values{}
	for k, v := range r.URL.Query() {
		if k != "token" {
			q[k] = v
		}
	}
	u := base + r.URL.Path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u
}

// FromListing converts listed elements into entries.
func FromListing(base string, list []content.ListedElement) []Entry {
	out := make([]Entry, 0, len(list))
	for _, e := range list {
		out = append(out, Entry{ID: fmt.Sprintf("%s/e/%d", base, e.ID), Title: e.Title, Link: fmt.Sprintf("%s/e/%d", base, e.ID),
			Published: e.CreatedAt, Updated: e.UpdatedAt, Author: e.Author, Summary: e.Summary, Content: e.HTML, Categories: e.Tags})
	}
	return out
}

// Pick returns the text in lang or any other language.
func Pick(m map[string]string, lang string) string {
	if v := m[lang]; v != "" {
		return v
	}
	for _, v := range m {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
