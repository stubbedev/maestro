package ui

import (
	"os"
	"regexp"
	"sync"
)

// Glyph is a symbol of decorated output, printed as Unicode where the
// console can show it and as an ASCII stand-in where it cannot (a Windows
// console on a legacy code page, a non-UTF-8 locale).
type Glyph uint8

// The glyphs.
const (
	// GlyphPrompt marks a question.
	GlyphPrompt Glyph = iota
	// GlyphBarDone is a done cell of a progress bar.
	GlyphBarDone
	// GlyphBarHead is the leading cell of a progress bar.
	GlyphBarHead
	// GlyphBarTodo is a cell of a progress bar still to do.
	GlyphBarTodo
	// GlyphItem is an item of a list.
	GlyphItem
	// GlyphInstall marks a package installed.
	GlyphInstall
	// GlyphUpgrade marks a package upgraded.
	GlyphUpgrade
	// GlyphDowngrade marks a package downgraded.
	GlyphDowngrade
	// GlyphSeparator separates the items of a line.
	GlyphSeparator
	// GlyphRemove marks a package removed.
	GlyphRemove
	// GlyphCheckOK marks a check that passed.
	GlyphCheckOK
	// GlyphCheckWarning marks a check that warned.
	GlyphCheckWarning
	// GlyphCheckFailed marks a check that failed.
	GlyphCheckFailed

	numGlyphs
)

// glyphs are the glyphs as Unicode and as ASCII.
var glyphs = [numGlyphs][2]string{
	GlyphPrompt:  {"?", "?"},
	GlyphBarDone: {"━", "="},
	GlyphBarHead: {"╸", ">"},
	GlyphBarTodo: {"━", "-"},

	GlyphItem:      {"-", "-"},
	GlyphInstall:   {"+", "+"},
	GlyphUpgrade:   {"↑", "^"},
	GlyphDowngrade: {"↓", "v"},
	GlyphSeparator: {"·", "-"},
	GlyphRemove:    {"−", "-"},

	GlyphCheckOK:      {"✔", "OK"},
	GlyphCheckWarning: {"!", "WARN"},
	GlyphCheckFailed:  {"✖", "FAIL"},
}

// String is the glyph as the console can show it.
func (g Glyph) String() string {
	if unicodeConsole() {
		return glyphs[g][0]
	}

	return glyphs[g][1]
}

// utf8LocaleRe matches a locale of the UTF-8 encoding ("en_US.UTF-8",
// "C.utf8").
var utf8LocaleRe = regexp.MustCompile(`(?i)utf-?8`)

// unicodeConsole is whether the console shows Unicode: on Windows, when
// its output code page is UTF-8; elsewhere, unless the locale (LC_ALL,
// LC_CTYPE, LANG, the first one set) names another encoding.
var unicodeConsole = sync.OnceValue(func() bool {
	if ok, known := consoleIsUTF8(); known {
		return ok
	}
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(name); v != "" {
			return utf8LocaleRe.MatchString(v)
		}
	}

	return true
})
