// Package errorstest compares maestro's output for the scenarios of
// internal/command/testdata/errors with the reference Composer's, recorded
// by tools/oracle/errors/errors.sh -w: exception boxes (throw site, class,
// previous exceptions), verbose and debug lines, exit codes, at default,
// -v, -vv and -vvv verbosity.
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
	"math/rand/v2"
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

func TestErrorRendering(t *testing.T) {
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
	defer server.Close()

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		name := e.Name()
		for _, v := range verbosities {
			golden := filepath.Join(dataDir, name, v.name+".txt")
			want, err := os.ReadFile(golden)
			if err != nil {
				continue
			}
			t.Run(name+"/"+v.name, func(t *testing.T) {
				got := runScenario(t, filepath.Join(dataDir, name), v.flag, server.URL)
				if got != string(want) {
					t.Errorf("output differs from Composer's (%s):\n%s", golden, lineDiff(string(want), got))
				}
			})
		}
	}
}

// runScenario runs the scenario as errors.sh does and returns the
// normalised output followed by "exit N".
func runScenario(t *testing.T, dir, flag, serverURL string) string {
	t.Helper()
	run := filepath.Join(workDir(t), "run")
	for _, d := range []string{"home", "cache"} {
		if err := os.MkdirAll(filepath.Join(run, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	copyTree(t, filepath.Join(dir, "project"), filepath.Join(run, "p"))
	if _, err := os.Stat(filepath.Join(dir, "home")); err == nil {
		copyTree(t, filepath.Join(dir, "home"), filepath.Join(run, "home"))
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

	var buf bytes.Buffer
	cmd := exec.Command(os.Args[0], args...)
	cmd.Dir = filepath.Join(run, "p")
	cmd.Env = env
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			t.Fatalf("run: %v", err)
		}
		code = ee.ExitCode()
	}

	host := strings.TrimPrefix(serverURL, "http://")

	return normalize(buf.String(), run, host) + "exit " + strconv.Itoa(code) + "\n"
}

// workDir creates the scenario's directory with the shape of the oracle's
// (mktemp -d /tmp/maestro-errors.XXXXXX): its length decides where the
// exception boxes wrap paths.
func workDir(t *testing.T) string {
	t.Helper()
	if fi, err := os.Stat("/tmp"); err != nil || !fi.IsDir() {
		t.Skip("the goldens' paths are under /tmp")
	}
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for {
		b := make([]byte, 6)
		for i := range b {
			b[i] = chars[rand.IntN(len(chars))]
		}
		dir := "/tmp/maestro-errors." + string(b)
		err := os.Mkdir(dir, 0o700)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })

		return dir
	}
}

var normalizers = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`(?m)^(  at )(/|phar://)[^ \n]*/([^/ \n]+:([0-9]+|n/a))$`), "${1}${3}"},
	{regexp.MustCompile(`(?m)^ [^ \n]+ at (/|phar://)[^ \n]*:[0-9]+\n`), ""},
	{regexp.MustCompile(`(?m)^Running cache garbage collection\n`), ""},
	{regexp.MustCompile(`(?m)^(Running [^ \n]+ \([^)\n]*\) with PHP ).* on .*$`), "${1}@PHP@ on @OS@"},
	{regexp.MustCompile(`/tmp/composer_archive[0-9a-f]+`), "/tmp/composer_archive@RAND@"},
	{regexp.MustCompile(`(?m)^(Memory usage: )[0-9.]+MiB \(peak: [0-9.]+MiB\), time: [0-9.]+s$`), "${1}@PROFILE@"},
	{regexp.MustCompile(`(?m)^(Analyzed )[0-9]+( (packages|rules) to resolve dependencies)$`), "${1}@N@${2}"},
	{regexp.MustCompile(`(?m)^(Dependency resolution completed in )[0-9.]+( seconds)$`), "${1}@TIME@${2}"},
	{regexp.MustCompile(`(but your php version \()[^)\n]*(\) does not satisfy)`), "${1}@PHPVERSION@${2}"},
}

// normalize applies errors.sh's normalisation.
func normalize(s, run, host string) string {
	s = strings.ReplaceAll(s, run, "@DIR@")
	s = strings.ReplaceAll(s, host, "@SERVER@")
	for _, n := range normalizers {
		s = n.re.ReplaceAllString(s, n.repl)
	}

	return s
}

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

// copyTree copies src into dst, keeping file modes (cp -a).
func copyTree(t *testing.T, src, dst string) {
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
