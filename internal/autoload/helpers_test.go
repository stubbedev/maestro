package autoload

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// testConfig is the stub Config of AutoloadGeneratorTest: a map of
// setting name => value or func() any.
type testConfig map[string]any

func (c testConfig) Get(key string, _ int) (any, error) {
	v := c[key]
	if f, ok := v.(func() any); ok {
		return f(), nil
	}

	return v, nil
}

// testRepo is the InstalledRepositoryInterface mock: CanonicalPackages
// returns each of calls in turn, then the last one again.
type testRepo struct {
	calls [][]pkg.PackageInterface
	n     int
}

func (r *testRepo) DevPackageNames() []string { return nil }

func (r *testRepo) CanonicalPackages() []pkg.PackageInterface {
	if len(r.calls) == 0 {
		return nil
	}
	c := r.calls[min(r.n, len(r.calls)-1)]
	r.n++

	return c
}

// testIM is the InstallationManager mock: vendorDir/name[/targetDir], null
// for metapackages.
type testIM struct{ vendorDir func() string }

func (m testIM) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	if p.Type() == "metapackage" {
		return "", false, nil
	}
	path := m.vendorDir() + "/" + p.Name()
	if td := p.TargetDir(); php.ToBool(td.Value()) {
		path += "/" + td.S
	}

	return path, true, nil
}

// dispatchedScript is one DispatchScript call.
type dispatchedScript struct {
	name string
	dev  bool
}

type testDispatcher struct{ calls []dispatchedScript }

func (d *testDispatcher) DispatchScript(name string, dev bool, _ []string, _ *php.Array) (int, error) {
	d.calls = append(d.calls, dispatchedScript{name, dev})

	return 0, nil
}

// env is AutoloadGeneratorTest's fixture.
type env struct {
	t          *testing.T
	workingDir string
	vendorDir  string
	config     testConfig
	im         testIM
	repo       *testRepo
	dispatcher *testDispatcher
	io         *io.BufferIO
	generator  *Generator
}

// setUp ports AutoloadGeneratorTest::setUp: it chdirs into a fresh working
// directory.
func setUp(t *testing.T) *env {
	t.Helper()
	workingDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// Composer's test joins with DIRECTORY_SEPARATOR.
	e := &env{t: t, workingDir: workingDir, vendorDir: workingDir + string(filepath.Separator) + "composer-test-autoload"}
	e.mkdir(e.vendorDir)
	e.config = testConfig{
		"vendor-dir":       func() any { return e.vendorDir },
		"platform-check":   true,
		"use-include-path": false,
	}
	e.io = newBufferIO(t)
	t.Chdir(workingDir)
	e.im = testIM{vendorDir: func() string { return e.vendorDir }}
	e.repo = &testRepo{}
	e.dispatcher = &testDispatcher{}
	e.generator = NewGenerator(e.dispatcher, e.io)

	return e
}

func newBufferIO(t *testing.T) *io.BufferIO {
	t.Helper()
	b, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

func (e *env) packages(p ...pkg.PackageInterface) { e.repo.calls = append(e.repo.calls, p) }

func (e *env) mkdir(dir string) {
	e.t.Helper()
	if err := os.MkdirAll(dir, 0o777); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) write(path, content string) {
	e.t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) dump(root pkg.RootPackageInterface, scanPsr bool, suffix string) {
	e.t.Helper()
	if _, err := e.generator.Dump(e.config, e.repo, root, e.im, "composer", scanPsr, suffix, nil, false); err != nil {
		e.t.Fatal(err)
	}
}

// assertFileContentEquals compares a fixture of testdata/Fixtures with a
// generated file, ignoring carriage returns.
func (e *env) assertFileContentEquals(fixture, actual string) {
	e.t.Helper()
	want, err := os.ReadFile(filepath.Join(testdataDir, "Fixtures", fixture))
	if err != nil {
		e.t.Fatal(err)
	}
	e.assertStringEqualsFile(actual, strings.ReplaceAll(string(want), "\r", ""))
}

func (e *env) assertStringEqualsFile(path, want string) {
	e.t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatal(err)
	}
	if string(got) != want {
		e.t.Errorf("%s:\n got: %q\nwant: %q", path, got, want)
	}
}

func (e *env) assertAutoloadFiles(name, dir, typ string) {
	e.t.Helper()
	e.assertFileContentEquals("autoload_"+name+".php", dir+"/autoload_"+typ+".php")
}

func (e *env) fileContains(path, needle string) bool {
	e.t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		e.t.Fatal(err)
	}

	return strings.Contains(string(got), needle)
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		t.Errorf("%s exists", path)
	}
}

func assertExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Errorf("%s does not exist", path)
	}
}

// testdataDir is the absolute testdata dir, for tests that chdir.
var testdataDir = func() string {
	wd, _ := os.Getwd()

	return wd + "/testdata"
}()

// include evaluates a generated autoload_*.php or include_paths.php file
// as `include` would: its entries, keyed (key => value) or not.
func include(t *testing.T, path string) [][2]string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := util.RealpathOK(filepath.Dir(path))
	lines := strings.Split(string(content), "\n")
	var vendorDir, baseDir string
	var entries [][2]string
	inArray := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "$vendorDir = "):
			vendorDir = evalPathCode(strings.TrimSuffix(line[len("$vendorDir = "):], ";"), dir, "")
		case strings.HasPrefix(line, "$baseDir = "):
			baseDir = evalPathCode(strings.TrimSuffix(line[len("$baseDir = "):], ";"), dir, vendorDir)
		case line == "return array(":
			inArray = true
		case inArray && strings.HasPrefix(line, "    "):
			e := pathCodeEvaluator{code: strings.TrimSuffix(line[4:], ","), dir: dir, vendorDir: vendorDir, baseDir: baseDir}
			first := e.concat()
			if strings.HasPrefix(e.code[e.pos:], "=> ") {
				e.pos += 3
				entries = append(entries, [2]string{first, e.concat()})
			} else {
				entries = append(entries, [2]string{"", first})
			}
		}
	}

	return entries
}

func assertEntries(t *testing.T, got, want [][2]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// runPHP runs code with the php binary, skipping the test without one.
func runPHP(t *testing.T, code string) string {
	t.Helper()
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("php not available")
	}
	// On stdin rather than with -r: Windows caps a command line at 32767
	// characters.
	cmd := exec.Command("php")
	cmd.Stdin = strings.NewReader("<?php\n" + code)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("php: %v\n%s", err, out)
	}

	return string(out)
}

// Package construction helpers.

func arr(kv ...any) *php.Array { return php.ArrayOf(kv...) }

func list(v ...any) *php.Array { return php.ListOf(v...) }

func link(source, target string) *pkg.Link {
	return pkg.NewLink(source, target, semver.NewMatchAllConstraint(), "", pkg.NullString{})
}

func links(l ...*pkg.Link) pkg.Links { return pkg.LinksOf(l...) }

func newPackage(name string) *pkg.Package { return pkg.NewPackage(name, "1.0", "1.0") }

func newRoot(name string) *pkg.RootPackage { return pkg.NewRootPackage(name, "1.0", "1.0") }
