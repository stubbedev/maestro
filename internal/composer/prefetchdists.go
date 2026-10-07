// Ports nothing: dist downloads started while the lock is verified
// (deliberate deviation 3, speed).

package composer

import (
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// prefetchDists starts the dist transfers of the packages an install from
// the lock will install or update, so that they run while the lock is
// verified against the platform (and the filter lists are fetched). Only
// the installer's own download of each package takes its transfer
// (downloader.DownloadManager.Prefetch): the output and the files cache
// are what they are without it. A dist the files cache holds whose
// release is in the package store is materialized from the store instead,
// for the download to take (downloader.FileDownloader.prefetchMaterial).
// The returned function ends what no download took; doInstall calls it
// once the operations ran or failed, or the lock did not verify.
func (i *Installer) prefetchDists(locked *repository.LockArrayRepository, localRepo repository.InstalledRepositoryInterface) func() {
	if !i.executeOperations {
		return func() {}
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
		return func() {}
	}

	// the transaction doInstall computes after the verification
	transaction, err := resolver.NewLocalRepoTransaction(locked, localRepo)
	if err != nil {
		return func() {}
	}

	var installed []pkg.PackageInterface

	for _, op := range transaction.Operations() {
		switch op := op.(type) {
		case *operation.InstallOperation:
			m.Prefetch(op.Package(), nil)
			installed = append(installed, op.Package())
		case *operation.UpdateOperation:
			m.Prefetch(op.TargetPackage(), op.InitialPackage())
			installed = append(installed, op.TargetPackage())
		}
	}

	// the install notifications run() posts once the operations ran
	if n, ok := i.installationManager.(notificationPreconnector); ok && i.install && len(installed) > 0 {
		if notify, err := i.configBool("notify-on-install"); err == nil && notify {
			n.PreconnectNotifications(installed)
		}
	}

	return m.DiscardPrefetched
}

// notificationPreconnector is an InstallationManager that can open the
// connections of its install notifications ahead
// (installer.Manager.PreconnectNotifications).
type notificationPreconnector interface {
	PreconnectNotifications(packages []pkg.PackageInterface)
}
