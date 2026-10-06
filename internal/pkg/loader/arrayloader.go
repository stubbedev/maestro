// Ports src/Composer/Package/Loader/ArrayLoader.php and
// src/Composer/Package/Loader/LoaderInterface.php.

package loader

import (
	"errors"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// LoaderInterface ports Composer\Package\Loader\LoaderInterface. class is
// pkg.ClassCompletePackage or pkg.ClassRootPackage.
type LoaderInterface interface {
	Load(config *php.Array, class string) (pkg.PackageInterface, error)
}

// ArrayLoader ports Composer\Package\Loader\ArrayLoader.
type ArrayLoader struct {
	versionParser *pkg.VersionParser
	loadOptions   bool
}

// NewArrayLoader ports ArrayLoader::__construct; a nil parser gets a new
// one. loadOptions loads transport-options.
func NewArrayLoader(parser *pkg.VersionParser, loadOptions bool) *ArrayLoader {
	if parser == nil {
		parser = pkg.NewVersionParser()
	}

	return &ArrayLoader{versionParser: parser, loadOptions: loadOptions}
}

// VersionParser returns the loader's parser.
func (l *ArrayLoader) VersionParser() *pkg.VersionParser { return l.versionParser }

// Load ports ArrayLoader::load. It returns the package, or an alias of it
// when the config defines a branch alias.
func (l *ArrayLoader) Load(config *php.Array, class string) (pkg.PackageInterface, error) {
	p, err := l.createObject(config, class)
	if err != nil {
		return nil, phperr.Call(err, `Composer\Package\Loader\ArrayLoader->createObject`, "ArrayLoader.php", 55)
	}

	pp, _ := pkg.AsPackage(p)

	for _, t := range pkg.SupportedLinkTypes() {
		links := subArray(config, t.Type)
		if links == nil {
			continue
		}

		parsed, err := l.ParseLinks(p.Name(), p.PrettyVersion(), t.Method, links)
		if err != nil {
			return nil, phperr.Call(err, `Composer\Package\Loader\ArrayLoader->parseLinks`, "ArrayLoader.php", 63)
		}

		setLinks(pp, t.Method, parsed)
	}

	leave := phperr.Enter(`Composer\Package\Loader\ArrayLoader->configureObject`, "ArrayLoader.php", 72)
	configured, err := l.configureObject(p, config)
	leave()

	return configured, phperr.Call(err, `Composer\Package\Loader\ArrayLoader->configureObject`, "ArrayLoader.php", 72)
}

// LoadPackages ports ArrayLoader::loadPackages: complete packages for many
// versions of the same packages (repository metadata), sharing the links
// that are equal between versions.
func (l *ArrayLoader) LoadPackages(versions []*php.Array) ([]pkg.PackageInterface, error) {
	packages := make([]pkg.PackageInterface, 0, len(versions))
	cache := &linkCache{links: map[string]map[linkCacheKey]*pkg.Link{}}

	for _, version := range versions {
		p, err := l.createObject(version, pkg.ClassCompletePackage)
		if err != nil {
			return nil, phperr.Call(err, `Composer\Package\Loader\ArrayLoader->createObject`, "ArrayLoader.php", 88)
		}

		if err := l.configureCachedLinks(cache, p, version); err != nil {
			return nil, phperr.Call(err, `Composer\Package\Loader\ArrayLoader->configureCachedLinks`, "ArrayLoader.php", 90)
		}

		configured, err := l.configureObject(p, version)
		if err != nil {
			return nil, phperr.Call(err, `Composer\Package\Loader\ArrayLoader->configureObject`, "ArrayLoader.php", 91)
		}

		packages = append(packages, configured)
	}

	return packages, nil
}

func setLinks(p *pkg.Package, method string, links pkg.Links) {
	switch method {
	case pkg.TypeRequire:
		p.SetRequires(links)
	case pkg.TypeDevRequire:
		p.SetDevRequires(links)
	case pkg.TypeProvide:
		p.SetProvides(links)
	case pkg.TypeConflict:
		p.SetConflicts(links)
	case pkg.TypeReplace:
		p.SetReplaces(links)
	}
}

// isset reports isset($a[$k]); a may be nil (a non-array).
func isset(a *php.Array, k string) bool { return get(a, k) != nil }

// get returns $a[$k] ?? null; a may be nil (a non-array).
func get(a *php.Array, k string) any {
	if a == nil {
		return nil
	}

	// GetKey with a Key value: Get(k any) would box k.
	v, _ := a.GetKey(php.StrKey(k))

	return v
}

// subArray returns $a[$k] when it is an array, else nil.
func subArray(a *php.Array, k string) *php.Array {
	v, _ := get(a, k).(*php.Array)

	return v
}

// empty reports empty($v).
func empty(v any) bool { return !php.ToBool(v) }

func isScalar(v any) bool {
	switch v.(type) {
	case bool, int64, int, float64, string:
		return true
	}

	return false
}

// jsonEncode is json_encode($v) as string concatenation sees it: "" when
// encoding fails (false).
func jsonEncode(v any) string {
	s, err := php.JSONEncode(v, 0)
	if err != nil {
		return ""
	}

	return s
}

// nullableString checks a value passed to a ?string parameter.
func nullableString(fn, param string, v any) (pkg.NullString, error) {
	switch v := v.(type) {
	case nil:
		return pkg.NullString{}, nil
	case string:
		return pkg.Str(v), nil
	}

	return pkg.NullString{}, calledFromArrayLoader(pkg.ArgumentTypeError(fn, 1, param, "?string", v), fn)
}

// arrayArg checks a value passed to an array parameter.
func arrayArg(fn, param, expected string, v any) (*php.Array, error) {
	if a, ok := v.(*php.Array); ok {
		return a, nil
	}

	if v == nil && expected == "?array" {
		return nil, nil
	}

	return nil, calledFromArrayLoader(pkg.ArgumentTypeError(fn, 1, param, expected, v), fn)
}

// arrayLoaderCall is where ArrayLoader.php calls a package method: the
// method's declaration (file:line) and the line of the call.
type arrayLoaderCall struct {
	file       string
	decl, call int
}

// arrayLoaderCalls are the package methods ArrayLoader calls with
// configuration values, each from one line.
var arrayLoaderCalls = map[string]arrayLoaderCall{
	"__construct":           {"Package.php", 115, 141},
	"setTargetDir":          {"Package.php", 155, 159},
	"setInstallationSource": {"Package.php", 207, 177},
	"setSourceType":         {"Package.php", 220, 192},
	"setSourceUrl":          {"Package.php", 233, 193},
	"setSourceMirrors":      {"Package.php", 259, 196},
	"setDistType":           {"Package.php", 280, 209},
	"setDistUrl":            {"Package.php", 293, 210},
	"setDistSha1Checksum":   {"Package.php", 319, 212},
	"setDistMirrors":        {"Package.php", 332, 214},
	"setTransportOptions":   {"Package.php", 364, 311},
	"setAutoload":           {"Package.php", 536, 228},
	"setDevAutoload":        {"Package.php", 556, 232},
	"setIncludePaths":       {"Package.php", 574, 236},
	"setPhpExt":             {"Package.php", 593, 240},
	"setNotificationUrl":    {"Package.php", 609, 254},
	"setArchiveName":        {"CompletePackage.php", 218, 259},
	"setArchiveExcludes":    {"CompletePackage.php", 234, 262},
}

// calledFromArrayLoader gives the TypeError of fn ("Class::method", a
// package method ArrayLoader calls) the declaration site, the "called in"
// suffix and the frame of ArrayLoader's call.
func calledFromArrayLoader(e *pkg.TypeError, fn string) *pkg.TypeError {
	class, method, _ := strings.Cut(fn, "::")
	c, ok := arrayLoaderCalls[method]
	if !ok {
		return e
	}

	return e.Called(class+"->"+method, phperr.At(c.file, c.decl), "ArrayLoader.php", c.call)
}

const packageClass = `Composer\Package\Package`

func (l *ArrayLoader) createObject(config *php.Array, class string) (pkg.PackageInterface, error) {
	if !isset(config, "name") {
		return nil, &util.UnexpectedValueError{Site: phperr.At("ArrayLoader.php", 112), Message: "Unknown package has no name defined (" + jsonEncode(config) + ")."}
	}

	nameValue := get(config, "name")

	versionValue, _ := config.Get("version")
	if versionValue == nil || !isScalar(versionValue) {
		return nil, &util.UnexpectedValueError{Site: phperr.At("ArrayLoader.php", 115), Message: "Package " + php.ToString(nameValue) + " has no version defined."}
	}

	prettyVersion := php.ToString(versionValue)

	var version string

	// handle already normalized versions
	if normalized, ok := get(config, "version_normalized").(string); ok {
		version = normalized

		// handling of existing repos which need to remain composer v1 compatible, in case the version_normalized contained VersionParser::DEFAULT_BRANCH_ALIAS, we renormalize it
		if version == pkg.DefaultBranchAlias {
			v, err := l.versionParser.Normalize(prettyVersion)
			if err != nil {
				return nil, err
			}

			version = v
		}
	} else {
		v, err := l.versionParser.Normalize(prettyVersion)
		if err != nil {
			if uv := (*semver.UnexpectedValueError)(nil); errors.As(err, &uv) {
				return nil, &util.UnexpectedValueError{Site: phperr.At("ArrayLoader.php", 133), Message: "Failed to normalize version for package \"" + php.ToString(nameValue) + "\": " + uv.Message, Prev: err}
			}

			return nil, err
		}

		version = v
	}

	name, ok := nameValue.(string)
	if !ok {
		return nil, calledFromArrayLoader(pkg.ArgumentTypeError(packageClass+"::__construct", 1, "name", "string", nameValue), packageClass+"::__construct")
	}

	switch class {
	case pkg.ClassCompletePackage:
		return pkg.NewCompletePackage(name, version, prettyVersion), nil
	case pkg.ClassRootPackage:
		return pkg.NewRootPackage(name, version, prettyVersion), nil
	}

	return nil, &util.LogicError{Site: phperr.At("ArrayLoader.php", 153), Message: `ArrayLoader expects instances of the Composer\Package\CompletePackage class to function correctly`}
}

var (
	digitsOnly    = php.MustCompile(`/^\d++$/D`)
	aliasNineRuns = php.MustCompile(`{(\.9{7})+}`)
	leadingV      = php.MustCompile(`{^v}`)
)

func (l *ArrayLoader) configureObject(p pkg.PackageInterface, config *php.Array) (pkg.PackageInterface, error) {
	cp, ok := pkg.AsCompletePackage(p)
	if !ok {
		return nil, &util.LogicError{Site: phperr.At("ArrayLoader.php", 153), Message: `ArrayLoader expects instances of the Composer\Package\CompletePackage class to function correctly`}
	}

	if err := configureFields(cp, config); err != nil {
		return nil, err
	}

	if err := configureCompleteFields(cp, config); err != nil {
		return nil, err
	}

	if l.loadOptions && isset(config, "transport-options") {
		options, err := arrayArg(packageClass+"::setTransportOptions", "options", "array", get(config, "transport-options"))
		if err != nil {
			return nil, err
		}

		cp.SetTransportOptions(options)
	}

	aliasNormalized, ok, err := l.GetBranchAlias(config)
	if err != nil {
		return nil, err
	}

	if ok && php.ToBool(aliasNormalized) {
		// The repeated group exhausts PCRE's limits on a long enough run of
		// ".9999999", and Preg::replace throws.
		prettyAlias, _, err := aliasNineRuns.Replace(aliasNormalized, ".x", -1)
		if err != nil {
			return nil, err
		}

		if root, ok := p.(*pkg.RootPackage); ok {
			return pkg.NewRootAliasPackage(root, aliasNormalized, prettyAlias), nil
		}

		return pkg.NewCompleteAliasPackage(cp, aliasNormalized, prettyAlias), nil
	}

	return p, nil
}

// mustReplace is Preg::replace for patterns that cannot fail at run time
// (leadingV, rootpackageloader's are anchored literals).
func mustReplace(re *php.Regexp, subject, replacement string) string {
	s, _, err := re.Replace(subject, replacement, -1)
	if err != nil {
		panic(err)
	}

	return s
}

// configureFields sets the Package properties of configureObject.
func configureFields(p *pkg.CompletePackage, config *php.Array) error {
	typ := "library"

	if v := get(config, "type"); v != nil {
		s, ok := v.(string)
		if !ok {
			return pkg.ArgumentTypeError("strtolower", 1, "string", "string", v).Raised("strtolower", "ArrayLoader.php", 156)
		}

		typ = php.Strtolower(s)
	}

	p.SetType(typ)

	if isset(config, "target-dir") {
		targetDir, err := nullableString(packageClass+"::setTargetDir", "targetDir", get(config, "target-dir"))
		if err != nil {
			return err
		}

		p.SetTargetDir(targetDir)
	}

	if extra := subArray(config, "extra"); extra != nil {
		p.SetExtra(extra)
	}

	if bin := get(config, "bin"); bin != nil {
		binaries, err := loadBinaries(bin)
		if err != nil {
			return err
		}

		p.SetBinaries(binaries)
	}

	if isset(config, "installation-source") {
		source, err := nullableString(packageClass+"::setInstallationSource", "type", get(config, "installation-source"))
		if err != nil {
			return err
		}

		p.SetInstallationSource(source)
	}

	if get(config, "default-branch") == true {
		p.SetIsDefaultBranch(true)
	}

	if err := configureSource(p, config); err != nil {
		return err
	}

	if err := configureDist(p, config); err != nil {
		return err
	}

	if suggest := subArray(config, "suggest"); suggest != nil {
		suggests, err := loadSuggests(suggest, p.PrettyVersion())
		if err != nil {
			return err
		}

		p.SetSuggests(suggests)
	}

	for _, f := range [...]struct{ key, method, param, expected string }{
		{"autoload", "setAutoload", "autoload", "array"},
		{"autoload-dev", "setDevAutoload", "devAutoload", "array"},
		{"include-path", "setIncludePaths", "includePaths", "array"},
		{"php-ext", "setPhpExt", "phpExt", "?array"},
	} {
		v := get(config, f.key)
		if v == nil {
			continue
		}

		a, err := arrayArg(packageClass+"::"+f.method, f.param, f.expected, v)
		if err != nil {
			return err
		}

		switch f.key {
		case "autoload":
			p.SetAutoload(a)
		case "autoload-dev":
			p.SetDevAutoload(a)
		case "include-path":
			p.SetIncludePaths(a)
		default:
			p.SetPhpExt(a)
		}
	}

	if t := get(config, "time"); !empty(t) {
		s, ok := t.(string)
		if !ok {
			return pkg.ArgumentTypeError(`Composer\Pcre\Preg::isMatch`, 2, "subject", "string", t).
				Called(`Composer\Pcre\Preg::isMatch`, phperr.At("vendor/composer/pcre/src/Preg.php", 289), "ArrayLoader.php", 244)
		}

		if mustMatch(digitsOnly, s) {
			s = "@" + s
		}

		if date, err := parseDateTime(s); err == nil {
			p.SetReleaseDate(date, true)
		}
	}

	if u := get(config, "notification-url"); !empty(u) {
		s, ok := u.(string)
		if !ok {
			return calledFromArrayLoader(pkg.ArgumentTypeError(packageClass+"::setNotificationUrl", 1, "notificationUrl", "string", u), packageClass+"::setNotificationUrl")
		}

		p.SetNotificationURL(s)
	}

	return nil
}

// mustMatch is Preg::isMatch for patterns that cannot fail at run time:
// bounded work per start position (a fixed-length match, or a repeat that
// cannot backtrack such as digitsOnly's possessive one).
func mustMatch(re *php.Regexp, subject string) bool {
	ok, err := re.IsMatch(subject)
	if err != nil {
		panic(err)
	}

	return ok
}

// loadBinaries ports the bin handling of configureObject: a list of the
// binaries without leading slashes.
func loadBinaries(bin any) (*php.Array, error) {
	in, ok := bin.(*php.Array)
	if !ok {
		in = php.ListOf(bin)
	}

	out := php.NewArrayCap(in.Len())

	for k, v := range in.All() {
		s, ok := v.(string)
		if !ok {
			return nil, pkg.ArgumentTypeError("ltrim", 1, "string", "string", v).Raised("ltrim", "ArrayLoader.php", 171)
		}

		out.SetKey(k, php.LtrimSet(s, "/"))
	}

	return out, nil
}

// loadSuggests ports the suggest handling of configureObject.
func loadSuggests(suggest *php.Array, prettyVersion string) (*php.Array, error) {
	var out *php.Array

	for target, reason := range suggest.All() {
		s, ok := reason.(string)
		if !ok {
			return nil, pkg.ArgumentTypeError("trim", 1, "string", "string", reason).Raised("trim", "ArrayLoader.php", 220)
		}

		if php.Trim(s) == "self.version" {
			if out == nil {
				out = suggest.Clone()
			}

			out.SetKey(target, prettyVersion)
		}
	}

	if out == nil {
		return suggest, nil
	}

	return out, nil
}

func configureSource(p *pkg.CompletePackage, config *php.Array) error {
	if !isset(config, "source") {
		return nil
	}

	source := subArray(config, "source")
	if source == nil || !isset(source, "type") || !isset(source, "url") || !isset(source, "reference") {
		return &util.UnexpectedValueError{Site: phperr.At("ArrayLoader.php", 186), Message: "Package " + php.ToString(get(config, "name")) +
			"'s source key should be specified as {\"type\": ..., \"url\": ..., \"reference\": ...},\n" +
			jsonEncode(get(config, "source")) + " given."}
	}

	typ, err := nullableString(packageClass+"::setSourceType", "type", get(source, "type"))
	if err != nil {
		return err
	}

	url, err := nullableString(packageClass+"::setSourceUrl", "url", get(source, "url"))
	if err != nil {
		return err
	}

	p.SetSourceType(typ)
	p.SetSourceURL(url)
	p.SetSourceReference(pkg.Str(php.ToString(get(source, "reference"))))

	if isset(source, "mirrors") {
		mirrors, err := arrayArg(packageClass+"::setSourceMirrors", "mirrors", "?array", get(source, "mirrors"))
		if err != nil {
			return err
		}

		p.SetSourceMirrors(mirrors)
	}

	return nil
}

func configureDist(p *pkg.CompletePackage, config *php.Array) error {
	if !isset(config, "dist") {
		return nil
	}

	dist := subArray(config, "dist")
	if dist == nil || !isset(dist, "type") || !isset(dist, "url") {
		return &util.UnexpectedValueError{Site: phperr.At("ArrayLoader.php", 202), Message: "Package " + php.ToString(get(config, "name")) +
			"'s dist key should be specified as {\"type\": ..., \"url\": ..., \"reference\": ..., \"shasum\": ...},\n" +
			jsonEncode(get(config, "dist")) + " given."}
	}

	typ, err := nullableString(packageClass+"::setDistType", "type", get(dist, "type"))
	if err != nil {
		return err
	}

	url, err := nullableString(packageClass+"::setDistUrl", "url", get(dist, "url"))
	if err != nil {
		return err
	}

	sum, err := nullableString(packageClass+"::setDistSha1Checksum", "sha1checksum", get(dist, "shasum"))
	if err != nil {
		return err
	}

	p.SetDistType(typ)
	p.SetDistURL(url)

	if ref := get(dist, "reference"); ref != nil {
		p.SetDistReference(pkg.Str(php.ToString(ref)))
	} else {
		p.SetDistReference(pkg.NullString{})
	}

	p.SetDistSha1Checksum(sum)

	if isset(dist, "mirrors") {
		mirrors, err := arrayArg(packageClass+"::setDistMirrors", "mirrors", "?array", get(dist, "mirrors"))
		if err != nil {
			return err
		}

		p.SetDistMirrors(mirrors)
	}

	return nil
}

const completePackageClass = `Composer\Package\CompletePackage`

// configureCompleteFields sets the CompletePackage properties of
// configureObject.
func configureCompleteFields(p *pkg.CompletePackage, config *php.Array) error {
	archive := subArray(config, "archive")

	if name := get(archive, "name"); !empty(name) {
		s, ok := name.(string)
		if !ok {
			return calledFromArrayLoader(pkg.ArgumentTypeError(completePackageClass+"::setArchiveName", 1, "name", "?string", name), completePackageClass+"::setArchiveName")
		}

		p.SetArchiveName(pkg.Str(s))
	}

	if exclude := get(archive, "exclude"); !empty(exclude) {
		a, err := arrayArg(completePackageClass+"::setArchiveExcludes", "excludes", "array", exclude)
		if err != nil {
			return err
		}

		p.SetArchiveExcludes(a)
	}

	if scripts := subArray(config, "scripts"); scripts != nil {
		scripts = castListeners(scripts)
		for _, reserved := range [...]string{"composer", "php", "putenv"} {
			if scripts.Has(reserved) {
				util.TriggerDeprecation("The `"+reserved+"` script name is reserved for internal use, please avoid defining it", phperr.At("ArrayLoader.php", 271))
			}
		}

		p.SetScripts(scripts)
	}

	if s, ok := get(config, "description").(string); ok && !empty(s) {
		p.SetDescription(pkg.Str(s))
	}

	if s, ok := get(config, "homepage").(string); ok && !empty(s) {
		p.SetHomepage(pkg.Str(s))
	}

	if keywords := subArray(config, "keywords"); keywords != nil && keywords.Len() > 0 {
		p.SetKeywords(stringValues(keywords))
	}

	if license := get(config, "license"); !empty(license) {
		a, ok := license.(*php.Array)
		if !ok {
			a = php.ListOf(license)
		}

		p.SetLicense(a)
	}

	if authors := subArray(config, "authors"); authors != nil && authors.Len() > 0 {
		p.SetAuthors(authors)
	}

	if support := subArray(config, "support"); support != nil {
		p.SetSupport(support)
	}

	if funding := subArray(config, "funding"); funding != nil && funding.Len() > 0 {
		p.SetFunding(funding)
	}

	if abandoned := get(config, "abandoned"); abandoned != nil {
		p.SetAbandoned(abandoned)
	}

	return nil
}

// stringValues ports array_map('strval', $a); an array of strings comes
// back as it is.
func stringValues(a *php.Array) *php.Array {
	for _, v := range a.All() {
		if _, ok := v.(string); !ok {
			return php.ArrayMap(a, func(v any) any { return php.ToString(v) })
		}
	}

	return a
}

// castListeners ports the (array) cast of every scripts entry.
func castListeners(scripts *php.Array) *php.Array {
	var out *php.Array

	for event, listeners := range scripts.All() {
		if _, ok := listeners.(*php.Array); ok {
			continue
		}

		if out == nil {
			out = scripts.Clone()
		}

		if listeners == nil {
			out.SetKey(event, php.NewArray())
		} else {
			out.SetKey(event, php.ListOf(listeners))
		}
	}

	if out == nil {
		return scripts
	}

	return out
}

// hasUpperKey reports whether a string key of a has an ASCII uppercase
// letter.
func hasUpperKey(a *php.Array) bool {
	for k := range a.All() {
		if !k.IsString() {
			continue
		}

		s := k.String()
		for i := range len(s) {
			if s[i] >= 'A' && s[i] <= 'Z' {
				return true
			}
		}
	}

	return false
}

// linkCache is ArrayLoader::loadPackages' $linkCache:
// [$name][$type][$target][$constraint] => link.
//
// It also remembers the links built for the previous version of each
// type: expanded repository metadata repeats the very same array for
// versions whose links did not change, and those versions get the same
// links (the same Link objects the cache would return, in the same
// order) without looking each one up again.
type linkCache struct {
	links map[string]map[linkCacheKey]*pkg.Link
	prev  [5]prevLinks
}

type prevLinks struct {
	config        *php.Array
	name          string
	prettyVersion string // only compared when the links use self.version
	selfVersion   bool
	links         pkg.Links
}

type linkCacheKey struct {
	typ                byte // index into pkg.SupportedLinkTypes
	target, constraint string
}

func (l *ArrayLoader) configureCachedLinks(cache *linkCache, p pkg.PackageInterface, config *php.Array) error {
	name := p.Name()
	prettyVersion := p.PrettyVersion()
	pp, _ := pkg.AsPackage(p)

	byName := cache.links[name]
	if byName == nil {
		byName = map[linkCacheKey]*pkg.Link{}
		cache.links[name] = byName
	}

	types := pkg.SupportedLinkTypes()

	for ti := range uint8(len(types)) {
		t := types[ti]

		if !isset(config, t.Type) {
			continue
		}

		links := subArray(config, t.Type)

		prev := &cache.prev[ti]
		if links != nil && links == prev.config && name == prev.name && (!prev.selfVersion || prettyVersion == prev.prettyVersion) {
			setLinks(pp, t.Method, prev.links)

			continue
		}

		var (
			b           pkg.LinksBuilder
			selfVersion bool
		)

		// foreach over a non-array only warns.
		if links != nil {
			b.Grow(links.Len())

			// lowercasing can only make keys collide when one has
			// uppercase letters
			add := b.Append
			if hasUpperKey(links) {
				add = b.Set
			}

			for prettyTarget, c := range links.All() {
				if prettyTarget.IsInt() {
					return pkg.ArgumentTypeError("strtolower", 1, "string", "string", prettyTarget.Value()).Raised("strtolower", "ArrayLoader.php", 342)
				}

				target := php.Strtolower(prettyTarget.String())

				// recursive links are not supported
				if target == name {
					continue
				}

				constraint, ok := c.(string)
				if _, isArray := c.(*php.Array); isArray {
					// isset($linkCache[$name][$type][$target][$constraint])
					// fails before createLink() is called
					return (&pkg.TypeError{Message: "Cannot access offset of type array in isset or empty"}).Raised("", "ArrayLoader.php", 352)
				}
				if !ok {
					return pkg.ArgumentTypeError(`Composer\Package\Loader\ArrayLoader::createLink`, 5, "prettyConstraint", "string", c).
						Called(`Composer\Package\Loader\ArrayLoader->createLink`, phperr.At("ArrayLoader.php", 397), "ArrayLoader.php", 353)
				}

				if constraint == "self.version" {
					link, err := l.createLink(name, prettyVersion, t.Method, target, constraint)
					if err != nil {
						return phperr.Call(err, `Composer\Package\Loader\ArrayLoader->createLink`, "ArrayLoader.php", 350)
					}

					add(target, link)

					selfVersion = true

					continue
				}

				key := linkCacheKey{ti, target, constraint}

				link, ok := byName[key]
				if !ok {
					var err error
					if link, err = l.createLink(name, prettyVersion, t.Method, target, constraint); err != nil {
						return phperr.Call(err, `Composer\Package\Loader\ArrayLoader->createLink`, "ArrayLoader.php", 353)
					}

					byName[key] = link
				}

				add(target, link)
			}
		}

		built := b.Build()
		setLinks(pp, t.Method, built)

		if links != nil {
			*prev = prevLinks{config: links, name: name, prettyVersion: prettyVersion, selfVersion: selfVersion, links: built}
		}
	}

	return nil
}

// ParseLinks ports ArrayLoader::parseLinks: the links of one type from a
// map of target => constraint; non-string constraints are skipped.
func (l *ArrayLoader) ParseLinks(source, sourceVersion, description string, links *php.Array) (pkg.Links, error) {
	var b pkg.LinksBuilder

	b.Grow(links.Len())

	// lowercasing can only make keys collide when one has uppercase
	// letters
	add := b.Append
	if hasUpperKey(links) {
		add = b.Set
	}

	for k, c := range links.All() {
		constraint, ok := c.(string)
		if !ok {
			continue
		}

		target := php.Strtolower(k.String())

		link, err := l.createLink(source, sourceVersion, description, target, constraint)
		if err != nil {
			return pkg.Links{}, phperr.Call(err, `Composer\Package\Loader\ArrayLoader->createLink`, "ArrayLoader.php", 384)
		}

		add(target, link)
	}

	return b.Build(), nil
}

func (l *ArrayLoader) createLink(source, sourceVersion, description, target, prettyConstraint string) (*pkg.Link, error) {
	constraint := prettyConstraint
	if prettyConstraint == "self.version" {
		constraint = sourceVersion
	}

	parsed, err := l.versionParser.ParseConstraints(constraint)
	if err != nil {
		phperr.Call(err, `Composer\Package\Version\VersionParser->parseConstraints`, "ArrayLoader.php", 410)
		if uv := (*semver.UnexpectedValueError)(nil); errors.As(err, &uv) {
			return nil, &util.UnexpectedValueError{Site: phperr.At("ArrayLoader.php", 412), Message: "Link constraint in " + source + " " + description + " > " + target +
				" should be a valid version constraint, got \"" + constraint + "\"", Prev: err}
		}

		return nil, err
	}

	return pkg.NewLink(source, target, parsed, description, pkg.Str(prettyConstraint)), nil
}

// GetBranchAlias ports ArrayLoader::getBranchAlias: the normalized branch
// alias the config defines, if any.
func (l *ArrayLoader) GetBranchAlias(config *php.Array) (string, bool, error) {
	versionValue, _ := config.Get("version")
	if versionValue == nil || !isScalar(versionValue) {
		return "", false, &util.UnexpectedValueError{Site: phperr.At("ArrayLoader.php", 432), Message: "no/invalid version defined"}
	}

	version := php.ToString(versionValue)

	if !strings.HasPrefix(version, "dev-") && !strings.HasSuffix(version, "-dev") {
		return "", false, nil
	}

	if aliases := subArray(subArray(config, "extra"), "branch-alias"); aliases != nil {
		for k, v := range aliases.All() {
			sourceBranch := k.String()

			targetBranch, ok := v.(string)
			if !ok {
				return "", false, pkg.ArgumentTypeError("substr", 1, "string", "string", v).Raised("substr", "ArrayLoader.php", 447)
			}

			// ensure it is an alias to a -dev package
			if !strings.HasSuffix(targetBranch, "-dev") {
				continue
			}

			// normalize without -dev and ensure it's a numeric branch that is parseable
			var validatedTargetBranch string
			if targetBranch == pkg.DefaultBranchAlias {
				validatedTargetBranch = pkg.DefaultBranchAlias
			} else {
				validatedTargetBranch = l.versionParser.NormalizeBranch(targetBranch[:len(targetBranch)-4])
			}

			if !strings.HasSuffix(validatedTargetBranch, "-dev") {
				continue
			}

			// ensure that it is the current branch aliasing itself
			if php.Strtolower(version) != php.Strtolower(sourceBranch) {
				continue
			}

			// If using numeric aliases ensure the alias is a valid subversion
			if !numericAliasCompatible(l.versionParser, sourceBranch, targetBranch) {
				continue
			}

			return validatedTargetBranch, true, nil
		}
	}

	if get(config, "default-branch") == true {
		if _, ok := l.versionParser.ParseNumericAliasPrefix(mustReplace(leadingV, version, "")); !ok {
			return pkg.DefaultBranchAlias, true, nil
		}
	}

	return "", false, nil
}

// numericAliasCompatible ports the check shared by ArrayLoader and
// ValidatingArrayLoader: false when both branches have numeric alias
// prefixes and the target's does not start with the source's (stripos).
func numericAliasCompatible(parser *pkg.VersionParser, sourceBranch, targetBranch string) bool {
	sourcePrefix, ok := parser.ParseNumericAliasPrefix(sourceBranch)
	if !ok || !php.ToBool(sourcePrefix) {
		return true
	}

	targetPrefix, ok := parser.ParseNumericAliasPrefix(targetBranch)
	if !ok || !php.ToBool(targetPrefix) {
		return true
	}

	return strings.HasPrefix(php.Strtolower(targetPrefix), php.Strtolower(sourcePrefix))
}
