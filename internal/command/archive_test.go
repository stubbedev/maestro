// Ports tests/Composer/Test/Command/ArchiveCommandTest.php. PHPUnit mocks
// ArchiveCommand's tryComposer/requireComposer/archive methods and the
// ArchiveManager; the Go command has the matching private hooks, so these
// tests live in package command.

package command

import (
	"bytes"
	"testing"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// archiveCall records ArchiveManager::archive's arguments.
type archiveCall struct {
	p             pkg.CompletePackageInterface
	format, dir   string
	fileName      pkg.NullString
	ignoreFilters bool
}

type fakeArchiveManager struct {
	calls  []archiveCall
	result string
}

func (m *fakeArchiveManager) Archive(p pkg.CompletePackageInterface, format, targetDir string, fileName pkg.NullString, ignoreFilters bool) (string, error) {
	m.calls = append(m.calls, archiveCall{p, format, targetDir, fileName, ignoreFilters})

	return m.result, nil
}

func archiveTestRun(t *testing.T, c *ArchiveCommand, params ...console.Param) int {
	t.Helper()
	in, err := console.NewArrayInput(params, nil)
	if err != nil {
		t.Fatal(err)
	}
	out := console.NewStreamOutput(&bytes.Buffer{}, console.VerbosityNormal, new(false), nil)
	code, err := c.Run(in, out)
	if err != nil {
		t.Fatal(err)
	}

	return code
}

// archiveTestComposer is the Composer the PHP tests assemble: a config
// with archive-format zip, an event dispatcher and the root package.
func archiveTestComposer(t *testing.T, root pkg.RootPackageInterface) *composer.Composer {
	t.Helper()
	comp := &composer.Composer{}
	cfg := config.New(true, "")
	if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("archive-format", "zip")), config.SourceUnknown); err != nil {
		t.Fatal(err)
	}
	comp.SetConfig(cfg)
	comp.SetPackage(root)
	comp.SetEventDispatcher(eventdispatcher.New(comp, nil, nil))

	return comp
}

func TestArchiveCommand_UsesConfigFromComposerObject(t *testing.T) {
	cwd, err := util.GetCwd(false)
	if err != nil {
		t.Fatal(err)
	}
	root := pkg.NewRootPackage("__root__", "1.0.0.0", "1.0.0")
	comp := archiveTestComposer(t, root)
	manager := &fakeArchiveManager{result: cwd}

	c := NewArchiveCommand()
	tried, required := 0, 0
	c.tryComposerFunc = func() (*composer.Composer, error) { tried++; return comp, nil }
	c.requireComposerFunc = func() (*composer.Composer, error) { required++; return comp, nil }
	c.archiveManagerOf = func(*composer.Composer) packageArchiver { return manager }

	archiveTestRun(t, c)

	if tried == 0 || required == 0 {
		t.Errorf("tryComposer called %d times, requireComposer %d times", tried, required)
	}
	if len(manager.calls) != 1 {
		t.Fatalf("archive called %d times", len(manager.calls))
	}
	call := manager.calls[0]
	if call.p != pkg.CompletePackageInterface(root) || call.format != "zip" || call.dir != "." || call.fileName.Valid || call.ignoreFilters {
		t.Errorf("archive called with %+v", call)
	}
}

func TestArchiveCommand_UsesConfigFromFactoryWhenComposerIsNotDefined(t *testing.T) {
	c := NewArchiveCommand()
	tried, archived := 0, 0
	c.tryComposerFunc = func() (*composer.Composer, error) { tried++; return nil, nil }
	var got archiveArgs
	c.archiveFunc = func(a archiveArgs) (int, error) { archived++; got = a; return 0, nil }

	if code := archiveTestRun(t, c); code != 0 {
		t.Errorf("exit code %d", code)
	}

	if tried != 1 || archived != 1 {
		t.Fatalf("tryComposer called %d times, archive %d times", tried, archived)
	}
	expected, err := (&composer.Factory{}).CreateConfig(nil, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"archive-format", "archive-dir", "home"} {
		want, _ := expected.Get(key, 0)
		have, _ := got.config.Get(key, 0)
		if !php.StrictEquals(want, have) {
			t.Errorf("config %s: %v, want %v", key, have, want)
		}
	}
	if got.io == nil || got.packageName.Valid || got.version.Valid || got.format != "tar" || got.dest != "." || got.fileName.Valid || got.ignoreFilters || got.composer != nil {
		t.Errorf("archive called with %+v", got)
	}
}

func TestArchiveCommand_UsesConfigFromComposerObjectWithPackageName(t *testing.T) {
	cwd, err := util.GetCwd(false)
	if err != nil {
		t.Fatal(err)
	}
	p := pkg.NewRootPackage("foo/bar", "1.0.0", "1.0")
	comp := archiveTestComposer(t, p)

	// PHP mocks the local repository's loadPackages to return the package
	// and the RepositoryManager to return no other repositories.
	installedRepository, err := repository.NewInstalledArrayRepository([]pkg.PackageInterface{p})
	if err != nil {
		t.Fatal(err)
	}
	repositoryManager := repository.NewRepositoryManager(nil, comp.Config(), nil, nil, nil)
	repositoryManager.SetLocalRepository(installedRepository)
	comp.SetRepositoryManager(repositoryManager)

	manager := &fakeArchiveManager{result: cwd}

	c := NewArchiveCommand()
	tried := 0
	c.tryComposerFunc = func() (*composer.Composer, error) { tried++; return comp, nil }
	c.requireComposerFunc = func() (*composer.Composer, error) { return comp, nil }
	c.archiveManagerOf = func(*composer.Composer) packageArchiver { return manager }

	archiveTestRun(t, c, console.P("package", "foo/bar"))

	if tried == 0 {
		t.Error("tryComposer not called")
	}
	if len(manager.calls) != 1 {
		t.Fatalf("archive called %d times", len(manager.calls))
	}
	call := manager.calls[0]
	if call.p.Name() != "foo/bar" || call.p.Version() != "1.0.0" || call.format != "zip" || call.dir != "." || call.fileName.Valid || call.ignoreFilters {
		t.Errorf("archive called with %+v", call)
	}
}
