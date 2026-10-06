package composer

import (
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// setIgnorePlatformRequirements raises Installer.php:1459's deprecation
// (Composer 2.10.3 on PHP 8.4.25 under ErrorHandler::register(new
// BufferIO())) and still sets the filter.
func TestInstaller_SetIgnorePlatformRequirementsDeprecation(t *testing.T) {
	util.ResetErrorHandler()
	t.Cleanup(util.ResetErrorHandler)

	out, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	util.RegisterErrorHandler(out)

	installer, err := (&Installer{}).SetIgnorePlatformRequirements(true)
	if err != nil {
		t.Fatal(err)
	}

	if installer.PlatformRequirementFilter() == nil {
		t.Error("no platform requirement filter")
	}

	want := "<warning>Deprecation Notice: Installer::setIgnorePlatformRequirements is deprecated since Composer 2.2, use setPlatformRequirementFilter instead. in " + phperr.AbsPath("Installer.php") + ":1459</warning>\n"
	if got := php.NormalizeEOL(out.Output()); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
