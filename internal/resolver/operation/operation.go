// Ports src/Composer/DependencyResolver/Operation/OperationInterface.php,
// SolverOperation.php, InstallOperation.php, UpdateOperation.php,
// UninstallOperation.php, MarkAliasInstalledOperation.php and
// MarkAliasUninstalledOperation.php.

// Package operation ports Composer\DependencyResolver\Operation: the
// operations a transaction is made of. It sits below internal/resolver
// (it needs only packages) so that the downloaders and installers, which
// print the operations' format() strings, can import it.
package operation

import "github.com/stubbedev/maestro/internal/pkg"

// The operation types, as getOperationType returns them.
const (
	TypeInstall              = "install"
	TypeUpdate               = "update"
	TypeUninstall            = "uninstall"
	TypeMarkAliasInstalled   = "markAliasInstalled"
	TypeMarkAliasUninstalled = "markAliasUninstalled"
)

// Operation ports OperationInterface. `instanceof` checks are type
// assertions on the concrete pointer types.
type Operation interface {
	// OperationType ports getOperationType.
	OperationType() string
	// Show ports show($lock): the operation as the installer prints it.
	// The error is UpdateOperation's version comparison failing, which
	// cannot happen for normalized versions.
	Show(lock bool) (string, error)
	// String ports __toString(): show(false), "" if that fails.
	String() string
}

// toString is SolverOperation::__toString.
func toString(op Operation) string {
	s, _ := op.Show(false)

	return s
}

// InstallOperation ports Composer\DependencyResolver\Operation\InstallOperation.
type InstallOperation struct {
	pkg pkg.PackageInterface
}

// NewInstallOperation is new InstallOperation($package).
func NewInstallOperation(p pkg.PackageInterface) *InstallOperation {
	return &InstallOperation{pkg: p}
}

// Package ports getPackage.
func (o *InstallOperation) Package() pkg.PackageInterface { return o.pkg }

// OperationType implements Operation.
func (o *InstallOperation) OperationType() string { return TypeInstall }

// Show implements Operation.
func (o *InstallOperation) Show(lock bool) (string, error) { return FormatInstall(o.pkg, lock), nil }

// String implements Operation.
func (o *InstallOperation) String() string { return toString(o) }

// FormatInstall ports InstallOperation::format($package, $lock).
func FormatInstall(p pkg.PackageInterface, lock bool) string {
	action := "Installing "
	if lock {
		action = "Locking "
	}

	return action + "<info>" + p.PrettyName() + "</info> (<comment>" + p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) + "</comment>)"
}

// UninstallOperation ports Composer\DependencyResolver\Operation\UninstallOperation.
type UninstallOperation struct {
	pkg pkg.PackageInterface
}

// NewUninstallOperation is new UninstallOperation($package).
func NewUninstallOperation(p pkg.PackageInterface) *UninstallOperation {
	return &UninstallOperation{pkg: p}
}

// Package ports getPackage.
func (o *UninstallOperation) Package() pkg.PackageInterface { return o.pkg }

// OperationType implements Operation.
func (o *UninstallOperation) OperationType() string { return TypeUninstall }

// Show implements Operation.
func (o *UninstallOperation) Show(bool) (string, error) { return FormatUninstall(o.pkg), nil }

// String implements Operation.
func (o *UninstallOperation) String() string { return toString(o) }

// FormatUninstall ports UninstallOperation::format($package): the same
// with or without $lock.
func FormatUninstall(p pkg.PackageInterface) string {
	return "Removing <info>" + p.PrettyName() + "</info> (<comment>" + p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) + "</comment>)"
}

// UpdateOperation ports Composer\DependencyResolver\Operation\UpdateOperation.
type UpdateOperation struct {
	initial, target pkg.PackageInterface
}

// NewUpdateOperation is new UpdateOperation($initial, $target).
func NewUpdateOperation(initial, target pkg.PackageInterface) *UpdateOperation {
	return &UpdateOperation{initial: initial, target: target}
}

// InitialPackage ports getInitialPackage.
func (o *UpdateOperation) InitialPackage() pkg.PackageInterface { return o.initial }

// TargetPackage ports getTargetPackage.
func (o *UpdateOperation) TargetPackage() pkg.PackageInterface { return o.target }

// OperationType implements Operation.
func (o *UpdateOperation) OperationType() string { return TypeUpdate }

// Show implements Operation.
func (o *UpdateOperation) Show(bool) (string, error) { return FormatUpdate(o.initial, o.target) }

// String implements Operation.
func (o *UpdateOperation) String() string { return toString(o) }

// FormatUpdate ports UpdateOperation::format($initial, $target): the same
// with or without $lock.
func FormatUpdate(initial, target pkg.PackageInterface) (string, error) {
	fromVersion := initial.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)
	toVersion := target.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev)

	if fromVersion == toVersion && !nullStringEqual(initial.SourceReference(), target.SourceReference()) {
		fromVersion = initial.FullPrettyVersion(true, pkg.DisplaySourceRef)
		toVersion = target.FullPrettyVersion(true, pkg.DisplaySourceRef)
	} else if fromVersion == toVersion && !nullStringEqual(initial.DistReference(), target.DistReference()) {
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

// MarkAliasInstalledOperation ports
// Composer\DependencyResolver\Operation\MarkAliasInstalledOperation.
type MarkAliasInstalledOperation struct {
	pkg pkg.Alias
}

// NewMarkAliasInstalledOperation is new MarkAliasInstalledOperation($package).
func NewMarkAliasInstalledOperation(p pkg.Alias) *MarkAliasInstalledOperation {
	return &MarkAliasInstalledOperation{pkg: p}
}

// Package ports getPackage.
func (o *MarkAliasInstalledOperation) Package() pkg.Alias { return o.pkg }

// OperationType implements Operation.
func (o *MarkAliasInstalledOperation) OperationType() string { return TypeMarkAliasInstalled }

// Show implements Operation.
func (o *MarkAliasInstalledOperation) Show(bool) (string, error) {
	return formatMarkAlias(o.pkg, "installed"), nil
}

// String implements Operation.
func (o *MarkAliasInstalledOperation) String() string { return toString(o) }

// MarkAliasUninstalledOperation ports
// Composer\DependencyResolver\Operation\MarkAliasUninstalledOperation.
type MarkAliasUninstalledOperation struct {
	pkg pkg.Alias
}

// NewMarkAliasUninstalledOperation is new MarkAliasUninstalledOperation($package).
func NewMarkAliasUninstalledOperation(p pkg.Alias) *MarkAliasUninstalledOperation {
	return &MarkAliasUninstalledOperation{pkg: p}
}

// Package ports getPackage.
func (o *MarkAliasUninstalledOperation) Package() pkg.Alias { return o.pkg }

// OperationType implements Operation.
func (o *MarkAliasUninstalledOperation) OperationType() string { return TypeMarkAliasUninstalled }

// Show implements Operation.
func (o *MarkAliasUninstalledOperation) Show(bool) (string, error) {
	return formatMarkAlias(o.pkg, "uninstalled"), nil
}

// String implements Operation.
func (o *MarkAliasUninstalledOperation) String() string { return toString(o) }

func formatMarkAlias(p pkg.Alias, state string) string {
	return "Marking <info>" + p.PrettyName() + "</info> (<comment>" + p.FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) + "</comment>) as " + state + ", alias of <info>" + p.AliasOf().PrettyName() + "</info> (<comment>" + p.AliasOf().FullPrettyVersion(true, pkg.DisplaySourceRefIfDev) + "</comment>)"
}

// nullStringEqual is ?string's ===.
func nullStringEqual(a, b pkg.NullString) bool {
	return a.Valid == b.Valid && (!a.Valid || a.S == b.S)
}
