package plugin

// Ports tests/Composer/Test/Plugin/PluginInstallerTest.php against the
// real shim: the fixtures' plugins run in php, and what they write to the
// IO is compared as Composer's test does. PluginInstaller's install,
// update and uninstall are its plugin manager calls (registerPackage,
// deactivatePackage then registerPackage, uninstallPackage); the
// download manager they go through first is a mock in Composer's test.

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
)

// fixtureIM is the InstallationManager mock of PluginInstallerTest:
// getInstallPath is the fixture directory named after the package.
type fixtureIM struct{ dir string }

func (m *fixtureIM) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	return filepath.Join(m.dir, p.PrettyName()), true, nil
}

func (*fixtureIM) IsPackageInstalled(repository.InstalledRepositoryInterface, pkg.PackageInterface) (bool, error) {
	return true, nil
}

func (*fixtureIM) Execute(repository.InstalledRepositoryInterface, []operation.Operation, bool, bool, bool) error {
	return nil
}

func (*fixtureIM) EnsureBinariesPresence(pkg.PackageInterface) error { return nil }

func (*fixtureIM) NotifyInstalls(io.IO) {}

func (*fixtureIM) DisablePlugins() error { return nil }

// pluginInstallerTest is PluginInstallerTest's fixture: a Composer
// instance with a local repository of given packages, the fixtures'
// packages, a BufferIO and a plugin manager.
type pluginInstallerTest struct {
	t        *testing.T
	rt       *Runtime
	io       *io.BufferIO
	composer *composer.Composer
	packages []pkg.PackageInterface
	pm       *Manager
	fixtures string
}

// setUp ports PluginInstallerTest::setUp; repoPackages are what the local
// repository holds (the mocked getPackages()).
func setUpPluginInstallerTest(t *testing.T) *pluginInstallerTest {
	t.Helper()

	fixtures, err := filepath.Abs(filepath.Join("testdata", "Plugin", "Fixtures"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := io.NewBufferIO("", console.VerbosityNormal, console.NewOutputFormatter(false))
	if err != nil {
		t.Fatal(err)
	}

	rt, _, _ := newTestRuntime(t)

	pt := &pluginInstallerTest{t: t, rt: rt, io: out, fixtures: fixtures}
	pt.packages = pt.loadFixtures()

	return pt
}

func (pt *pluginInstallerTest) loadFixtures() []pkg.PackageInterface {
	jl := loader.NewJsonLoader(loader.NewArrayLoader(pkg.NewVersionParser(), false))
	var packages []pkg.PackageInterface
	for i := 1; i <= 8; i++ {
		p, err := jl.Load(filepath.Join(pt.fixtures, "plugin-v"+string(rune('0'+i)), "composer.json"))
		if err != nil {
			pt.t.Fatal(err)
		}
		packages = append(packages, p)
	}

	return packages
}

// composerWith builds the Composer instance whose local repository holds
// repoPackages, and its plugin manager.
func (pt *pluginInstallerTest) composerWith(repoPackages ...pkg.PackageInterface) {
	pt.t.Helper()

	cfg := config.New(false, "")
	dir := pt.t.TempDir()
	if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf(
		"vendor-dir", dir+"/Fixtures/",
		"home", dir+"/Fixtures",
		"bin-dir", dir+"/Fixtures/bin",
		"allow-plugins", true,
	)), "unknown"); err != nil {
		pt.t.Fatal(err)
	}

	c := &composer.Composer{}
	c.SetConfig(cfg)
	ed := eventdispatcher.New(c, pt.io, nil)
	ed.SetScriptRuntime(pt.rt)
	c.SetEventDispatcher(ed)
	rm := repository.NewRepositoryManager(pt.io, cfg, nil, ed, nil)
	repo, err := repository.NewInstalledArrayRepository(nil)
	if err != nil {
		pt.t.Fatal(err)
	}
	for _, p := range repoPackages {
		if err := repo.AddPackage(pkg.Clone(p)); err != nil {
			pt.t.Fatal(err)
		}
	}
	rm.SetLocalRepository(repo)
	c.SetRepositoryManager(rm)
	im := &fixtureIM{dir: pt.fixtures}
	c.SetInstallationManager(im)
	c.SetAutoloadGenerator(autoload.NewGenerator(ed, pt.io))
	c.SetPackage(pkg.NewRootPackage("dummy/root", "1.0.0.0", "1.0.0"))
	lockFile, err := json.NewFile(util.GetDevNull(), nil, pt.io)
	if err != nil {
		pt.t.Fatal(err)
	}
	l, err := locker.New(pt.io, lockFile, im, "{}", nil)
	if err != nil {
		pt.t.Fatal(err)
	}
	c.SetLocker(l)
	pt.composer = c

	pm, err := NewManager(pt.rt, pt.io, c, nil, composer.PluginsEnabled)
	if err != nil {
		pt.t.Fatal(err)
	}
	c.SetPluginManager(pm)
	pt.pm = pm
}

func (pt *pluginInstallerTest) start() {
	pt.t.Helper()
	if err := pt.rt.Start("test"); err != nil {
		pt.t.Fatal(err)
	}
}

// install is PluginInstaller::install's plugin part.
func (pt *pluginInstallerTest) install(p pkg.PackageInterface) {
	pt.t.Helper()
	if err := pt.pm.RegisterPackage(p, true, false); err != nil {
		pt.t.Fatal(err)
	}
}

// update is PluginInstaller::update's plugin part.
func (pt *pluginInstallerTest) update(initial, target pkg.PackageInterface) {
	pt.t.Helper()
	if err := pt.pm.DeactivatePackage(initial); err != nil {
		pt.t.Fatal(err)
	}
	if err := pt.pm.RegisterPackage(target, true, false); err != nil {
		pt.t.Fatal(err)
	}
}

type pluginInfo struct {
	key   int
	class string
	props *php.Array
}

func (pt *pluginInstallerTest) plugins() []pluginInfo {
	pt.t.Helper()
	pt.start()
	v, err := pt.rt.Call("test.plugins", php.ArrayOf("pm", pt.pm))
	if err != nil {
		pt.t.Fatal(err)
	}
	var out []pluginInfo
	for _, item := range v.(*php.Array).Values() {
		a := item.(*php.Array)
		key, _ := a.Get("key")
		class, _ := a.GetString("class")
		props, _ := a.GetArray("props")
		out = append(out, pluginInfo{key: int(php.ToInt(key)), class: class, props: props})
	}

	return out
}

func prop(p pluginInfo, name string) string {
	v, _ := p.props.Get(name)

	return php.ToString(v)
}

func (pt *pluginInstallerTest) output() string { return strings.ReplaceAll(pt.io.Output(), "\r", "") }

func TestPluginInstaller_InstallNewPlugin(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith()
	if err := pt.pm.LoadInstalledPlugins(); err != nil {
		t.Fatal(err)
	}
	pt.install(pt.packages[0])

	plugins := pt.plugins()
	if len(plugins) != 1 || prop(plugins[0], "version") != "installer-v1" {
		t.Errorf("plugins = %+v", plugins)
	}
	if got := pt.output(); got != "activate v1\n" {
		t.Errorf("output %q", got)
	}
}

func TestPluginInstaller_InstallPluginWithRootPackageHavingFilesAutoload(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith()
	if err := pt.pm.LoadInstalledPlugins(); err != nil {
		t.Fatal(err)
	}

	pt.composer.AutoloadGenerator().SetDevMode(true)
	files := php.ArrayOf("files", php.ListOf(filepath.Join(pt.fixtures, "files_autoload_which_should_not_run.php")))
	pt.composer.Package().SetAutoload(files)
	pt.composer.Package().SetDevAutoload(files.Clone())
	pt.install(pt.packages[0])

	plugins := pt.plugins()
	if got := pt.output(); got != "activate v1\n" {
		t.Errorf("output %q", got)
	}
	if len(plugins) != 1 || prop(plugins[0], "version") != "installer-v1" {
		t.Errorf("plugins = %+v", plugins)
	}
}

func TestPluginInstaller_InstallMultiplePlugins(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith(pt.packages[3])
	if err := pt.pm.LoadInstalledPlugins(); err != nil {
		t.Fatal(err)
	}
	pt.install(pt.packages[3])

	plugins := pt.plugins()
	if len(plugins) != 2 ||
		prop(plugins[0], "name") != "plugin1" || prop(plugins[0], "version") != "installer-v4" ||
		prop(plugins[1], "name") != "plugin2" || prop(plugins[1], "version") != "installer-v4" {
		t.Errorf("plugins = %+v", plugins)
	}
	if got := pt.output(); got != "activate v4-plugin1\nactivate v4-plugin2\n" {
		t.Errorf("output %q", got)
	}
}

func TestPluginInstaller_UpgradeWithNewClassName(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith(pt.packages[0])
	if err := pt.pm.LoadInstalledPlugins(); err != nil {
		t.Fatal(err)
	}
	local, _ := pt.composer.RepositoryManager().LocalRepository().Packages()
	pt.update(local[0], pt.packages[1])

	plugins := pt.plugins()
	if len(plugins) != 1 || plugins[0].key != 1 || prop(plugins[0], "version") != "installer-v2" {
		t.Errorf("plugins = %+v", plugins)
	}
	if got := pt.output(); got != "activate v1\ndeactivate v1\nactivate v2\n" {
		t.Errorf("output %q", got)
	}
}

func TestPluginInstaller_Uninstall(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith(pt.packages[0])
	if err := pt.pm.LoadInstalledPlugins(); err != nil {
		t.Fatal(err)
	}
	local, _ := pt.composer.RepositoryManager().LocalRepository().Packages()
	if err := pt.pm.UninstallPackage(local[0]); err != nil {
		t.Fatal(err)
	}

	if plugins := pt.plugins(); len(plugins) != 0 {
		t.Errorf("plugins = %+v", plugins)
	}
	if got := pt.output(); got != "activate v1\ndeactivate v1\nuninstall v1\n" {
		t.Errorf("output %q", got)
	}
}

func TestPluginInstaller_UpgradeWithSameClassName(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith(pt.packages[1])
	if err := pt.pm.LoadInstalledPlugins(); err != nil {
		t.Fatal(err)
	}
	local, _ := pt.composer.RepositoryManager().LocalRepository().Packages()
	pt.update(local[0], pt.packages[2])

	plugins := pt.plugins()
	if len(plugins) != 1 || plugins[0].key != 1 || prop(plugins[0], "version") != "installer-v3" {
		t.Errorf("plugins = %+v", plugins)
	}
	if got := pt.output(); got != "activate v2\ndeactivate v2\nactivate v3\n" {
		t.Errorf("output %q", got)
	}
}

func TestPluginInstaller_RegisterPluginOnlyOneTime(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith()
	if err := pt.pm.LoadInstalledPlugins(); err != nil {
		t.Fatal(err)
	}
	pt.install(pt.packages[0])
	pt.install(pkg.Clone(pt.packages[0]))

	plugins := pt.plugins()
	if len(plugins) != 1 || prop(plugins[0], "version") != "installer-v1" {
		t.Errorf("plugins = %+v", plugins)
	}
	if got := pt.output(); got != "activate v1\n" {
		t.Errorf("output %q", got)
	}
}

// pluginsWithAPIVersion ports setPluginApiVersionWithPlugins: a new plugin
// manager whose getPluginApiVersion() is version, over a local repository
// holding the composer-plugin-api package and plugins; it returns how many
// plugins loaded.
func (pt *pluginInstallerTest) pluginsWithAPIVersion(version string, plugins ...pkg.PackageInterface) int {
	pt.t.Helper()

	parser := pkg.NewVersionParser()
	normalized, err := parser.Normalize(version)
	if err != nil {
		pt.t.Fatal(err)
	}
	api := pkg.NewCompletePackage("composer-plugin-api", normalized, version)
	pt.composerWith(append([]pkg.PackageInterface{api}, plugins...)...)
	pt.pm.SetPluginAPIVersion(version)
	if err := pt.pm.LoadInstalledPlugins(); err != nil {
		pt.t.Fatal(err)
	}

	return len(pt.plugins())
}

func TestPluginInstaller_StarPluginVersionWorksWithAnyAPIVersion(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	for _, version := range []string{"1.0.0", "1.9.9", "2.0.0-dev", "100.0.0-stable"} {
		if n := pt.pluginsWithAPIVersion(version, pt.packages[4]); n != 1 {
			t.Errorf("API %s: %d plugins", version, n)
		}
	}
}

func TestPluginInstaller_PluginConstraintWorksOnlyWithCertainAPIVersion(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	for version, want := range map[string]int{"1.0.0": 0, "1.1.9": 0, "1.2.0": 1, "1.9.9": 1} {
		if n := pt.pluginsWithAPIVersion(version, pt.packages[5]); n != want {
			t.Errorf("API %s: %d plugins, want %d", version, n, want)
		}
	}
}

func TestPluginInstaller_PluginRangeConstraintsWorkOnlyWithCertainAPIVersion(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	for version, want := range map[string]int{"1.0.0": 0, "3.0.0": 1, "5.5.0": 0} {
		if n := pt.pluginsWithAPIVersion(version, pt.packages[6]); n != want {
			t.Errorf("API %s: %d plugins, want %d", version, n, want)
		}
	}
}

func (pt *pluginInstallerTest) capability(params *php.Array) (any, error) {
	pt.t.Helper()
	pt.start()
	params.Set("pm", pt.pm)

	return pt.rt.Call("test.capability", params)
}

func TestPluginInstaller_CommandProviderCapability(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith(pt.packages[7])
	if err := pt.pm.LoadInstalledPlugins(); err != nil {
		t.Fatal(err)
	}

	v, err := pt.capability(php.ArrayOf("commands", true, "composer", pt.rt.value(pt.composer), "io", pt.rt.value(io.IO(pt.io))))
	if err != nil {
		t.Fatal(err)
	}
	caps := v.(*php.Array)
	if caps.Len() != 1 {
		t.Fatalf("capabilities %v", caps)
	}
	c, _ := caps.Get(0)
	ca := c.(*php.Array)
	for _, k := range []string{"provider", "baseCommand"} {
		if v, _ := ca.Get(k); v != true {
			t.Errorf("%s = %v", k, v)
		}
	}
	if n, _ := ca.Get("commands"); n != int64(1) {
		t.Errorf("commands = %v", n)
	}
}

func TestPluginInstaller_IncapablePluginIsCorrectlyDetected(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith()
	v, err := pt.capability(php.ArrayOf("incapable", true, "capabilities", php.NewArray(), "api", `Fake\Ability`))
	if err != nil || v != nil {
		t.Errorf("capability = %v, %v", v, err)
	}
}

func TestPluginInstaller_CapabilityImplementsComposerPluginApiClassAndIsConstructedWithArgs(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith()
	api := `Composer\Plugin\Capability\Capability`
	v, err := pt.capability(php.ArrayOf(
		"capabilities", php.ArrayOf(api, `Composer\Test\Plugin\Mock\Capability`),
		"api", api,
		"args", php.ArrayOf("a", int64(1), "b", int64(2)),
	))
	if err != nil {
		t.Fatal(err)
	}
	a := v.(*php.Array)
	if class, _ := a.GetString("class"); class != `Composer\Test\Plugin\Mock\Capability` {
		t.Errorf("class %s", class)
	}
	for _, k := range []string{"api", "argsOk"} {
		if v, _ := a.Get(k); v != true {
			t.Errorf("%s = %v", k, v)
		}
	}
}

func TestPluginInstaller_QueryingWithInvalidCapabilityClassNameThrows(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith()
	api := `Composer\Plugin\Capability\Capability`
	for name, tc := range map[string]struct {
		value  any
		expect string
	}{
		"null":                            {nil, "UnexpectedValueException"},
		"empty":                           {"", "UnexpectedValueException"},
		"zero":                            {int64(0), "UnexpectedValueException"},
		"1000":                            {int64(1000), "UnexpectedValueException"},
		"blank":                           {"   ", "UnexpectedValueException"},
		"list":                            {php.ListOf(int64(1)), "UnexpectedValueException"},
		"empty list":                      {php.NewArray(), "UnexpectedValueException"},
		"stdClass object":                 {php.NewObject(), "UnexpectedValueException"},
		"stdClass":                        {`\stdClass`, "RuntimeException"},
		"NonExistentClassLikeMiddleClass": {"NonExistentClassLikeMiddleClass", "RuntimeException"},
	} {
		_, err := pt.capability(php.ArrayOf("capabilities", php.ArrayOf(api, tc.value), "api", api))
		pe, ok := errors.AsType[*PHPException](err)
		if !ok || pe.Class != tc.expect {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPluginInstaller_QueryingNonProvidedCapabilityReturnsNullSafely(t *testing.T) {
	requirePHP(t)

	pt := setUpPluginInstallerTest(t)
	pt.composerWith()
	v, err := pt.capability(php.ArrayOf("capabilities", php.NewArray(), "api", `Composer\Plugin\Capability\MadeUpCapability`))
	if err != nil || v != nil {
		t.Errorf("capability = %v, %v", v, err)
	}
}
