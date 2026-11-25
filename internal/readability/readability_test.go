package readability

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

type cases struct {
	Texts []struct {
		Name      string   `json:"name"`
		Text      string   `json:"text"`
		Words     []int    `json:"words"`
		LongWords []string `json:"long_words"`
	} `json:"texts"`
	Active []struct {
		Site   string `json:"site"`
		Pref   string `json:"pref"`
		Active bool   `json:"active"`
	} `json:"active"`
}

func load(t *testing.T) cases {
	t.Helper()
	b, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var c cases
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestSharedCases: the counting of the shared cases (the browser runs the
// same file in web/tests/unit/readability.test.ts).
func TestSharedCases(t *testing.T) {
	c := load(t)
	if len(c.Texts) < 10 {
		t.Fatalf("too few cases: %d", len(c.Texts))
	}
	for _, tc := range c.Texts {
		var words []int
		for _, s := range Sentences(tc.Text) {
			words = append(words, Words(s))
		}
		if !slices.Equal(words, tc.Words) {
			t.Errorf("%s: words per sentence %v, want %v", tc.Name, words, tc.Words)
		}
		long := LongWords(tc.Text, DefaultWordLetters)
		if long == nil {
			long = []string{}
		}
		if !slices.Equal(long, tc.LongWords) {
			t.Errorf("%s: long words %q, want %q", tc.Name, long, tc.LongWords)
		}
	}
}

// TestActive: the site decides whether the hint is offered at all; within
// that, the person (an unknown site mode is off).
func TestActive(t *testing.T) {
	for _, tc := range load(t).Active {
		if got := Active(tc.Site, tc.Pref); got != tc.Active {
			t.Errorf("site %q, person %q: %v, want %v", tc.Site, tc.Pref, got, tc.Active)
		}
	}
}

func TestDefaultsInRange(t *testing.T) {
	if DefaultSentenceWords < MinSentenceWords || DefaultSentenceWords > MaxSentenceWords || DefaultWordLetters < MinWordLetters || DefaultWordLetters > MaxWordLetters {
		t.Fatal("defaults outside of the ranges")
	}
}
