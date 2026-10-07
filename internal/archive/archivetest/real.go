package archivetest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/switches"
)

// Result is what a real extractor left behind.
type Result struct {
	// Tree is the package tree after ArchiveDownloader's single-directory
	// rule; nil when the extractor failed or the rule refused.
	Tree Tree
	// Output is the extractor's combined output.
	Output string
	// Exit is the extractor's exit status.
	Exit int
}

// OK reports whether the extraction succeeded cleanly, as Composer requires.
func (r Result) OK() bool {
	return r.Exit == 0 && r.Tree != nil
}

// Need skips the test when a tool is not on PATH.
func Need(t testing.TB, tools ...string) {
	t.Helper()

	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found on PATH", tool)
		}
	}
}

// Umasks are the umasks the differential tests run under.
var Umasks = []int{0o022, 0o002}

// run makes Composer's temporary directory (mkdir under the umask, as
// ensureDirectoryExists does), runs script in sh with $1 set to it and the
// given arguments after, and snapshots the package tree.
func run(t testing.TB, umask int, env []string, script string, args ...string) Result {
	t.Helper()

	work := t.TempDir()
	dir := filepath.Join(work, "x")

	argv := append([]string{"-c", "umask " + strconv.FormatInt(int64(umask), 8) + ` && mkdir "$1" && ` + script, "sh", dir}, args...)
	cmd := exec.Command("sh", argv...) //nolint:gosec // the tests' own scripts.
	cmd.Env = append(os.Environ(), env...)

	var out bytes.Buffer

	cmd.Stdout, cmd.Stderr = &out, &out

	r := Result{}

	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatal(err)
		}

		r.Exit = ee.ExitCode()
	}

	r.Output = out.String()

	t.Cleanup(func() { _ = RemoveAll(work) })

	if r.Exit != 0 {
		return r
	}

	root, err := PackageRoot(dir)
	if err != nil {
		r.Output += "\n" + err.Error()
		return r
	}

	if r.Tree, err = Snapshot(root); err != nil {
		t.Fatal(err)
	}

	return r
}

// Unzip runs ZipDownloader's `unzip -qq <file> -d <dir>` with the given
// binary, umask and LC_ALL.
func Unzip(t testing.TB, unzip, file string, umask int, lcAll string) Result {
	t.Helper()

	return run(t, umask, []string{"LC_ALL=" + lcAll}, `exec "$2" -qq "$3" -d "$1"`, unzip, file)
}

// PharData runs TarDownloader's extraction. file must carry a tar
// extension, which PharData insists on. It runs with the memory limit
// bin/composer raises PHP's to (1536M): with php.ini's default of 128M,
// PharData runs out of memory on large tarballs such as phpstan's.
func PharData(t testing.TB, file string, umask int) Result {
	t.Helper()

	const php = `$p = new PharData($argv[1]); $p->extractTo($argv[2], null, true);`

	return run(t, umask, nil, `exec php -d memory_limit=1536M -r '`+php+`' "$2" "$1"`, file)
}

// TarXz runs XzDownloader's `tar -xJf <file> -C <dir>` with the given tar
// binary (GNUTar's).
func TarXz(t testing.TB, tar, file string, umask int) Result {
	t.Helper()

	return run(t, umask, nil, `exec "$2" -xJf "$3" -C "$1"`, tar, file)
}

// Gunzip runs GzipDownloader's `gzip -cd -- <file> > <dir>/<name>`.
func Gunzip(t testing.TB, file, name string, umask int) Result {
	t.Helper()

	return run(t, umask, nil, `gzip -cd -- "$2" > "$1/$3"`, file, name)
}

// infoZip finds the reference unzip once; see InfoZip.
var infoZip = sync.OnceValues(func() (string, string) {
	unzip := os.Getenv(switches.TestUnzip)
	if unzip == "" {
		unzip = "unzip"
	}

	path, err := exec.LookPath(unzip)
	if err != nil {
		return "", unzip + " not found on PATH"
	}

	out, _ := exec.Command(path, "-v").Output() //nolint:gosec // the test's own binary.
	v := string(out)

	switch {
	case !strings.HasPrefix(v, "UnZip 6.00 ") || !strings.Contains(v, "Info-ZIP"):
		first, _, _ := strings.Cut(v, "\n")
		return "", fmt.Sprintf("%s is not Info-ZIP UnZip 6.00 (%q)", path, first)
	case !strings.Contains(v, "UNICODE_SUPPORT"):
		return "", path + " is Info-ZIP UnZip 6.00 built without UNICODE_SUPPORT (macOS's /usr/bin/unzip is)"
	}

	return path, ""
})

// InfoZip returns the unzip the zip tests compare against: the extractor
// maestro's zip support reproduces, Info-ZIP UnZip 6.00 built with
// UNICODE_SUPPORT as Linux distributions ship it ($MAESTRO_TEST_UNZIP, else
// unzip on PATH). When that binary is something else (macOS's unzip has no
// UNICODE_SUPPORT and ignores Info-ZIP Unicode Path fields), it returns ""
// and why.
func InfoZip() (path, reason string) {
	return infoZip()
}

// NeedInfoZip returns InfoZip's binary, or skips the test.
func NeedInfoZip(t testing.TB) string {
	t.Helper()

	path, reason := InfoZip()
	if path == "" {
		t.Skipf("the zip reference extractor is unavailable: %s; set MAESTRO_TEST_UNZIP to an Info-ZIP UnZip 6.00 built with UNICODE_SUPPORT", reason)
	}

	return path
}

// nonUTF8Names probes once whether the temporary directory's file system
// stores names that are not valid UTF-8.
var nonUTF8Names = sync.OnceValue(func() bool {
	dir, err := os.MkdirTemp("", "utf8-probe")
	if err != nil {
		return true
	}

	defer func() { _ = os.RemoveAll(dir) }()

	return os.WriteFile(filepath.Join(dir, "caf\xe9"), nil, 0o600) == nil
})

// Unstorable reports whether tree holds a name the temporary directory's
// file system refuses: APFS rejects names that are not valid UTF-8
// (EILSEQ), so no extractor can produce such a tree there.
func Unstorable(tree Tree) bool {
	if nonUTF8Names() {
		return false
	}

	for path := range tree {
		if !utf8.ValidString(path) {
			return true
		}
	}

	return false
}

// gnuTar finds the reference tar once; see GNUTar.
var gnuTar = sync.OnceValues(func() (string, string) {
	candidates := []string{"tar", "gtar"}
	if tar := os.Getenv(switches.TestTar); tar != "" {
		candidates = []string{tar}
	}

	var reasons []string

	for _, tar := range candidates {
		path, err := exec.LookPath(tar)
		if err != nil {
			reasons = append(reasons, tar+" not found on PATH")
			continue
		}

		out, _ := exec.Command(path, "--version").Output() //nolint:gosec // the test's own binary.
		first, _, _ := strings.Cut(string(out), "\n")

		if strings.Contains(first, "GNU tar") {
			return path, ""
		}

		reasons = append(reasons, fmt.Sprintf("%s is not GNU tar (%q)", path, first))
	}

	return "", strings.Join(reasons, "; ")
})

// GNUTar returns the tar the tar.xz tests compare against: GNU tar, which
// XzDownloader's `tar -xJf` is on Linux and which maestro reproduces
// ($MAESTRO_TEST_TAR, else tar or gtar on PATH). macOS's tar is bsdtar,
// which extracts differently (directory modes among others); then it
// returns "" and why.
func GNUTar() (path, reason string) {
	return gnuTar()
}

// NeedGNUTar returns GNUTar's binary, or skips the test.
func NeedGNUTar(t testing.TB) string {
	t.Helper()

	path, reason := GNUTar()
	if path == "" {
		t.Skipf("the tar.xz reference extractor is unavailable: %s; set MAESTRO_TEST_TAR to GNU tar", reason)
	}

	return path
}

// UnstorableArchive is Unstorable for the entries maestro would extract
// from the archive at path (in the default locale); an archive maestro
// refuses is storable as far as this goes.
func UnstorableArchive(path string, format archive.Format) bool {
	if nonUTF8Names() {
		return false
	}

	a, err := archive.Open(path, format, nil)
	if err != nil {
		return false
	}

	defer func() { _ = a.Close() }()

	for _, e := range a.Entries() {
		if !utf8.ValidString(e.Path) {
			return true
		}
	}

	return false
}
