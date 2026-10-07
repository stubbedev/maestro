// Package commandtest ports the test drivers Composer's command tests use:
// tests/Composer/Test/TestCase.php (initTempComposer, createInstalledJson,
// createComposerLock, getApplicationTester, getPackage, trimLines, ...),
// tests/bootstrap.php (Main) and symfony/console's ApplicationTester.
//
// Tests that use it change the process working directory and environment,
// so they must not call t.Parallel(). Command tests live in the external
// test package command_test (commandtest imports internal/command).
package commandtest

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// Main ports tests/bootstrap.php: call it from TestMain.
//
//	func TestMain(m *testing.M) { commandtest.Main(m) }
func Main(m *testing.M) {
	util.PutEnv("COMPOSER_TESTS_ARE_RUNNING", "1")
	util.PutEnv("COMPOSER_ALLOW_UNSAFE_PHAR_METADATA", "1")
	util.PutEnv("NO_COLOR", "1")
	util.ClearEnv("COMPOSER")
	util.ClearEnv("COMPOSER_VENDOR_DIR")
	util.ClearEnv("COMPOSER_BIN_DIR")
	// phpunit.xml.dist
	util.PutEnv("COMPOSER_TEST_SUITE", "1")
	os.Exit(m.Run())
}

var (
	runtimeOnce sync.Once
	runtime     *composer.Runtime
)

// Runtime returns the composer.Runtime shared by the tests of a process
// (one per process, as in production; it probes the php on the PATH once).
func Runtime() *composer.Runtime {
	runtimeOnce.Do(func() { runtime = composer.NewRuntime("test", nil) })

	return runtime
}

// NewApplication returns a Composer Application on the shared runtime, as
// `new Application()` in a test.
func NewApplication() *command.Application {
	return command.NewApplication(&composer.Factory{Runtime: Runtime()})
}

// UniqueTmpDirectory ports getUniqueTmpDirectory; the directory is removed
// when the test ends.
func UniqueTmpDirectory(t testing.TB) string {
	t.Helper()
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	dir := filepath.Join(php.SysGetTempDir(), "composer-test-"+hex.EncodeToString(b))
	if err := os.Mkdir(dir, 0o777); err != nil {
		t.Fatalf("Failed to create a unique temporary directory: %v", err)
	}
	real, ok := util.RealpathOK(dir)
	if !ok {
		real = dir
	}
	t.Cleanup(func() { _ = os.RemoveAll(real) })

	return real
}

// ToArray converts a test value to the PHP array Composer's tests write:
// a *php.Array as is, a string as JSON (decoded with assoc = true), nil
// as [].
func ToArray(t testing.TB, v any) *php.Array {
	t.Helper()
	switch v := v.(type) {
	case nil:
		return php.NewArray()
	case *php.Array:
		return v
	case string:
		d, err := php.JSONDecode(v, true)
		if err != nil {
			t.Fatalf("invalid JSON %q: %v", v, err)
		}
		a, ok := d.(*php.Array)
		if !ok {
			t.Fatalf("JSON %q is not an object or array", v)
		}

		return a
	}
	t.Fatalf("unsupported value %T", v)

	return nil
}

const encodeFlags = php.JSONUnescapedSlashes | php.JSONPrettyPrint | php.JSONUnescapedUnicode

func encode(t testing.TB, data any) []byte {
	t.Helper()
	s, err := json.Encode(data, encodeFlags, json.IndentDefault)
	if err != nil {
		t.Fatal(err)
	}

	return []byte(s)
}

// InitTempComposer ports initTempComposer: it writes composer.json and
// auth.json (and composer.lock when lock is not empty) into a new temp
// directory, sets COMPOSER_HOME into it and changes into it. Everything is
// restored when the test ends. The arguments are as ToArray takes them.
func InitTempComposer(t testing.TB, composerJSON, authJSON, composerLock any, setupRepositories bool) string {
	t.Helper()
	dir := UniqueTmpDirectory(t)

	prevCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(prevCwd)
		util.ClearEnv("COMPOSER_HOME")
		util.ClearEnv("COMPOSER_DISABLE_XDEBUG_WARN")
	})

	util.PutEnv("COMPOSER_HOME", dir+"/composer-home")
	util.PutEnv("COMPOSER_DISABLE_XDEBUG_WARN", "1")

	cj := ToArray(t, composerJSON)
	aj := ToArray(t, authJSON)

	if setupRepositories {
		if repos, ok := cj.GetArray("repositories"); ok && !repos.Has("packagist.org") && !containsPackagistFalse(repos) {
			repos = repos.Clone()
			if repos.IsList() {
				repos.Append(php.ArrayOf("packagist.org", false))
			} else {
				repos.Set("packagist.org", false)
			}
			cj = cj.Clone()
			cj.Set("repositories", repos)
		}
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	var cjData, ajData any = cj, aj
	if cj.Len() == 0 {
		cjData = php.NewObject()
	}
	if aj.Len() == 0 {
		ajData = php.NewObject()
	}
	writeFile(t, dir+"/composer.json", encode(t, cjData))
	writeFile(t, dir+"/auth.json", encode(t, ajData))

	if lock := ToArray(t, composerLock); lock.Len() > 0 {
		writeFile(t, dir+"/composer.lock", encode(t, lock))
	}

	return dir
}

// containsPackagistFalse is in_array(['packagist.org' => false], $repos, true).
func containsPackagistFalse(repos *php.Array) bool {
	for _, v := range repos.All() {
		if a, ok := v.(*php.Array); ok && a.Len() == 1 {
			if pv, ok := a.Get("packagist.org"); ok && pv == false {
				return true
			}
		}
	}

	return false
}

func writeFile(t testing.TB, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o666); err != nil {
		t.Fatal(err)
	}
}

// installPathIM is InstallationManagerMock::getInstallPath.
type installPathIM struct{}

func (installPathIM) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	return "vendor/" + p.Name(), true, nil
}

// CreateInstalledJSON ports createInstalledJson: a
// vendor/composer/installed.json (and installed.php) in the working
// directory holding packages and devPackages.
func CreateInstalledJSON(t testing.TB, packages, devPackages []pkg.PackageInterface, devMode bool) {
	t.Helper()
	if err := os.MkdirAll("vendor/composer", 0o777); err != nil {
		t.Fatal(err)
	}
	file, err := json.NewFile("vendor/composer/installed.json", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := repository.NewInstalledFilesystemRepository(file, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(devPackages))
	for i, p := range devPackages {
		names[i] = p.PrettyName()
	}
	repo.SetDevPackageNames(names)
	for _, p := range append(append([]pkg.PackageInterface{}, packages...), devPackages...) {
		if err := repo.AddPackage(p); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("vendor/"+p.Name(), 0o777); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Write(devMode, installPathIM{}); err != nil {
		t.Fatal(err)
	}
}

// CreateComposerLock ports createComposerLock: a composer.lock in the
// working directory with packages and devPackages.
func CreateComposerLock(t testing.TB, packages, devPackages []pkg.PackageInterface) {
	t.Helper()
	contents, err := os.ReadFile("./composer.json")
	if err != nil {
		contents = nil
	}
	file, err := json.NewFile("./composer.lock", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := locker.New(io.NewNullIO(), file, installPathIM{}, string(contents), util.NewProcessExecutor(nil))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.SetLockData(locker.LockDataInput{
		Packages:          packages,
		DevPackages:       php.Some(devPackages), // $devPackages = []: never null
		PlatformReqs:      php.NewArray(),
		PlatformDevReqs:   php.NewArray(),
		Aliases:           php.NewArray(),
		MinimumStability:  "dev",
		StabilityFlags:    php.NewArray(),
		PlatformOverrides: php.NewArray(),
	}, true); err != nil {
		t.Fatal(err)
	}
}

// TrimLines ports trimLines: trims the string and the trailing spaces of
// every line.
func TrimLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}

	return php.Trim(strings.Join(lines, "\n"))
}

var parser = pkg.NewVersionParser()

func normalize(t testing.TB, version string) string {
	t.Helper()
	v, err := parser.Normalize(version)
	if err != nil {
		t.Fatal(err)
	}

	return v
}

// GetPackage ports getPackage($name, $version) (a CompletePackage).
func GetPackage(t testing.TB, name, version string) *pkg.CompletePackage {
	t.Helper()

	return pkg.NewCompletePackage(name, normalize(t, version), version)
}

// linkSetter is the setter configureLinks calls for a link type.
type linkSetter interface {
	SetRequires(pkg.Links)
	SetConflicts(pkg.Links)
	SetProvides(pkg.Links)
	SetReplaces(pkg.Links)
	SetDevRequires(pkg.Links)
}

// ConfigureLinks ports configureLinks: config is link type (require,
// conflict, provide, replace, require-dev) => target => constraint, as
// ToArray takes it.
func ConfigureLinks(t testing.TB, p pkg.PackageInterface, config any) {
	t.Helper()
	cfg := ToArray(t, config)
	setter, ok := p.(linkSetter)
	if !ok {
		t.Fatalf("%T has no link setters", p)
	}
	arrayLoader := loader.NewArrayLoader(parser, false)
	for _, lt := range pkg.SupportedLinkTypes() {
		links, ok := cfg.GetArray(lt.Type)
		if !ok {
			continue
		}
		parsed, err := arrayLoader.ParseLinks(p.Name(), p.PrettyVersion(), lt.Method, links)
		if err != nil {
			t.Fatal(err)
		}
		switch lt.Method {
		case pkg.TypeRequire:
			setter.SetRequires(parsed)
		case pkg.TypeConflict:
			setter.SetConflicts(parsed)
		case pkg.TypeProvide:
			setter.SetProvides(parsed)
		case pkg.TypeReplace:
			setter.SetReplaces(parsed)
		case pkg.TypeDevRequire:
			setter.SetDevRequires(parsed)
		}
	}
}
