package repository

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// The errors carry the messages of Composer's exceptions (where they were
// thrown is free, docs/PORTING.md "The contract").
func TestErrorMessages(t *testing.T) {
	_, pathErr := NewPathRepository(php.ArrayOf(), nil, nil)
	_, onlyErr := NewFilterRepository(filterFixture(t), php.ArrayOf("only", "foo"))
	_, excludeErr := NewFilterRepository(filterFixture(t), php.ArrayOf("exclude", "foo"))
	_, bothErr := NewFilterRepository(filterFixture(t), filterOptions([]string{}, []string{}, nil))
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"path url", pathErr, "You must specify the `url` configuration for the path repository"},
		{"only", onlyErr, `"only" key for repository array repo (defining 4 packages) should be an array`},
		{"exclude", excludeErr, `"exclude" key for repository array repo (defining 4 packages) should be an array`},
		{"only and exclude", bothErr, `Only one of "only" and "exclude" can be specified for repository array repo (defining 4 packages)`},
	} {
		if tc.err == nil || tc.err.Error() != tc.want {
			t.Errorf("%s: %v, want %q", tc.name, tc.err, tc.want)
		}
	}
}

// A wrappedError carries its previous exception.
func TestWrappedErrorChain(t *testing.T) {
	prev := &InvalidRepositoryError{Message: "inner"}
	err := error(&wrappedError{err: &InvalidRepositoryError{Message: "outer"}, previous: prev})
	if err.Error() != "outer" {
		t.Errorf("message %q, want %q", err.Error(), "outer")
	}
	if got := phperr.PreviousOf(err); got != prev { //nolint:errorlint // the object itself
		t.Errorf("previous = %v, want %v", got, prev)
	}
}
