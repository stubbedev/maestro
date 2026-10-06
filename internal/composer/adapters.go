package composer

import (
	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util/http"
)

// downloadManagerAdapter is a *downloader.DownloadManager as the
// Installer's DownloadManager.
type downloadManagerAdapter struct{ m *downloader.DownloadManager }

func (a downloadManagerAdapter) SetPreferSource(preferSource bool) { a.m.SetPreferSource(preferSource) }

func (a downloadManagerAdapter) SetPreferDist(preferDist bool) { a.m.SetPreferDist(preferDist) }

// GeneratorAdapter is a *autoload.Generator as the Installer's
// AutoloadGenerator.
type GeneratorAdapter struct{ *autoload.Generator }

// DumpAutoloads implements AutoloadGenerator: AutoloadGenerator::dump with
// the local repository, the installation manager and the locker.
func (g GeneratorAdapter) DumpAutoloads(config ConfigReader, localRepo repository.InstalledRepositoryInterface, root pkg.RootPackageInterface, im InstallationManager, targetDir string, scanPsrPackages bool, suffix string, locker AutoloadLocker) (*classmap.ClassMap, error) {
	repo := &autoloadRepository{repo: localRepo}
	var l autoload.Locker
	if locker != nil {
		l = locker
	}
	classMap, err := g.Dump(config, repo, root, im, targetDir, scanPsrPackages, suffix, l, false)
	if err != nil {
		return nil, err
	}

	return classMap, repo.err
}

// WarmAutoloads starts parsing, in the background, the files a later
// DumpAutoloads will scan (autoload.Generator.Warm).
func (g GeneratorAdapter) WarmAutoloads(config ConfigReader, localRepo repository.InstalledRepositoryInterface, root pkg.RootPackageInterface, im InstallationManager, scanPsrPackages bool) {
	g.Warm(config, &autoloadRepository{repo: localRepo}, root, im, scanPsrPackages)
}

// SpeculateAutoloads starts, in the background, the class map scan of a
// later DumpAutoloads (autoload.Generator.Speculate).
func (g GeneratorAdapter) SpeculateAutoloads(config ConfigReader, localRepo repository.InstalledRepositoryInterface, root pkg.RootPackageInterface, im InstallationManager, scanPsrPackages bool) {
	g.Speculate(config, &autoloadRepository{repo: localRepo}, root, im, scanPsrPackages)
}

// DiscardAutoloadSpeculation drops the scan SpeculateAutoloads started.
func (g GeneratorAdapter) DiscardAutoloadSpeculation() { g.DiscardSpeculation() }

// autoloadRepository adapts the local repository to
// autoload.InstalledRepository, whose CanonicalPackages cannot fail: the
// error is kept and returned after the dump.
type autoloadRepository struct {
	repo repository.InstalledRepositoryInterface
	err  error
}

func (r *autoloadRepository) DevPackageNames() []string { return r.repo.DevPackageNames() }

func (r *autoloadRepository) CanonicalPackages() []pkg.PackageInterface {
	packages, err := r.repo.CanonicalPackages()
	if err != nil && r.err == nil {
		r.err = err
	}

	return packages
}

// getter returns the repository manager's HttpDownloader as an
// http.Getter, never a typed nil.
func getter(rm *repository.RepositoryManager) http.Getter {
	if d := rm.HTTPDownloader(); d != nil {
		return d
	}

	return nil
}
