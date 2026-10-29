// Tests console's copy of ConsoleIO::ensureValidUtf8(), which moved here
// with ConsoleIO::sanitize() (src/Composer/IO/ConsoleIO.php, Composer).

package console

import "testing"

// mb_convert_encoding($s, 'UTF-8', 'UTF-8') results from PHP 8.4.
func TestEnsureValidUTF8(t *testing.T) {
	for in, want := range map[string]string{
		"\xE4\xB8A":        "?A",
		"\xE4\xB8":         "?",
		"\xC3\x28":         "?(",
		"\xF0\x90\x80":     "?",
		"\xED\xA0\x80":     "???",
		"\xC0\x80":         "??",
		"\xF5\x80":         "??",
		"\x80\x80":         "??",
		"\xE0\x80\x80":     "???",
		"\xF4\x90\x80\x80": "????",
	} {
		if got := ensureValidUTF8(in); got != want {
			t.Errorf("ensureValidUTF8(%q) = %q, want %q", in, got, want)
		}
	}
}
