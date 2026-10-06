package classmap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// PhpFileParser::findClasses appends error_get_last()'s message after
// PHP_EOL, "\r\n" on Windows.
func TestHelpful_UsesWindowsEOL(t *testing.T) {
	php.SetEOLForTest(t, "\r\n")

	path := filepath.Join(t.TempDir(), "missing.php")
	_, err := os.Open(path)
	got := helpful("file_get_contents", path, err)
	want := "\r\nThe following message may be helpful:\r\nfile_get_contents(" + path + "): Failed to open stream: "
	if !strings.HasPrefix(got, want) || strings.Count(got, "\n") != 2 {
		t.Errorf("got %q, want the prefix %q", got, want)
	}
}
