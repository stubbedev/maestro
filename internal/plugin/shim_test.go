package plugin

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/plugin/shimbuild"
	"github.com/stubbedev/maestro/internal/repository"
)

// TestShim_ManifestIsCurrent: the committed manifest lists the embedded
// files (run `go generate ./internal/plugin` after editing the shim).
func TestShim_ManifestIsCurrent(t *testing.T) {
	want, err := shimbuild.Manifest(shimFS())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := manifest()
	if !bytes.Equal(got, want) {
		t.Error("php/MANIFEST is stale: run `go generate ./internal/plugin`")
	}
}

// TestShim_IndexIsCurrent: the committed autoload index matches the shim's
// classes.
func TestShim_IndexIsCurrent(t *testing.T) {
	idx, err := shimbuild.BuildIndex("php")
	if err != nil {
		t.Fatal(err)
	}
	got, err := fs.ReadFile(shimFS(), shimbuild.IndexFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, idx.PHP()) {
		t.Error("php/autoload.php is stale: run `go generate ./internal/plugin`")
	}
	for _, class := range []string{`Maestro\Shim\Rpc`, `Composer\Util\ErrorHandler`, `Composer\Composer`, `Symfony\Component\Console\Application`, `Composer\Semver\Semver`, `React\Promise\PromiseInterface`, `Composer\Autoload\ClassLoader`} {
		if _, ok := idx.Classmap[class]; !ok {
			t.Errorf("%s is not in the index", class)
		}
	}
}

// TestShim_PinnedVersions: the shim's constants equal maestro's (§5.13).
func TestShim_PinnedVersions(t *testing.T) {
	for file, want := range map[string]string{
		"stubs/Composer/Plugin/PluginInterface.php": "public const PLUGIN_API_VERSION = '" + repository.PluginAPIVersion + "';",
		"src/Composer/Composer.php":                 "public const VERSION = '" + repository.ComposerVersion + "';",
	} {
		data, err := fs.ReadFile(shimFS(), file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Errorf("%s does not contain %s", file, want)
		}
	}
	data, _ := fs.ReadFile(shimFS(), "src/Composer/Composer.php")
	if !strings.Contains(string(data), "public const RUNTIME_API_VERSION = '"+repository.RuntimeAPIVersion+"';") {
		t.Error("Composer::RUNTIME_API_VERSION differs")
	}
}

func TestShim_Extract(t *testing.T) {
	cache := t.TempDir()

	dir, err := extractShim(cache)
	if err != nil {
		t.Fatal(err)
	}
	if dir != ShimDir(cache) || !strings.HasPrefix(dir, filepath.Join(cache, "maestro", "shim")+string(filepath.Separator)) {
		t.Errorf("dir = %s", dir)
	}

	// Every manifest entry is there, read-only.
	m, _ := manifest()
	for line := range strings.SplitSeq(strings.TrimSpace(string(m)), "\n") {
		_, rel, _ := strings.Cut(line, " ")
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0o222 != 0 {
			t.Errorf("%s is writable: %v", rel, info.Mode())
		}
	}
	// Windows keeps no execute or group and other permission bits.
	unix := runtime.GOOS != "windows"
	if info, err := os.Stat(ComposerBinary(cache)); err != nil || unix && info.Mode().Perm()&0o111 == 0 {
		t.Errorf("launcher %v, %v", info, err)
	}
	for rel, content := range shimbuild.VirtualFiles() {
		data, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil || string(data) != content {
			t.Errorf("%s differs from its embedded source", rel)
		}
	}
	if info, err := os.Stat(filepath.Dir(dir)); err != nil || unix && info.Mode().Perm() != 0o700 {
		t.Errorf("shim parent %v, %v", info, err)
	}

	// Again: nothing to do.
	marker := filepath.Join(dir, "marker")
	if err := os.WriteFile(marker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if again, err := extractShim(cache); err != nil || again != dir {
		t.Fatalf("again: %s, %v", again, err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("a valid shim was extracted again")
	}

	// A broken copy (manifest differs) is replaced.
	manifestFile := filepath.Join(dir, shimbuild.ManifestFile)
	_ = os.Chmod(manifestFile, 0o600)
	if err := os.WriteFile(manifestFile, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if again, err := extractShim(cache); err != nil || again != dir || !validShim(dir) {
		t.Fatalf("after breaking: %s, %v", again, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, fs.ErrNotExist) {
		t.Error("the broken copy was kept")
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), ".tmp-*"))
	if len(leftovers) > 0 {
		t.Errorf("temporary directories left: %v", leftovers)
	}
}

func TestShim_ExtractConcurrently(t *testing.T) {
	cache := t.TempDir()

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = extractShim(cache)
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !validShim(ShimDir(cache)) {
		t.Error("no valid shim")
	}
	entries, _ := os.ReadDir(filepath.Dir(ShimDir(cache)))
	if len(entries) != 1 {
		t.Errorf("entries %v", entries)
	}
}

func TestSetProcessEnv(t *testing.T) {
	t.Setenv(ComposerBinaryEnv, "")
	t.Setenv(MaestroBinaryEnv, "")
	cache := t.TempDir()

	if err := SetProcessEnv(cache); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv(ComposerBinaryEnv); got != ComposerBinary(cache) {
		t.Errorf("COMPOSER_BINARY = %s", got)
	}
	exe, _ := os.Executable()
	if got := os.Getenv(MaestroBinaryEnv); got != exe {
		t.Errorf("MAESTRO_BINARY = %s", got)
	}
	// Nothing was extracted.
	if _, err := os.Stat(ShimDir(cache)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("SetProcessEnv extracted the shim")
	}

	// The dispatcher's hook extracts it, without starting PHP.
	rt := New(Options{CacheDir: cache, FindPHP: func() (string, bool) { return "", false }})
	if err := rt.EnsureComposerBinary(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ComposerBinary(cache)); err != nil {
		t.Error(err)
	}
	if rt.Started() {
		t.Error("PHP was started")
	}
}

func TestRuntime_NoPHP(t *testing.T) {
	rt := New(Options{CacheDir: t.TempDir(), FindPHP: func() (string, bool) { return "", false }})

	err := rt.Start(`plugin "a/b"`)
	if err == nil || err.Error() != `maestro: plugin "a/b" requires PHP but no php binary was found in PATH` || !errors.Is(err, platform.ErrPHPNotFound) {
		t.Fatalf("Start = %v", err)
	}
	if again := rt.Start(`script A::b`); again != err { //nolint:errorlint // the very same error
		t.Errorf("a second Start = %v", again)
	}
	if _, err := rt.Call("ping", nil); err == nil {
		t.Error("Call without PHP succeeded")
	}
}

// TestRuntime_EarlyExit: a php that ends before the handshake (one below
// 7.2.5 prints Composer's message and exits 1) gives its exit status.
func TestRuntime_EarlyExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake php is a shell script, which Windows cannot run as a program")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	fake := filepath.Join(t.TempDir(), "php")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'Composer 2.3.0 dropped support for PHP <7.2.5'\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, transport := range []Transport{TransportPipes, TransportTCP} {
		var out bytes.Buffer
		stdout, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{})
		go func() {
			_, _ = io.Copy(&out, stdout)
			close(done)
		}()

		rt := New(Options{CacheDir: t.TempDir(), FindPHP: func() (string, bool) { return fake, true }, Stdout: w, Transport: transport})
		err = rt.Start("test")
		w.Close()
		<-done

		var exit *PHPExit
		if !errors.As(err, &exit) || exit.Code != 1 {
			t.Errorf("transport %d: Start = %v, want PHPExit 1", transport, err)
		}
		if !strings.Contains(out.String(), "dropped support") {
			t.Errorf("output %q", out.String())
		}
	}
}

// TestShim_Launcher runs COMPOSER_BINARY as scripts and plugins do:
// maestro runs with the same arguments, stdio and environment, and its
// exit code comes back (docs/PLUGINS.md D13).
func TestShim_Launcher(t *testing.T) {
	requirePHP(t)
	if runtime.GOOS == "windows" {
		t.Skip("the fake maestro is a shell script, which Windows cannot run as a program")
	}

	phpBinary, ok := platform.FindPHP()
	if !ok {
		t.Fatal("no php")
	}
	cache := t.TempDir()
	if _, err := extractShim(cache); err != nil {
		t.Fatal(err)
	}

	fake := filepath.Join(t.TempDir(), "maestro")
	script := "#!/bin/sh\nprintf '%s|' \"$@\"\nprintf 'env=%s ' \"$LAUNCHER_TEST\"\nread line\nprintf 'stdin=%s' \"$line\"\necho oops >&2\nexit 7\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(phpBinary, ComposerBinary(cache), "install", "--no-dev", "a b", "")
	cmd.Env = append(os.Environ(), MaestroBinaryEnv+"="+fake, "LAUNCHER_TEST=yes")
	cmd.Stdin = strings.NewReader("typed\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("exit = %v", err)
	}
	if got := stdout.String(); got != "install|--no-dev|a b||env=yes stdin=typed" {
		t.Errorf("stdout = %q", got)
	}
	if got := stderr.String(); got != "oops\n" {
		t.Errorf("stderr = %q", got)
	}

	// Without MAESTRO_BINARY it says why.
	cmd = exec.Command(phpBinary, ComposerBinary(cache))
	cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
	out, err := cmd.CombinedOutput()
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), "MAESTRO_BINARY is not set") {
		t.Errorf("without MAESTRO_BINARY: %v, %s", err, out)
	}
}

// TestShim_Parity reflects the shim in php and requires Composer 2.10.3's
// API exactly (docs/PLUGINS.md D7, §9.2): every class, interface and
// trait, with the same parents, interfaces, constants, properties and
// method signatures, and no Composer class Composer lacks.
func TestShim_Parity(t *testing.T) {
	requirePHP(t)

	phpBinary, ok := platform.FindPHP()
	if !ok {
		t.Fatal("no php")
	}
	// The golden is Composer's API as PHP 8.4 reflects it (attributes,
	// union types, default values): an older php renders the same
	// declarations differently, and reflect.php needs PHP 8. The other
	// shim tests run on every supported version (CI's php-7.2 job).
	if v, err := exec.Command(phpBinary, "-r", "echo PHP_MAJOR_VERSION;").Output(); err == nil {
		if major, _ := strconv.Atoi(string(v)); major < 8 {
			t.Skip("the API golden is reflected with PHP 8.4; this php is " + string(v) + ".x")
		}
	}
	dir, err := extractShim(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(phpBinary, "../../tools/shimgen/reflect.php", "shim", dir).Output()
	if err != nil {
		t.Fatalf("reflect.php: %v", err)
	}

	f, err := os.Open("testdata/apiparity.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	golden, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(out, golden) {
		return
	}

	shim, err := shimbuild.ParseAPI(out)
	if err != nil {
		t.Fatal(err)
	}
	want, err := shimbuild.ParseAPI(golden)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range want.Names() {
		if _, ok := shim.Classes[name]; !ok {
			t.Errorf("missing: %s", name)
		}
	}
	for _, name := range shim.Names() {
		if _, ok := want.Classes[name]; !ok {
			t.Errorf("not in Composer: %s", name)
		}
	}
	t.Errorf("the shim's reflection differs from Composer's (apiparity.json.gz); compare `php tools/shimgen/reflect.php shim %s`", dir)
}

// TestShim_VendoredDrift: the vendored libraries are exactly the versions
// Composer 2.10.3 locks, with exactly the files its phar ships
// (docs/PLUGINS.md §5.1, §9.2). It needs .ref/composer (`ref-sync`).
func TestShim_VendoredDrift(t *testing.T) {
	const ref = "../../.ref/composer"
	if _, err := os.Stat(ref + "/composer.lock"); err != nil {
		t.Skip("no .ref/composer; run ref-sync")
	}

	locked, err := shimbuild.LockedVersions(ref + "/composer.lock")
	if err != nil {
		t.Fatal(err)
	}
	in, err := shimbuild.ReadInstalled("php/lib/composer/installed.json")
	if err != nil {
		t.Fatal(err)
	}
	vendored := map[string]string{}
	for _, p := range in.Packages {
		vendored[p.Name] = p.Version
	}
	for name, version := range locked {
		if vendored[name] != version {
			t.Errorf("%s: vendored %q, Composer locks %q", name, vendored[name], version)
		}
	}
	for name := range vendored {
		if _, ok := locked[name]; !ok {
			t.Errorf("%s is vendored but not in Composer's lock", name)
		}
	}

	files, err := shimbuild.VendorFiles(ref + "/vendor")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, rel := range files {
		want[rel] = true
		got, err := fs.ReadFile(shimFS(), "lib/"+rel)
		if err != nil {
			t.Errorf("not vendored: %s", rel)

			continue
		}
		orig, err := os.ReadFile(filepath.Join(ref, "vendor", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, orig) {
			t.Errorf("changed: %s", rel)
		}
	}
	_ = fs.WalkDir(shimFS(), "lib", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !want[strings.TrimPrefix(p, "lib/")] {
			t.Errorf("extra file: %s", p)
		}

		return err
	})
}
