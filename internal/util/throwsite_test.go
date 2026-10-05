package util

import (
	"testing"

	"github.com/stubbedev/maestro/internal/phperr"
)

// The throw sites of a few exceptions, as Composer 2.10.3's getFile() and
// getLine() report them.
func TestThrowSites(t *testing.T) {
	_, sizeErr := Size(t.TempDir() + "/missing")
	_, _, tarErr := TarGetComposerJSON(tarFixtures + "nojson.tar.gz")
	t.Setenv("MAESTRO_TEST_BOOL_ENV", "maybe")
	_, _, boolErr := GetBoolEnv("MAESTRO_TEST_BOOL_ENV")
	_, shortestErr := FindShortestPath("a", "/b", false, false)

	cases := []struct {
		name string
		err  error
		file string
		line int
	}{
		{"Filesystem::size", sizeErr, "Filesystem.php", 589},
		{"Tar::getComposerJson", tarErr, "Tar.php", 60},
		{"Platform::getBoolEnv", boolErr, "Platform.php", 106},
		{"Filesystem::findShortestPath", shortestErr, "Filesystem.php", 475},
	}
	for _, c := range cases {
		if !phperr.Is(c.err, c.file, c.line) {
			site, _ := phperr.SiteOf(c.err)
			t.Errorf("%s: got %v at %s:%d, want %s:%d", c.name, c.err, site.File, site.Line, c.file, c.line)
		}
	}
}
