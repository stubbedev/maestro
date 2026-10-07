// The end-to-end comparison with real Composer (docs/PORTING.md, Tests §3):
// every scenario (e2e_scenarios_test.go) runs once with the official
// composer.phar 2.10.3 and once with maestro, from a cold cache and store
// and again from the warm ones the cold run left, and each step must give
// the same exit code, stdout, stderr and files, except how errors are
// rendered (free, docs/PORTING.md "The contract").
//
// Both tools run a scenario in the same directory (one after the other; the
// first run's directory is renamed away), with the same environment apart
// from separate COMPOSER_HOME/COMPOSER_CACHE_DIR/MAESTRO_CACHE_DIR
// directories that also sit at the same paths, so absolute paths need no
// normalisation. COMPOSER_TEST_SUITE=1 is set for both: it is the only
// switch Composer has for its cache garbage collection (Cache::gcIsNecessary,
// otherwise a 1 in 51 draw per run, which removes cached files and prints
// "Running cache garbage collection" at -vv), and it has no other effect.
// What is compared after every step:
//
//   - the exit code;
//   - stdout and stderr, after the normalisations below; where Composer
//     rendered an exception on stderr, its rendering is compared by the
//     messages it reports, and its deprecation notices by their texts
//     (compareStderr);
//   - every file under the scenario root except the two caches: the project
//     (composer.json, composer.lock byte for byte, the whole vendor/ tree,
//     files scripts wrote), COMPOSER_HOME and any other directory a step
//     wrote (create-project targets, repositories). Paths, types, permission
//     bits, symlink targets and file contents are compared; mtimes are not
//     (deviation 1).
//
// Normalisations (the only ones):
//
//   - Progress bars: Composer redraws a non-decorated progress bar at most
//     every 100 ms (Loop::wait), so the intermediate "n/N [===>   ]" lines
//     depend on timing, and so does N when processes are involved: a
//     process (rm -rf, unzip) that already exited when Loop::wait first
//     counts the active jobs is not counted, and when none is left the bar
//     has no maximum ("    0 [>---]    0 [->--]"). A run of progress bar
//     lines of either form (with the elapsed time at -v) becomes one
//     "<progress bar>" line.
//   - Git working copies (any .git directory, from --prefer-source and vcs
//     downloads): their index, logs, packed refs and object store hold
//     timestamps and pack layouts, so a .git directory is compared as the
//     checked-out commit, the current branch and the remotes' URLs.
//   - The name of a dist's temporary file in messages
//     (vendor/composer/tmp-<md5 including spl_object_hash()>).
//   - Durations printed at -vv ("... completed in 0.003 seconds").
//   - The "maestro version X" line `--version` adds on stderr (maestro's
//     own build, cmd/maestro/main.go).
//   - The banner of `list` and a bare run (Composer's logo and long
//     version, maestro's logo and version; docs/PORTING.md deviation 8),
//     at the start of the output only: the command list and options after
//     it are compared.
//   - The random APCu prefix in vendor/composer/autoload_real.php
//     (dump-autoload --apcu without --apcu-prefix: bin2hex(random_bytes(10))).
//   - Scenario-specific ones, each documented at its step (normalize):
//     fund's package order (normalizeFund) and diagnose's phar-only checks
//     and binary path (normalizeDiagnose).
//   - An empty vendor/bin that Composer leaves behind and maestro doesn't
//     create (emptyBinDirs; deviation 7 in docs/PORTING.md).
//
// It needs php, git, unzip and the network, so it only runs with
// MAESTRO_E2E=1. Knobs: MAESTRO_E2E_BIN (a prebuilt maestro instead of
// building this package), MAESTRO_E2E_KEEP=<dir> (keep each scenario's
// directories there), MAESTRO_E2E_REPORT=<file> (write the speed table as
// Markdown), MAESTRO_E2E_PRIVATE_APP (a private application's checkout;
// its scenario is skipped when unset), MAESTRO_E2E_WARM=0
// (skip the warm runs). COMPOSER_AUTH, when set, is passed to both tools
// (GitHub tokens for the real-world projects' rate limits).
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/testutil"
)

// The official composer.phar 2.10.3 and the sha256 getcomposer.org
// publishes for it (https://getcomposer.org/download/2.10.3/composer.phar.sha256sum).
const (
	composerPharURL    = "https://getcomposer.org/download/2.10.3/composer.phar"
	composerPharSHA256 = "7a2d379d5b8ffdaa028580ef26494c36d2feef4b178d3dd1473a4dbc5e17c8d6"
)

// step is one command run in a scenario.
type step struct {
	args []string
	// dir is the working directory relative to the scenario root
	// (default "project").
	dir string
	// env is added to the environment.
	env []string
	// stdin is fed to the process (nothing by default).
	stdin string
	// setup runs before the step in both tools' runs (edits a file, adds a
	// commit to a repository, ...).
	setup func(t *testing.T, root string)
	// normalize is applied to stdout and stderr of both tools on top of
	// the global normalisations; each use says why.
	normalize func(string) string
	// normalizeTree adjusts Composer's and maestro's file trees before
	// they are compared; each use says why.
	normalizeTree func(composer, maestro map[string]entry)
	// coldOnly restricts the step to the cold phase.
	coldOnly bool
	// mustSucceed marks a step the later ones build on (a real-world
	// project's create-project and install): Composer failing it means the
	// environment cannot run the scenario (a missing PHP extension, the
	// network), which is reported as such instead of as the differences
	// the later steps then show.
	mustSucceed bool
}

// scenario is a fixture project and the steps run in it.
type scenario struct {
	name string
	// fixture is a directory under testdata/e2e copied to <root>/project
	// ("" = an empty project directory). "@ROOT@" in its files is replaced
	// by the scenario root.
	fixture string
	// setup prepares the scenario root (repositories, archives, a fixture
	// built at run time) after the fixture was copied.
	setup func(t *testing.T, root string)
	steps []step
	// skip returns a reason to skip the scenario ("" = run it).
	skip func() string
}

// stepResult is what a step produced.
type stepResult struct {
	code   int
	stdout string
	stderr string
	tree   map[string]entry
	dur    time.Duration
}

// timing is a scenario's wall time per tool and phase.
type timing struct {
	scenario string
	times    map[string]time.Duration // "composer cold", "maestro warm", ...
}

func TestE2E(t *testing.T) {
	if os.Getenv("MAESTRO_E2E") == "" {
		t.Skip("set MAESTRO_E2E=1 to compare maestro with Composer 2.10.3 (php, git, unzip and the network)")
	}

	requireTools(t)

	phar := composerPhar(t)
	maestro := os.Getenv("MAESTRO_E2E_BIN")
	if maestro == "" {
		maestro = buildMaestro(t, t.TempDir())
	}

	base := t.TempDir()
	if keep := os.Getenv("MAESTRO_E2E_KEEP"); keep != "" {
		base = keep
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	tools := map[string][]string{
		"composer": {"php", phar},
		"maestro":  {maestro},
	}

	phases := []string{"cold", "warm"}
	if os.Getenv("MAESTRO_E2E_WARM") == "0" {
		phases = phases[:1]
	}

	var timings []timing

	for _, sc := range scenarios() {
		t.Run(sc.name, func(t *testing.T) {
			if sc.skip != nil {
				if reason := sc.skip(); reason != "" {
					t.Skip(reason)
				}
			}

			dir := filepath.Join(base, sc.name)
			if err := os.RemoveAll(dir); err != nil {
				t.Fatal(err)
			}

			tm := timing{scenario: sc.name, times: map[string]time.Duration{}}

			for _, phase := range phases {
				results := map[string][]stepResult{}

				for _, tool := range []string{"composer", "maestro"} {
					res, total := runScenario(t, sc, dir, tool, tools[tool], phase)
					results[tool] = res
					tm.times[tool+" "+phase] = total
				}

				compareResults(t, sc, phase, results["composer"], results["maestro"])
			}

			timings = append(timings, tm)
			t.Logf("%s: %s", sc.name, formatTimes(tm))
		})
	}

	report := speedReport(timings, phases)
	t.Log("\n" + report)

	if path := os.Getenv("MAESTRO_E2E_REPORT"); path != "" {
		if err := os.WriteFile(path, []byte(report), 0o644); err != nil {
			t.Error(err)
		}
	}
}

// requireTools fails the test when a tool the scenarios need is missing:
// php, git, and unzip, which Composer's ZipDownloader prefers to
// ZipArchive on Unix (on Windows it tries 7-Zip first and falls back to
// ZipArchive, so unzip is optional there).
func requireTools(t *testing.T) {
	t.Helper()

	tools := []string{"php", "git"}
	if runtime.GOOS != "windows" {
		tools = append(tools, "unzip")
	}

	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is required: %v", tool, err)
		}
	}
}

// composerPhar returns the checksum-verified composer.phar, downloading it
// into the user cache directory once.
func composerPhar(t *testing.T) string {
	t.Helper()

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}

	dir := filepath.Join(cacheDir, "maestro-e2e")
	path := filepath.Join(dir, "composer-2.10.3.phar")

	if sum, err := fileSHA256(path); err == nil && sum == composerPharSHA256 {
		return path
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(composerPharURL) //nolint:noctx // a test download
	if err != nil {
		t.Fatalf("downloading composer.phar: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("downloading composer.phar: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != composerPharSHA256 {
		t.Fatalf("composer.phar has sha256 %x, want %s", sum, composerPharSHA256)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:]), nil
}

func buildMaestro(t *testing.T, dir string) string {
	t.Helper()

	bin := filepath.Join(dir, "maestro"+exeSuffix)

	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building maestro: %v\n%s", err, out)
	}

	return bin
}

// runScenario runs every step of sc with one tool in <dir>/run and, with
// MAESTRO_E2E_KEEP, moves the result to <dir>/<tool>-<phase> (else it is
// removed once snapshotted). The cold phase starts with empty caches; the
// warm phase starts a fresh project with the caches (and the store) the
// cold phase left. dir is removed when the (sub)test t ends, unless
// MAESTRO_E2E_KEEP is set: real-world scenarios hold several vendor trees
// and caches, which must not pile up in the temporary directory.
func runScenario(t *testing.T, sc scenario, dir, tool string, cmd []string, phase string) ([]stepResult, time.Duration) {
	t.Helper()

	keep := os.Getenv("MAESTRO_E2E_KEEP") != ""
	if !keep {
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
	}

	root := filepath.Join(dir, "run")
	caches := filepath.Join(dir, tool+"-caches")

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}

	for _, d := range []string{"home", "userhome", "cache", "mcache"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if phase == "warm" {
		for _, d := range []string{"cache", "mcache"} {
			if err := os.RemoveAll(filepath.Join(root, d)); err != nil {
				t.Fatal(err)
			}

			if err := os.Rename(filepath.Join(caches, d), filepath.Join(root, d)); err != nil {
				t.Fatal(err)
			}
		}
	}

	project := filepath.Join(root, "project")
	if sc.fixture != "" {
		copyFixture(t, filepath.Join("testdata", "e2e", sc.fixture), project, root)
	} else if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}

	if sc.setup != nil {
		sc.setup(t, root)
	}

	env := e2eEnv(root)

	var (
		results []stepResult
		total   time.Duration
	)

	for _, s := range sc.steps {
		if s.coldOnly && phase != "cold" {
			continue
		}

		if s.setup != nil {
			s.setup(t, root)
		}

		r := runStep(t, root, env, cmd, s)
		total += r.dur
		r.tree = snapshot(t, root)
		results = append(results, r)
	}

	// keep the caches for the warm phase and the run for inspection
	if err := os.RemoveAll(caches); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(caches, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, d := range []string{"cache", "mcache"} {
		if err := os.Rename(filepath.Join(root, d), filepath.Join(caches, d)); err != nil {
			t.Fatal(err)
		}
	}

	if !keep {
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}

		return results, total
	}

	done := filepath.Join(dir, tool+"-"+phase)
	if err := os.RemoveAll(done); err != nil {
		t.Fatal(err)
	}

	if err := os.RemoveAll(done + ".log"); err != nil {
		t.Fatal(err)
	}

	if err := os.Rename(root, done); err != nil {
		t.Fatal(err)
	}

	// the outputs, for inspection next to the run
	logs := done + ".log"
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}

	for i, r := range results {
		name := filepath.Join(logs, fmt.Sprintf("step%d", i+1))
		_ = os.WriteFile(name+".stdout", []byte(r.stdout), 0o644)
		_ = os.WriteFile(name+".stderr", []byte(r.stderr), 0o644)
		_ = os.WriteFile(name+".code", []byte(fmt.Sprintln(r.code)), 0o644)
	}

	return results, total
}

// e2eEnv is the environment both tools run with: the caller's, without
// anything that configures Composer or maestro, plus per-run homes and
// caches.
func e2eEnv(root string) []string {
	var env []string

	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")

		switch {
		case name == "COMPOSER_AUTH":
		case strings.HasPrefix(name, "COMPOSER"), strings.HasPrefix(name, "MAESTRO"),
			strings.HasPrefix(name, "XDG_"), name == "HOME", name == "COLUMNS", name == "LINES",
			strings.HasPrefix(name, "GIT_"), name == "SHELL_VERBOSITY",
			// Git for Windows' bash sets MSYSTEM=MINGW64, which makes
			// StreamOutput::hasColorSupport() decorate pipes; the steps
			// compare plain output as on Unix (and --ansi where they ask).
			strings.EqualFold(name, "MSYSTEM"):
			continue
		}

		env = append(env, kv)
	}

	return append(env,
		"HOME="+filepath.Join(root, "userhome"),
		"COMPOSER_HOME="+filepath.Join(root, "home"),
		"COMPOSER_CACHE_DIR="+filepath.Join(root, "cache"),
		"MAESTRO_CACHE_DIR="+filepath.Join(root, "mcache"),
		"COMPOSER_NO_INTERACTION=1",
		"COMPOSER_TEST_SUITE=1",
		"COLUMNS=120",
		"GIT_CONFIG_NOSYSTEM=1",
	)
}

func runStep(t *testing.T, root string, env, cmd []string, s step) stepResult {
	t.Helper()

	dir := filepath.Join(root, "project")
	if s.dir != "" {
		dir = filepath.Join(root, s.dir)
	}

	args := make([]string, 0, len(cmd)+len(s.args))
	args = append(args, cmd[1:]...)

	for _, a := range s.args {
		args = append(args, strings.ReplaceAll(a, "@ROOT@", rootPath(root)))
	}

	c := exec.Command(cmd[0], args...)
	c.Dir = dir
	c.Env = append(slices.Clone(env), s.env...)
	c.Stdin = strings.NewReader(s.stdin)

	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr

	start := time.Now()
	err := c.Run()
	dur := time.Since(start)

	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError) //nolint:errorlint // exec returns it unwrapped
		if !ok {
			t.Fatalf("running %v: %v", args, err)
		}

		code = ee.ExitCode()
	}

	return stepResult{code: code, stdout: stdout.String(), stderr: stderr.String(), dur: dur}
}

// rootPath is what @ROOT@ stands for in fixtures and arguments: the
// scenario root with forward slashes, as composer.json files and JSON
// arguments can hold it (Windows' backslashes would be JSON escapes;
// Composer takes either separator there).
func rootPath(root string) string { return filepath.ToSlash(root) }

// copyFixture copies a fixture directory, replacing @ROOT@ in its files.
func copyFixture(t *testing.T, src, dst, root string) {
	t.Helper()

	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		data = bytes.ReplaceAll(data, []byte("@ROOT@"), []byte(rootPath(root)))

		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
}

// compareResults reports every difference between Composer's and
// maestro's steps.
func compareResults(t *testing.T, sc scenario, phase string, want, got []stepResult) {
	t.Helper()

	steps := make([]step, 0, len(sc.steps))
	for _, s := range sc.steps {
		if !s.coldOnly || phase == "cold" {
			steps = append(steps, s)
		}
	}

	for i, s := range steps {
		w, g := want[i], got[i]
		label := fmt.Sprintf("[%s] step %d `composer %s`", phase, i+1, strings.Join(s.args, " "))

		if s.mustSucceed && w.code != 0 {
			t.Errorf("%s: Composer itself failed (exit code %d), so the scenario cannot run here:\n%s", label, w.code, w.stderr)
		}

		if w.code != g.code {
			t.Errorf("%s: exit code %d, Composer's %d", label, g.code, w.code)
		}

		norm := func(s string) string { return s }
		if s.normalize != nil {
			norm = s.normalize
		}

		if d := textDiff(norm(normalizeOutput(w.stdout)), norm(normalizeOutput(g.stdout))); d != "" {
			t.Errorf("%s: stdout differs (- Composer, + maestro):\n%s", label, d)
		}

		compareStderr(t, label, norm(normalizeOutput(w.stderr)), norm(normalizeOutput(g.stderr)))

		emptyBinDirs(w.tree, g.tree)

		if s.normalizeTree != nil {
			s.normalizeTree(w.tree, g.tree)
		}

		if d := compareTrees(w.tree, g.tree); d != "" {
			t.Errorf("%s: files differ:\n%s", label, d)
		}
	}
}

// compareStderr compares a step's stderr. How errors are rendered is free
// (docs/PORTING.md "The contract", #13): where Composer rendered an
// exception, what it wrote before the rendering must start maestro's stderr
// as it is, and the rendering must report the messages of Composer's
// exception and its previous ones (testutil.ErrorRendering, compared
// without whitespace); the boxes, classes, "In File.php line N:" headings,
// "Exception trace:" stacks and the command synopsis after them are not
// compared. The hints Composer writes just before the rendering
// (composerHints) are part of maestro's error, as many "Hint: " lines in
// maestro's wording, which keep the links Composer's hints give.
// Deprecation notices are free as well: each one Composer
// printed (and its note that more were hidden) must be reported by a line
// of maestro's (testutil.ComposerNotices, RemoveNotices), in the same
// order; their locations and stack traces are not compared. Any other
// stderr is compared as it is.
func compareStderr(t *testing.T, label, want, got string) {
	t.Helper()

	notices, want := testutil.ComposerNotices(want)
	got, missing := testutil.RemoveNotices(got, notices)
	for _, n := range missing {
		t.Errorf("%s: stderr does not report Composer's deprecation notice %q", label, n)
	}

	messages, start := testutil.ErrorRendering(want)
	if len(messages) == 0 {
		if d := textDiff(want, got); d != "" {
			t.Errorf("%s: stderr differs (- Composer, + maestro):\n%s", label, d)
		}

		return
	}

	head, hints, links := composerHints(want[:start])
	if hints > 0 && strings.Count(got, "Hint: ") < hints {
		t.Errorf("%s: stderr does not report Composer's %d hint(s) about the error:\n%s", label, hints, got)
	}
	for _, link := range links {
		if !strings.Contains(got, link) {
			t.Errorf("%s: stderr does not give the link %s of Composer's hint about the error:\n%s", label, link, got)
		}
	}
	if !strings.HasPrefix(got, head) {
		gotHead := got
		if n := strings.Count(head, "\n"); n < strings.Count(got, "\n") {
			gotHead = got[:lineEnd(got, n)]
		}

		if d := textDiff(head, gotHead); d != "" {
			t.Errorf("%s: stderr before the error differs (- Composer, + maestro):\n%s", label, d)
		}
	}

	rest := testutil.CompactMessage(got[min(len(head), len(got)):])
	for _, m := range messages {
		if !strings.Contains(rest, m) {
			t.Errorf("%s: stderr does not report Composer's error %q:\n%s", label, m, got)
		}
	}
}

// composerHint matches a line of Application::hintCommonErrors (and
// HttpDownloader::getExceptionHints) that Composer writes before rendering
// an exception. maestro reports them with the error as hints ("Hint: "),
// in its own wording (#13).
var composerHint = regexp.MustCompile(`^(?:The following exception |The disk hosting |Check https://getcomposer\.org/|Plugins have been disabled|If you intend to run Composer without connecting to the internet)`)

// hintLink is a link in a hint of Composer's.
var hintLink = regexp.MustCompile(`https://\S+`)

// composerHints removes the hint lines that end what Composer wrote
// before an error's rendering, returning the rest, how many hints there
// were (a "Check ... for details" line counts with the hint it follows)
// and the links they give.
func composerHints(head string) (rest string, hints int, links []string) {
	lines := strings.SplitAfter(head, "\n")
	end := len(lines)
	if end > 0 && lines[end-1] == "" {
		end--
	}
	n := end
	for n > 0 && composerHint.MatchString(lines[n-1]) {
		if !strings.HasPrefix(lines[n-1], "Check ") {
			hints++
		}
		links = append(links, hintLink.FindAllString(lines[n-1], -1)...)
		n--
	}

	return strings.Join(lines[:n], ""), hints, links
}

// lineEnd is the byte offset just after the n-th "\n" of s.
func lineEnd(s string, n int) int {
	off := 0

	for range n {
		off += strings.IndexByte(s[off:], '\n') + 1
	}

	return off
}

// progressLine matches a progress bar line: "n/N [===>---] p%" or, for a
// bar without a maximum (no job was active when Loop::wait started), its
// redraws on one line ("    0 [>---]    0 [->--]").
// At -v the bar shows the elapsed time, and redraws while still at step 0
// stay on one line.
var progressLine = regexp.MustCompile(`^(?:(?: *\d+/\d+ \[[=>-]*\] +\d+%(?: +(?:< )?\d+ [a-z]+)?)+|(?: +\d+ \[[=>-]*\](?: +(?:< )?\d+ [a-z]+)?)+)$`)

// timings are the durations Composer prints at -vv ("Pool optimizer
// completed in 0.003 seconds", "Dependency resolution completed in ...").
var timings = regexp.MustCompile(`(completed in )\d+(?:\.\d+)? seconds`)

// tmpFile is FileDownloader's temporary dist file, vendor/composer/tmp-<md5
// of the package and spl_object_hash($package)>: the object hash varies
// between processes. Its hex digits become "x", also where an exception
// box wrapped the name (at a PHP_EOL, "\r\n" on Windows), so the layout is
// kept. Messages may name it with Windows' backslashes.
var tmpFile = regexp.MustCompile(`[/\\]tmp-[0-9a-f]+(?: *\r?\n +[0-9a-f]+)?`)

// maestroVersion is the line `maestro --version` adds on stderr after
// Composer's (cmd/maestro's doc): maestro names its own build there.
var maestroVersion = regexp.MustCompile(`(?m)^maestro version .*\n`)

// banner is the logo and version line heading `list`'s output: lines of
// ASCII art, then "<name> version ..." (with --ansi, styled).
var banner = regexp.MustCompile("\\A(?:[ _/\\\\|().,'`-]+\r?\n)+(?:\x1b\\[[0-9;]*m)*(?:Composer|maestro)(?:\x1b\\[[0-9;]*m)* version [^\n]*\n")

// normalizeOutput applies the global normalisations (see the file comment).
func normalizeOutput(s string) string {
	lines := strings.Split(s, "\n")
	out := lines[:0]

	inBar := false

	for _, line := range lines {
		// On Windows a line ends in PHP_EOL, "\r\n": the "\r" stays (it is
		// compared) but is not part of a progress bar line.
		if progressLine.MatchString(strings.TrimSuffix(line, "\r")) {
			if !inBar {
				out = append(out, "<progress bar>")
			}

			inBar = true

			continue
		}

		inBar = false

		out = append(out, line)
	}

	s = strings.Join(out, "\n")
	s = timings.ReplaceAllString(s, "${1}<time> seconds")
	s = tmpFile.ReplaceAllStringFunc(s, func(m string) string {
		return m[:len("/tmp-")] + regexp.MustCompile(`[0-9a-f]`).ReplaceAllString(m[len("/tmp-"):], "x")
	})

	s = banner.ReplaceAllString(s, "<banner>\n")

	return maestroVersion.ReplaceAllString(s, "")
}

// textDiff is a unified diff of two texts ("" when equal).
func textDiff(want, got string) string {
	if want == got {
		return ""
	}

	dir, err := os.MkdirTemp("", "e2e-diff")
	if err != nil {
		return err.Error()
	}
	defer os.RemoveAll(dir)

	a, b := filepath.Join(dir, "composer"), filepath.Join(dir, "maestro")
	_ = os.WriteFile(a, []byte(want), 0o644)
	_ = os.WriteFile(b, []byte(got), 0o644)

	out, err := exec.Command("diff", "-u", a, b).CombinedOutput()
	if _, notRun := err.(*exec.Error); notRun { //nolint:errorlint // exec returns it unwrapped
		// no diff (a Windows without Git's usr/bin on PATH): both texts,
		// quoted so that a "\r" shows
		return fmt.Sprintf("- %q\n+ %q", want, got)
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) > 2 {
		lines = lines[2:] // the file names
	}

	const maxLines = 80
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], fmt.Sprintf("... %d more lines", len(lines)-maxLines))
	}

	return strings.Join(lines, "\n")
}

func formatTimes(tm timing) string {
	var parts []string

	for _, k := range []string{"composer cold", "maestro cold", "composer warm", "maestro warm"} {
		if d, ok := tm.times[k]; ok {
			parts = append(parts, fmt.Sprintf("%s %s", k, d.Round(10*time.Millisecond)))
		}
	}

	return strings.Join(parts, ", ")
}

// speedReport is the Markdown table of wall times (summed over a
// scenario's steps).
func speedReport(timings []timing, phases []string) string {
	var b strings.Builder

	b.WriteString("| Scenario |")

	for _, p := range phases {
		fmt.Fprintf(&b, " Composer %s | maestro %s | speed-up |", p, p)
	}

	b.WriteString("\n|---|")

	for range phases {
		b.WriteString("---:|---:|---:|")
	}

	b.WriteString("\n")

	sums := map[string]time.Duration{}

	row := func(name string, times map[string]time.Duration) {
		fmt.Fprintf(&b, "| %s |", name)

		for _, p := range phases {
			c, m := times["composer "+p], times["maestro "+p]
			speedup := "-"

			if m > 0 {
				speedup = fmt.Sprintf("%.1fx", float64(c)/float64(m))
			}

			fmt.Fprintf(&b, " %.2fs | %.2fs | %s |", c.Seconds(), m.Seconds(), speedup)
		}

		b.WriteString("\n")
	}

	for _, tm := range timings {
		row(tm.scenario, tm.times)

		for k, d := range tm.times {
			sums[k] += d
		}
	}

	row("**total**", sums)

	return b.String()
}

// emptyBinDirs drops an empty */vendor/bin from Composer's tree when
// maestro has none: deviation 7 in docs/PORTING.md. Composer's
// BinaryInstaller::removeBinaries creates the bin dir even for packages
// without binaries, so removals leave it behind empty depending on the order
// they finish (plugin-captainhook's `install --no-dev` left it in 4 of 80
// runs); maestro never creates it there.
func emptyBinDirs(composer, maestro map[string]entry) {
	for dir, e := range composer {
		if e.kind != "dir" || !strings.HasSuffix(dir, "/vendor/bin") && dir != "vendor/bin" {
			continue
		}

		if _, ok := maestro[dir]; ok {
			continue
		}

		empty := true
		for p := range composer {
			if strings.HasPrefix(p, dir+"/") {
				empty = false

				break
			}
		}

		if empty {
			delete(composer, dir)
		}
	}
}

func TestComposerHints(t *testing.T) {
	head := "Loading composer repositories\r\n" +
		"The following exception is caused by a process timeout\r\n" +
		"Check https://getcomposer.org/doc/06-config.md#process-timeout for details\r\n"

	rest, hints, links := composerHints(head)
	if rest != "Loading composer repositories\r\n" || hints != 1 || !slices.Equal(links, []string{"https://getcomposer.org/doc/06-config.md#process-timeout"}) {
		t.Errorf("composerHints: %q, %d, %q", rest, hints, links)
	}
}
