package upstream

import (
	"regexp"
	"testing"
)

// TestUpstream_Constants: each constant has the form its readers (and
// tools/upstream/bump.sh) expect.
func TestUpstream_Constants(t *testing.T) {
	version := `^\d+\.\d+\.\d+(-(alpha|beta|RC)\d+)?$`
	for _, c := range []struct{ name, value, pattern string }{
		{"ComposerVersion", ComposerVersion, version},
		{"ComposerReleaseDate", ComposerReleaseDate, `^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$`},
		{"ComposerPharSHA256", ComposerPharSHA256, `^[0-9a-f]{64}$`},
		{"PluginAPIVersion", PluginAPIVersion, version},
		{"RuntimeAPIVersion", RuntimeAPIVersion, version},
	} {
		if !regexp.MustCompile(c.pattern).MatchString(c.value) {
			t.Errorf("%s = %q, want the form %s", c.name, c.value, c.pattern)
		}
	}
}
