// Ports src/Composer/Package/Package.php.

package pkg

import (
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// Bits of Package.set: which nullable properties are non-null.
const (
	setType uint16 = 1 << iota
	setTargetDir
	setInstallationSource
	setSourceType
	setSourceURL
	setSourceReference
	setDistType
	setDistURL
	setDistReference
	setDistSha1Checksum
	setNotificationURL
	setReleaseDate
	setDescription
	setHomepage
	setArchiveName
)

// stability is a package's stability as an index into stabilityNames.
type stability uint8

const stabilityIndexDev stability = 4

func parseStability(version string) stability {
	switch semver.ParseStability(version) {
	case semver.StabilityRC:
		return 1
	case semver.StabilityBeta:
		return 2
	case semver.StabilityAlpha:
		return 3
	case semver.StabilityDev:
		return stabilityIndexDev
	}

	return 0
}

// Package ports Composer\Package\Package, the core package definition.
type Package struct {
	basePackage

	version       string
	prettyVersion string

	typ                string
	targetDir          string
	installationSource InstallationSource
	sourceType         string
	sourceURL          string
	sourceReference    string
	distType           string
	distURL            string
	distReference      string
	distSha1Checksum   string
	notificationURL    string
	releaseDate        time.Time

	sourceMirrors    *php.Array
	distMirrors      *php.Array
	extra            *php.Array
	binaries         *php.Array
	suggests         *php.Array
	autoload         *php.Array
	devAutoload      *php.Array
	includePaths     *php.Array
	transportOptions *php.Array
	phpExt           *php.Array

	requires    Links
	conflicts   Links
	provides    Links
	replaces    Links
	devRequires Links

	set             uint16
	stability       stability
	isDefaultBranch bool

	// lazy is set for a skeleton package (NewSkeletonPackage): the methods
	// that read or write more than its skeleton fields call need first
	lazy *lazyRest
}

// NewPackage ports Package::__construct: version is the normalized
// version, prettyVersion the one to display.
func NewPackage(name, version, prettyVersion string) *Package {
	p := &Package{}
	p.init(name, version, prettyVersion)

	return p
}

func (p *Package) init(name, version, prettyVersion string) {
	p.basePackage = newBasePackage(name)
	p.version = version
	p.prettyVersion = prettyVersion
	p.stability = parseStability(version)
}

func nullable[T ~string](p *Package, bit uint16, s T) Null[T] {
	return Null[T]{S: s, Valid: p.set&bit != 0}
}

func setNullable[T ~string](p *Package, bit uint16, dst *T, v Null[T]) {
	*dst = v.S
	if v.Valid {
		p.set |= bit
	} else {
		p.set &^= bit
	}

	p.changed()
}

func orEmpty(a *php.Array) *php.Array {
	if a == nil {
		return php.NewArray()
	}

	return a
}

// PHPClass returns ClassPackage.
func (p *Package) PHPClass() string { return ClassPackage }

// Rev returns the change counter.
func (p *Package) Rev() uint64 {
	p.need()

	return p.rev + p.idRev
}

// FieldRev returns the change counter of every field but the id.
func (p *Package) FieldRev() uint64 {
	p.need()

	return p.rev
}

// RevsSoFar returns Rev and FieldRev of p without loading a skeleton,
// whose own counters they then are: those of a view that reads the
// skeleton's properties only until something loads the rest (the
// plugin runtime's mirror of a package's core fields). The load may move
// them, as the configuring put off until then may change the package.
func RevsSoFar(p PackageInterface) (rev, fieldRev uint64) {
	if r, ok := p.(interface{ revsSoFar() (uint64, uint64) }); ok {
		return r.revsSoFar()
	}

	return p.Rev(), p.FieldRev()
}

func (p *Package) revsSoFar() (rev, fieldRev uint64) { return p.rev + p.idRev, p.rev }

// RawTypeSoFar is RawType without loading a skeleton, whose type is
// always set (the loader's configureType sets it first, and the load
// leaves it).
func RawTypeSoFar(p PackageInterface) NullString {
	if l := skeletonOf(p); l != nil && l.owner == p {
		return Str(l.owner.typ)
	}
	if pp, ok := AsPackage(p); ok {
		return pp.RawType()
	}

	return Str(p.Type())
}

// Clone ports `clone $package`.
func (p *Package) Clone() *Package {
	p.need()
	c := *p
	c.clearForClone()

	return &c
}

// IsDev ports Package::isDev.
func (p *Package) IsDev() bool { return p.stability == stabilityIndexDev }

// SetType ports Package::setType.
func (p *Package) SetType(typ string) {
	p.need()

	setNullable(p, setType, &p.typ, Str(typ))
}

// RawType returns the type as stored, null when never set.
func (p *Package) RawType() NullString {
	p.need()

	return nullable(p, setType, p.typ)
}

// Type ports Package::getType: "library" when unset or falsy.
func (p *Package) Type() string {
	if !php.Truthy(p.typ) {
		return LibraryType
	}

	return p.typ
}

// Stability ports Package::getStability.
func (p *Package) Stability() string { return stabilityNames[p.stability] }

// SetTargetDir ports Package::setTargetDir.
func (p *Package) SetTargetDir(targetDir NullString) {
	p.need()
	setNullable(p, setTargetDir, &p.targetDir, targetDir)
}

var targetDirDots = php.MustCompile(`{ (?:^|[\\/]+) \.\.? (?:[\\/]+|$) (?:\.\.? (?:[\\/]+|$) )*}x`)

// RawTargetDir returns the target dir as set.
func (p *Package) RawTargetDir() NullString {
	p.need()

	return nullable(p, setTargetDir, p.targetDir)
}

// TargetDir ports Package::getTargetDir: the target dir with . and ..
// segments removed.
func (p *Package) TargetDir() NullString {
	p.need()
	if p.set&setTargetDir == 0 {
		return NullString{}
	}

	s, _, err := targetDirDots.Replace(p.targetDir, "/", -1)
	if err != nil {
		// Preg::replace throws on a matching error, which this pattern
		// cannot reach on any input.
		panic(err)
	}

	return Str(php.LtrimSet(s, "/"))
}

// SetExtra ports Package::setExtra.
func (p *Package) SetExtra(extra *php.Array) {
	p.need()
	p.extra = extra
	p.changed()
}

// Extra ports Package::getExtra; a skeleton has it (plugins read it of
// every package in a pool).
func (p *Package) Extra() *php.Array {
	return orEmpty(p.extra)
}

// SetBinaries ports Package::setBinaries.
func (p *Package) SetBinaries(binaries *php.Array) {
	p.need()
	p.binaries = binaries
	p.changed()
}

// Binaries ports Package::getBinaries.
func (p *Package) Binaries() *php.Array {
	p.need()

	return orEmpty(p.binaries)
}

// SetInstallationSource ports Package::setInstallationSource.
func (p *Package) SetInstallationSource(source Null[InstallationSource]) {
	p.need()
	setNullable(p, setInstallationSource, &p.installationSource, source)
}

// InstallationSource ports Package::getInstallationSource.
func (p *Package) InstallationSource() Null[InstallationSource] {
	p.need()
	return nullable(p, setInstallationSource, p.installationSource)
}

// SetSourceType ports Package::setSourceType.
func (p *Package) SetSourceType(typ NullString) {
	p.need()

	setNullable(p, setSourceType, &p.sourceType, typ)
}

// SourceType ports Package::getSourceType.
func (p *Package) SourceType() NullString {
	p.need()

	return nullable(p, setSourceType, p.sourceType)
}

// SetSourceURL ports Package::setSourceUrl.
func (p *Package) SetSourceURL(url NullString) {
	p.need()

	setNullable(p, setSourceURL, &p.sourceURL, url)
}

// SourceURL ports Package::getSourceUrl.
func (p *Package) SourceURL() NullString {
	p.need()

	return nullable(p, setSourceURL, p.sourceURL)
}

// SetSourceReference ports Package::setSourceReference.
func (p *Package) SetSourceReference(reference NullString) {
	p.need()
	setNullable(p, setSourceReference, &p.sourceReference, reference)
}

// SourceReference ports Package::getSourceReference.
func (p *Package) SourceReference() NullString {
	p.need()
	return nullable(p, setSourceReference, p.sourceReference)
}

// SetSourceMirrors ports Package::setSourceMirrors; nil is null.
func (p *Package) SetSourceMirrors(mirrors *php.Array) {
	p.need()
	p.sourceMirrors = mirrors
	p.changed()
}

// SourceMirrors ports Package::getSourceMirrors; nil is null.
func (p *Package) SourceMirrors() *php.Array {
	p.need()

	return p.sourceMirrors
}

// SourceURLs ports Package::getSourceUrls.
func (p *Package) SourceURLs() []string {
	p.need()
	return p.urls(p.SourceURL(), p.sourceMirrors, p.SourceReference(), p.SourceType(), FromSource)
}

// SetDistType ports Package::setDistType: "" is stored as null.
func (p *Package) SetDistType(typ NullString) {
	p.need()
	if typ.S == "" {
		typ = NullString{}
	}

	setNullable(p, setDistType, &p.distType, typ)
}

// DistType ports Package::getDistType.
func (p *Package) DistType() NullString {
	p.need()

	return nullable(p, setDistType, p.distType)
}

// SetDistURL ports Package::setDistUrl: "" is stored as null.
func (p *Package) SetDistURL(url NullString) {
	p.need()
	if url.S == "" {
		url = NullString{}
	}

	setNullable(p, setDistURL, &p.distURL, url)
}

// DistURL ports Package::getDistUrl.
func (p *Package) DistURL() NullString {
	p.need()

	return nullable(p, setDistURL, p.distURL)
}

// SetDistReference ports Package::setDistReference.
func (p *Package) SetDistReference(reference NullString) {
	p.need()
	setNullable(p, setDistReference, &p.distReference, reference)
}

// DistReference ports Package::getDistReference.
func (p *Package) DistReference() NullString {
	p.need()

	return nullable(p, setDistReference, p.distReference)
}

// SetDistSha1Checksum ports Package::setDistSha1Checksum.
func (p *Package) SetDistSha1Checksum(sha1checksum NullString) {
	p.need()
	setNullable(p, setDistSha1Checksum, &p.distSha1Checksum, sha1checksum)
}

// DistSha1Checksum ports Package::getDistSha1Checksum.
func (p *Package) DistSha1Checksum() NullString {
	p.need()
	return nullable(p, setDistSha1Checksum, p.distSha1Checksum)
}

// SetDistMirrors ports Package::setDistMirrors; nil is null.
func (p *Package) SetDistMirrors(mirrors *php.Array) {
	p.need()
	p.distMirrors = mirrors
	p.changed()
}

// DistMirrors ports Package::getDistMirrors; nil is null.
func (p *Package) DistMirrors() *php.Array {
	p.need()

	return p.distMirrors
}

// DistURLs ports Package::getDistUrls.
func (p *Package) DistURLs() []string {
	p.need()
	return p.urls(p.DistURL(), p.distMirrors, p.DistReference(), p.DistType(), FromDist)
}

// TransportOptions ports Package::getTransportOptions.
func (p *Package) TransportOptions() *php.Array {
	p.need()

	return orEmpty(p.transportOptions)
}

// SetTransportOptions ports Package::setTransportOptions.
func (p *Package) SetTransportOptions(options *php.Array) {
	p.need()
	p.transportOptions = options
	p.changed()
}

// Version ports Package::getVersion.
func (p *Package) Version() string { return p.version }

// PrettyVersion ports Package::getPrettyVersion.
func (p *Package) PrettyVersion() string { return p.prettyVersion }

// SetReleaseDate ports Package::setReleaseDate; ok false is null.
func (p *Package) SetReleaseDate(releaseDate time.Time, ok bool) {
	p.need()
	p.releaseDate = releaseDate
	if ok {
		p.set |= setReleaseDate
	} else {
		p.set &^= setReleaseDate
		p.releaseDate = time.Time{}
	}

	p.changed()
}

// ReleaseDate ports Package::getReleaseDate; false is null.
func (p *Package) ReleaseDate() (time.Time, bool) {
	p.need()
	return p.releaseDate, p.set&setReleaseDate != 0
}

// setLinks stores links, converting a list as Package::setRequires and
// friends do when the notice they raise is not thrown (under Composer's
// ErrorHandler it is: LinksListError).
func (p *Package) setLinks(dst *Links, links Links) {
	if links.hasZeroKey() {
		links = LinksOf(collectLinks(links)...)
	}

	*dst = links
	p.changed()
}

func collectLinks(l Links) []*Link {
	out := make([]*Link, 0, l.Len())
	for v := range l.Values() {
		out = append(out, v)
	}

	return out
}

// SetRequires ports Package::setRequires.
func (p *Package) SetRequires(requires Links) {
	p.need()

	p.setLinks(&p.requires, requires)
}

// Requires ports Package::getRequires.
func (p *Package) Requires() Links { return p.requires }

// SetConflicts ports Package::setConflicts.
func (p *Package) SetConflicts(conflicts Links) {
	p.need()

	p.setLinks(&p.conflicts, conflicts)
}

// Conflicts ports Package::getConflicts.
func (p *Package) Conflicts() Links { return p.conflicts }

// SetProvides ports Package::setProvides.
func (p *Package) SetProvides(provides Links) {
	p.need()

	p.setLinks(&p.provides, provides)
}

// Provides ports Package::getProvides.
func (p *Package) Provides() Links { return p.provides }

// SetReplaces ports Package::setReplaces.
func (p *Package) SetReplaces(replaces Links) {
	p.need()

	p.setLinks(&p.replaces, replaces)
}

// Replaces ports Package::getReplaces.
func (p *Package) Replaces() Links { return p.replaces }

// SetDevRequires ports Package::setDevRequires.
func (p *Package) SetDevRequires(devRequires Links) {
	p.need()

	p.setLinks(&p.devRequires, devRequires)
}

// DevRequires ports Package::getDevRequires.
func (p *Package) DevRequires() Links { return p.devRequires }

// SetSuggests ports Package::setSuggests.
func (p *Package) SetSuggests(suggests *php.Array) {
	p.need()
	p.suggests = suggests
	p.changed()
}

// Suggests ports Package::getSuggests.
func (p *Package) Suggests() *php.Array {
	p.need()

	return orEmpty(p.suggests)
}

// SetAutoload ports Package::setAutoload.
func (p *Package) SetAutoload(autoload *php.Array) {
	p.need()
	p.autoload = autoload
	p.changed()
}

// Autoload ports Package::getAutoload.
func (p *Package) Autoload() *php.Array {
	p.need()

	return orEmpty(p.autoload)
}

// SetDevAutoload ports Package::setDevAutoload.
func (p *Package) SetDevAutoload(devAutoload *php.Array) {
	p.need()
	p.devAutoload = devAutoload
	p.changed()
}

// DevAutoload ports Package::getDevAutoload.
func (p *Package) DevAutoload() *php.Array {
	p.need()

	return orEmpty(p.devAutoload)
}

// SetIncludePaths ports Package::setIncludePaths.
func (p *Package) SetIncludePaths(includePaths *php.Array) {
	p.need()
	p.includePaths = includePaths
	p.changed()
}

// IncludePaths ports Package::getIncludePaths.
func (p *Package) IncludePaths() *php.Array {
	p.need()

	return orEmpty(p.includePaths)
}

// SetPhpExt ports Package::setPhpExt; nil is null.
func (p *Package) SetPhpExt(phpExt *php.Array) {
	p.need()
	p.phpExt = phpExt
	p.changed()
}

// PhpExt ports Package::getPhpExt; nil is null.
func (p *Package) PhpExt() *php.Array {
	p.need()

	return p.phpExt
}

// SetNotificationURL ports Package::setNotificationUrl.
func (p *Package) SetNotificationURL(notificationURL string) {
	p.need()
	setNullable(p, setNotificationURL, &p.notificationURL, Str(notificationURL))
}

// NotificationURL ports Package::getNotificationUrl.
func (p *Package) NotificationURL() NullString {
	p.need()
	return nullable(p, setNotificationURL, p.notificationURL)
}

// SetIsDefaultBranch ports Package::setIsDefaultBranch.
func (p *Package) SetIsDefaultBranch(defaultBranch bool) {
	p.need()
	p.isDefaultBranch = defaultBranch
	p.changed()
}

// IsDefaultBranch ports Package::isDefaultBranch.
func (p *Package) IsDefaultBranch() bool { return p.isDefaultBranch }

var (
	autoDistURLHost = php.MustCompile(`{^https?://(?:(?:www\.)?bitbucket\.org|(api\.)?github\.com|(?:www\.)?gitlab\.com)/}i`)
	distURLSha      = php.MustCompile(`{(?<=/|sha=)[a-f0-9]{40}(?=/|$)}i`)
)

// SetSourceDistReferences ports Package::setSourceDistReferences: the
// source reference, and the dist reference (and URL, for GitHub, GitLab
// and Bitbucket) when there is one.
func (p *Package) SetSourceDistReferences(reference string) {
	p.need()
	p.SetSourceReference(Str(reference))

	// only bitbucket, github and gitlab have auto generated dist URLs that easily allow replacing the reference in the dist URL
	distURL := p.DistURL()
	if distURL.Valid && mustMatch(autoDistURLHost, distURL.S) {
		p.SetDistReference(Str(reference))

		url, _, err := distURLSha.Replace(distURL.S, reference, -1)
		if err != nil {
			panic(err)
		}

		p.SetDistURL(Str(url))
	} else if ref := p.DistReference(); php.ToBool(ref.Value()) { // update the dist reference if there was one, but if none was provided ignore it
		p.SetDistReference(Str(reference))
	}
}

// mustMatch is Preg::isMatch for patterns that cannot fail at run time.
func mustMatch(re *php.Regexp, subject string) bool {
	ok, err := re.IsMatch(subject)
	if err != nil {
		panic(err)
	}

	return ok
}

// ReplaceVersion ports Package::replaceVersion.
func (p *Package) ReplaceVersion(version, prettyVersion string) {
	p.need()
	p.version = version
	p.prettyVersion = prettyVersion
	p.stability = parseStability(version)
	p.changed()
}

// urls ports Package::getUrls.
func (p *Package) urls(url NullString, mirrors *php.Array, ref, typ NullString, urlType InstallationSource) []string {
	if !php.ToBool(url.Value()) {
		return []string{}
	}

	u := url.S
	if urlType == FromDist && strings.Contains(u, "%") {
		u = util.ComposerMirrorProcessURL(u, p.name[0], p.version, ref.ptr(), typ.ptr(), &p.prettyVersion)
	}

	urls := []string{u}

	if mirrors != nil && mirrors.Len() > 0 {
		for _, m := range mirrors.All() {
			mirror, _ := m.(*php.Array)

			var mirrorURL string

			switch {
			case urlType == FromDist:
				mirrorURL = util.ComposerMirrorProcessURL(arrayString(mirror, "url"), p.name[0], p.version, ref.ptr(), typ.ptr(), &p.prettyVersion)
			case urlType == FromSource && typ == Str("git"):
				mirrorURL = util.ComposerMirrorProcessGitURL(arrayString(mirror, "url"), p.name[0], u, typ.ptr())
			case urlType == FromSource && typ == Str("hg"):
				mirrorURL = util.ComposerMirrorProcessHgURL(arrayString(mirror, "url"), p.name[0], u, typ.S)
			default:
				continue
			}

			if !slices.ContainsFunc(urls, func(u string) bool { return php.LooseEquals(u, mirrorURL) }) {
				if php.ToBool(mirror.At("preferred")) {
					urls = slices.Insert(urls, 0, mirrorURL)
				} else {
					urls = append(urls, mirrorURL)
				}
			}
		}
	}

	return urls
}

// arrayString returns (string) $a[$k].
func arrayString(a *php.Array, k string) string { return php.ToString(a.At(k)) }

// Methods implemented on top of the interface, as BasePackage's are.

// Names ports BasePackage::getNames.
func (p *Package) Names(provides bool) []string { return names(p, &p.basePackage, provides) }

// UniqueName ports BasePackage::getUniqueName.
func (p *Package) UniqueName() string { return uniqueName(p) }

// String ports BasePackage::__toString.
func (p *Package) String() string { return uniqueName(p) }

// PrettyString ports BasePackage::getPrettyString.
func (p *Package) PrettyString() string { return prettyString(p) }

// FullPrettyVersion ports BasePackage::getFullPrettyVersion.
func (p *Package) FullPrettyVersion(truncate bool, displayMode DisplayMode) string {
	return fullPrettyVersion(p, truncate, displayMode)
}

// Equals ports BasePackage::equals.
func (p *Package) Equals(other PackageInterface) bool { return equals(p, other) }

// StabilityPriority ports BasePackage::getStabilityPriority.
func (p *Package) StabilityPriority() int { return stabilityPriority(p) }
