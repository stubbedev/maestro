package ui

import (
	"regexp"
	"strings"
	"testing"
)

// gradientTagRe matches the tags gradient wraps a column in.
var gradientTagRe = regexp.MustCompile(`<fg=#[0-9a-f]{6}>|</>`)

// TestLogo_Gradient checks that the logo's markup is its text and nothing
// else, and that its colours run from logoFrom at the left edge to logoTo
// at the right.
func TestLogo_Gradient(t *testing.T) {
	for _, text := range []string{wordmark, asciiWordmark} {
		got := gradient(text, logoFrom, logoTo)
		if plain := gradientTagRe.ReplaceAllString(got, ""); plain != text {
			t.Errorf("markup without tags:\n%s\nwant:\n%s", plain, text)
		}
	}

	width := 0
	for line := range strings.SplitSeq(wordmark, "\n") {
		width = max(width, len([]rune(line)))
	}
	if got := lerp(logoFrom, logoTo, 0, width); got != logoFrom {
		t.Errorf("first column: %v, want %v", got, logoFrom)
	}
	if got := lerp(logoFrom, logoTo, width-1, width); got != logoTo {
		t.Errorf("last column: %v, want %v", got, logoTo)
	}
}
