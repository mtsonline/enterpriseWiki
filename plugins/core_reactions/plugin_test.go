package core_reactions

import "testing"

func TestValidEmoji(t *testing.T) {
	ok := []string{"👍", "👍🏽", "❤️", "🎉", "👨‍👩‍👧", "🇦🇹", "1️⃣", "#️⃣", "🏳️‍🌈", "☕", "🏴󠁧󠁢󠁳󠁣󠁴󠁿"}
	bad := []string{"", "a", "1", "hello", "👍a", "<script>", "  ", "👍👍👍👍👍👍👍👍👍👍👍"}
	for _, s := range ok {
		if !ValidEmoji(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range bad {
		if ValidEmoji(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
	if Base("👍🏿") != "👍" || Base("❤️") != "❤" {
		t.Error("base form")
	}
}
