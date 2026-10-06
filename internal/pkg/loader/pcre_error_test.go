package loader_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// A constraint whose "as" alias runs into a newline makes
// '{^([^,\s@]+) as .+$}' backtrack past the limit: Preg::replace throws.
var backtrackingAliasConstraint = "dev-main as " + strings.Repeat("x", 1100000) + "\ny"

func TestExtractStabilityFlags_PcreError(t *testing.T) {
	_, err := loader.ExtractStabilityFlags(php.ArrayOf("vendor/pkg", backtrackingAliasConstraint), "stable", php.NewArray())
	if _, ok := errors.AsType[*php.PcreError](err); !ok {
		t.Fatalf("got %v, want a *php.PcreError", err)
	}
}

func TestExtractReferences_PcreError(t *testing.T) {
	_, err := loader.ExtractReferences(php.ArrayOf("vendor/pkg", backtrackingAliasConstraint), php.NewArray())
	if _, ok := errors.AsType[*php.PcreError](err); !ok {
		t.Fatalf("got %v, want a *php.PcreError", err)
	}
}
