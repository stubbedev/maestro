package util

import (
	"testing"
)

// The messages of a few exceptions, as Composer 2.10.3 words them (where
// they were thrown is free, docs/PORTING.md "The contract").
func TestErrorMessages(t *testing.T) {
	missing := t.TempDir() + "/missing"
	_, sizeErr := Size(missing)
	_, _, tarErr := TarGetComposerJSON(tarFixtures + "nojson.tar.gz")
	t.Setenv("MAESTRO_TEST_BOOL_ENV", "maybe")
	_, _, boolErr := GetBoolEnv("MAESTRO_TEST_BOOL_ENV")
	_, shortestErr := FindShortestPath("a", "/b", false, false)

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"Filesystem::size", sizeErr, missing + " does not exist."},
		{"Tar::getComposerJson", tarErr, "No composer.json found either at the top level or within the topmost directory"},
		{"Platform::getBoolEnv", boolErr, "Invalid value for MAESTRO_TEST_BOOL_ENV: maybe. Expected 0, 1, false, true, off, or on."},
		{"Filesystem::findShortestPath", shortestErr, "$from (a) and $to (/b) must be absolute paths."},
	}
	for _, c := range cases {
		if c.err == nil || c.err.Error() != c.want {
			t.Errorf("%s: %v, want %q", c.name, c.err, c.want)
		}
	}
}
