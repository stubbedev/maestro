// Ports src/Composer/Package/AliasPackage.php.

package pkg

import (
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
)

// AliasPackage ports Composer\Package\AliasPackage: another version of a
// package, created by a branch alias or an inline alias.
type AliasPackage struct {
	basePackage

	version                string
	prettyVersion          string
	aliasOf                PackageInterface
	requires               Links
	devRequires            Links
	conflicts              Links
	provides               Links
	replaces               Links
	stability              stability
	rootPackageAlias       bool
	hasSelfVersionRequires bool
}

// NewAliasPackage ports AliasPackage::__construct: aliasOf under the
// normalized version and prettyVersion. Links with a self.version
// constraint are rewritten to the alias's version.
//
// PHP throws when one of aliasOf's links has no pretty constraint (which
// only a plugin can build); such links are left as they are here.
func NewAliasPackage(aliasOf PackageInterface, version, prettyVersion string) *AliasPackage {
	a := &AliasPackage{}
	a.init(aliasOf, version, prettyVersion)

	return a
}

func (a *AliasPackage) init(aliasOf PackageInterface, version, prettyVersion string) {
	a.basePackage = newBasePackage(aliasOf.Name())
	a.version = version
	a.prettyVersion = prettyVersion
	a.aliasOf = aliasOf
	a.stability = parseStability(version)

	a.requires = a.replaceSelfVersionDependencies(aliasOf.Requires(), TypeRequire)
	a.devRequires = a.replaceSelfVersionDependencies(aliasOf.DevRequires(), TypeDevRequire)
	a.provides = a.replaceSelfVersionDependencies(aliasOf.Provides(), TypeProvide)
	a.conflicts = a.replaceSelfVersionDependencies(aliasOf.Conflicts(), TypeConflict)
	a.replaces = a.replaceSelfVersionDependencies(aliasOf.Replaces(), TypeReplace)
}

// PHPClass returns ClassAliasPackage.
func (a *AliasPackage) PHPClass() string { return ClassAliasPackage }

// Rev returns the change counter of the alias plus the aliased package's.
func (a *AliasPackage) Rev() uint64 { return a.rev + a.idRev + a.aliasOf.Rev() }

// FieldRev returns Rev without the changes of either package's id.
func (a *AliasPackage) FieldRev() uint64 { return a.rev + a.aliasOf.FieldRev() }

// Clone ports `clone $alias` (the aliased package stays shared).
func (a *AliasPackage) Clone() *AliasPackage {
	c := *a
	c.clearForClone()

	return &c
}

// AliasOf ports AliasPackage::getAliasOf.
func (a *AliasPackage) AliasOf() PackageInterface { return a.aliasOf }

// Version ports AliasPackage::getVersion.
func (a *AliasPackage) Version() string { return a.version }

// Stability ports AliasPackage::getStability.
func (a *AliasPackage) Stability() string { return stabilityNames[a.stability] }

// PrettyVersion ports AliasPackage::getPrettyVersion.
func (a *AliasPackage) PrettyVersion() string { return a.prettyVersion }

// IsDev ports AliasPackage::isDev.
func (a *AliasPackage) IsDev() bool { return a.stability == stabilityIndexDev }

// Requires ports AliasPackage::getRequires.
func (a *AliasPackage) Requires() Links { return a.requires }

// Conflicts ports AliasPackage::getConflicts.
func (a *AliasPackage) Conflicts() Links { return a.conflicts }

// Provides ports AliasPackage::getProvides.
func (a *AliasPackage) Provides() Links { return a.provides }

// Replaces ports AliasPackage::getReplaces.
func (a *AliasPackage) Replaces() Links { return a.replaces }

// DevRequires ports AliasPackage::getDevRequires.
func (a *AliasPackage) DevRequires() Links { return a.devRequires }

// SetRootPackageAlias ports AliasPackage::setRootPackageAlias: stores
// whether the alias was defined by the root package.
func (a *AliasPackage) SetRootPackageAlias(value bool) {
	a.rootPackageAlias = value
	a.changed()
}

// IsRootPackageAlias ports AliasPackage::isRootPackageAlias.
func (a *AliasPackage) IsRootPackageAlias() bool { return a.rootPackageAlias }

// replaceSelfVersionDependencies ports
// AliasPackage::replaceSelfVersionDependencies.
func (a *AliasPackage) replaceSelfVersionDependencies(links Links, linkType string) Links {
	// for self.version requirements, we use the original package's branch name instead, to avoid leaking the magic dev-master-alias to users
	prettyVersion := a.prettyVersion
	if prettyVersion == DefaultBranchAlias {
		prettyVersion = a.aliasOf.PrettyVersion()
	}

	newLink := func(l *Link) *Link {
		constraint := semver.NewConstraintOp(semver.OpEQ, a.version)
		constraint.SetPrettyString(prettyVersion)

		return NewLink(l.source, l.target, constraint, linkType, Str(prettyVersion))
	}

	if linkType == TypeConflict || linkType == TypeProvide || linkType == TypeReplace {
		var added []*Link

		for l := range links.Values() {
			// link is self.version, but must be replacing also the replaced version
			if l.isSelfVersion() {
				added = append(added, newLink(l))
			}
		}

		return links.mergeList(added)
	}

	var b *LinksBuilder

	for i := range links.Len() {
		l := links.At(i)
		if !l.isSelfVersion() {
			continue
		}

		if linkType == TypeRequire {
			a.hasSelfVersionRequires = true
		}

		if b == nil {
			b = &LinksBuilder{}
			b.Grow(links.Len())

			for k, v := range links.All() {
				b.Append(k, v)
			}
		}

		b.Set(links.Key(i), newLink(l))
	}

	if b == nil {
		return links
	}

	return b.Build()
}

// HasSelfVersionRequires ports AliasPackage::hasSelfVersionRequires.
func (a *AliasPackage) HasSelfVersionRequires() bool { return a.hasSelfVersionRequires }

// String ports AliasPackage::__toString.
func (a *AliasPackage) String() string {
	root := ""
	if a.rootPackageAlias {
		root = "root "
	}

	return uniqueName(a) + " (" + root + "alias of " + a.aliasOf.Version() + ")"
}

// Wrappers for the aliased package.

// Type ports AliasPackage::getType.
func (a *AliasPackage) Type() string { return a.aliasOf.Type() }

// TargetDir ports AliasPackage::getTargetDir.
func (a *AliasPackage) TargetDir() NullString { return a.aliasOf.TargetDir() }

// Extra ports AliasPackage::getExtra.
func (a *AliasPackage) Extra() *php.Array { return a.aliasOf.Extra() }

// SetInstallationSource ports AliasPackage::setInstallationSource.
func (a *AliasPackage) SetInstallationSource(source Null[InstallationSource]) {
	a.aliasOf.SetInstallationSource(source)
}

// InstallationSource ports AliasPackage::getInstallationSource.
func (a *AliasPackage) InstallationSource() Null[InstallationSource] {
	return a.aliasOf.InstallationSource()
}

// SourceType ports AliasPackage::getSourceType.
func (a *AliasPackage) SourceType() NullString { return a.aliasOf.SourceType() }

// SourceURL ports AliasPackage::getSourceUrl.
func (a *AliasPackage) SourceURL() NullString { return a.aliasOf.SourceURL() }

// SourceURLs ports AliasPackage::getSourceUrls.
func (a *AliasPackage) SourceURLs() []string { return a.aliasOf.SourceURLs() }

// SourceReference ports AliasPackage::getSourceReference.
func (a *AliasPackage) SourceReference() NullString { return a.aliasOf.SourceReference() }

// SetSourceReference ports AliasPackage::setSourceReference.
func (a *AliasPackage) SetSourceReference(reference NullString) {
	a.aliasOf.SetSourceReference(reference)
}

// SetSourceMirrors ports AliasPackage::setSourceMirrors.
func (a *AliasPackage) SetSourceMirrors(mirrors *php.Array) { a.aliasOf.SetSourceMirrors(mirrors) }

// SourceMirrors ports AliasPackage::getSourceMirrors.
func (a *AliasPackage) SourceMirrors() *php.Array { return a.aliasOf.SourceMirrors() }

// DistType ports AliasPackage::getDistType.
func (a *AliasPackage) DistType() NullString { return a.aliasOf.DistType() }

// DistURL ports AliasPackage::getDistUrl.
func (a *AliasPackage) DistURL() NullString { return a.aliasOf.DistURL() }

// DistURLs ports AliasPackage::getDistUrls.
func (a *AliasPackage) DistURLs() []string { return a.aliasOf.DistURLs() }

// DistReference ports AliasPackage::getDistReference.
func (a *AliasPackage) DistReference() NullString { return a.aliasOf.DistReference() }

// SetDistReference ports AliasPackage::setDistReference.
func (a *AliasPackage) SetDistReference(reference NullString) { a.aliasOf.SetDistReference(reference) }

// DistSha1Checksum ports AliasPackage::getDistSha1Checksum.
func (a *AliasPackage) DistSha1Checksum() NullString { return a.aliasOf.DistSha1Checksum() }

// SetTransportOptions ports AliasPackage::setTransportOptions.
func (a *AliasPackage) SetTransportOptions(options *php.Array) {
	a.aliasOf.SetTransportOptions(options)
}

// TransportOptions ports AliasPackage::getTransportOptions.
func (a *AliasPackage) TransportOptions() *php.Array { return a.aliasOf.TransportOptions() }

// SetDistMirrors ports AliasPackage::setDistMirrors.
func (a *AliasPackage) SetDistMirrors(mirrors *php.Array) { a.aliasOf.SetDistMirrors(mirrors) }

// DistMirrors ports AliasPackage::getDistMirrors.
func (a *AliasPackage) DistMirrors() *php.Array { return a.aliasOf.DistMirrors() }

// Autoload ports AliasPackage::getAutoload.
func (a *AliasPackage) Autoload() *php.Array { return a.aliasOf.Autoload() }

// DevAutoload ports AliasPackage::getDevAutoload.
func (a *AliasPackage) DevAutoload() *php.Array { return a.aliasOf.DevAutoload() }

// IncludePaths ports AliasPackage::getIncludePaths.
func (a *AliasPackage) IncludePaths() *php.Array { return a.aliasOf.IncludePaths() }

// PhpExt ports AliasPackage::getPhpExt.
func (a *AliasPackage) PhpExt() *php.Array { return a.aliasOf.PhpExt() }

// ReleaseDate ports AliasPackage::getReleaseDate.
func (a *AliasPackage) ReleaseDate() (time.Time, bool) { return a.aliasOf.ReleaseDate() }

// Binaries ports AliasPackage::getBinaries.
func (a *AliasPackage) Binaries() *php.Array { return a.aliasOf.Binaries() }

// Suggests ports AliasPackage::getSuggests.
func (a *AliasPackage) Suggests() *php.Array { return a.aliasOf.Suggests() }

// NotificationURL ports AliasPackage::getNotificationUrl.
func (a *AliasPackage) NotificationURL() NullString { return a.aliasOf.NotificationURL() }

// IsDefaultBranch ports AliasPackage::isDefaultBranch.
func (a *AliasPackage) IsDefaultBranch() bool { return a.aliasOf.IsDefaultBranch() }

// SetDistURL ports AliasPackage::setDistUrl.
func (a *AliasPackage) SetDistURL(url NullString) { a.aliasOf.SetDistURL(url) }

// SetDistType ports AliasPackage::setDistType.
func (a *AliasPackage) SetDistType(typ NullString) { a.aliasOf.SetDistType(typ) }

// SetSourceDistReferences ports AliasPackage::setSourceDistReferences.
func (a *AliasPackage) SetSourceDistReferences(reference string) {
	a.aliasOf.SetSourceDistReferences(reference)
}

// BasePackage methods.

// Names ports BasePackage::getNames.
func (a *AliasPackage) Names(provides bool) []string { return names(a, provides) }

// UniqueName ports BasePackage::getUniqueName.
func (a *AliasPackage) UniqueName() string { return uniqueName(a) }

// PrettyString ports BasePackage::getPrettyString.
func (a *AliasPackage) PrettyString() string { return prettyString(a) }

// FullPrettyVersion ports BasePackage::getFullPrettyVersion.
func (a *AliasPackage) FullPrettyVersion(truncate bool, displayMode DisplayMode) string {
	return fullPrettyVersion(a, truncate, displayMode)
}

// Equals ports BasePackage::equals.
func (a *AliasPackage) Equals(other PackageInterface) bool { return equals(a, other) }

// StabilityPriority ports BasePackage::getStabilityPriority.
func (a *AliasPackage) StabilityPriority() int { return stabilityPriority(a) }
