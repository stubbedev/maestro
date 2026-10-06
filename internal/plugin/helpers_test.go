package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// requirePHP skips a test that runs the real shim unless
// MAESTRO_PHP_TESTS=1 (docs/PLUGINS.md §9.2).
func requirePHP(t *testing.T) {
	t.Helper()

	if os.Getenv("MAESTRO_PHP_TESTS") != "1" {
		t.Skip("set MAESTRO_PHP_TESTS=1 to run the shim with php")
	}
}

// withComposerRoot names Composer's files under phar:///maestro
// (phperr.Root) for the test, as maestro's executable names them.
func withComposerRoot(t *testing.T) {
	t.Helper()

	old := phperr.Root()
	phperr.SetRoot("phar:///maestro")
	t.Cleanup(func() { phperr.SetRoot(old) })
}

// handlersPHP is the PHP side of the tests.
func handlersPHP(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs("testdata/php/handlers.php")
	if err != nil {
		t.Fatal(err)
	}

	return path
}

// newTestRuntime returns a started Runtime with the test handlers, its
// standard output and error going to files the test can read.
func newTestRuntime(t *testing.T, configure ...func(*Options)) (*Runtime, *os.File, *os.File) {
	t.Helper()

	dir := t.TempDir()
	stdout, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdout.Close()
		stderr.Close()
	})

	opts := Options{
		CacheDir: filepath.Join(dir, "cache"),
		Args:     []string{"maestro", "install", "--no-dev"},
		Require:  []string{handlersPHP(t)},
		Stdout:   stdout,
		Stderr:   stderr,
	}
	for _, c := range configure {
		c(&opts)
	}

	rt := New(opts)
	t.Cleanup(rt.Close)

	return rt, stdout, stderr
}

func start(t *testing.T, rt *Runtime) {
	t.Helper()

	if err := rt.Start("test"); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

func readFile(t *testing.T, f *os.File) string {
	t.Helper()

	data, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func get(t *testing.T, v any, key string) any {
	t.Helper()

	a, ok := v.(*php.Array)
	if !ok {
		t.Fatalf("%#v is not an array", v)
	}
	out, ok := a.Get(key)
	if !ok {
		t.Fatalf("no %q in %v", key, a.Keys())
	}

	return out
}
