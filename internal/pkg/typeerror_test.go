package pkg_test

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// A TypeError carries PHP's message for the argument or return value that
// failed, without where it was raised or PHP's call site (", called in X
// on line N"; docs/PORTING.md "The contract").
func TestTypeErrorMessages(t *testing.T) {
	e := pkg.ArgumentTypeError(`Composer\Pcre\Preg::isMatch`, 2, "subject", "string", true)
	if want := `Composer\Pcre\Preg::isMatch(): Argument #2 ($subject) must be of type string, true given`; !strings.HasPrefix(e.Error(), want) {
		t.Errorf("message %q, want %q", e.Error(), want)
	}

	i := &php.EngineError{Class: "TypeError", Message: "array_merge(): Argument #2 must be of type array, string given"}
	if want := "array_merge(): Argument #2 must be of type array, string given"; i.Error() != want {
		t.Errorf("internal TypeError %q, want %q", i.Error(), want)
	}

	r := &pkg.TypeError{Message: "x(): Return value must be of type array, false returned"}
	if want := "x(): Return value must be of type array, false returned"; r.Error() != want {
		t.Errorf("return type error %q, want %q", r.Error(), want)
	}
}
