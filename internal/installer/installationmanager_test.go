package installer

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
)

func supportsType(typ string) func(string) bool {
	return func(t string) bool { return t == typ }
}

func TestInstallationManager_AddGetInstaller(t *testing.T) {
	installer := newMockInstaller(nil, supportsType("vendor"))

	manager := NewManager(nil, newBufferIO(t), nil)
	manager.AddInstaller(installer)

	if got, err := manager.Installer("vendor"); err != nil || got != installer {
		t.Fatalf("Installer(vendor) = %v, %v", got, err)
	}

	_, err := manager.Installer("unregistered")

	var iae *util.InvalidArgumentError
	if !errors.As(err, &iae) || iae.Message != "Unknown installer type: unregistered" {
		t.Errorf("err = %v", err)
	}

	if n := installer.rec.count("supports"); n != 2 {
		t.Errorf("supports called %d times, want 2", n)
	}
}

func TestInstallationManager_AddRemoveInstaller(t *testing.T) {
	installer := newMockInstaller(nil, supportsType("vendor"))
	installer2 := newMockInstaller(nil, supportsType("vendor"))

	manager := NewManager(nil, newBufferIO(t), nil)

	manager.AddInstaller(installer)
	if got, _ := manager.Installer("vendor"); got != installer {
		t.Fatal("want installer")
	}

	manager.AddInstaller(installer2)
	if got, _ := manager.Installer("vendor"); got != installer2 {
		t.Fatal("want installer2")
	}

	manager.RemoveInstaller(installer2)
	if got, _ := manager.Installer("vendor"); got != installer {
		t.Fatal("want installer")
	}

	if n := installer.rec.count("supports"); n != 2 {
		t.Errorf("installer supports called %d times, want 2", n)
	}

	if n := installer2.rec.count("supports"); n != 1 {
		t.Errorf("installer2 supports called %d times, want 1", n)
	}
}

func TestInstallationManager_Execute(t *testing.T) {
	rec := &recorder{}
	installer := newMockInstaller(rec, supportsType("library"))

	manager := NewManager(nil, newBufferIO(t), nil)
	manager.AddInstaller(NewNoopInstaller())
	manager.AddInstaller(installer)

	p := newPackage("foo/bar", "1.0.0")
	installOperation := operation.NewInstallOperation(p)
	removeOperation := operation.NewUninstallOperation(p)
	updateOperation := operation.NewUpdateOperation(p, p)

	repo := newMockRepo(t)

	if err := manager.Execute(repo, []operation.Operation{installOperation, removeOperation, updateOperation}, true, true, false); err != nil {
		t.Fatal(err)
	}

	for _, call := range []string{"install foo/bar-1.0.0.0", "uninstall foo/bar-1.0.0.0", "update foo/bar-1.0.0.0 foo/bar-1.0.0.0"} {
		if n := rec.count(call); n != 1 {
			t.Errorf("%s called %d times, want 1", call, n)
		}
	}
}

func TestInstallationManager_Install(t *testing.T) {
	installer := newMockInstaller(nil, supportsType("library"))
	manager := NewManager(nil, newBufferIO(t), nil)
	manager.AddInstaller(installer)

	p := newPackage("foo/bar", "1.0.0")
	repo := newMockRepo(t)

	if _, err := manager.Install(repo, operation.NewInstallOperation(p)); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, installer.rec.list(), []string{"supports library", "install foo/bar-1.0.0.0"})
}

func TestInstallationManager_UpdateWithEqualTypes(t *testing.T) {
	installer := newMockInstaller(nil, supportsType("library"))
	manager := NewManager(nil, newBufferIO(t), nil)
	manager.AddInstaller(installer)

	initial := newPackage("foo/bar", "1.0.0")
	target := newPackage("foo/bar", "2.0.0")
	repo := newMockRepo(t)

	if _, err := manager.Update(repo, operation.NewUpdateOperation(initial, target)); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, installer.rec.list(), []string{"supports library", "update foo/bar-1.0.0.0 foo/bar-2.0.0.0"})
}

func TestInstallationManager_UpdateWithNotEqualTypes(t *testing.T) {
	libInstaller := newMockInstaller(nil, func(string) bool { return true })
	bundleInstaller := newMockInstaller(nil, supportsType("bundles"))

	manager := NewManager(nil, newBufferIO(t), nil)
	manager.AddInstaller(libInstaller)
	manager.AddInstaller(bundleInstaller)

	initial := newPackage("foo/bar", "1.0.0")
	initial.SetType("library")

	target := newPackage("foo/bar", "2.0.0")
	target.SetType("bundles")

	repo := newMockRepo(t)

	promise, err := manager.Update(repo, operation.NewUpdateOperation(initial, target))
	if err != nil {
		t.Fatal(err)
	}

	if err := Await(nil, promise); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, bundleInstaller.rec.list(), []string{"supports library", "supports bundles", "install foo/bar-2.0.0.0"})
	equalCalls(t, libInstaller.rec.list(), []string{"supports library", "uninstall foo/bar-1.0.0.0"})
}

func TestInstallationManager_Uninstall(t *testing.T) {
	installer := newMockInstaller(nil, supportsType("library"))
	manager := NewManager(nil, newBufferIO(t), nil)
	manager.AddInstaller(installer)

	p := newPackage("foo/bar", "1.0.0")
	repo := newMockRepo(t)

	if _, err := manager.Uninstall(repo, operation.NewUninstallOperation(p)); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, installer.rec.list(), []string{"supports library", "uninstall foo/bar-1.0.0.0"})
}

func TestInstallationManager_InstallBinary(t *testing.T) {
	f, _ := newLibraryFixture(t)
	bi := &mockBinaries{rec: &recorder{}}
	library := f.installer(t, pkg.Str("library"), nil, bi)

	manager := NewManager(nil, newBufferIO(t), nil)
	manager.AddInstaller(library)

	p := newPackage("foo/bar", "1.0.0")

	if err := manager.EnsureBinariesPresence(p); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, bi.rec.list(), []string{"installBinaries foo/bar-1.0.0.0 " + f.vendorDir + "/foo/bar false"})

	// no installer for the type: nothing happens
	unknown := newPackage("foo/baz", "1.0.0")
	unknown.SetType("unknown")

	if err := NewManager(nil, newBufferIO(t), nil).EnsureBinariesPresence(unknown); err != nil {
		t.Errorf("err = %v", err)
	}
}

// eventRecorder is an EventDispatcher recording package events.
type eventRecorder struct{ rec *recorder }

func (e *eventRecorder) DispatchPackageEvent(eventName string, _ bool, _ pkg.Repository, _ []eventdispatcher.Operation, op eventdispatcher.Operation) (int, error) {
	e.rec.add("event %s %s", eventName, op.String())

	return 0, nil
}

// installedPackage gives a package an installation source, so that the
// manager runs its cleanup step.
func installedPackage(name, version, typ string) *pkg.Package {
	p := newPackage(name, version)
	p.SetType(typ)
	p.SetInstallationSource(pkg.Str("dist"))

	return p
}

// TestInstallationManager_ExecuteOrder checks the order Composer executes
// in: downloads of a batch first, then per operation the PRE event,
// prepare, the operation, cleanup and a repository write, then the POST
// events; plugins and installers in batches of their own; plugins
// modifying downloads downloaded and installed before anything else is
// downloaded.
func TestInstallationManager_ExecuteOrder(t *testing.T) {
	rec := &recorder{}
	installer := newMockInstaller(rec, func(string) bool { return true })

	io, err := mio.NewBufferIO("", console.VerbosityDebug, nil) // alias operations are shown
	if err != nil {
		t.Fatal(err)
	}

	manager := NewManager(nil, io, &eventRecorder{rec: rec})
	manager.AddInstaller(installer)

	a := installedPackage("a/a", "1.0.0", "library")
	b := installedPackage("b/b", "1.0.0", "library")
	plugin := installedPackage("p/plugin", "1.0.0", "composer-plugin")
	dlPlugin := installedPackage("p/downloads", "1.0.0", "composer-plugin")
	dlPlugin.SetExtra(php.ArrayOf("plugin-modifies-downloads", true))
	c := installedPackage("c/c", "1.0.0", "library")
	alias := pkg.NewAliasPackage(c, "2.0.0.0", "2.0.0")

	repo := newMockRepo(t)
	repo.rec = rec

	ops := []operation.Operation{
		operation.NewInstallOperation(a),
		operation.NewUninstallOperation(b),
		operation.NewInstallOperation(plugin),
		operation.NewInstallOperation(dlPlugin),
		operation.NewInstallOperation(c),
		operation.NewMarkAliasInstalledOperation(alias),
	}

	if err := manager.Execute(repo, ops, true, true, false); err != nil {
		t.Fatal(err)
	}

	var calls []string

	for _, call := range rec.list() {
		if !strings.HasPrefix(call, "supports") && !strings.HasPrefix(call, "hasPackage") {
			calls = append(calls, call)
		}
	}

	equalCalls(t, calls, []string{
		// batch 1: everything before the download-modifying plugin
		"download a/a-1.0.0.0",
		"download p/plugin-1.0.0.0",
		"event pre-package-install Installing <info>a/a</info> (<comment>1.0.0</comment>)",
		"prepare install a/a-1.0.0.0",
		"install a/a-1.0.0.0",
		"cleanup install a/a-1.0.0.0",
		"write true",
		"event pre-package-uninstall Removing <info>b/b</info> (<comment>1.0.0</comment>)",
		"prepare uninstall b/b-1.0.0.0",
		"uninstall b/b-1.0.0.0",
		"cleanup uninstall b/b-1.0.0.0",
		"write true",
		"event post-package-install Installing <info>a/a</info> (<comment>1.0.0</comment>)",
		"event post-package-uninstall Removing <info>b/b</info> (<comment>1.0.0</comment>)",
		"event pre-package-install Installing <info>p/plugin</info> (<comment>1.0.0</comment>)",
		"prepare install p/plugin-1.0.0.0",
		"install p/plugin-1.0.0.0",
		"cleanup install p/plugin-1.0.0.0",
		"write true",
		"event post-package-install Installing <info>p/plugin</info> (<comment>1.0.0</comment>)",
		// batch 2: the download-modifying plugin
		"download p/downloads-1.0.0.0",
		"event pre-package-install Installing <info>p/downloads</info> (<comment>1.0.0</comment>)",
		"prepare install p/downloads-1.0.0.0",
		"install p/downloads-1.0.0.0",
		"cleanup install p/downloads-1.0.0.0",
		"write true",
		"event post-package-install Installing <info>p/downloads</info> (<comment>1.0.0</comment>)",
		// batch 3
		"download c/c-1.0.0.0",
		"event pre-package-install Installing <info>c/c</info> (<comment>1.0.0</comment>)",
		"prepare install c/c-1.0.0.0",
		"install c/c-1.0.0.0",
		"cleanup install c/c-1.0.0.0",
		"write true",
		"addPackage c/c-2.0.0.0 (alias of 1.0.0.0)",
		"event post-package-install Installing <info>c/c</info> (<comment>1.0.0</comment>)",
		// the final write
		"write true",
	})

	if got := io.Output(); got != "  - Marking c/c (2.0.0) as installed, alias of c/c (1.0.0)\n" {
		t.Errorf("output %q", got)
	}
}

// TestInstallationManager_ExecuteAsync checks that operations whose
// promises settle out of order still have their callbacks (here the
// repository writes and the POST events) run in operation order, and that
// the downloads ran in parallel.
func TestInstallationManager_ExecuteAsync(t *testing.T) {
	rec := &recorder{}
	installer := newMockInstaller(rec, func(string) bool { return true })

	const n = 8

	started := make(chan string, n)
	release := map[string]func(string){}

	installer.result = func(method string, p pkg.PackageInterface) (*Promise, error) {
		if method != "download" && method != "install" {
			return Resolved(), nil
		}

		promise, resolve, _ := util.NewDeferred[string](nil)
		key := method + " " + p.Name()

		rec.mu.Lock()
		release[key] = resolve
		rec.mu.Unlock()

		started <- key

		return Of(promise), nil
	}

	manager := NewManager(nil, newBufferIO(t), &eventRecorder{rec: rec})
	manager.AddInstaller(installer)

	var ops []operation.Operation

	for i := range n {
		ops = append(ops, operation.NewInstallOperation(installedPackage("v/p"+string(rune('a'+i)), "1.0.0", "library")))
	}

	repo := newMockRepo(t)
	repo.rec = rec

	// settle the downloads, then the installs, in reverse order
	go func() {
		for range 2 { // the downloads, then the installs
			var keys []string
			for range n {
				keys = append(keys, <-started)
			}

			time.Sleep(10 * time.Millisecond)

			for _, key := range slices.Backward(keys) {
				rec.mu.Lock()
				resolve := release[key]
				rec.mu.Unlock()
				resolve("")
				time.Sleep(time.Millisecond)
			}
		}
	}()

	if err := manager.Execute(repo, ops, false, true, false); err != nil {
		t.Fatal(err)
	}

	var got []string

	for _, call := range rec.list() {
		if strings.HasPrefix(call, "write") || strings.HasPrefix(call, "cleanup") || strings.HasPrefix(call, "event post") {
			got = append(got, call)
		}
	}

	var want []string
	for i := range n {
		want = append(want, "cleanup install v/p"+string(rune('a'+i))+"-1.0.0.0", "write false")
	}

	for i := range n {
		want = append(want, "event post-package-install Installing <info>v/p"+string(rune('a'+i))+"</info> (<comment>1.0.0</comment>)")
	}

	want = append(want, "write false")

	equalCalls(t, got, want)
}

// TestInstallationManager_ExecuteFailure checks the failure message, that
// every started operation is cleaned up and that the error is returned.
func TestInstallationManager_ExecuteFailure(t *testing.T) {
	rec := &recorder{}
	installer := newMockInstaller(rec, func(string) bool { return true })
	installer.result = func(method string, p pkg.PackageInterface) (*Promise, error) {
		if method == "install" && p.Name() == "b/b" {
			return Rejected(&util.RuntimeError{Message: "boom"}), nil
		}

		return Resolved(), nil
	}

	io := newBufferIO(t)
	manager := NewManager(nil, io, nil)
	manager.AddInstaller(installer)

	a := installedPackage("a/a", "1.0.0", "library")
	b := installedPackage("b/b", "1.0.0", "library")
	c := newPackage("c/c", "1.0.0") // no installation source: no cleanup

	repo := newMockRepo(t)
	repo.rec = rec

	err := manager.Execute(repo, []operation.Operation{
		operation.NewInstallOperation(a),
		operation.NewInstallOperation(b),
		operation.NewInstallOperation(c),
	}, true, true, false)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v", err)
	}

	if got := io.Output(); got != "    Install of b/b failed\n" {
		t.Errorf("output %q", got)
	}

	var cleanups []string

	for _, call := range rec.list() {
		if strings.HasPrefix(call, "cleanup") || strings.HasPrefix(call, "write") {
			cleanups = append(cleanups, call)
		}
	}

	equalCalls(t, cleanups, []string{
		"cleanup install a/a-1.0.0.0",
		"write true",
		"write true",
		// runCleanup
		"cleanup install a/a-1.0.0.0",
		"cleanup install b/b-1.0.0.0",
	})
}

func TestInstallationManager_ExecuteDownloadOnly(t *testing.T) {
	rec := &recorder{}
	installer := newMockInstaller(rec, func(string) bool { return true })

	manager := NewManager(nil, newBufferIO(t), &eventRecorder{rec: rec})
	manager.AddInstaller(installer)

	a := installedPackage("a/a", "1.0.0", "library")
	repo := newMockRepo(t)
	repo.rec = rec

	if err := manager.Execute(repo, []operation.Operation{operation.NewInstallOperation(a)}, true, true, true); err != nil {
		t.Fatal(err)
	}

	var calls []string

	for _, call := range rec.list() {
		if !strings.HasPrefix(call, "supports") {
			calls = append(calls, call)
		}
	}

	equalCalls(t, calls, []string{"download a/a-1.0.0.0", "cleanup install a/a-1.0.0.0"})
}

func TestInstallationManager_IsPackageInstalled(t *testing.T) {
	installer := newMockInstaller(nil, func(string) bool { return true })
	manager := NewManager(nil, newBufferIO(t), nil)
	manager.AddInstaller(installer)

	p := newPackage("a/a", "1.0.0")
	alias := pkg.NewAliasPackage(p, "2.0.0.0", "2.0.0")

	repo := newMockRepo(t, false)
	if ok, err := manager.IsPackageInstalled(repo, alias); err != nil || ok {
		t.Errorf("alias not in repo: %v, %v", ok, err)
	}

	repo = newMockRepo(t, true)
	if ok, err := manager.IsPackageInstalled(repo, alias); err != nil || !ok {
		t.Errorf("alias in repo: %v, %v", ok, err)
	}

	equalCalls(t, installer.rec.list(), []string{"supports library", "isInstalled a/a-1.0.0.0"})
}

func TestInstallationManager_InstallPathAndDisablePlugins(t *testing.T) {
	manager := NewManager(nil, newBufferIO(t), nil)
	manager.AddInstaller(NewMetapackageInstaller(newBufferIO(t)))

	p := newPackage("a/a", "1.0.0")
	p.SetType("metapackage")

	if path, ok, err := manager.InstallPath(p); err != nil || ok || path != "" {
		t.Errorf("InstallPath = %q, %v, %v; want null", path, ok, err)
	}

	pm := &recordingPluginManager{rec: &recorder{}}
	f, _ := newLibraryFixture(t)
	c := &fullTestComposer{testComposer: *f.composer, pm: pm}

	pi, err := NewPluginInstaller(newBufferIO(t), c, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	manager.AddInstaller(pi)

	if err := manager.DisablePlugins(); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, pm.rec.list(), []string{"disablePlugins"})
}
