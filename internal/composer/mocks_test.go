package composer

// Ports the mocks of tests/Composer/Test/Mock used by InstallerTest:
// FactoryMock, InstallationManagerMock, InstalledFilesystemRepositoryMock and
// VersionGuesserMock, plus the PHPUnit mocks of EventDispatcher,
// AutoloadGenerator and JsonFile.

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// testProbe is the probe output of testdata/platform, read by TestMain
// before any test changes the working directory.
var testProbe []byte

// testRuntime is a Runtime whose php is the PHP 8.4 build recorded in
// testdata/platform (internal/platform's oracle), so the tests need no php,
// with extraExtensions dummy extensions added.
func testRuntime(t testing.TB, extraExtensions int) *Runtime {
	t.Helper()
	snapshot, err := platform.ParseSnapshot("/usr/bin/php", testProbe)
	if err != nil {
		t.Fatal(err)
	}
	for i := range extraExtensions {
		snapshot.Extensions = append(snapshot.Extensions, platform.Extension{Name: "maestrotest" + strconv.Itoa(i), Version: "1.0.0", VersionOK: true})
	}
	detector := &platform.Detector{
		FindPHP: func() (string, bool) { return "/usr/bin/php", true },
		Probe:   func(context.Context, string) (*platform.Snapshot, error) { return snapshot, nil },
	}
	rt := NewRuntime("test", detector)
	rt.getenv = func(string) (string, bool) { return "", false }

	return rt
}

// installationManagerMock ports InstallationManagerMock.
type installationManagerMock struct {
	installed   []pkg.PackageInterface
	updated     [][2]pkg.PackageInterface
	uninstalled []pkg.PackageInterface
	trace       []string
}

func (m *installationManagerMock) Execute(repo repository.InstalledRepositoryInterface, operations []operation.Operation, _, _, _ bool) error {
	for _, op := range operations {
		// skipping download() step here for tests
		m.trace = append(m.trace, php.StripTags(op.String()))
		switch op := op.(type) {
		case *operation.InstallOperation:
			m.installed = append(m.installed, op.Package())
			if err := repo.AddPackage(pkg.Clone(op.Package())); err != nil {
				return err
			}
		case *operation.UpdateOperation:
			m.updated = append(m.updated, [2]pkg.PackageInterface{op.InitialPackage(), op.TargetPackage()})
			if err := repo.RemovePackage(op.InitialPackage()); err != nil {
				return err
			}
			has, err := repo.HasPackage(op.TargetPackage())
			if err != nil {
				return err
			}
			if !has {
				if err := repo.AddPackage(pkg.Clone(op.TargetPackage())); err != nil {
					return err
				}
			}
		case *operation.UninstallOperation:
			m.uninstalled = append(m.uninstalled, op.Package())
			if err := repo.RemovePackage(op.Package()); err != nil {
				return err
			}
		case *operation.MarkAliasInstalledOperation:
			m.installed = append(m.installed, op.Package())
			// InstallationManager::markAliasInstalled
			has, err := repo.HasPackage(op.Package())
			if err != nil {
				return err
			}
			if !has {
				if err := repo.AddPackage(pkg.Clone(op.Package())); err != nil {
					return err
				}
			}
		case *operation.MarkAliasUninstalledOperation:
			m.uninstalled = append(m.uninstalled, op.Package())
			// InstallationManager::markAliasUninstalled
			if err := repo.RemovePackage(op.Package()); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *installationManagerMock) InstallPath(p pkg.PackageInterface) (string, bool, error) {
	return "vendor/" + p.Name(), true, nil
}

func (m *installationManagerMock) IsPackageInstalled(repo repository.InstalledRepositoryInterface, p pkg.PackageInterface) (bool, error) {
	return repo.HasPackage(p)
}

// EnsureBinariesPresence finds no installer for any type (the mock has
// none), so it does nothing, as InstallationManager does then.
func (m *installationManagerMock) EnsureBinariesPresence(pkg.PackageInterface) error { return nil }

func (m *installationManagerMock) NotifyInstalls(io.IO) {}

func (m *installationManagerMock) DisablePlugins() error { return nil }

// versionGuesserMock ports VersionGuesserMock: no version is guessed.
type versionGuesserMock struct{ *version.VersionGuesser }

func (versionGuesserMock) GuessVersion(*php.Array, string) (*loader.VersionData, error) {
	return nil, nil
}

// newFactoryMock ports FactoryMock.
func newFactoryMock(t testing.TB, rt *Runtime) *Factory {
	t.Helper()

	return &Factory{
		Runtime: rt,
		CreateConfigFunc: func(_ io.IO, cwd string) (*config.Config, error) {
			cfg := config.New(true, cwd)
			err := cfg.Merge(php.ArrayOf(
				"config", php.ArrayOf("home", tempDir(t)),
				"repositories", php.ArrayOf("packagist", false),
			), config.SourceUnknown)

			return cfg, err
		},
		LoadRootPackageFunc: func(rm *repository.RepositoryManager, cfg *config.Config, parser *pkg.VersionParser, _ loader.VersionGuesser, out io.IO) *loader.RootPackageLoader {
			return NewRootPackageLoader(rm, cfg, parser, versionGuesserMock{version.NewVersionGuesser(nil, out)}, out)
		},
		AddLocalRepositoryFunc: func(_ io.IO, rm *repository.RepositoryManager, _ string, _ pkg.RootPackageInterface, _ *util.ProcessExecutor) error {
			repo, err := repository.NewInstalledArrayRepository(nil)
			rm.SetLocalRepository(repo)

			return err
		},
		CreateInstallationManagerFunc: func(*http.Loop, io.IO, *eventdispatcher.EventDispatcher) (InstallationManager, error) {
			return &installationManagerMock{}, nil
		},
		CreateDefaultInstallersFunc: func(InstallationManager, *PartialComposer, *Composer, io.IO, *util.ProcessExecutor) error {
			return nil
		},
		PurgePackagesFunc: func(repository.InstalledRepositoryInterface, InstallationManager) error { return nil },
	}
}

// tempDir is TestCase::getUniqueTmpDirectory, removed after the test.
func tempDir(t testing.TB) string {
	dir, err := os.MkdirTemp("", "composer-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	real, ok := php.Realpath(dir)
	if !ok {
		return dir
	}

	return real
}

// installedRepositoryMock ports InstalledFilesystemRepositoryMock: reload()
// and write() do nothing.
type installedRepositoryMock struct {
	*repository.InstalledFilesystemRepository
}

func (installedRepositoryMock) Reload() error { return nil }

func (installedRepositoryMock) Write(bool, repository.InstallationManager) error { return nil }

// jsonFileMock is the PHPUnit mock of JsonFile the installed repository
// reads.
type jsonFileMock struct{ data any }

func (jsonFileMock) Path() string                  { return "installed.json" }
func (jsonFileMock) Exists() bool                  { return true }
func (m jsonFileMock) Read() (any, error)          { return m.data, nil }
func (jsonFileMock) Write(any, php.JSONFlag) error { return nil }

// lockFileMock emulates a writable lock file: Read decodes what was last
// written (json_encode with JSON_PRETTY_PRINT), Write keeps the data.
type lockFileMock struct {
	lockData *string
	writes   int
	actual   any
}

func (*lockFileMock) Path() string { return "composer.lock" }

func (m *lockFileMock) Exists() bool { return m.lockData != nil }

func (m *lockFileMock) Read() (any, error) {
	if m.lockData == nil {
		return nil, nil
	}

	return php.JSONDecode(*m.lockData, true)
}

func (m *lockFileMock) Write(hash any, _ php.JSONFlag) error {
	encoded, err := php.JSONEncode(hash, php.JSONPrettyPrint)
	if err != nil {
		return err
	}
	m.lockData = &encoded
	m.writes++
	m.actual = hash

	return nil
}

// eventDispatcherMock is the PHPUnit mock of EventDispatcher: nothing
// listens.
type eventDispatcherMock struct{}

func (eventDispatcherMock) Dispatch(string, eventdispatcher.Event) (int, error) { return 0, nil }

func (eventDispatcherMock) DispatchScript(string, bool, []string, *php.Array) (int, error) {
	return 0, nil
}

func (eventDispatcherMock) DispatchInstallerEvent(string, bool, bool, eventdispatcher.Transaction) (int, error) {
	return 0, nil
}

// autoloadGeneratorMock is the PHPUnit mock of AutoloadGenerator.
type autoloadGeneratorMock struct{}

func (autoloadGeneratorMock) SetClassMapAuthoritative(bool)                                  {}
func (autoloadGeneratorMock) SetApcu(bool, *string)                                          {}
func (autoloadGeneratorMock) SetRunScripts(bool)                                             {}
func (autoloadGeneratorMock) SetPlatformRequirementFilter(version.PlatformRequirementFilter) {}

func (autoloadGeneratorMock) DumpAutoloads(ConfigReader, repository.InstalledRepositoryInterface, pkg.RootPackageInterface, InstallationManager, string, bool, string, AutoloadLocker) (*classmap.ClassMap, error) {
	return nil, nil
}

// downloadManagerMock is the PHPUnit mock of DownloadManager.
type downloadManagerMock struct{}

func (downloadManagerMock) SetPreferSource(bool) {}
func (downloadManagerMock) SetPreferDist(bool)   {}
