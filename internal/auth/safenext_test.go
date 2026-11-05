package auth

import "testing"

func TestSafeNextOpenRedirects(t *testing.T) {
	for in, want := range map[string]string{
		"/":                              "/",
		"/wiki/handbook?x=1#top":         "/wiki/handbook?x=1#top",
		"/über-uns":                      "/über-uns",
		"":                               "/",
		"https://evil.example":           "/",
		"//evil.example":                 "/",
		"/\\evil.example":                "/",
		"/\t/evil.example":               "/",
		"/\n/evil.example":               "/",
		"/\r//evil.example":              "/",
		"/ /evil.example":                "/",
		"/\u0085/evil.example":           "/",
		"/ /evil.example":                "/",
		"javascript:alert(1)":            "/",
		"\\\\evil.example":               "/",
		"/%2F%2Fevil.example":            "/%2F%2Fevil.example", // stays a local path
		"/" + string(make([]byte, 3000)): "/",
	} {
		if got := SafeNext(in); got != want {
			t.Errorf("SafeNext(%q) = %q, want %q", in, got, want)
		}
	}
}
