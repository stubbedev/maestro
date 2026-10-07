// Ports src/Composer/Package/RootAliasPackage.php.

package pkg

import "github.com/stubbedev/maestro/internal/php"

// RootAliasPackage ports Composer\Package\RootAliasPackage: an alias of
// the root package. Its setters update the aliased root package too.
type RootAliasPackage struct {
	CompleteAliasPackage

	root RootPackageInterface
}

// NewRootAliasPackage ports RootAliasPackage::__construct.
func NewRootAliasPackage(aliasOf RootPackageInterface, version, prettyVersion string) *RootAliasPackage {
	a := &RootAliasPackage{root: aliasOf}
	a.complete = aliasOf
	a.init(aliasOf, version, prettyVersion)

	return a
}

// PHPClass returns ClassRootAliasPackage.
func (a *RootAliasPackage) PHPClass() string { return ClassRootAliasPackage }

// Clone ports RootAliasPackage::__clone: the aliased root package is
// cloned too.
func (a *RootAliasPackage) Clone() *RootAliasPackage {
	c := *a
	c.clearForClone()

	root, _ := Clone(a.root).(RootPackageInterface)
	c.root = root
	c.complete = root
	c.aliasOf = root

	return &c
}

// Aliases ports RootAliasPackage::getAliases.
func (a *RootAliasPackage) Aliases() *php.Array { return a.root.Aliases() }

// MinimumStability ports RootAliasPackage::getMinimumStability.
func (a *RootAliasPackage) MinimumStability() string { return a.root.MinimumStability() }

// StabilityFlags ports RootAliasPackage::getStabilityFlags.
func (a *RootAliasPackage) StabilityFlags() *php.Array { return a.root.StabilityFlags() }

// References ports RootAliasPackage::getReferences.
func (a *RootAliasPackage) References() *php.Array { return a.root.References() }

// PreferStable ports RootAliasPackage::getPreferStable.
func (a *RootAliasPackage) PreferStable() bool { return a.root.PreferStable() }

// Config ports RootAliasPackage::getConfig.
func (a *RootAliasPackage) Config() *php.Array { return a.root.Config() }

// SetRequires ports RootAliasPackage::setRequires.
func (a *RootAliasPackage) SetRequires(requires Links) {
	a.requires = a.replaceSelfVersionDependencies(requires, TypeRequire)
	a.rev++
	a.root.SetRequires(requires)
}

// SetDevRequires ports RootAliasPackage::setDevRequires.
func (a *RootAliasPackage) SetDevRequires(devRequires Links) {
	a.devRequires = a.replaceSelfVersionDependencies(devRequires, TypeDevRequire)
	a.rev++
	a.root.SetDevRequires(devRequires)
}

// SetConflicts ports RootAliasPackage::setConflicts.
func (a *RootAliasPackage) SetConflicts(conflicts Links) {
	a.conflicts = a.replaceSelfVersionDependencies(conflicts, TypeConflict)
	a.rev++
	a.root.SetConflicts(conflicts)
}

// SetProvides ports RootAliasPackage::setProvides.
func (a *RootAliasPackage) SetProvides(provides Links) {
	a.provides = a.replaceSelfVersionDependencies(provides, TypeProvide)
	a.rev++
	a.root.SetProvides(provides)
}

// SetReplaces ports RootAliasPackage::setReplaces.
func (a *RootAliasPackage) SetReplaces(replaces Links) {
	a.replaces = a.replaceSelfVersionDependencies(replaces, TypeReplace)
	a.rev++
	a.root.SetReplaces(replaces)
}

// SetOwnLinks is the first statement of RootAliasPackage::setRequires
// (setDevRequires, ... as setter names): the alias's own links, without
// the aliased package's. With list-shaped links PHP stops there, as the
// aliased package's setter throws (LinksListError).
func (a *RootAliasPackage) SetOwnLinks(setter string, links Links) {
	switch setter {
	case "setRequires":
		a.requires = a.replaceSelfVersionDependencies(links, TypeRequire)
	case "setDevRequires":
		a.devRequires = a.replaceSelfVersionDependencies(links, TypeDevRequire)
	case "setConflicts":
		a.conflicts = a.replaceSelfVersionDependencies(links, TypeConflict)
	case "setProvides":
		a.provides = a.replaceSelfVersionDependencies(links, TypeProvide)
	case "setReplaces":
		a.replaces = a.replaceSelfVersionDependencies(links, TypeReplace)
	default:
		return
	}

	a.rev++
}

// SetAutoload ports RootAliasPackage::setAutoload.
func (a *RootAliasPackage) SetAutoload(autoload *php.Array) { a.root.SetAutoload(autoload) }

// SetDevAutoload ports RootAliasPackage::setDevAutoload.
func (a *RootAliasPackage) SetDevAutoload(devAutoload *php.Array) { a.root.SetDevAutoload(devAutoload) }

// SetStabilityFlags ports RootAliasPackage::setStabilityFlags.
func (a *RootAliasPackage) SetStabilityFlags(stabilityFlags *php.Array) {
	a.root.SetStabilityFlags(stabilityFlags)
}

// SetMinimumStability ports RootAliasPackage::setMinimumStability.
func (a *RootAliasPackage) SetMinimumStability(minimumStability string) {
	a.root.SetMinimumStability(minimumStability)
}

// SetPreferStable ports RootAliasPackage::setPreferStable.
func (a *RootAliasPackage) SetPreferStable(preferStable bool) { a.root.SetPreferStable(preferStable) }

// SetConfig ports RootAliasPackage::setConfig.
func (a *RootAliasPackage) SetConfig(config *php.Array) { a.root.SetConfig(config) }

// SetReferences ports RootAliasPackage::setReferences.
func (a *RootAliasPackage) SetReferences(references *php.Array) { a.root.SetReferences(references) }

// SetAliases ports RootAliasPackage::setAliases.
func (a *RootAliasPackage) SetAliases(aliases *php.Array) { a.root.SetAliases(aliases) }

// SetSuggests ports RootAliasPackage::setSuggests.
func (a *RootAliasPackage) SetSuggests(suggests *php.Array) { a.root.SetSuggests(suggests) }

// SetExtra ports RootAliasPackage::setExtra.
func (a *RootAliasPackage) SetExtra(extra *php.Array) { a.root.SetExtra(extra) }
