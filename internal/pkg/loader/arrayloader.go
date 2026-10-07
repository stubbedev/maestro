// Ports src/Composer/Package/Loader/ArrayLoader.php and
// src/Composer/Package/Loader/LoaderInterface.php.

package loader

import (
	"errors"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
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
		return nil, err
	}

	pp, _ := pkg.AsPackage(p)

	for _, t := range pkg.SupportedLinkTypes() {
		links := config.ArrayAt(t.Type)
		if links == nil {
			continue
		}

		parsed, err := l.ParseLinks(p.Name(), p.PrettyVersion(), t.Method, links)
		if err != nil {
			return nil, err
		}

		setLinks(pp, t.Method, parsed)
	}

	configured, err := l.configureObject(p, config)

	return configured, err
}

// LoadPackages ports ArrayLoader::loadPackages: complete packages for many
// versions of the same packages (repository metadata), sharing the links
// that are equal between versions.
func (l *ArrayLoader) LoadPackages(versions []*php.Array) ([]pkg.PackageInterface, error) {
	packages := make([]pkg.PackageInterface, 0, len(versions))
	b := l.Batch()

	for _, version := range versions {
		configured, err := b.Load(version)
		if err != nil {
			return nil, err
		}

		packages = append(packages, configured)
	}

	return packages, nil
}

// Batch returns a PackageBatch: LoadPackages one version at a time.
func (l *ArrayLoader) Batch() *PackageBatch {
	return &PackageBatch{l: l, cache: &linkCache{links: map[string]map[linkCacheKey]*pkg.Link{}}}
}

// PackageBatch loads versions as one LoadPackages call loads its list,
// sharing the links that are equal between them, one at a time (maestro's;
// see ArrayLoader.Batch). Only the package returned refers to the version
// array, never to the array itself (only to the values it holds), so the
// caller may change the array once Load returns.
type PackageBatch struct {
	l     *ArrayLoader
	cache *linkCache
}

// Load is LoadPackages' work for one version. After an error the batch
// must not be used again.
func (b *PackageBatch) Load(version *php.Array) (pkg.PackageInterface, error) {
	p, err := b.l.createObject(version, pkg.ClassCompletePackage)
	if err != nil {
		return nil, err
	}

	if err := b.l.configureCachedLinks(b.cache, p, version); err != nil {
		return nil, err
	}

	return b.l.configureObject(p, version)
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

	return pkg.NullString{}, pkg.ArgumentTypeError(fn, 1, param, "?string", v)
}

// arrayArg checks a value passed to an array parameter.
func arrayArg(fn, param, expected string, v any) (*php.Array, error) {
	if a, ok := v.(*php.Array); ok {
		return a, nil
	}

	if v == nil && expected == "?array" {
		return nil, nil
	}

	return nil, pkg.ArgumentTypeError(fn, 1, param, expected, v)
}

const packageClass = `Composer\Package\Package`

func (l *ArrayLoader) createObject(config *php.Array, class string) (pkg.PackageInterface, error) {
	if !config.Isset("name") {
		return nil, &util.UnexpectedValueError{Message: "Unknown package has no name defined (" + jsonEncode(config) + ")."}
	}

	nameValue := config.At("name")

	versionValue, _ := config.Get("version")
	if versionValue == nil || !isScalar(versionValue) {
		return nil, &util.UnexpectedValueError{Message: "Package " + php.ToString(nameValue) + " has no version defined."}
	}

	prettyVersion := php.ToString(versionValue)

	var version string

	// handle already normalized versions
	if normalized, ok := config.At("version_normalized").(string); ok {
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
				return nil, &util.UnexpectedValueError{Message: "Failed to normalize version for package \"" + php.ToString(nameValue) + "\": " + uv.Message, Prev: err}
			}

			return nil, err
		}

		version = v
	}

	name, ok := nameValue.(string)
	if !ok {
		return nil, pkg.ArgumentTypeError(packageClass+"::__construct", 1, "name", "string", nameValue)
	}

	switch class {
	case pkg.ClassCompletePackage:
		return pkg.NewCompletePackage(name, version, prettyVersion), nil
	case pkg.ClassRootPackage:
		return pkg.NewRootPackage(name, version, prettyVersion), nil
	}

	return nil, &util.LogicError{Message: `ArrayLoader expects instances of the Composer\Package\CompletePackage class to function correctly`}
}

var (
	digitsOnly    = php.MustCompile(`/^\d++$/D`)
	aliasNineRuns = php.MustCompile(`{(\.9{7})+}`)
	leadingV      = php.MustCompile(`{^v}`)
)

func (l *ArrayLoader) configureObject(p pkg.PackageInterface, config *php.Array) (pkg.PackageInterface, error) {
	cp, ok := pkg.AsCompletePackage(p)
	if !ok {
		return nil, &util.LogicError{Message: `ArrayLoader expects instances of the Composer\Package\CompletePackage class to function correctly`}
	}

	if err := configureFields(cp, config); err != nil {
		return nil, err
	}

	if err := configureCompleteFields(cp, config); err != nil {
		return nil, err
	}

	if l.loadOptions && config.Isset("transport-options") {
		options, err := transportOptions(config)
		if err != nil {
			return nil, err
		}

		cp.SetTransportOptions(options)
	}

	return l.aliased(p, config)
}

// transportOptions is what configureObject passes to setTransportOptions
// for config's transport-options.
func transportOptions(config *php.Array) (*php.Array, error) {
	return arrayArg(packageClass+"::setTransportOptions", "options", "array", config.At("transport-options"))
}

// aliased is the end of configureObject: the package, or its alias when
// config defines a branch alias.
func (l *ArrayLoader) aliased(p pkg.PackageInterface, config *php.Array) (pkg.PackageInterface, error) {
	cp, _ := pkg.AsCompletePackage(p)

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
	if err := configureType(p, config); err != nil {
		return err
	}

	if config.Isset("target-dir") {
		targetDir, err := nullableString(packageClass+"::setTargetDir", "targetDir", config.At("target-dir"))
		if err != nil {
			return err
		}

		p.SetTargetDir(targetDir)
	}

	if extra := config.ArrayAt("extra"); extra != nil {
		p.SetExtra(extra)
	}

	if bin := config.At("bin"); bin != nil {
		binaries, err := loadBinaries(bin)
		if err != nil {
			return err
		}

		p.SetBinaries(binaries)
	}

	if config.Isset("installation-source") {
		source, err := nullableString(packageClass+"::setInstallationSource", "type", config.At("installation-source"))
		if err != nil {
			return err
		}

		p.SetInstallationSource(pkg.NullAs[pkg.InstallationSource](source))
	}

	configureDefaultBranch(p, config)

	if err := configureSource(p, config); err != nil {
		return err
	}

	if err := configureDist(p, config); err != nil {
		return err
	}

	if suggest := config.ArrayAt("suggest"); suggest != nil {
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
		v := config.At(f.key)
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

	if t := config.At("time"); !empty(t) {
		s, ok := t.(string)
		if !ok {
			return pkg.ArgumentTypeError(`Composer\Pcre\Preg::isMatch`, 2, "subject", "string", t)
		}

		if mustMatch(digitsOnly, s) {
			s = "@" + s
		}

		if date, err := parseDateTime(s); err == nil {
			p.SetReleaseDate(date, true)
		}
	}

	if u := config.At("notification-url"); !empty(u) {
		s, ok := u.(string)
		if !ok {
			return pkg.ArgumentTypeError(packageClass+"::setNotificationUrl", 1, "notificationUrl", "string", u)
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
			return nil, pkg.ArgumentTypeError("ltrim", 1, "string", "string", v)
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
			return nil, pkg.ArgumentTypeError("trim", 1, "string", "string", reason)
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
	if !config.Isset("source") {
		return nil
	}

	source := config.ArrayAt("source")
	if source == nil || !source.Isset("type") || !source.Isset("url") || !source.Isset("reference") {
		return &util.UnexpectedValueError{Message: "Package " + php.ToString(config.At("name")) +
			"'s source key should be specified as {\"type\": ..., \"url\": ..., \"reference\": ...},\n" +
			jsonEncode(config.At("source")) + " given."}
	}

	typ, err := nullableString(packageClass+"::setSourceType", "type", source.At("type"))
	if err != nil {
		return err
	}

	url, err := nullableString(packageClass+"::setSourceUrl", "url", source.At("url"))
	if err != nil {
		return err
	}

	p.SetSourceType(typ)
	p.SetSourceURL(url)
	p.SetSourceReference(pkg.Str(php.ToString(source.At("reference"))))

	if source.Isset("mirrors") {
		mirrors, err := arrayArg(packageClass+"::setSourceMirrors", "mirrors", "?array", source.At("mirrors"))
		if err != nil {
			return err
		}

		p.SetSourceMirrors(mirrors)
	}

	return nil
}

func configureDist(p *pkg.CompletePackage, config *php.Array) error {
	if !config.Isset("dist") {
		return nil
	}

	dist := config.ArrayAt("dist")
	if dist == nil || !dist.Isset("type") || !dist.Isset("url") {
		return &util.UnexpectedValueError{Message: "Package " + php.ToString(config.At("name")) +
			"'s dist key should be specified as {\"type\": ..., \"url\": ..., \"reference\": ..., \"shasum\": ...},\n" +
			jsonEncode(config.At("dist")) + " given."}
	}

	typ, err := nullableString(packageClass+"::setDistType", "type", dist.At("type"))
	if err != nil {
		return err
	}

	url, err := nullableString(packageClass+"::setDistUrl", "url", dist.At("url"))
	if err != nil {
		return err
	}

	sum, err := nullableString(packageClass+"::setDistSha1Checksum", "sha1checksum", dist.At("shasum"))
	if err != nil {
		return err
	}

	p.SetDistType(typ)
	p.SetDistURL(url)

	if ref := dist.At("reference"); ref != nil {
		p.SetDistReference(pkg.Str(php.ToString(ref)))
	} else {
		p.SetDistReference(pkg.NullString{})
	}

	p.SetDistSha1Checksum(sum)

	if dist.Isset("mirrors") {
		mirrors, err := arrayArg(packageClass+"::setDistMirrors", "mirrors", "?array", dist.At("mirrors"))
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
	archive := config.ArrayAt("archive")

	if name := archive.At("name"); !empty(name) {
		s, ok := name.(string)
		if !ok {
			return pkg.ArgumentTypeError(completePackageClass+"::setArchiveName", 1, "name", "?string", name)
		}

		p.SetArchiveName(pkg.Str(s))
	}

	if exclude := archive.At("exclude"); !empty(exclude) {
		a, err := arrayArg(completePackageClass+"::setArchiveExcludes", "excludes", "array", exclude)
		if err != nil {
			return err
		}

		p.SetArchiveExcludes(a)
	}

	if scripts := config.ArrayAt("scripts"); scripts != nil {
		scripts = castListeners(scripts)
		for _, reserved := range [...]string{"composer", "php", "putenv"} {
			if scripts.Has(reserved) {
				util.TriggerDeprecation("The `" + reserved + "` script name is reserved for internal use, please avoid defining it")
			}
		}

		p.SetScripts(scripts)
	}

	if s, ok := config.At("description").(string); ok && !empty(s) {
		p.SetDescription(pkg.Str(s))
	}

	if s, ok := config.At("homepage").(string); ok && !empty(s) {
		p.SetHomepage(pkg.Str(s))
	}

	if keywords := config.ArrayAt("keywords"); keywords != nil && keywords.Len() > 0 {
		p.SetKeywords(stringValues(keywords))
	}

	if license := config.At("license"); !empty(license) {
		a, ok := license.(*php.Array)
		if !ok {
			a = php.ListOf(license)
		}

		p.SetLicense(a)
	}

	if authors := config.ArrayAt("authors"); authors != nil && authors.Len() > 0 {
		p.SetAuthors(authors)
	}

	if support := config.ArrayAt("support"); support != nil {
		p.SetSupport(support)
	}

	if funding := config.ArrayAt("funding"); funding != nil && funding.Len() > 0 {
		p.SetFunding(funding)
	}

	configureAbandoned(p, config)

	return nil
}

// configureType sets the type, the first thing configureObject sets.
func configureType(p *pkg.CompletePackage, config *php.Array) error {
	typ := pkg.LibraryType

	if v := config.At("type"); v != nil {
		s, ok := v.(string)
		if !ok {
			return pkg.ArgumentTypeError("strtolower", 1, "string", "string", v)
		}

		typ = php.Strtolower(s)
	}

	p.SetType(typ)

	return nil
}

// configureDefaultBranch sets whether the package is the default branch.
func configureDefaultBranch(p *pkg.CompletePackage, config *php.Array) {
	if config.At("default-branch") == true {
		p.SetIsDefaultBranch(true)
	}
}

// configureAbandoned sets the abandoned value.
func configureAbandoned(p *pkg.CompletePackage, config *php.Array) {
	if abandoned := config.At("abandoned"); abandoned != nil {
		p.SetAbandoned(abandoned)
	}
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

		if !config.Isset(t.Type) {
			continue
		}

		links := config.ArrayAt(t.Type)

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
					return pkg.ArgumentTypeError("strtolower", 1, "string", "string", prettyTarget.Value())
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
					return &php.EngineError{Class: php.ClassTypeError, Message: "Cannot access offset of type array in isset or empty"}
				}
				if !ok {
					return pkg.ArgumentTypeError(`Composer\Package\Loader\ArrayLoader::createLink`, 5, "prettyConstraint", "string", c)
				}

				if constraint == "self.version" {
					link, err := l.createLink(name, prettyVersion, t.Method, target, constraint)
					if err != nil {
						return err
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
						return err
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
			return pkg.Links{}, err
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
		if uv := (*semver.UnexpectedValueError)(nil); errors.As(err, &uv) {
			return nil, &util.UnexpectedValueError{Message: "Link constraint in " + source + " " + description + " > " + target +
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
		return "", false, &util.UnexpectedValueError{Message: "no/invalid version defined"}
	}

	version := php.ToString(versionValue)

	if !strings.HasPrefix(version, "dev-") && !strings.HasSuffix(version, "-dev") {
		return "", false, nil
	}

	if aliases := config.ArrayAt("extra").ArrayAt("branch-alias"); aliases != nil {
		for k, v := range aliases.All() {
			sourceBranch := k.String()

			targetBranch, ok := v.(string)
			if !ok {
				return "", false, pkg.ArgumentTypeError("substr", 1, "string", "string", v)
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

	if config.At("default-branch") == true {
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
