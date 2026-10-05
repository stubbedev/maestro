package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// Runtime::parseHtmlExtensionInfo uses Preg::matchAll, which throws.
func TestParseHtmlExtensionInfo_PcreError(t *testing.T) {
	_, err := ParseHtmlExtensionInfo(`<tr><td class="e">` + strings.Repeat(" ", 3000) + "x")
	if _, ok := errors.AsType[*php.PcreError](err); !ok {
		t.Fatalf("got %v, want a *php.PcreError", err)
	}
}
