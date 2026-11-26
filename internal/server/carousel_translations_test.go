package server_test

import (
	"fmt"
	"testing"
)

// TestCarouselSlideTranslations: an own slide carries its title, text and
// alternative text in the other languages of the wiki; readers get it in
// their language, the default language otherwise. Only languages of the
// wiki are accepted, an image needs its alternative text in every language,
// and an empty title leaves a language out.
func TestCarouselSlideTranslations(t *testing.T) {
	e := setup(t)
	admin := e.client()
	admin.login("admin", adminPW)
	var c struct {
		ID int64 `json:"id"`
	}
	admin.must("POST", "/api/v1/x/core.carousel/carousels", map[string]any{"name": "Languages", "position": "content-top", "enabled": true}, &c, 201)
	slides := fmt.Sprintf("/api/v1/x/core.carousel/carousels/%d/slides", c.ID)
	for name, tr := range map[string]map[string]any{
		"unknown language":   {"fr": map[string]string{"title": "Bienvenue"}},
		"default language":   {"en": map[string]string{"title": "Welcome"}},
		"alt text missing":   {"de": map[string]string{"title": "Willkommen"}},
		"title far too long": {"de": map[string]string{"title": string(make([]byte, 201)), "image_alt": "x"}},
	} {
		if code := admin.do("POST", slides, map[string]any{"title": "Welcome", "image": "/api/v1/files/abc", "image_alt": "A hill", "translations": tr}, nil); code != 400 {
			t.Fatalf("%s: %d", name, code)
		}
	}
	admin.must("POST", slides, map[string]any{"title": "Welcome", "text": "Hello", "image": "/api/v1/files/abc", "image_alt": "A hill",
		"translations": map[string]any{"de": map[string]string{"title": "Willkommen", "text": "Hallo", "image_alt": "Ein Hügel"}}}, nil, 201)
	admin.must("POST", slides, map[string]any{"title": "Only English", "translations": map[string]any{"de": map[string]string{"title": " "}}}, nil, 201)
	type shown []struct {
		Slides []struct {
			Title, Text  string
			ImageAlt     string         `json:"image_alt"`
			Translations map[string]any `json:"translations"`
		} `json:"slides"`
	}
	for lang, want := range map[string][3]string{"de": {"Willkommen", "Hallo", "Ein Hügel"}, "en": {"Welcome", "Hello", "A hill"}} {
		var s shown
		admin.must("GET", "/api/v1/x/core.carousel/shown?lang="+lang, nil, &s, 200)
		if len(s) != 1 || len(s[0].Slides) != 2 {
			t.Fatalf("%s: %+v", lang, s)
		}
		got := s[0].Slides[0]
		if got.Title != want[0] || got.Text != want[1] || got.ImageAlt != want[2] || got.Translations != nil {
			t.Fatalf("%s: %+v", lang, got)
		}
		if s[0].Slides[1].Title != "Only English" {
			t.Fatalf("%s, a slide without a translation: %+v", lang, s[0].Slides[1])
		}
	}
}
