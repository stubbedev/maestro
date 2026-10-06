package pkg_test

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
)

// A userland function's TypeError names the function's declaration as its
// site, appends where the call came from and starts its trace with the
// call; an internal function's is raised at the call (PHP 8.4).
func TestTypeErrorSites(t *testing.T) {
	phperr.SetRoot("phar:///opt/maestro")
	t.Cleanup(func() { phperr.SetRoot("") })

	e := pkg.ArgumentTypeError(`Composer\Pcre\Preg::isMatch`, 2, "subject", "string", true).
		Called(`Composer\Pcre\Preg::isMatch`, phperr.At("vendor/composer/pcre/src/Preg.php", 289), "ConfigValidator.php", 134)
	if want := `Composer\Pcre\Preg::isMatch(): Argument #2 ($subject) must be of type string, true given, called in phar:///opt/maestro/src/Composer/Util/ConfigValidator.php on line 134`; e.Error() != want {
		t.Errorf("message %q, want %q", e.Error(), want)
	}
	if !phperr.Is(e, "Preg.php", 289) {
		t.Errorf("site %v, want Preg.php:289", e.Site)
	}
	if got, want := phperr.TraceOf(e), []phperr.Frame{{Function: `Composer\Pcre\Preg::isMatch`, File: "ConfigValidator.php", Line: 134}}; !slices.Equal(got, want) {
		t.Errorf("trace %v, want %v", got, want)
	}

	i := (&php.EngineError{Class: "TypeError", Message: "array_merge(): Argument #2 must be of type array, string given"}).
		Raised("array_merge", "ConfigValidator.php", 177)
	if !phperr.Is(i, "ConfigValidator.php", 177) || i.Error() != "array_merge(): Argument #2 must be of type array, string given" {
		t.Errorf("internal TypeError %q at %v", i.Error(), i.Site)
	}
	if got, want := phperr.TraceOf(i), []phperr.Frame{{Function: "array_merge", File: "ConfigValidator.php", Line: 177}}; !slices.Equal(got, want) {
		t.Errorf("trace %v, want %v", got, want)
	}

	// a return type error or an operator's has no frame of its own
	r := (&pkg.TypeError{Message: "x(): Return value must be of type array, false returned"}).Raised("", "Locker.php", 341)
	if !phperr.Is(r, "Locker.php", 341) || len(phperr.TraceOf(r)) != 0 {
		t.Errorf("return type error at %v with trace %v", r.Site, phperr.TraceOf(r))
	}
}
