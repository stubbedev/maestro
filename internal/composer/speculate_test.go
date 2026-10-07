package composer

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// speculatingGenerator is an autoloadGeneratorMock that counts the scans
// started ahead, dropped and taken by a dump.
type speculatingGenerator struct {
	autoloadGeneratorMock
	pending                   bool
	started, discarded, taken int
}

func (g *speculatingGenerator) SpeculateAutoloads(ConfigReader, repository.InstalledRepositoryInterface, pkg.RootPackageInterface, InstallationManager, bool) {
	g.pending = true
	g.started++
}

func (g *speculatingGenerator) DiscardAutoloadSpeculation() {
	if g.pending {
		g.pending = false
		g.discarded++
	}
}

func (g *speculatingGenerator) DumpAutoloads(ConfigReader, repository.InstalledRepositoryInterface, pkg.RootPackageInterface, InstallationManager, string, bool, string, AutoloadLocker) (*classmap.ClassMap, error) {
	if g.pending {
		g.pending = false
		g.taken++
	}

	return nil, nil
}

// preparingRepo is a local repository that counts the writes prepared
// ahead and dropped.
type preparingRepo struct {
	*repository.InstalledArrayRepository
	prepared, discarded int
}

func (r *preparingRepo) PrepareWrite(bool, repository.InstallationManager, []string) { r.prepared++ }

func (r *preparingRepo) DiscardPreparedWrite() { r.discarded++ }

// failingDispatcher is an eventDispatcherMock that tells nothing listens,
// and fails the installer event fail.
type failingDispatcher struct {
	eventDispatcherMock
	fail string
}

func (failingDispatcher) WillDispatchTo(eventdispatcher.Event) bool { return false }

func (d failingDispatcher) DispatchInstallerEvent(name string, _, _ bool, _ eventdispatcher.Transaction) (int, error) {
	if name == d.fail {
		return 0, errors.New(name + " failed")
	}

	return 0, nil
}

// failingManager is an installationManagerMock whose Execute fails.
type failingManager struct{ *installationManagerMock }

func (failingManager) Execute(repository.InstalledRepositoryInterface, []operation.Operation, bool, bool, bool) error {
	return errors.New("execute failed")
}

// runNoOpInstall installs from a lock file that asks for no operation,
// with work started ahead.
func runNoOpInstall(t *testing.T, failEvent string, failExecute bool) (*speculatingGenerator, *preparingRepo, error) {
	t.Helper()
	t.Chdir(tempDir(t))
	out := newBufferIO(t)
	cfg := config.New(false, "")
	repositoryManager := repository.NewRepositoryManager(out, cfg, nil, nil, nil)
	installed, err := repository.NewInstalledArrayRepository(nil)
	if err != nil {
		t.Fatal(err)
	}
	localRepo := &preparingRepo{InstalledArrayRepository: installed}
	repositoryManager.SetLocalRepository(localRepo)
	var im InstallationManager = &installationManagerMock{}
	if failExecute {
		im = failingManager{&installationManagerMock{}}
	}
	lockData := `{"content-hash": "", "packages": [], "packages-dev": [], "aliases": [], "minimum-stability": "stable", "stability-flags": {}, "prefer-stable": false, "prefer-lowest": false, "platform": {}, "platform-dev": {}}`
	l, err := locker.New(out, &lockFileMock{lockData: &lockData}, im, "{}", nil)
	if err != nil {
		t.Fatal(err)
	}
	generator := &speculatingGenerator{}
	installer, err := NewInstaller(InstallerDeps{
		IO:                  out,
		Config:              installerTestConfig{},
		Package:             pkg.NewRootPackage("a/a", "1.0.0.0", "1.0.0"),
		DownloadManager:     downloadManagerMock{},
		RepositoryManager:   repositoryManager,
		Locker:              l,
		InstallationManager: im,
		EventDispatcher:     failingDispatcher{fail: failEvent},
		AutoloadGenerator:   generator,
		Runtime:             testRuntime(t, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	installer.SetAuditConfig(advisory.AuditConfig{Audit: false})
	_, err = installer.Run()
	if generator.started != 1 || localRepo.prepared != 1 {
		t.Fatalf("started %d scans and %d writes ahead, want 1 each\n%s", generator.started, localRepo.prepared, bufferOutput(out))
	}

	return generator, localRepo, err
}

// An install that fails after starting work ahead leaves none of it: the
// generator, which must stay re-entrant, keeps no scan for a later dump.
func TestInstaller_DoInstallDiscardsAheadOnError(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failEvent   string
		failExecute bool
	}{
		{name: "pre-operations-exec listener", failEvent: installerPreOperationsExec},
		{name: "execute", failExecute: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			generator, localRepo, err := runNoOpInstall(t, tc.failEvent, tc.failExecute)
			if err == nil {
				t.Fatal("the install did not fail")
			}
			if generator.pending || generator.discarded != 1 || generator.taken != 0 {
				t.Errorf("the scan started ahead was left %v, discarded %d and taken %d times, want discarded once", generator.pending, generator.discarded, generator.taken)
			}
			if localRepo.discarded != 1 {
				t.Errorf("the write prepared ahead was discarded %d times, want 1", localRepo.discarded)
			}
		})
	}
}

// A no-op install hands the scan started ahead over to the dump.
func TestInstaller_DoInstallKeepsScanForDump(t *testing.T) {
	generator, _, err := runNoOpInstall(t, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if generator.discarded != 0 || generator.taken != 1 {
		t.Errorf("the scan started ahead was discarded %d and taken %d times, want taken once", generator.discarded, generator.taken)
	}
}

// The local repository transaction is computed once before the lock is
// verified, and taken after while the repositories' packages are the same
// objects at the same revisions; it is computed again otherwise.
func TestLocalRepoTransaction_Take(t *testing.T) {
	newRepos := func(t *testing.T) (*repository.ArrayRepository, *repository.InstalledArrayRepository, *pkg.Package) {
		t.Helper()
		b := pkg.NewPackage("b/b", "1.0.0.0", "1.0.0")
		locked, err := repository.NewArrayRepository([]pkg.PackageInterface{b})
		if err != nil {
			t.Fatal(err)
		}
		local, err := repository.NewInstalledArrayRepository([]pkg.PackageInterface{pkg.NewPackage("b/b", "1.0.0.0", "1.0.0")})
		if err != nil {
			t.Fatal(err)
		}

		return locked, local, b
	}

	t.Run("unchanged", func(t *testing.T) {
		locked, local, _ := newRepos(t)
		early := newLocalRepoTransaction(locked, local)
		if early.err != nil || len(early.transaction.Operations()) != 0 {
			t.Fatalf("early transaction %v, %v", early.transaction, early.err)
		}
		taken, err := early.take()
		if err != nil || taken != early.transaction {
			t.Fatalf("take computed the transaction again: %p, %v; want %p", taken, err, early.transaction)
		}
	})

	t.Run("changed in place", func(t *testing.T) {
		locked, local, b := newRepos(t)
		early := newLocalRepoTransaction(locked, local)
		b.SetType("composer-plugin")
		taken, err := early.take()
		if err != nil || taken == early.transaction {
			t.Fatalf("take kept the transaction of a package changed since: %v", err)
		}
	})

	t.Run("package added", func(t *testing.T) {
		locked, local, _ := newRepos(t)
		early := newLocalRepoTransaction(locked, local)
		if err := local.AddPackage(pkg.NewPackage("c/c", "1.0.0.0", "1.0.0")); err != nil {
			t.Fatal(err)
		}
		taken, err := early.take()
		if err != nil || taken == early.transaction {
			t.Fatalf("take kept the transaction of a repository changed since: %v", err)
		}
		if ops := taken.Operations(); len(ops) != 1 || ops[0].OperationType() != "uninstall" {
			t.Fatalf("operations %v, want c/c's removal", ops)
		}
	})
}
