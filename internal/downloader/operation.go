// The format() helpers of
// src/Composer/DependencyResolver/Operation/{InstallOperation,UpdateOperation,UninstallOperation}.php,
// which the downloaders print, are ported in internal/resolver/operation;
// these are the downloader's names for them.

package downloader

import (
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/resolver/operation"
)

// FormatInstall is InstallOperation::format($package).
func FormatInstall(p pkg.PackageInterface) string { return operation.FormatInstall(p, false) }

// FormatUninstall is UninstallOperation::format($package).
func FormatUninstall(p pkg.PackageInterface) string { return operation.FormatUninstall(p) }

// FormatUpdate is UpdateOperation::format($initial, $target).
func FormatUpdate(initial, target pkg.PackageInterface) (string, error) {
	return operation.FormatUpdate(initial, target)
}
