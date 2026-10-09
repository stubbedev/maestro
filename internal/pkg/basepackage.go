// Ports src/Composer/Package/BasePackage.php.

package pkg

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// BasePackage::STABILITY_* constants.
const (
	StabilityStable = 0
	StabilityRC     = 5
	StabilityBeta   = 10
	StabilityAlpha  = 15
	StabilityDev    = 20
)

// stabilityNames lists BasePackage::STABILITIES' keys in order.
var stabilityNames = [...]string{"stable", "RC", "beta", "alpha", "dev"}

// StabilityNames returns array_keys(BasePackage::STABILITIES) (a new
// slice).
func StabilityNames() []string { return slices.Clone(stabilityNames[:]) }

// StabilityValue returns BasePackage::STABILITIES[$name] and whether the
// name is one of its keys (case-sensitive, as isset is).
func StabilityValue(name string) (int, bool) {
	switch name {
	case "stable":
		return StabilityStable, true
	case "RC":
		return StabilityRC, true
	case "beta":
		return StabilityBeta, true
	case "alpha":
		return StabilityAlpha, true
	case "dev":
		return StabilityDev, true
	}

	return 0, false
}

// Stabilities returns BasePackage::STABILITIES as a PHP array.
func Stabilities() *php.Array {
	return php.ArrayOf("stable", StabilityStable, "RC", StabilityRC, "beta", StabilityBeta, "alpha", StabilityAlpha, "dev", StabilityDev)
}

// LinkType is an entry of BasePackage::$supportedLinkTypes.
type LinkType struct {
	Type        string // the composer.json key: require, conflict, ...
	Description string
	Method      string // a Type* constant
}

// SupportedLinkTypes returns BasePackage::$supportedLinkTypes in order.
func SupportedLinkTypes() [5]LinkType {
	return [5]LinkType{
		{"require", "requires", TypeRequire},
		{"conflict", "conflicts", TypeConflict},
		{"provide", "provides", TypeProvide},
		{"replace", "replaces", TypeReplace},
		{"require-dev", "requires (for development)", TypeDevRequire},
	}
}

// SupportedLinkType returns BasePackage::$supportedLinkTypes[$typ].
func SupportedLinkType(typ string) (LinkType, bool) {
	for _, t := range SupportedLinkTypes() {
		if t.Type == typ {
			return t, true
		}
	}

	return LinkType{}, false
}

// LinksByMethod returns $package->{'get'.ucfirst($method)}() for a Type*
// method name.
func LinksByMethod(p PackageInterface, method string) Links {
	switch method {
	case TypeRequire:
		return p.Requires()
	case TypeDevRequire:
		return p.DevRequires()
	case TypeProvide:
		return p.Provides()
	case TypeConflict:
		return p.Conflicts()
	case TypeReplace:
		return p.Replaces()
	}

	panic("pkg: unknown link method " + method)
}

// basePackage holds BasePackage's properties.
type basePackage struct {
	id int
	// name is the lowercased name, in an array so that Names can return
	// it as a slice without allocating one.
	name       [1]string
	prettyName string
	repository Repository
	// rev counts the changes of every field but the id, idRev those of
	// the id (Rev is their sum).
	rev   uint64
	idRev uint64
	// watched is non-zero for a package whose changes ChangeClock counts
	// (Watch); atomic. Clone copies packages, which an atomic.Uint32
	// would forbid.
	watched uint32 //nolint:modernize // see above
}

func newBasePackage(name string) basePackage {
	return basePackage{id: -1, name: [1]string{php.Strtolower(name)}, prettyName: name}
}

func (b *basePackage) base() *basePackage { return b }

// Name ports BasePackage::getName: the lowercased name.
func (b *basePackage) Name() string { return b.name[0] }

// PrettyName ports BasePackage::getPrettyName.
func (b *basePackage) PrettyName() string { return b.prettyName }

// SetID ports BasePackage::setId.
func (b *basePackage) SetID(id int) {
	b.id = id
	b.idRev++
	b.tick()
}

// changed counts a change of a field but the id.
func (b *basePackage) changed() {
	b.rev++
	b.tick()
}

// tick moves ChangeClock for a watched package.
func (b *basePackage) tick() {
	if atomic.LoadUint32(&b.watched) != 0 {
		changeClock.Add(1)
	}
}

// changeClock counts the changes of the watched packages (ChangeClock).
var changeClock atomic.Uint64

// Watch has ChangeClock count the changes of p (and of the package it
// aliases).
func Watch(p PackageInterface) {
	if a, ok := p.(Alias); ok {
		Watch(a.AliasOf())
	}
	if b, ok := p.(interface{ base() *basePackage }); ok {
		atomic.StoreUint32(&b.base().watched, 1)
	}
}

// ChangeClock is a counter of the changes of the packages Watch was
// called for: the Rev of none of them changed while it did not move.
func ChangeClock() uint64 { return changeClock.Load() }

// ID ports BasePackage::getId.
func (b *basePackage) ID() int { return b.id }

// SetRepository ports BasePackage::setRepository: a package belongs to at
// most one repository.
func (b *basePackage) SetRepository(repository Repository) error {
	if b.repository != nil && repository != b.repository {
		return &util.LogicError{Message: "Package \"" + b.prettyName + "\" cannot be added to repository \"" +
			repository.RepoName() + "\" as it is already in repository \"" + b.repository.RepoName() + "\"."}
	}

	b.repository = repository
	b.changed()

	return nil
}

// Repository ports BasePackage::getRepository.
func (b *basePackage) Repository() Repository { return b.repository }

// IsPlatform ports BasePackage::isPlatform.
func (b *basePackage) IsPlatform() bool {
	m, ok := b.repository.(PlatformRepositoryMarker)

	return ok && m.IsPlatformRepository()
}

// clearForClone ports BasePackage::__clone.
func (b *basePackage) clearForClone() {
	b.repository = nil
	b.id = -1
}

// names ports BasePackage::getNames. PHP returns the array keys, so a
// numeric name comes back as an int there; here it stays a string. The
// slice must not be modified: without provided or replaced names it is
// b's own name.
func names(p PackageInterface, b *basePackage, provides bool) []string {
	// capacity 1, so the first add copies it
	out := b.name[:]

	add := func(name string) {
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}

	if provides {
		for l := range p.Provides().Values() {
			add(l.Target())
		}
	}

	for l := range p.Replaces().Values() {
		add(l.Target())
	}

	return out
}

// uniqueName ports BasePackage::getUniqueName.
func uniqueName(p PackageInterface) string { return p.Name() + "-" + p.Version() }

// prettyString ports BasePackage::getPrettyString.
func prettyString(p PackageInterface) string { return p.PrettyName() + " " + p.PrettyVersion() }

// equals ports BasePackage::equals: the same package, looking through
// aliases.
func equals(self, other PackageInterface) bool {
	if a, ok := self.(Alias); ok {
		self = a.AliasOf()
	}

	if a, ok := other.(Alias); ok {
		other = a.AliasOf()
	}

	return self.base() == other.base()
}

// stabilityPriority ports BasePackage::getStabilityPriority.
func stabilityPriority(p PackageInterface) int {
	v, _ := StabilityValue(p.Stability())

	return v
}

// fullPrettyVersion ports BasePackage::getFullPrettyVersion. An unknown
// display mode panics with PHP's UnexpectedValueException message.
func fullPrettyVersion(p PackageInterface, truncate bool, displayMode DisplayMode) string {
	if displayMode == DisplaySourceRefIfDev {
		sourceType := p.SourceType()
		isGitOrHg := sourceType == Str("hg") || sourceType == Str("git")

		if !p.IsDev() || (!isGitOrHg && (sourceType.S != "" || p.DistReference().S == "")) {
			return p.PrettyVersion()
		}
	}

	var reference NullString

	switch displayMode {
	case DisplaySourceRefIfDev:
		reference = p.SourceReference()
		if reference.S == "" {
			reference = p.DistReference()
		}
	case DisplaySourceRef:
		reference = p.SourceReference()
	case DisplayDistRef:
		reference = p.DistReference()
	default:
		panic(&util.UnexpectedValueError{Message: "Display mode " + php.ToString(int64(displayMode)) + " is not supported"})
	}

	if !reference.Valid {
		return p.PrettyVersion()
	}

	// if source reference is a sha1 hash -- truncate
	if truncate && len(reference.S) == 40 && p.SourceType() != Str("svn") {
		return p.PrettyVersion() + " " + reference.S[:7]
	}

	return p.PrettyVersion() + " " + reference.S
}

// PackageNameToRegexp ports BasePackage::packageNameToRegexp: a regex
// matching names like allowPattern, where * matches anything. wrap is a
// sprintf format with one %s; pass "{^%s$}i" for PHP's default.
func PackageNameToRegexp(allowPattern, wrap string) string {
	cleaned := strings.ReplaceAll(php.PregQuote(allowPattern, ""), `\*`, ".*")

	return sprintfS(wrap, cleaned)
}

// PackageNamesToRegexp ports BasePackage::packageNamesToRegexp; pass
// "{^(?:%s)$}iD" for PHP's default wrap.
func PackageNamesToRegexp(packageNames []string, wrap string) string {
	parts := make([]string, len(packageNames))
	for i, name := range packageNames {
		parts[i] = PackageNameToRegexp(name, "%s")
	}

	return sprintfS(wrap, strings.Join(parts, "|"))
}

// sprintfS is sprintf($format, $s) for formats whose only conversions are
// %s (one) and %%.
func sprintfS(format, s string) string {
	var b strings.Builder

	b.Grow(len(format) + len(s))

	used := false

	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i+1 == len(format) {
			b.WriteByte(c)

			continue
		}

		i++

		switch format[i] {
		case '%':
			b.WriteByte('%')
		case 's':
			if !used {
				b.WriteString(s)
				used = true
			}
		default:
			b.WriteByte('%')
			b.WriteByte(format[i])
		}
	}

	return b.String()
}
