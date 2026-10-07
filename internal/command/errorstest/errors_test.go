// Package errorstest checks maestro on the scenarios of
// internal/command/testdata/errors against the reference Composer's runs,
// recorded by tools/oracle/errors/errors.sh -w, at default, -v, -vv and
// -vvv verbosity. It checks what docs/PORTING.md's contract freezes and
// leaves how errors are rendered free:
//
//   - the exit code, exactly;
//   - stdout, exactly (<verbosity>.stdout, empty when absent): it is
//     frozen, and error output must not land there;
//   - the error's information, on stderr: the messages of Composer's
//     exception and its previous ones, read from the error boxes of the
//     recorded output (<verbosity>.txt), or, where the scenario has a
//     messages file, its lines (for errors that are no exception, such as
//     solver problems and failed scripts, or where only part of a message
//     carries the information). Each listed line must occur in Composer's
//     recorded output too; one Composer wrote on stdout is checked by the
//     comparison of stdout. Messages are compared without whitespace (box
//     padding and wrapping, indentation and line breaks don't matter),
//     box-drawing characters and PHP's TypeError call site
//     (", called in X on line N"). Where Composer failed writing nothing
//     at all, stderr must be empty;
//   - where the scenario has a files list, the content those files of the
//     working directory have after the run (after/<path>, absent when
//     Composer's run left no such file).
//
// The box, the exception class, the "In File.php line N:" heading, the
// "Exception trace:" stack, the command synopsis and the rest of stderr
// (warnings, progress, debug output) are not compared.
//
// Each run is a child process (the test binary itself, which TestMain
// turns into cmd/maestro's main when ERRORSTEST_CHILD is set): Composer's
// per-process state (the CA bundle and version guesses, the platform probe)
// must start fresh as it does for every invocation. The child gets only
// PATH and the scenario's variables, like the oracle's `env -i`. Composer's
// startup probes php, so the test needs php on the PATH (the devenv shell
// has it) and is skipped without it and in -short mode.
package errorstest

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/testutil"
)

const dataDir = "../testdata/errors"

// verbosities are the golden file names and the flag each adds.
var verbosities = []struct{ name, flag string }{
	{"default", ""}, {"v", "-v"}, {"vv", "-vv"}, {"vvv", "-vvv"},
}

const childEnv = "ERRORSTEST_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(childEnv) == "1" {
		os.Exit(childMain())
	}
	os.Exit(m.Run())
}

// childMain is cmd/maestro's main without the plugin runtime: the
// Application on a new Runtime, run on the process arguments.
func childMain() int {
	app := command.NewApplication(&composer.Factory{Runtime: composer.NewRuntime("", nil)})
	app.SetAutoExit(false)
	code, err := app.Run(nil, nil)
	if err != nil {
		return 1
	}

	return min(code, 255)
}

func TestErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("runs every scenario's Composer startup; skipped in -short mode")
	}
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("needs php on the PATH (Composer's startup probes it)")
	}
	if runtime.GOOS != "linux" {
		// Composer's output depends on the system: the CA bundle locations
		// it probes, /tmp being a symlink (macOS), the shell's messages.
		t.Skip("the goldens record Composer on Linux (tools/oracle/errors/errors.sh)")
	}

	server := httptest.NewServer(routerHandler(filepath.Join(dataDir, "_server")))
	// Cleanup, not defer: the parallel subtests run after this returns.
	t.Cleanup(server.Close)

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		name := e.Name()
		dir := filepath.Join(dataDir, name)
		var listed []string
		if _, err := os.Stat(filepath.Join(dir, "messages")); err == nil {
			listed = readLines(t, filepath.Join(dir, "messages"))
		}
		if _, err := os.Stat(filepath.Join(dir, "readonly")); err == nil && os.Geteuid() == 0 {
			// root writes read-only files.
			t.Logf("%s: skipped, it makes files read-only and runs as root", name)

			continue
		}
		for _, v := range verbosities {
			golden := filepath.Join(dir, v.name)
			recorded, err := os.ReadFile(golden + ".txt")
			if err != nil {
				continue
			}
			want, err := readGolden(string(recorded), listed)
			if err != nil {
				t.Fatalf("%s.txt: %v", golden, err)
			}
			if want.code != 0 && len(want.messages) == 0 && !want.silent {
				t.Fatalf("%s.txt: Composer failed without an error box; list the lines that report the error in %s",
					golden, filepath.Join(dir, "messages"))
			}
			if stdout, err := os.ReadFile(golden + ".stdout"); err == nil {
				want.stdout = string(stdout)
			}
			t.Run(name+"/"+v.name, func(t *testing.T) {
				// Every run is a child process with directories of its own.
				t.Parallel()
				got := runScenario(t, dir, v.flag, server.URL)
				compare(t, golden, want, got)
				compareFiles(t, dir, got)
			})
		}
	}
}

// expected is what a run must give: Composer's exit code and stdout, and
// the messages its error reports (see testutil.CompactMessage).
type expected struct {
	code     int
	stdout   string
	messages []string
	// silent: Composer wrote nothing at all (a failure it reports by its
	// exit code alone, such as _complete's), so maestro's stderr must be
	// empty too.
	silent bool
}

// result is a normalised run of maestro: its exit code, stdout, and
// stderr compacted (see testutil.CompactMessage).
type result struct {
	code   int
	stdout string
	stderr string
	// files are the scenario's listed files after the run, normalised,
	// nil for those that don't exist.
	files map[string]*string
}

// compareFiles checks the scenario's listed files against the content
// Composer's run left (after/<path>).
func compareFiles(t *testing.T, dir string, got result) {
	t.Helper()
	for path, content := range got.files {
		want, err := os.ReadFile(filepath.Join(dir, "after", filepath.FromSlash(path)))
		switch {
		case errors.Is(err, os.ErrNotExist) && content != nil:
			t.Errorf("%s exists after the run, Composer's run leaves none", path)
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			t.Fatal(err)
		case content == nil:
			t.Errorf("%s missing after the run, Composer's run leaves it (%s)", path, filepath.Join(dir, "after", path))
		case *content != string(want):
			t.Errorf("%s after the run differs from Composer's (%s):\n%s", path, filepath.Join(dir, "after", path), lineDiff(string(want), *content))
		}
	}
}

func compare(t *testing.T, golden string, want expected, got result) {
	t.Helper()
	if got.code != want.code {
		t.Errorf("exit code %d, Composer's %d (%s.txt)", got.code, want.code, golden)
	}
	if got.stdout != want.stdout {
		t.Errorf("stdout differs from Composer's (%s.stdout):\n%s", golden, lineDiff(want.stdout, got.stdout))
	}
	if want.silent && got.stderr != "" {
		t.Errorf("stderr not empty, Composer wrote nothing (%s.txt): %q", golden, got.stderr)
	}
	stdout := testutil.CompactMessage(got.stdout)
	reported := testutil.CompactMessage(want.stdout)
	for _, m := range want.messages {
		switch {
		case strings.Contains(got.stderr, m):
		case strings.Contains(reported, m):
			// Composer reports it on stdout (diagnose's checks, show's
			// outdated packages), which is compared exactly.
		case strings.Contains(stdout, m):
			t.Errorf("error message on stdout, not stderr (%s.txt): %q", golden, m)
		default:
			t.Errorf("error message missing from stderr (%s.txt): %q\nstderr, compacted: %q", golden, m, got.stderr)
		}
	}
}

// exitLine is the oracle's last line: "exit N" after the output (on the
// output's last line when that has no newline).
var exitLine = regexp.MustCompile(`exit ([0-9]+)\n$`)

// readGolden reads a recorded run (stdout and stderr together, then
// "exit N"): its exit code, and the messages its error reports, which are
// the listed ones (the scenario's messages file; each must occur in the
// recorded output) or else those of its exception boxes.
func readGolden(recorded string, listed []string) (expected, error) {
	m := exitLine.FindStringSubmatchIndex(recorded)
	if m == nil {
		return expected{}, errors.New(`no "exit N" line`)
	}
	code, _ := strconv.Atoi(recorded[m[2]:m[3]])
	output := recorded[:m[0]]
	want := expected{code: code, silent: output == ""}
	if listed == nil {
		want.messages, _ = testutil.ErrorRendering(output)
		for i, m := range want.messages {
			want.messages[i] = normalizeCompact(m)
		}

		return want, nil
	}
	all := normalizeCompact(testutil.CompactMessage(output))
	for _, l := range listed {
		c := normalizeCompact(testutil.CompactMessage(l))
		if !strings.Contains(all, c) {
			return expected{}, fmt.Errorf("listed message not in Composer's output: %q", l)
		}
		want.messages = append(want.messages, c)
	}

	return want, nil
}

// runScenario runs the scenario as errors.sh does.
func runScenario(t *testing.T, dir, flag, serverURL string) result {
	t.Helper()
	work := t.TempDir()
	run := filepath.Join(work, "run")
	for _, d := range []string{"home", "cache", "p"} {
		if err := os.MkdirAll(filepath.Join(run, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "base")); err == nil {
		for _, l := range readLines(t, filepath.Join(dir, "base")) {
			copyTree(t, filepath.Join(dataDir, "_projects", l), filepath.Join(run, "p"), serverURL)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "project")); err == nil {
		copyTree(t, filepath.Join(dir, "project"), filepath.Join(run, "p"), serverURL)
	}
	if _, err := os.Stat(filepath.Join(dir, "home")); err == nil {
		copyTree(t, filepath.Join(dir, "home"), filepath.Join(run, "home"), serverURL)
	}
	if _, err := os.Stat(filepath.Join(dir, "readonly")); err == nil {
		for _, l := range readLines(t, filepath.Join(dir, "readonly")) {
			path := filepath.Join(run, "p", filepath.FromSlash(l))
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, info.Mode().Perm()&^0o222); err != nil {
				t.Fatal(err)
			}
		}
	}

	args := readLines(t, filepath.Join(dir, "args"))
	if flag != "" {
		args = append(args, flag)
	}
	args = append(args, "--no-ansi")

	env := []string{
		childEnv + "=1",
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + filepath.Join(run, "home"),
		"COMPOSER_HOME=" + filepath.Join(run, "home"),
		"COMPOSER_CACHE_DIR=" + filepath.Join(run, "cache"),
		"COMPOSER_NO_INTERACTION=1",
		"NO_COLOR=1",
		"COLUMNS=80",
		// Composer's only use of it: Cache::gcIsNecessary never collects,
		// where it otherwise does in one run of 51 and creates the files
		// cache directory on the way (see errors.sh).
		"COMPOSER_TEST_SUITE=1",
	}
	if _, err := os.Stat(filepath.Join(dir, "env")); err == nil {
		for _, l := range readLines(t, filepath.Join(dir, "env")) {
			env = append(env, strings.ReplaceAll(l, "@SERVER@", serverURL))
		}
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = filepath.Join(run, "p")
	cmd.Env = env
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			t.Fatalf("run: %v", err)
		}
		code = ee.ExitCode()
	}

	host := strings.TrimPrefix(serverURL, "http://")
	oracle := testutil.OracleRun{Dir: run, Server: host}

	var files map[string]*string
	if _, err := os.Stat(filepath.Join(dir, "files")); err == nil {
		files = map[string]*string{}
		for _, l := range readLines(t, filepath.Join(dir, "files")) {
			data, err := os.ReadFile(filepath.Join(run, "p", filepath.FromSlash(l)))
			switch {
			case errors.Is(err, os.ErrNotExist):
				files[l] = nil
			case err != nil:
				t.Fatal(err)
			default:
				content := testutil.NormalizeOracle(string(data), oracle)
				files[l] = &content
			}
		}
	}

	// The goldens record Composer on Linux, where PHP_EOL is "\n". stderr
	// is compacted before its paths are replaced: an error box may wrap a
	// path anywhere.
	return result{
		code:   code,
		stdout: testutil.NormalizeOracle(php.NormalizeEOL(stdout.String()), oracle),
		stderr: replacePaths(testutil.CompactMessage(stderr.String()), run, host),
		files:  files,
	}
}

// replacePaths replaces the run's directory and the server's address
// with the goldens' placeholders, and so php's version in a compacted
// solver problem.
func replacePaths(s, run, host string) string {
	s = strings.ReplaceAll(s, run, "@DIR@")
	s = strings.ReplaceAll(s, host, "@SERVER@")

	return normalizeCompact(compactPHPVersion.ReplaceAllString(s, "${1}@PHPVERSION@${2}"))
}

// compactRandom are what differs between runs in a message that an error
// box may wrap anywhere, out of reach of testutil.NormalizeOracle, which
// matches lines: the server's port and the random name of the temporary
// file a dist is downloaded to.
var compactRandom = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`127\.0\.0\.1:[0-9]+`), "@SERVER@"},
	{regexp.MustCompile(`/tmp-[0-9a-f]{32}\.`), "/tmp-@RAND@."},
}

// normalizeCompact replaces compactRandom in a compacted message.
func normalizeCompact(s string) string {
	for _, r := range compactRandom {
		s = r.re.ReplaceAllString(s, r.repl)
	}

	return s
}

var compactPHPVersion = regexp.MustCompile(`(butyourphpversion\()[^)]*(\)doesnotsatisfy)`)

// routerHandler ports tools/oracle/errors/router.php.
func routerHandler(root string) http.Handler {
	status := regexp.MustCompile(`^/status/(\d{3})(/|$)`)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m := status.FindStringSubmatch(r.URL.Path); m != nil {
			code, _ := strconv.Atoi(m[1])
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(code)
			_, _ = fmt.Fprintf(w, "status %s\n", m[1])

			return
		}
		file := filepath.Join(root, filepath.FromSlash(r.URL.Path))
		if fi, err := os.Stat(file); err == nil && fi.Mode().IsRegular() {
			if strings.HasSuffix(file, ".json") {
				w.Header().Set("Content-Type", "application/json")
			} else {
				w.Header().Set("Content-Type", "application/octet-stream")
			}
			data, _ := os.ReadFile(file)
			_, _ = w.Write(data)

			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found\n"))
	})
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for l := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n"), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}

	return lines
}

// copyTree copies src into dst, keeping file modes (cp -a), with
// @SERVER@ in the files replaced by serverURL.
func copyTree(t *testing.T, src, dst, serverURL string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		data = bytes.ReplaceAll(data, []byte("@SERVER@"), []byte(serverURL))

		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

// lineDiff shows the first differing lines of want and got.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	shown := 0
	for i := 0; i < max(len(w), len(g)) && shown < 12; i++ {
		var wl, gl string
		if i < len(w) {
			wl = w[i]
		}
		if i < len(g) {
			gl = g[i]
		}
		if wl != gl {
			fmt.Fprintf(&b, "line %d:\n  composer: %q\n  maestro:  %q\n", i+1, wl, gl)
			shown++
		}
	}

	return b.String()
}
