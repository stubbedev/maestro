// Ports the format() helpers of
// src/Composer/DependencyResolver/Operation/{InstallOperation,UpdateOperation,UninstallOperation}.php,
// which the downloaders print. internal/resolver owns the operations; it
// is above this package, so they are repeated here.

package downloader

import "github.com/stubbedev/maestro/internal/pkg"

// FormatInstall is InstallOperation::format($package).
func FormatInstall(p pkg.PackageInterface) string {
	return "Installing <info>" + p.PrettyName() + "</info> (<comment>" + p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) + "</comment>)"
}

// FormatUninstall is UninstallOperation::format($package).
func FormatUninstall(p pkg.PackageInterface) string {
	return "Removing <info>" + p.PrettyName() + "</info> (<comment>" + p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) + "</comment>)"
}

// FormatUpdate is UpdateOperation::format($initial, $target).
func FormatUpdate(initial, target pkg.PackageInterface) (string, error) {
	fromVersion := initial.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)
	toVersion := target.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)

	if fromVersion == toVersion && initial.SourceReference() != target.SourceReference() {
		fromVersion = initial.FullPrettyVersion(true, pkg.DisplaySourceRef)
		toVersion = target.FullPrettyVersion(true, pkg.DisplaySourceRef)
	} else if fromVersion == toVersion && initial.DistReference() != target.DistReference() {
		fromVersion = initial.FullPrettyVersion(true, pkg.DisplayDistRef)
		toVersion = target.FullPrettyVersion(true, pkg.DisplayDistRef)
	}

	upgrade, err := pkg.IsUpgrade(initial.Version(), target.Version())
	if err != nil {
		return "", err
	}

	actionName := "Downgrading"
	if upgrade {
		actionName = "Upgrading"
	}

	return actionName + " <info>" + initial.PrettyName() + "</info> (<comment>" + fromVersion + "</comment> => <comment>" + toVersion + "</comment>)", nil
}
