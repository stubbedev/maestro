// Ports src/Composer/Package/RootPackage.php.

package pkg

import "github.com/stubbedev/maestro/internal/php"

// DefaultPrettyVersion is RootPackage::DEFAULT_PRETTY_VERSION.
const DefaultPrettyVersion = "1.0.0+no-version-set"

// RootPackage ports Composer\Package\RootPackage: the project's own
// package.
type RootPackage struct {
	CompletePackage

	minimumStability string
	preferStable     bool
	stabilityFlags   *php.Array
	config           *php.Array
	references       *php.Array
	aliases          *php.Array
}

// NewRootPackage ports RootPackage::__construct (Package's).
func NewRootPackage(name, version, prettyVersion string) *RootPackage {
	p := &RootPackage{minimumStability: "stable"}
	p.init(name, version, prettyVersion)
	p.abandoned = false

	return p
}

// PHPClass returns ClassRootPackage.
func (p *RootPackage) PHPClass() string { return ClassRootPackage }

// Clone ports `clone $package`.
func (p *RootPackage) Clone() *RootPackage {
	c := *p
	c.clearForClone()

	return &c
}

// SetMinimumStability ports RootPackage::setMinimumStability.
func (p *RootPackage) SetMinimumStability(minimumStability string) {
	p.minimumStability = minimumStability
	p.changed()
}

// MinimumStability ports RootPackage::getMinimumStability.
func (p *RootPackage) MinimumStability() string { return p.minimumStability }

// SetStabilityFlags ports RootPackage::setStabilityFlags: package name =>
// BasePackage::STABILITY_* value.
func (p *RootPackage) SetStabilityFlags(stabilityFlags *php.Array) {
	p.stabilityFlags = stabilityFlags
	p.changed()
}

// StabilityFlags ports RootPackage::getStabilityFlags.
func (p *RootPackage) StabilityFlags() *php.Array { return orEmpty(p.stabilityFlags) }

// SetPreferStable ports RootPackage::setPreferStable.
func (p *RootPackage) SetPreferStable(preferStable bool) {
	p.preferStable = preferStable
	p.changed()
}

// PreferStable ports RootPackage::getPreferStable.
func (p *RootPackage) PreferStable() bool { return p.preferStable }

// SetConfig ports RootPackage::setConfig.
func (p *RootPackage) SetConfig(config *php.Array) {
	p.config = config
	p.changed()
}

// Config ports RootPackage::getConfig.
func (p *RootPackage) Config() *php.Array { return orEmpty(p.config) }

// SetReferences ports RootPackage::setReferences.
func (p *RootPackage) SetReferences(references *php.Array) {
	p.references = references
	p.changed()
}

// References ports RootPackage::getReferences.
func (p *RootPackage) References() *php.Array { return orEmpty(p.references) }

// SetAliases ports RootPackage::setAliases.
func (p *RootPackage) SetAliases(aliases *php.Array) {
	p.aliases = aliases
	p.changed()
}

// Aliases ports RootPackage::getAliases.
func (p *RootPackage) Aliases() *php.Array { return orEmpty(p.aliases) }
