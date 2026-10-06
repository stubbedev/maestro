// Ports nothing: dist downloads started while the lock is verified
// (deliberate deviation 3, speed).

package composer

import (
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// prefetchDists starts the dist transfers of the packages an install from
// the lock will install or update, so that they run while the lock is
// verified against the platform (and the filter lists are fetched). Only
// the installer's own download of each package takes its transfer
// (downloader.DownloadManager.Prefetch): the output and the files cache
// are what they are without it.
func (i *Installer) prefetchDists(locked *repository.LockArrayRepository, localRepo repository.InstalledRepositoryInterface) {
	if !i.executeOperations {
		return
	}

	var m *downloader.DownloadManager

	switch a := i.downloadManager.(type) {
	case downloadManagerAdapter:
		m = a.m
	case interface {
		Manager() *downloader.DownloadManager
	}:
		m = a.Manager()
	}

	if m == nil {
		return
	}

	// the transaction doInstall computes after the verification
	transaction, err := resolver.NewLocalRepoTransaction(locked, localRepo)
	if err != nil {
		return
	}

	for _, op := range transaction.Operations() {
		switch op := op.(type) {
		case *operation.InstallOperation:
			m.Prefetch(op.Package(), nil)
		case *operation.UpdateOperation:
			m.Prefetch(op.TargetPackage(), op.InitialPackage())
		}
	}
}
