package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// Runtime::parseHtmlExtensionInfo uses Preg::matchAll, which throws.
func TestParseHtmlExtensionInfo_PcreError(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the regex engine up to its backtrack limit; skipped in -short mode")
	}
	_, err := ParseHtmlExtensionInfo(`<tr><td class="e">` + strings.Repeat(" ", 3000) + "x")
	if _, ok := errors.AsType[*php.PcreError](err); !ok {
		t.Fatalf("got %v, want a *php.PcreError", err)
	}
}

// Version::parseOpenssl, parseLibjpeg and parseZoneinfoVersion use
// Preg::isMatchStrictGroups, which throws.
func TestVersion_ParsePcreError(t *testing.T) {
	_, _, _, err := ParseOpenssl("1.0.2 (" + strings.Repeat("x", 1100000))
	if _, ok := errors.AsType[*php.PcreError](err); !ok {
		t.Errorf("ParseOpenssl: got %v, want a *php.PcreError", err)
	}

	_, _, err = ParseLibjpeg("9" + strings.Repeat("a", 1100000) + "!")
	if _, ok := errors.AsType[*php.PcreError](err); !ok {
		t.Errorf("ParseLibjpeg: got %v, want a *php.PcreError", err)
	}

	_, _, err = ParseZoneinfoVersion("2024" + strings.Repeat("a", 1100000) + "!")
	if _, ok := errors.AsType[*php.PcreError](err); !ok {
		t.Errorf("ParseZoneinfoVersion: got %v, want a *php.PcreError", err)
	}
}
