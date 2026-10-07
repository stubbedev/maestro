package operation

import (
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/ui"
)

// The operations as items of the lists Composer prints ("  - " and the
// operation's format()), their bullets marking what each does
// (ui.Mark): Composer's text undecorated.

// InstallItem is "  - " . InstallOperation::format($package, $lock).
func InstallItem(p pkg.PackageInterface, lock bool) string {
	return ui.MarkInstall.Item() + FormatInstall(p, lock)
}

// UninstallItem is "  - " . UninstallOperation::format($package).
func UninstallItem(p pkg.PackageInterface) string {
	return ui.MarkRemove.Item() + FormatUninstall(p)
}

// UpdateItem is "  - " . UpdateOperation::format($initial, $target).
func UpdateItem(initial, target pkg.PackageInterface) (string, error) {
	text, err := FormatUpdate(initial, target)
	if err != nil {
		return "", err
	}
	upgrade, err := pkg.IsUpgrade(initial.Version(), target.Version())
	if err != nil {
		return "", err
	}
	mark := ui.MarkDowngrade
	if upgrade {
		mark = ui.MarkUpgrade
	}

	return mark.Item() + text, nil
}

// Item is "  - " . $operation->show($lock).
func Item(op Operation, lock bool) (string, error) {
	switch op := op.(type) {
	case *InstallOperation:
		return InstallItem(op.pkg, lock), nil
	case *UninstallOperation:
		return UninstallItem(op.pkg), nil
	case *UpdateOperation:
		return UpdateItem(op.initial, op.target)
	}
	text, err := op.Show(lock)
	if err != nil {
		return "", err
	}

	return ui.MarkItem.Item() + text, nil
}
