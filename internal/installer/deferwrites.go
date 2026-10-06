// Ports nothing: the deferral of InstallationManager::executeBatch's
// repository writes (deliberate deviation 3, speed).

package installer

import (
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/repository"
)

// writeDeferrer is a repository whose writes can be deferred
// (repository.FilesystemRepository).
type writeDeferrer interface {
	DeferWrites()
	FlushWrites() error
}

// deferWrites makes repo defer the write executeBatch asks for after each
// operation while the batch's promises are waited on, when nothing but
// maestro's own installers runs in that time, and returns the flush to
// call when the wait ends. Composer writes installed.json and
// installed.php, each holding every package, after every operation; the
// files are only read again after the batch (POST_PACKAGE_* listeners,
// the next batch's plugins, the autoload dump), so writing them once, as
// the last of those writes would have, leaves the same files. A plugin's
// installer (PHP code, which may read the files or InstalledVersions)
// keeps the writes immediate for its batch; a signal flushes before the
// clean-up (handleSignals).
func (m *Manager) deferWrites(repo any, operations []indexedOp) func() error {
	d, ok := repo.(writeDeferrer)
	if !ok || m.loop == nil || !m.nativeBatch(operations) {
		return func() error { return nil }
	}

	d.DeferWrites()
	m.flushWrites = d.FlushWrites

	return func() error {
		m.flushWrites = nil

		return d.FlushWrites()
	}
}

// nativeBatch reports whether every operation of a batch is handled by one
// of maestro's own installers with maestro's own collaborators, so that no
// plugin code runs while its promises settle.
func (m *Manager) nativeBatch(operations []indexedOp) bool {
	for _, iop := range operations {
		if !executesCode(iop.op.OperationType()) {
			continue
		}
		// the installers executeBatch and Update look up anyway
		p, initial := operationPackages(iop.op)
		types := []string{p.Type()}
		if initial != nil && initial.Type() != p.Type() {
			types = append(types, initial.Type())
		}
		for _, typ := range types {
			installer, err := m.Installer(typ)
			if err != nil || !nativeInstaller(installer) {
				return false
			}
		}
	}

	return true
}

// nativeInstaller reports whether an installer is maestro's own (not a
// plugin's, nor a PHP subclass of one).
func nativeInstaller(i Installer) bool {
	switch i := i.(type) {
	case *MetapackageInstaller:
		return true
	case *LibraryInstaller:
		if i.virt != Virtuals(i) {
			return false
		}
		if _, ok := i.binaryInstaller.(*BinaryInstaller); !ok {
			return false
		}
		if i.downloadManager == nil {
			return true
		}
		_, ok := i.downloadManager.(*downloader.DownloadManager)

		return ok
	}

	return false
}

var _ writeDeferrer = (*repository.FilesystemRepository)(nil)
