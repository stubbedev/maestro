package installer

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
)

// recordingPluginManager is a PluginManager mock.
type recordingPluginManager struct {
	rec         *recorder
	disabled    bool
	registerErr error
	allowedErr  error
}

func (m *recordingPluginManager) RegisterPackage(p pkg.PackageInterface, failOnMissingClasses, isGlobalPlugin bool) error {
	m.rec.add("registerPackage %s %v %v", p.String(), failOnMissingClasses, isGlobalPlugin)

	return m.registerErr
}

func (m *recordingPluginManager) DeactivatePackage(p pkg.PackageInterface) error {
	m.rec.add("deactivatePackage %s", p.String())

	return nil
}

func (m *recordingPluginManager) UninstallPackage(p pkg.PackageInterface) error {
	m.rec.add("uninstallPackage %s", p.String())

	return nil
}

func (m *recordingPluginManager) DisablePlugins() { m.rec.add("disablePlugins") }

func (m *recordingPluginManager) ArePluginsDisabled(typ string) bool {
	m.rec.add("arePluginsDisabled %s", typ)

	return m.disabled
}

func (m *recordingPluginManager) IsPluginAllowed(name string, isGlobalPlugin, optional bool) (bool, error) {
	m.rec.add("isPluginAllowed %s %v %v", name, isGlobalPlugin, optional)

	return m.allowedErr == nil, m.allowedErr
}

func newPluginFixture(t *testing.T) (*PluginInstaller, *recordingPluginManager, *mockDM, *libraryFixture) {
	t.Helper()

	f, _ := newLibraryFixture(t)
	pm := &recordingPluginManager{rec: &recorder{}}
	c := &fullTestComposer{testComposer: *f.composer, pm: pm}

	pi, err := NewPluginInstaller(newBufferIO(t), c, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	pi.downloadManager = f.dm

	return pi, pm, f.dm, f
}

func pluginPackage(class any) *pkg.Package {
	p := newPackage("foo/plugin", "1.0.0")
	p.SetType("composer-plugin")

	extra := php.NewArray()
	if class != nil {
		extra.Set("class", class)
	}

	p.SetExtra(extra)

	return p
}

func TestPluginInstaller_Supports(t *testing.T) {
	pi, _, _, _ := newPluginFixture(t)

	for typ, want := range map[string]bool{"composer-plugin": true, "composer-installer": true, "library": false} {
		if got, _ := pi.Supports(typ); got != want {
			t.Errorf("Supports(%s) = %v", typ, got)
		}
	}
}

func TestPluginInstaller_DownloadNeedsClass(t *testing.T) {
	pi, _, dm, _ := newPluginFixture(t)

	_, err := pi.Download(pluginPackage(nil), nil)

	var uve *util.UnexpectedValueError
	if !errors.As(err, &uve) || uve.Message != "Error while installing foo/plugin, composer-plugin packages should have a class defined in their extra key to be usable." {
		t.Errorf("err = %v", err)
	}

	if _, err := pi.Download(pluginPackage("Foo\\Plugin"), nil); err != nil {
		t.Fatal(err)
	}

	if len(dm.rec.list()) != 1 {
		t.Errorf("download manager calls: %v", dm.rec.list())
	}
}

func TestPluginInstaller_PrepareChecksAllowed(t *testing.T) {
	pi, pm, _, _ := newPluginFixture(t)

	p := pluginPackage("Foo\\Plugin")
	p.Extra().Set("plugin-optional", true)

	if _, err := pi.Prepare(operation.TypeInstall, p, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := pi.Prepare(operation.TypeUninstall, p, nil); err != nil {
		t.Fatal(err)
	}

	pm.allowedErr = &util.RuntimeError{Message: "not allowed"}

	if _, err := pi.Prepare(operation.TypeUpdate, p, nil); err == nil || err.Error() != "not allowed" {
		t.Errorf("err = %v", err)
	}

	equalCalls(t, pm.rec.list(), []string{
		"arePluginsDisabled local", "isPluginAllowed foo/plugin false true",
		"arePluginsDisabled local", "isPluginAllowed foo/plugin false true",
	})
}

func TestPluginInstaller_InstallRegisters(t *testing.T) {
	pi, pm, _, _ := newPluginFixture(t)
	p := pluginPackage("Foo\\Plugin")
	repo := newMockRepo(t)

	promise, err := pi.Install(repo, p)
	if err != nil {
		t.Fatal(err)
	}

	if err := Await(nil, promise); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, pm.rec.list(), []string{"registerPackage foo/plugin-1.0.0.0 true false"})

	if repo.rec.count("addPackage foo/plugin-1.0.0.0") != 1 {
		t.Errorf("repository calls: %v", repo.rec.list())
	}
}

func TestPluginInstaller_InstallRollsBack(t *testing.T) {
	pi, pm, dm, _ := newPluginFixture(t)
	pm.registerErr = &util.RuntimeError{Message: "broken plugin"}

	io := newBufferIO(t)
	pi.io = io

	p := pluginPackage("Foo\\Plugin")
	repo := newMockRepo(t, false, false, true)

	promise, err := pi.Install(repo, p)
	if err != nil {
		t.Fatal(err)
	}

	if err := Await(nil, promise); err == nil || err.Error() != "broken plugin" {
		t.Fatalf("err = %v", err)
	}

	if got := php.NormalizeEOL(io.Output()); got != "Plugin initialization failed (broken plugin), uninstalling plugin\n" {
		t.Errorf("output %q", got)
	}

	// the rollback is LibraryInstaller::uninstall, not the plugin
	// manager's uninstall
	equalCalls(t, pm.rec.list(), []string{"registerPackage foo/plugin-1.0.0.0 true false"})

	if dm.rec.count("remove foo/plugin-1.0.0.0") != 1 || repo.rec.count("removePackage foo/plugin-1.0.0.0") != 1 {
		t.Errorf("rollback calls: %v %v", dm.rec.list(), repo.rec.list())
	}
}

func TestPluginInstaller_UpdateAndUninstall(t *testing.T) {
	pi, pm, _, _ := newPluginFixture(t)

	initial := pluginPackage("Foo\\Plugin")
	target := newPackage("foo/plugin", "2.0.0")
	target.SetType("composer-plugin")

	repo := newMockRepo(t, true, false, true)

	promise, err := pi.Update(repo, initial, target)
	if err != nil {
		t.Fatal(err)
	}

	if err := Await(nil, promise); err != nil {
		t.Fatal(err)
	}

	promise, err = pi.Uninstall(repo, target)
	if err != nil {
		t.Fatal(err)
	}

	if err := Await(nil, promise); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, pm.rec.list(), []string{
		"deactivatePackage foo/plugin-1.0.0.0",
		"registerPackage foo/plugin-2.0.0.0 true false",
		"uninstallPackage foo/plugin-2.0.0.0",
	})
}

func TestPluginInstaller_PartialComposer(t *testing.T) {
	f, _ := newLibraryFixture(t)

	pi, err := NewPluginInstaller(newBufferIO(t), f.composer, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	var le *util.LogicError
	if err := pi.DisablePlugins(); !errors.As(err, &le) {
		t.Errorf("err = %v, want LogicException", err)
	}
}
