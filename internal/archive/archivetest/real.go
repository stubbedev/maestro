package archivetest

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
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
// extension, which PharData insists on.
func PharData(t testing.TB, file string, umask int) Result {
	t.Helper()

	const php = `$p = new PharData($argv[1]); $p->extractTo($argv[2], null, true);`

	return run(t, umask, nil, `exec php -r '`+php+`' "$2" "$1"`, file)
}

// TarXz runs XzDownloader's `tar -xJf <file> -C <dir>`.
func TarXz(t testing.TB, file string, umask int) Result {
	t.Helper()

	return run(t, umask, nil, `exec tar -xJf "$2" -C "$1"`, file)
}

// Gunzip runs GzipDownloader's `gzip -cd -- <file> > <dir>/<name>`.
func Gunzip(t testing.TB, file, name string, umask int) Result {
	t.Helper()

	return run(t, umask, nil, `gzip -cd -- "$2" > "$1/$3"`, file, name)
}
