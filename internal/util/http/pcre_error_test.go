package http

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// Response::findHeaderValue's Preg::isMatch throws on a header line with a
// long run of whitespace inside its value.
func TestFindHeaderValue_PcreError(t *testing.T) {
	headers := []string{"HTTP/1.1 302 Found", "Location: a" + strings.Repeat(" ", 20000) + "b"}

	if _, _, err := FindHeaderValueChecked(headers, "location"); err == nil {
		t.Fatal("expected a PcreException")
	} else if _, ok := errors.AsType[*php.PcreError](err); !ok {
		t.Fatalf("got %v, want a *php.PcreError", err)
	}

	response := NewResponse("https://example.org/", 302, headers, "")
	if _, err := redirectTarget("https://example.org/", response); err == nil {
		t.Error("redirectTarget: expected the PcreException")
	}

	if value, ok := FindHeaderValue(headers, "location"); ok || value != "" {
		t.Errorf("FindHeaderValue = %q, %v; want null", value, ok)
	}
}
