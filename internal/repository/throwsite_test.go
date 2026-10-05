package repository

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// The errors carry the throw site of Composer's exceptions, which the
// console renders ("In PathRepository.php line 113:").
func TestThrowSites(t *testing.T) {
	_, pathErr := NewPathRepository(php.ArrayOf(), nil, nil)
	_, onlyErr := NewFilterRepository(filterFixture(t), php.ArrayOf("only", "foo"))
	_, excludeErr := NewFilterRepository(filterFixture(t), php.ArrayOf("exclude", "foo"))
	_, bothErr := NewFilterRepository(filterFixture(t), filterOptions([]string{}, []string{}, nil))
	for _, tc := range []struct {
		name string
		err  error
		file string
		line int
	}{
		{"path url", pathErr, "PathRepository.php", 113},
		{"only", onlyErr, "FilterRepository.php", 43},
		{"exclude", excludeErr, "FilterRepository.php", 49},
		{"only and exclude", bothErr, "FilterRepository.php", 54},
	} {
		if !phperr.Is(tc.err, tc.file, tc.line) {
			site, _ := phperr.SiteOf(tc.err)
			t.Errorf("%s: %v at %v, want %s:%d", tc.name, tc.err, site, tc.file, tc.line)
		}
	}
}

// A wrappedError renders with its previous exception, and its site is the
// wrapping exception's.
func TestWrappedErrorChain(t *testing.T) {
	prev := &InvalidRepositoryError{Message: "inner", Site: phperr.At("X.php", 1)}
	err := error(&wrappedError{err: &InvalidRepositoryError{Message: "outer", Site: phperr.At("PathRepository.php", 229)}, previous: prev})
	if !phperr.Is(err, "PathRepository.php", 229) {
		t.Errorf("site of %v", err)
	}
	if got := phperr.PreviousOf(err); got != prev { //nolint:errorlint // the object itself
		t.Errorf("previous = %v, want %v", got, prev)
	}
}
