package server

import (
	"testing"

	"github.com/mtsonline/enterprisewiki/internal/config"
)

// TestEmbedCSP: the frame-src additions come only from valid entries, even
// when config.yaml was edited by hand, and at most config.MaxEmbedOrigins.
func TestEmbedCSP(t *testing.T) {
	c := &config.Config{}
	c.Media.EmbedAllow = []string{"https://player.example.com", "https://x.example.com; script-src *", "http://y.example.com", "https://ok.example.com:8443"}
	if got, want := embedCSP(c), " https://player.example.com https://ok.example.com:8443"; got != want {
		t.Errorf("embedCSP = %q, want %q", got, want)
	}
	c.Media.EmbedAllow = nil
	for range config.MaxEmbedOrigins + 5 {
		c.Media.EmbedAllow = append(c.Media.EmbedAllow, "https://a.example.com")
	}
	if got := len(embedCSP(c)); got != config.MaxEmbedOrigins*len(" https://a.example.com") {
		t.Errorf("not bounded: %d", got)
	}
}
