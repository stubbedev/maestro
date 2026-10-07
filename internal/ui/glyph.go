package ui

import (
	"os"
	"strings"
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
	// GlyphRemove marks a package removed.
	GlyphRemove

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
	GlyphRemove:    {"−", "-"},
}

// String is the glyph as the console can show it.
func (g Glyph) String() string {
	if unicodeConsole() {
		return glyphs[g][0]
	}

	return glyphs[g][1]
}

// unicodeConsole is whether the console shows Unicode: on Windows, when
// its output code page is UTF-8; elsewhere, unless the locale (LC_ALL,
// LC_CTYPE, LANG, the first one set) names another encoding.
var unicodeConsole = sync.OnceValue(func() bool {
	if ok, known := consoleIsUTF8(); known {
		return ok
	}
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := os.Getenv(name); v != "" {
			v = strings.ToLower(v)

			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}

	return true
})
