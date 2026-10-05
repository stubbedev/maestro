package archiver

import "testing"

// Glob::toRegex is otherwise covered by the oracle goldens, through the
// exclude patterns.
func TestGlobToRegex(t *testing.T) {
	for glob, want := range map[string]string{
		"*.md":     `#^(?=[^\.])[^/]*\.md$#`,
		"src/**/x": `#^(?=[^\.])src/(?:(?=[^\.])[^/]++/)*(?=[^\.])x$#`,
		"src/**":   `#^(?=[^\.])src/(?:(?=[^\.])[^/]++/?)*$#`,
		"{a,b}?":   `#^(?=[^\.])(a|b)[^/]$#`,
		`\*`:       `#^(?=[^\.])\*$#`,
		`\\`:       `#^(?=[^\.])\\$#`,
		`\{a,b\}`:  `#^(?=[^\.])\{a,b}$#`,
		"a#b":      `#^(?=[^\.])a\#b$#`,
		".hidden":  `#^\.hidden$#`,
		"tests":    `#^(?=[^\.])tests$#`,
		"":         `#^$#`,
	} {
		if got := globToRegex(glob); got != want {
			t.Errorf("globToRegex(%q) = %q, want %q", glob, got, want)
		}
	}
}
