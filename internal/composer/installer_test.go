package composer

// Ports tests/Composer/Test/InstallerTest.php.

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/filter"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/semver"
)

func TestMain(m *testing.M) {
	// what Composer's test bootstrap sets
	_ = os.Setenv("COMPOSER_TESTS_ARE_RUNNING", "1")
	cacheDir, err := os.MkdirTemp("", "maestro-composer-test-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("MAESTRO_CACHE_DIR", cacheDir)
	if testProbe, err = os.ReadFile("testdata/platform/php84.probe"); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(cacheDir)
	os.Exit(code)
}

func newBufferIO(t testing.TB) *io.BufferIO {
	t.Helper()
	out, err := io.NewBufferIO("", console.VerbosityNormal, console.NewOutputFormatter(false))
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func bufferOutput(out *io.BufferIO) string {
	return strings.ReplaceAll(out.Output(), "\r", "")
}

// installerTestConfig is the Config mock of testInstaller.
type installerTestConfig struct{}

func (installerTestConfig) Get(key string, _ int) (any, error) {
	switch key {
	case "vendor-dir":
		return "foo", nil
	case "lock", "notify-on-install":
		return true, nil
	case "platform", "audit":
		return php.NewArray(), nil
	case "policy":
		return false, nil
	}

	return nil, errors.New("Unknown key " + key)
}

func versionConstraint(t testing.TB, operator, version string) semver.ConstraintInterface {
	t.Helper()
	c, err := semver.NewConstraint(operator, normalizeVersion(t, version))
	if err != nil {
		t.Fatal(err)
	}
	c.SetPrettyString(operator + " " + version)

	return c
}

func normalizeVersion(t testing.TB, version string) string {
	t.Helper()
	n, err := semver.VersionParser{}.Normalize(version)
	if err != nil {
		t.Fatal(err)
	}

	return n
}

func requireLink(t testing.TB, source, target, version string) pkg.Links {
	c := versionConstraint(t, "=", version)

	return pkg.LinksOf(pkg.NewLink(source, target, c, "requires", pkg.Str(c.PrettyString())))
}

func TestInstaller_Installer(t *testing.T) {
	// when A requires B and B requires A, and A is a non-published root package
	// the install of B should succeed
	newCase := func(publishRoot bool) (*pkg.RootPackage, []repository.RepositoryInterface, []pkg.PackageInterface) {
		a := pkg.NewRootPackage("a/a", normalizeVersion(t, "1.0.0"), "1.0.0")
		a.SetRequires(requireLink(t, "a/a", "b/b", "1.0.0"))
		b := pkg.NewPackage("b/b", normalizeVersion(t, "1.0.0"), "1.0.0")
		b.SetRequires(requireLink(t, "b/b", "a/a", "1.0.0"))
		packages := []pkg.PackageInterface{b}
		if publishRoot {
			// #480: when A requires B and B requires A, and A is a published root package
			// only B should be installed, as A is the root
			packages = []pkg.PackageInterface{a, b}
		}
		repo, err := repository.NewArrayRepository(packages)
		if err != nil {
			t.Fatal(err)
		}

		return a, []repository.RepositoryInterface{repo}, []pkg.PackageInterface{b}
	}

	for _, publishRoot := range []bool{false, true} {
		t.Run(strconv.FormatBool(publishRoot), func(t *testing.T) {
			rootPackage, repositories, expectInstalled := newCase(publishRoot)
			out := newBufferIO(t)

			cfg := config.New(false, "")
			repositoryManager := repository.NewRepositoryManager(out, cfg, nil, nil, nil)
			localRepo, err := repository.NewInstalledArrayRepository(nil)
			if err != nil {
				t.Fatal(err)
			}
			repositoryManager.SetLocalRepository(localRepo)
			for _, repo := range repositories {
				repositoryManager.AddRepository(repo)
			}
			installationManager := &installationManagerMock{}

			// emulate a writable lock file
			lockFile := &lockFileMock{}
			l, err := locker.New(out, lockFile, installationManager, "{}", nil)
			if err != nil {
				t.Fatal(err)
			}

			installer, err := NewInstaller(InstallerDeps{
				IO:                  out,
				Config:              installerTestConfig{},
				Package:             rootPackage.Clone(),
				DownloadManager:     downloadManagerMock{},
				RepositoryManager:   repositoryManager,
				Locker:              l,
				InstallationManager: installationManager,
				EventDispatcher:     eventDispatcherMock{},
				AutoloadGenerator:   autoloadGeneratorMock{},
				Runtime:             testRuntime(t, 0),
			})
			if err != nil {
				t.Fatal(err)
			}
			installer.SetAuditConfig(advisory.AuditConfig{Audit: false})
			result, err := installer.Run()
			output := bufferOutput(out)
			if err != nil || result != 0 {
				t.Fatalf("run = %d, %v\n%s", result, err, output)
			}

			if got, want := comparablePackages(t, installationManager.installed), comparablePackages(t, expectInstalled); !slices.Equal(got, want) {
				t.Errorf("installed = %v, want %v", got, want)
			}
			if len(installationManager.updated) != 0 || len(installationManager.uninstalled) != 0 {
				t.Errorf("updated %v, uninstalled %v", installationManager.updated, installationManager.uninstalled)
			}
		})
	}
}

// comparablePackages ports makePackagesComparable.
func comparablePackages(t testing.TB, packages []pkg.PackageInterface) []string {
	t.Helper()
	var comparable []string
	for _, p := range packages {
		dumped, err := dumper.ArrayDumper{}.Dump(p)
		if err != nil {
			t.Fatal(err)
		}
		s, err := php.JSONEncode(dumped, 0)
		if err != nil {
			t.Fatal(err)
		}
		comparable = append(comparable, s)
	}

	return comparable
}

// integrationTest is one *.test fixture (loadIntegrationTests).
type integrationTest struct {
	file, message, condition string
	composer                 *php.Array
	lock, installed          any
	run                      string
	expectLock               any // nil: none given, false: never written
	expectInstalled          any
	expectOutput             *string
	expectOutputOptimized    *string
	expect                   string
	expectResult             int
	expectException          string
}

var fixtureSections = []struct {
	name     string
	required bool
}{
	{"TEST", true},
	{"CONDITION", false},
	{"COMPOSER", true},
	{"LOCK", false},
	{"INSTALLED", false},
	{"RUN", true},
	{"EXPECT-LOCK", false},
	{"EXPECT-INSTALLED", false},
	{"EXPECT-OUTPUT", false},
	{"EXPECT-OUTPUT-OPTIMIZED", false},
	{"EXPECT-EXIT-CODE", false},
	{"EXPECT-EXCEPTION", false},
	{"EXPECT", true},
}

var sectionSplit = php.MustCompile(`#(?:^|\n*)--([A-Z-]+)--\n#`)

// readTestFile ports readTestFile.
func readTestFile(file, fixturesDir string) (map[string]string, error) {
	contents, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	tokens, err := sectionSplit.Split(string(contents), -1, php.PregSplitDelimCapture)
	if err != nil {
		return nil, err
	}

	name := strings.TrimPrefix(file, fixturesDir+"/")
	known := map[string]bool{}
	for _, s := range fixtureSections {
		known[s.name] = true
	}

	section := ""
	inSection := false
	data := map[string]string{}
	for _, token := range tokens {
		if !inSection && (token == "" || token == "0") {
			continue // skip leading blank
		}

		if !inSection {
			if !known[token] {
				return nil, fmt.Errorf("The test file %q must not contain a section named %q.", name, token)
			}
			section = token
			inSection = true

			continue
		}

		data[section] = token
		inSection = false
	}

	for _, s := range fixtureSections {
		if _, ok := data[s.name]; s.required && !ok {
			return nil, fmt.Errorf("The test file %q must have a section named %q.", name, s.name)
		}
	}

	return data, nil
}

var fileURL = php.MustCompile(`{^file://[^/]}`)

// loadIntegrationTests ports loadIntegrationTests.
func loadIntegrationTests(t testing.TB, path string) []integrationTest {
	t.Helper()
	fixturesDir, err := filepath.Abs("testdata/Fixtures/" + path)
	if err != nil {
		t.Fatal(err)
	}
	fixturesDir = strings.TrimSuffix(fixturesDir, "/")

	var tests []integrationTest
	err = filepath.WalkDir(fixturesDir, func(file string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(file, ".test") {
			return err
		}

		testData, err := readTestFile(file, fixturesDir)
		if err != nil {
			return err
		}

		test := integrationTest{
			file:      strings.TrimPrefix(file, fixturesDir+"/"),
			message:   testData["TEST"],
			condition: testData["CONDITION"],
			run:       testData["RUN"],
			expect:    testData["EXPECT"],
		}
		composerData, err := json.ParseJSON(testData["COMPOSER"], "")
		if err != nil {
			return fmt.Errorf("%s: %w", test.file, err)
		}
		test.composer = composerData.(*php.Array)

		if repos, ok := test.composer.Get("repositories"); ok {
			if list, ok := repos.(*php.Array); ok {
				for key, repo := range list.All() {
					repoArray, ok := repo.(*php.Array)
					if !ok {
						continue
					}
					if typ, _ := repoArray.Get("type"); typ != "composer" {
						continue
					}
					// Change paths like file://foobar to file:///path/to/fixtures
					url, _ := repoArray.Get("url")
					if ok, _ := fileURL.IsMatch(php.ToString(url)); ok {
						repoArray = repoArray.Clone()
						repoArray.Set("url", "file://"+fixturesDir+"/"+php.ToString(url)[7:])
						list.Set(key, repoArray)
					}
				}
			}
		}

		if lock := testData["LOCK"]; !phpEmpty(lock) {
			parsed, err := json.ParseJSON(lock, "")
			if err != nil {
				return fmt.Errorf("%s: %w", test.file, err)
			}
			lockArray := parsed.(*php.Array)
			if _, ok := lockArray.Get("hash"); !ok {
				encoded, err := json.Encode(test.composer, 0, json.IndentDefault)
				if err != nil {
					return err
				}
				lockArray.Set("hash", md5Hex(encoded))
			}
			test.lock = lockArray
		}
		if installed := testData["INSTALLED"]; !phpEmpty(installed) {
			if test.installed, err = json.ParseJSON(installed, ""); err != nil {
				return fmt.Errorf("%s: %w", test.file, err)
			}
		}
		if expectLock := testData["EXPECT-LOCK"]; !phpEmpty(expectLock) {
			if expectLock == "false" {
				test.expectLock = false
			} else if test.expectLock, err = json.ParseJSON(expectLock, ""); err != nil {
				return fmt.Errorf("%s: %w", test.file, err)
			}
		}
		if expectInstalled := testData["EXPECT-INSTALLED"]; !phpEmpty(expectInstalled) {
			if test.expectInstalled, err = json.ParseJSON(expectInstalled, ""); err != nil {
				return fmt.Errorf("%s: %w", test.file, err)
			}
		}
		if s, ok := testData["EXPECT-OUTPUT"]; ok {
			test.expectOutput = &s
		}
		if s, ok := testData["EXPECT-OUTPUT-OPTIMIZED"]; ok {
			test.expectOutputOptimized = &s
		}
		if e := testData["EXPECT-EXCEPTION"]; !phpEmpty(e) {
			test.expectException = e
			if !phpEmpty(testData["EXPECT-EXIT-CODE"]) {
				return errors.New("EXPECT-EXCEPTION and EXPECT-EXIT-CODE are mutually exclusive")
			}
		} else if code := testData["EXPECT-EXIT-CODE"]; !phpEmpty(code) {
			test.expectResult = int(php.ToInt(code))
		}

		tests = append(tests, test)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return tests
}

func TestInstaller_SlowIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	t.Setenv("COMPOSER_POOL_OPTIMIZER", "0")
	for _, test := range loadIntegrationTests(t, "installer-slow/") {
		t.Run(test.file, func(t *testing.T) {
			doTestIntegration(t, test, test.expectOutput, slowFixtureExtensions)
		})
	}
}

// slowFixtureExtensions are the dummy extensions the slow fixtures run
// with. Every platform package is fixed in the request, so the number of
// extensions shifts the ids of the pool's packages, and
// github-issues-7665.test's learned rules are listed in the order of
// their literals as strings ("-123" sorts before "-45-67"): its expected
// output holds for a PHP with at least 12 more extensions than the
// recorded PHP 8.4 build (Composer's CI has them).
const slowFixtureExtensions = 30

func TestInstaller_IntegrationWithPoolOptimizer(t *testing.T) {
	t.Setenv("COMPOSER_POOL_OPTIMIZER", "1")
	for _, test := range loadIntegrationTests(t, "installer/") {
		t.Run(test.file, func(t *testing.T) {
			expectOutput := test.expectOutputOptimized
			if expectOutput == nil || *expectOutput == "" {
				expectOutput = test.expectOutput
			}
			doTestIntegration(t, test, expectOutput, 0)
		})
	}
}

func TestInstaller_IntegrationWithRawPool(t *testing.T) {
	t.Setenv("COMPOSER_POOL_OPTIMIZER", "0")
	for _, test := range loadIntegrationTests(t, "installer/") {
		t.Run(test.file, func(t *testing.T) {
			doTestIntegration(t, test, test.expectOutput, 0)
		})
	}
}

// evalCondition evaluates the --CONDITION-- PHP expressions the fixtures
// use.
func evalCondition(t *testing.T, condition string) bool {
	switch strings.TrimSpace(condition) {
	case "!defined('HHVM_VERSION')":
		return true
	case "putenv('COMPOSER_FUND=1')":
		t.Setenv("COMPOSER_FUND", "1")

		return true
	case "putenv('COMPOSER_FUND=0')":
		t.Setenv("COMPOSER_FUND", "0")

		return true
	}
	t.Fatalf("unsupported condition %q", condition)

	return false
}

var (
	iniListLine = php.MustCompile(`{^    - .*?\.ini$}m`)
	iniListRun  = php.MustCompile(`{(__inilist__\r?\n)+}`)
)

func doTestIntegration(t *testing.T, test integrationTest, expectOutput *string, extraExtensions int) {
	if test.condition != "" && !evalCondition(t, test.condition) {
		t.Skip(test.condition)
	}
	t.Setenv("COMPOSER_FUND", os.Getenv("COMPOSER_FUND"))
	if test.condition == "" {
		_ = os.Unsetenv("COMPOSER_FUND")
	}
	t.Chdir("testdata")

	out := newBufferIO(t)

	// Create Composer mock object according to configuration
	rt := testRuntime(t, extraExtensions)
	factory := newFactoryMock(t, rt)
	c, err := factory.Create(out, test.composer, PluginsEnabled, false)
	if err != nil {
		checkException(t, test, err)

		return
	}

	jsonMock := jsonFileMock{data: test.installed}
	if test.installed == nil {
		jsonMock.data = php.NewArray()
	}
	installedRepo, err := repository.NewInstalledFilesystemRepository(jsonMock, true, c.Package())
	if err != nil {
		t.Fatal(err)
	}
	repositoryManager := c.RepositoryManager()
	repositoryManager.SetLocalRepository(installedRepositoryMock{installedRepo})

	// emulate a writable lock file
	lockFile := &lockFileMock{}
	if test.lock != nil {
		encoded, err := php.JSONEncode(test.lock, php.JSONPrettyPrint)
		if err != nil {
			t.Fatal(err)
		}
		lockFile.lockData = &encoded
	}

	contents, err := php.JSONEncode(test.composer, 0)
	if err != nil {
		t.Fatal(err)
	}
	l, err := locker.New(out, lockFile, c.InstallationManager(), contents, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetLocker(l)

	installer, err := NewInstaller(InstallerDeps{
		IO:                  out,
		Config:              c.Config(),
		Package:             c.Package(),
		DownloadManager:     downloadManagerAdapter{c.DownloadManager()},
		RepositoryManager:   repositoryManager,
		Locker:              l,
		InstallationManager: c.InstallationManager(),
		EventDispatcher:     eventDispatcherMock{},
		AutoloadGenerator:   autoloadGeneratorMock{},
		Runtime:             rt,
		Process:             c.ProcessExecutor(),
	})
	if err != nil {
		t.Fatal(err)
	}

	result, runErr := runFixtureCommand(t, installer, test.run)
	if test.expectException != "" {
		checkException(t, test, runErr)

		return
	}
	output := bufferOutput(out)
	if runErr != nil {
		t.Fatalf("run failed: %v\n%s", runErr, output)
	}

	if result != test.expectResult {
		t.Fatalf("exit code = %d, want %d\n%s", result, test.expectResult, output)
	}

	switch expectLock := test.expectLock.(type) {
	case bool:
		if lockFile.writes > 0 {
			t.Errorf("the lock file was written:\n%v", lockFile.actual)
		}
	case *php.Array:
		if lockFile.writes == 0 {
			t.Errorf("the lock file was not written")
		} else {
			actual := lockFile.actual.(*php.Array).Clone()
			for _, key := range []string{"hash", "content-hash", "_readme", "plugin-api-version"} {
				actual.Delete(key)
			}
			for _, key := range []string{"stability-flags", "platform", "platform-dev"} {
				if v, ok := expectLock.Get(key); ok {
					if a, ok := v.(*php.Array); ok && a.Len() == 0 {
						expectLock.Set(key, php.NewObject())
					}
				}
			}
			if !assertEquals(expectLock, actual) {
				want, _ := php.JSONEncode(expectLock, php.JSONPrettyPrint)
				got, _ := php.JSONEncode(actual, php.JSONPrettyPrint)
				t.Errorf("lock file:\n%s\nwant:\n%s", got, want)
			}
		}
	}

	if test.expectInstalled != nil {
		packages, err := repositoryManager.LocalRepository().CanonicalPackages()
		if err != nil {
			t.Fatal(err)
		}
		actualInstalled := php.NewArray()
		var dumps []*php.Array
		for _, p := range packages {
			dumped, err := dumper.ArrayDumper{}.Dump(p)
			if err != nil {
				t.Fatal(err)
			}
			dumped.Delete("version_normalized")
			dumps = append(dumps, dumped)
		}
		php.SortSlice(dumps, func(a, b *php.Array) int {
			an, _ := a.Get("name")
			bn, _ := b.Get("name")

			return strings.Compare(php.ToString(an), php.ToString(bn))
		})
		for _, d := range dumps {
			actualInstalled.Append(d)
		}
		if !php.StrictEquals(test.expectInstalled, actualInstalled) {
			want, _ := php.JSONEncode(test.expectInstalled, php.JSONPrettyPrint)
			got, _ := php.JSONEncode(actualInstalled, php.JSONPrettyPrint)
			t.Errorf("installed:\n%s\nwant:\n%s", got, want)
		}
	}

	installationManager := c.InstallationManager().(*installationManagerMock)
	if got, want := strings.Join(installationManager.trace, "\n"), php.Rtrim(test.expect); got != want {
		t.Errorf("trace:\n%s\nwant:\n%s\noutput:\n%s", got, want, output)
	}

	if expectOutput != nil && *expectOutput != "" {
		output, _, _ = iniListLine.Replace(output, "__inilist__", -1)
		output, _, _ = iniListRun.Replace(output, "__inilist__\n", -1)

		if !stringMatchesFormat(t, php.Rtrim(*expectOutput), php.Rtrim(output)) {
			t.Errorf("output:\n%s\nwant (format):\n%s", php.Rtrim(output), php.Rtrim(*expectOutput))
		}
	}
}

func checkException(t *testing.T, test integrationTest, err error) {
	t.Helper()
	if test.expectException == "" {
		t.Fatalf("unexpected error: %v", err)
	}
	if err == nil {
		t.Fatalf("expected %s", test.expectException)
	}
	// InstallerTest expects rtrim(str_replace("\n", PHP_EOL, $expect)): the
	// fixtures' exception messages are written with "\n" where Composer
	// uses PHP_EOL.
	if want := php.Rtrim(strings.ReplaceAll(test.expect, "\n", php.EOL)); !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err.Error(), want)
	}
}

// runFixtureCommand runs the --RUN-- command line through a console
// application with the install and update commands of the test.
func runFixtureCommand(t *testing.T, installer *Installer, run string) (int, error) {
	t.Helper()
	if ok, _ := php.MustCompile(`{^(install|update)\b}`).IsMatch(run); !ok {
		t.Fatal("The run command only supports install and update")
	}

	var runErr error
	application := console.NewApplication("", "")
	install := console.NewCommand("install")
	install.AddOption("ignore-platform-reqs", "", console.OptionValueNone, "", nil)
	install.AddOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "", nil)
	install.AddOption("no-dev", "", console.OptionValueNone, "", nil)
	install.AddOption("dry-run", "", console.OptionValueNone, "", nil)
	install.SetCode(func(in console.Input, _ console.Output) (int, error) {
		installer.
			SetDevMode(in.Option("no-dev") == false).
			SetDryRun(in.Option("dry-run") == true).
			SetPlatformRequirementFilter(platformFilter(in)).
			SetAuditConfig(advisory.AuditConfig{Audit: false})

		code, err := installer.Run()
		runErr = err

		return code, err
	})
	if _, err := application.Add(install); err != nil {
		t.Fatal(err)
	}

	update := console.NewCommand("update")
	update.AddOption("ignore-platform-reqs", "", console.OptionValueNone, "", nil)
	update.AddOption("ignore-platform-req", "", console.OptionValueRequired|console.OptionValueIsArray, "", nil)
	update.AddOption("no-dev", "", console.OptionValueNone, "", nil)
	update.AddOption("no-install", "", console.OptionValueNone, "", nil)
	update.AddOption("dry-run", "", console.OptionValueNone, "", nil)
	update.AddOption("lock", "", console.OptionValueNone, "", nil)
	update.AddOption("with-all-dependencies", "", console.OptionValueNone, "", nil)
	update.AddOption("with-dependencies", "", console.OptionValueNone, "", nil)
	update.AddOption("minimal-changes", "", console.OptionValueNone, "", nil)
	update.AddOption("prefer-stable", "", console.OptionValueNone, "", nil)
	update.AddOption("prefer-lowest", "", console.OptionValueNone, "", nil)
	update.AddArgument("packages", console.ArgumentIsArray|console.ArgumentOptional, "", nil)
	update.SetCode(func(in console.Input, _ console.Output) (int, error) {
		packages := stringList(in.Argument("packages"))
		var filteredPackages []string
		for _, p := range packages {
			if p != "lock" && p != "nothing" && p != "mirrors" {
				filteredPackages = append(filteredPackages, p)
			}
		}
		updateMirrors := in.Option("lock") == true || len(filteredPackages) != len(packages)
		packages = filteredPackages

		updateAllowTransitiveDependencies := resolver.UpdateOnlyListed
		if in.Option("with-all-dependencies") == true {
			updateAllowTransitiveDependencies = resolver.UpdateListedWithTransitiveDeps
		} else if in.Option("with-dependencies") == true {
			updateAllowTransitiveDependencies = resolver.UpdateListedWithTransitiveDepsNoRootRequire
		}

		installer.
			SetDevMode(in.Option("no-dev") == false).
			SetUpdate(true).
			SetInstall(in.Option("no-install") == false).
			SetDryRun(in.Option("dry-run") == true).
			SetUpdateMirrors(updateMirrors).
			SetUpdateAllowList(packages)
		if _, err := installer.SetUpdateAllowTransitiveDependencies(updateAllowTransitiveDependencies); err != nil {
			return 0, err
		}
		installer.
			SetPreferStable(in.Option("prefer-stable") == true).
			SetPreferLowest(in.Option("prefer-lowest") == true).
			SetPlatformRequirementFilter(platformFilter(in)).
			SetAuditConfig(advisory.AuditConfig{Audit: false}).
			SetMinimalUpdate(in.Option("minimal-changes") == true)

		code, err := installer.Run()
		runErr = err

		return code, err
	})
	if _, err := application.Add(update); err != nil {
		t.Fatal(err)
	}

	application.SetAutoExit(false)
	input, err := console.NewStringInput(run + " -vvv")
	if err != nil {
		t.Fatal(err)
	}
	input.SetInteractive(false)
	var appOutput strings.Builder
	result, err := application.Run(input, console.NewStreamOutput(&appOutput, console.VerbosityNormal, nil, nil))
	if runErr != nil {
		return result, runErr
	}
	if err != nil {
		return result, err
	}

	return result, nil
}

func platformFilter(in console.Input) filter.PlatformRequirementFilter {
	return filter.FromIgnoreOptions(in.Option("ignore-platform-reqs") == true, stringList(in.Option("ignore-platform-req")))
}

func stringList(v any) []string {
	switch v := v.(type) {
	case []string:
		return v
	case []any:
		list := make([]string, len(v))
		for i, s := range v {
			list[i] = php.ToString(s)
		}

		return list
	case *php.Array:
		var list []string
		for _, s := range v.Values() {
			list = append(list, php.ToString(s))
		}

		return list
	}

	return nil
}

// phpEmpty is empty() on a string.
func phpEmpty(s string) bool { return s == "" || s == "0" }

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))

	return hex.EncodeToString(sum[:])
}

// assertEquals is PHPUnit's assertEquals: loose scalar comparison, arrays
// equal when they have the same keys (in any order) with equal values,
// stdClass objects only equal to objects.
func assertEquals(expected, actual any) bool {
	switch e := expected.(type) {
	case *php.Array:
		a, ok := actual.(*php.Array)
		if !ok || a.Len() != e.Len() {
			return false
		}
		for k, v := range e.All() {
			av, ok := a.Get(k)
			if !ok || !assertEquals(v, av) {
				return false
			}
		}

		return true
	case *php.Object:
		a, ok := actual.(*php.Object)
		if !ok {
			return false
		}

		return assertEquals(e.ToArray(), a.ToArray())
	}
	switch actual.(type) {
	case *php.Array, *php.Object:
		return false
	}

	return php.LooseEquals(expected, actual)
}

// stringMatchesFormat is PHPUnit's assertStringMatchesFormat.
func stringMatchesFormat(t *testing.T, format, s string) bool {
	t.Helper()
	pattern := strings.NewReplacer(
		"%%", "%",
		"%e", `\/`,
		"%s", `[^\r\n]+`,
		"%S", `[^\r\n]*`,
		"%a", `.+`,
		"%A", `.*`,
		"%w", `\s*`,
		"%i", `[+-]?\d+`,
		"%d", `\d+`,
		"%x", `[0-9a-fA-F]+`,
		"%f", `[+-]?\.?\d+\.?\d*(?:[Ee][+-]?\d+)?`,
		"%c", `.`,
	).Replace(php.PregQuote(format, "/"))
	re, err := php.Compile("/^" + pattern + "$/s")
	if err != nil {
		t.Fatal(err)
	}
	ok, err := re.IsMatch(strings.ReplaceAll(s, "\r\n", "\n"))

	return err == nil && ok
}
