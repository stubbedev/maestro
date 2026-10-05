package filterlist_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
)

// An ignore pattern that exhausts the backtrack limit makes Preg::isMatch
// throw a PcreException, which getMatchingBlockEntries lets through.
func TestFilterListAuditor_IgnorePatternPcreError(t *testing.T) {
	name := strings.Repeat("a", 150) + "cb"
	p := pkg.NewCompletePackage(name, "1.0.0.0", "1.0")
	m := filterListMap(createEntry(t, "list", php.ArrayOf("package", name, "constraint", "*")))
	pc := policyConfig(t, php.ArrayOf("list", php.ArrayOf("ignore", php.ListOf("*****c"))))

	_, err := filterlist.FilterListAuditor{}.GetMatchingBlockEntries(p, m, pc, policy.BlockScopeUpdate)
	if _, ok := errors.AsType[*php.PcreError](err); !ok {
		t.Fatalf("got %v, want a *php.PcreError", err)
	}
}
