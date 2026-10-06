// Ports src/Composer/Package/Loader/ValidatingArrayLoader.php.

package loader

import (
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/spdx"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

// ValidatingArrayLoader flags: ValidatingArrayLoader::CHECK_*.
const (
	CheckAll                = 3
	CheckUnboundConstraints = 1
	CheckStrictConstraints  = 2
)

// ValidatingArrayLoader ports Composer\Package\Loader\ValidatingArrayLoader:
// it checks a package array, collecting errors and warnings, drops the
// invalid parts and hands the rest to another loader.
type ValidatingArrayLoader struct {
	loader        LoaderInterface
	versionParser *pkg.VersionParser
	flags         int
	errors        []string
	warnings      []string
	config        *php.Array

	// Now returns the current time; the license check depends on it
	// (strtotime('-8days')).
	Now func() time.Time
}

// NewValidatingArrayLoader ports ValidatingArrayLoader::__construct (the
// deprecated $strictName argument is gone); a nil parser gets a new one.
func NewValidatingArrayLoader(loader LoaderInterface, parser *pkg.VersionParser, flags int) *ValidatingArrayLoader {
	if parser == nil {
		parser = pkg.NewVersionParser()
	}

	return &ValidatingArrayLoader{loader: loader, versionParser: parser, flags: flags, Now: time.Now}
}

// Warnings ports ValidatingArrayLoader::getWarnings.
func (l *ValidatingArrayLoader) Warnings() []string { return l.warnings }

// Errors ports ValidatingArrayLoader::getErrors.
func (l *ValidatingArrayLoader) Errors() []string { return l.errors }

func (l *ValidatingArrayLoader) errorf(parts ...string) {
	l.errors = append(l.errors, strings.Join(parts, ""))
}

func (l *ValidatingArrayLoader) warnf(parts ...string) {
	l.warnings = append(l.warnings, strings.Join(parts, ""))
}

var (
	binParentSegment   = php.MustCompile(`{(?:^|[\\/])\.\.(?:[\\/]|$)}`)
	startsWithDash     = php.MustCompile(`{^\s*-}`)
	validLinkKey       = php.MustCompile(`{^[A-Za-z0-9_./-]+$}`)
	unboundConstraint  = semver.NewConstraintOp(semver.OpEQ, "10000000-dev")
	stableOrHigherDev  = semver.NewConstraintOp(semver.OpGE, "1.0.0.0-dev")
	supportStringKeys  = [...]string{"issues", "forum", "wiki", "source", "email", "irc", "docs", "rss", "chat", "security"}
	supportURLKeys     = [...]string{"issues", "forum", "wiki", "source", "docs", "chat", "security"}
	downloadURLMethods = [...]string{"composer-default", "pre-packaged-source", "pre-packaged-binary"}
	osFamilies         = [...]string{"windows", "bsd", "darwin", "solaris", "linux", "unknown"}
	autoloadTypes      = [...]string{"psr-0", "psr-4", "classmap", "files", "exclude-from-classmap"}
)

// Load ports ValidatingArrayLoader::load. Validation errors make it fail
// with an *InvalidPackageError; warnings are kept for Warnings.
func (l *ValidatingArrayLoader) Load(config *php.Array, class string) (pkg.PackageInterface, error) {
	l.errors = nil
	l.warnings = nil
	l.config = config.Clone()

	if err := l.validate(config); err != nil {
		return nil, err
	}

	if len(l.errors) > 0 {
		e := NewInvalidPackageError(l.errors, l.warnings, config)
		e.Site = phperr.At("ValidatingArrayLoader.php", 615)

		return nil, e
	}

	// the inner loader is an ArrayLoader wherever Composer builds one
	leave := phperr.Push(`Composer\Package\Loader\ArrayLoader->load`, "ValidatingArrayLoader.php", 618)
	p, err := l.loader.Load(l.config, class)
	leave()
	l.config = php.NewArray()

	return p, phperr.Call(err, `Composer\Package\Loader\ArrayLoader->load`, "ValidatingArrayLoader.php", 618)
}

func (l *ValidatingArrayLoader) validate(config *php.Array) error {
	l.validateString("name", true)

	if name := get(config, "name"); name != nil {
		s, ok := name.(string)
		if !ok {
			return pkg.ArgumentTypeError(`Composer\Package\Loader\ValidatingArrayLoader::hasPackageNamingError`, 1, "name", "string", name).
				Called(`Composer\Package\Loader\ValidatingArrayLoader::hasPackageNamingError`, phperr.At("ValidatingArrayLoader.php", 640), "ValidatingArrayLoader.php", 74)
		}

		if msg, bad, err := HasPackageNamingError(s, false); err != nil {
			return err
		} else if bad {
			l.errorf("name : ", msg)
		}
	}

	l.validateVersion()

	if err := l.validatePlatform(); err != nil {
		return err
	}

	if _, err := l.validateRegex("type", "[A-Za-z0-9-]+", false); err != nil {
		return err
	}

	l.validateString("target-dir", false)
	l.validateArray("extra")

	if err := l.validateBin(); err != nil {
		return err
	}

	l.validateArray("scripts") // TODO validate event names & listener syntax
	l.validateString("description", false)

	l.validateURL("homepage")

	if err := l.validateFlatArray("keywords", `[\p{N}\p{L} ._-]+`); err != nil {
		return err
	}

	releaseDate, hasReleaseDate := l.validateTime()

	l.validateLicense(releaseDate, hasReleaseDate)

	if err := l.validateAuthors(); err != nil {
		return err
	}

	if err := l.validateSupport(); err != nil {
		return err
	}

	if err := l.validateFunding(); err != nil {
		return err
	}

	l.validatePhpExt()

	if err := l.validateLinks(); err != nil {
		return err
	}

	if l.validateArray("suggest") && isset(l.config, "suggest") {
		suggest := subArray(l.config, "suggest")
		for k, description := range suggest.All() {
			if _, ok := description.(string); !ok {
				l.errorf("suggest.", k.String(), " : invalid value, must be a string describing why the package is suggested")
				suggest.DeleteKey(k)
			}
		}
	}

	if l.validateString("minimum-stability", false) && isset(l.config, "minimum-stability") {
		ms, _ := get(l.config, "minimum-stability").(string)
		if _, ok := pkg.StabilityValue(php.Strtolower(ms)); !ok && ms != "RC" {
			l.errorf("minimum-stability : invalid value (", ms, "), must be one of ", strings.Join(pkg.StabilityNames(), ", "))
			l.config.Delete("minimum-stability")
		}
	}

	l.validateAutoload()

	if err := l.validateSourceDist(); err != nil {
		return err
	}

	// TODO validate repositories
	// TODO validate package repositories' packages using this recursively

	if err := l.validateFlatArray("include-path", ""); err != nil {
		return err
	}

	l.validateArray("transport-options")

	return l.validateBranchAlias()
}

func (l *ValidatingArrayLoader) validateVersion() {
	v := get(l.config, "version")
	if v == nil {
		return
	}

	if !isScalar(v) {
		l.validateString("version", false)

		return
	}

	version := php.ToString(v)
	if _, ok := v.(string); !ok {
		l.config.Set("version", version)
	}

	if _, err := l.versionParser.Normalize(version); err != nil {
		l.errorf("version : invalid value (", version, "): ", err.Error())
		l.config.Delete("version")
	}
}

func (l *ValidatingArrayLoader) validatePlatform() error {
	platform := get(subArray(l.config, "config"), "platform")
	if platform == nil {
		return nil
	}

	entries, ok := platform.(*php.Array)
	if !ok {
		entries = php.ListOf(platform)
	}

	for k, v := range entries.All() {
		if v == false {
			continue
		}

		s, ok := v.(string)
		if !ok {
			l.errorf("config.platform.", k.String(), " : invalid value (", php.TypeName(v), " ", php.VarExport(v), "): expected string or false")

			continue
		}

		if _, err := l.versionParser.Normalize(s); err != nil {
			l.errorf("config.platform.", k.String(), " : invalid value (", s, "): ", err.Error())
		}
	}

	return nil
}

func (l *ValidatingArrayLoader) validateBin() error {
	if !isset(l.config, "bin") {
		return nil
	}

	if _, ok := get(l.config, "bin").(string); ok {
		l.validateString("bin", false)
	} else if err := l.validateFlatArray("bin", ""); err != nil {
		return err
	}

	// A ".." path segment in a bin escapes the package install directory and lets the
	// package chmod/point at an arbitrary host file during install (GHSA-gjfg-22fp-rrxx).
	switch bin := get(l.config, "bin").(type) {
	case string:
		if mustMatch(binParentSegment, bin) {
			l.errorf("bin : invalid value (", bin, "), must not contain a \"..\" path component")
			l.config.Delete("bin")
		}
	case *php.Array:
		for k, v := range bin.All() {
			if s, ok := v.(string); ok && mustMatch(binParentSegment, s) {
				l.errorf("bin.", k.String(), " : invalid value (", s, "), must not contain a \"..\" path component")
				bin.DeleteKey(k)
			}
		}
	}

	return nil
}

func (l *ValidatingArrayLoader) validateTime() (time.Time, bool) {
	l.validateString("time", false)

	s, ok := get(l.config, "time").(string)
	if !ok {
		return time.Time{}, false
	}

	date, err := parseDateTimeAt(s, l.Now())
	if err != nil {
		l.errorf("time : invalid value (", s, "): ", err.Error())
		l.config.Delete("time")

		return time.Time{}, false
	}

	return date, true
}

func (l *ValidatingArrayLoader) validateLicense(releaseDate time.Time, hasReleaseDate bool) {
	license := get(l.config, "license")
	if license == nil {
		return
	}

	var licenses *php.Array

	switch v := license.(type) {
	case *php.Array:
		licenses = v.Clone()
	case string:
		licenses = php.ListOf(v)
	default:
		l.warnf("License must be a string or array of strings, got ", jsonEncode(license), ".")
		l.config.Delete("license")

		return
	}

	// validate main data types
	for k, v := range licenses.All() {
		if _, ok := v.(string); !ok {
			l.warnf("License ", jsonEncode(v), " should be a string.")
			licenses.DeleteKey(k)
		}
	}

	// check for license validity on newly updated branches/tags
	if !hasReleaseDate || releaseDate.Unix() >= l.Now().Unix()-8*86400 {
		validator := spdx.New()

		for _, v := range licenses.All() {
			lic, _ := v.(string)

			// replace proprietary by MIT for validation purposes since it's not a valid SPDX identifier, but is accepted by composer
			if lic == "proprietary" {
				continue
			}

			toValidate := strings.ReplaceAll(lic, "proprietary", "MIT")
			if validator.Validate(toValidate) {
				continue
			}

			if validator.Validate(php.Trim(toValidate)) {
				l.warnf("License ", jsonEncode(lic), " must not contain extra spaces, make sure to trim it.")
			} else {
				l.warnf("License ", jsonEncode(lic), " is not a valid SPDX license identifier, see https://spdx.org/licenses/ if you use an open license.", php.EOL,
					"If the software is closed-source, you may use \"proprietary\" as license.")
			}
		}
	}

	l.config.Set("license", php.ArrayValues(licenses))
}

func (l *ValidatingArrayLoader) validateAuthors() error {
	if !l.validateArray("authors") {
		return nil
	}

	authors := subArray(l.config, "authors")

	for k, v := range authors.All() {
		key := k.String()

		author, ok := v.(*php.Array)
		if !ok {
			l.errorf("authors.", key, " : should be an array, ", php.TypeName(v), " given")
			authors.DeleteKey(k)

			continue
		}

		// $author is a copy: the checks below look at the original values.
		original := author.Clone()

		for _, field := range [...]string{"homepage", "email", "name", "role"} {
			if fv := get(original, field); fv != nil {
				if _, ok := fv.(string); !ok {
					l.errorf("authors.", key, ".", field, " : invalid value, must be a string")
					author.Delete(field)
				}
			}
		}

		if homepage := get(original, "homepage"); homepage != nil {
			ok, err := filterURL(homepage, "http", "https")
			if err != nil {
				return err
			}

			if !ok {
				l.warnf("authors.", key, ".homepage : invalid value (", php.ToString(homepage), "), must be an http/https URL")
				author.Delete("homepage")
			}
		}

		if email := get(original, "email"); email != nil && !filterEmail(email) {
			l.warnf("authors.", key, ".email : invalid value (", php.ToString(email), "), must be a valid email address")
			author.Delete("email")
		}

		if author.Len() == 0 {
			authors.DeleteKey(k)
		}
	}

	if authors.Len() == 0 {
		l.config.Delete("authors")
	}

	return nil
}

func (l *ValidatingArrayLoader) validateSupport() error {
	if !l.validateArray("support") {
		return nil
	}

	support := subArray(l.config, "support")
	if support.Len() == 0 {
		return nil
	}

	for _, key := range supportStringKeys {
		if v := get(support, key); v != nil {
			if _, ok := v.(string); !ok {
				l.errorf("support.", key, " : invalid value, must be a string")
				support.Delete(key)
			}
		}
	}

	if email, ok := get(support, "email").(string); ok && !filterEmail(email) {
		l.warnf("support.email : invalid value (", email, "), must be a valid email address")
		support.Delete("email")
	}

	if irc, ok := get(support, "irc").(string); ok {
		if valid, _ := filterURL(irc, "irc", "ircs"); !valid {
			l.warnf("support.irc : invalid value (", irc, "), must be a irc://<server>/<channel> or ircs:// URL")
			support.Delete("irc")
		}
	}

	for _, key := range supportURLKeys {
		if v, ok := get(support, key).(string); ok {
			if valid, _ := filterURL(v, "http", "https"); !valid {
				l.warnf("support.", key, " : invalid value (", v, "), must be an http/https URL")
				support.Delete(key)
			}
		}
	}

	if support.Len() == 0 {
		l.config.Delete("support")
	}

	return nil
}

func (l *ValidatingArrayLoader) validateFunding() error {
	if !l.validateArray("funding") {
		return nil
	}

	funding := subArray(l.config, "funding")
	if funding.Len() == 0 {
		return nil
	}

	for k, v := range funding.All() {
		key := k.String()

		option, ok := v.(*php.Array)
		if !ok {
			l.errorf("funding.", key, " : should be an array, ", php.TypeName(v), " given")
			funding.DeleteKey(k)

			continue
		}

		original := option.Clone()

		for _, field := range [...]string{"type", "url"} {
			if fv := get(original, field); fv != nil {
				if _, ok := fv.(string); !ok {
					l.errorf("funding.", key, ".", field, " : invalid value, must be a string")
					option.Delete(field)
				}
			}
		}

		if url := get(original, "url"); url != nil {
			ok, err := filterURL(url, "http", "https")
			if err != nil {
				return err
			}

			if !ok {
				l.warnf("funding.", key, ".url : invalid value (", php.ToString(url), "), must be an http/https URL")
				option.Delete("url")
			}
		}

		if option.Len() == 0 {
			funding.DeleteKey(k)
		}
	}

	if funding.Len() == 0 {
		l.config.Delete("funding")
	}

	return nil
}

func (l *ValidatingArrayLoader) validatePhpExt() {
	if !isset(l.config, "php-ext") || !l.validateArray("php-ext") {
		return
	}

	if typ, _ := get(l.config, "type").(string); typ != "php-ext" && typ != "php-ext-zend" {
		l.errorf(`php-ext can only be set by packages of type "php-ext" or "php-ext-zend" which must be C extensions`)
		// PHP unsets it, then takes a reference to it, which brings the
		// key back (at the end) holding null.
		l.config.Delete("php-ext")
		l.config.Set("php-ext", nil)

		return
	}

	phpExt := subArray(l.config, "php-ext")

	if v := get(phpExt, "extension-name"); v != nil {
		if _, ok := v.(string); !ok {
			l.errorf("php-ext.extension-name : should be a string, ", php.TypeName(v), " given")
			phpExt.Delete("extension-name")
		}
	}

	if v := get(phpExt, "priority"); v != nil {
		if _, ok := v.(int64); !ok {
			l.errorf("php-ext.priority : should be an integer, ", php.TypeName(v), " given")
			phpExt.Delete("priority")
		}
	}

	for _, field := range [...]string{"support-zts", "support-nts"} {
		if v := get(phpExt, field); v != nil {
			if _, ok := v.(bool); !ok {
				l.errorf("php-ext.", field, " : should be a boolean, ", php.TypeName(v), " given")
				phpExt.Delete(field)
			}
		}
	}

	if v := get(phpExt, "build-path"); v != nil {
		if _, ok := v.(string); !ok {
			l.errorf("php-ext.build-path : should be a string or null, ", php.TypeName(v), " given")
			phpExt.Delete("build-path")
		}
	}

	l.validateDownloadURLMethod(phpExt)
	l.validateOsFamilies(phpExt)
	l.validateConfigureOptions(phpExt)

	// If php-ext is now empty, unset it
	if phpExt.Len() == 0 {
		l.config.Delete("php-ext")
	}
}

func (l *ValidatingArrayLoader) validateDownloadURLMethod(phpExt *php.Array) {
	v := get(phpExt, "download-url-method")
	if v == nil {
		return
	}

	var defined *php.Array

	switch v := v.(type) {
	case *php.Array:
		defined = v
	case string:
		defined = php.ListOf(v)
	default:
		l.errorf("php-ext.download-url-method : should be an array or a string, ", php.TypeName(v), " given")
		phpExt.Delete("download-url-method")

		return
	}

	if defined.Len() == 0 {
		l.errorf("php-ext.download-url-method : must contain at least one element")
		phpExt.Delete("download-url-method")

		return
	}

	for k, method := range defined.All() {
		s, ok := method.(string)

		switch {
		case !ok:
			l.errorf("php-ext.download-url-method.", k.String(), " : should be a string, ", php.TypeName(method), " given")
			phpExt.Delete("download-url-method")
		case !slices.Contains(downloadURLMethods[:], s):
			l.errorf("php-ext.download-url-method.", k.String(), " : invalid value (", s, "), must be one of ", strings.Join(downloadURLMethods[:], ", "))
			phpExt.Delete("download-url-method")
		}
	}
}

func (l *ValidatingArrayLoader) validateOsFamilies(phpExt *php.Array) {
	if isset(phpExt, "os-families") && isset(phpExt, "os-families-exclude") {
		l.errorf("php-ext : os-families and os-families-exclude cannot both be specified")
		phpExt.Delete("os-families")
		phpExt.Delete("os-families-exclude")

		return
	}

	for _, field := range [...]string{"os-families", "os-families-exclude"} {
		v := get(phpExt, field)
		if v == nil {
			continue
		}

		families, ok := v.(*php.Array)

		switch {
		case !ok:
			l.errorf("php-ext.", field, " : should be an array, ", php.TypeName(v), " given")
			phpExt.Delete(field)
		case families.Len() == 0:
			l.errorf("php-ext.", field, " : must contain at least one element")
			phpExt.Delete(field)
		default:
			for k, family := range families.All() {
				s, ok := family.(string)

				switch {
				case !ok:
					l.errorf("php-ext.", field, ".", k.String(), " : should be a string, ", php.TypeName(family), " given")
					families.DeleteKey(k)
				case !slices.Contains(osFamilies[:], s):
					l.errorf("php-ext.", field, ".", k.String(), " : invalid value (", s, "), must be one of ", strings.Join(osFamilies[:], ", "))
					families.DeleteKey(k)
				}
			}

			if families.Len() == 0 {
				phpExt.Delete(field)
			}
		}
	}
}

func (l *ValidatingArrayLoader) validateConfigureOptions(phpExt *php.Array) {
	v := get(phpExt, "configure-options")
	if v == nil {
		return
	}

	options, ok := v.(*php.Array)
	if !ok {
		l.errorf("php-ext.configure-options : should be an array, ", php.TypeName(v), " given")
		phpExt.Delete("configure-options")

		return
	}

	for k, o := range options.All() {
		key := k.String()

		option, ok := o.(*php.Array)
		if !ok {
			l.errorf("php-ext.configure-options.", key, " : should be an array, ", php.TypeName(o), " given")
			options.DeleteKey(k)

			continue
		}

		name := get(option, "name")
		if name == nil {
			l.errorf("php-ext.configure-options.", key, ".name : must be present")
			options.DeleteKey(k)

			continue
		}

		if _, ok := name.(string); !ok {
			l.errorf("php-ext.configure-options.", key, ".name : should be a string, ", php.TypeName(name), " given")
			options.DeleteKey(k)

			continue
		}

		if nv := get(option, "needs-value"); nv != nil {
			if _, ok := nv.(bool); !ok {
				l.errorf("php-ext.configure-options.", key, ".needs-value : should be a boolean, ", php.TypeName(nv), " given")
				option.Delete("needs-value")
			}
		}

		if d := get(option, "description"); d != nil {
			if _, ok := d.(string); !ok {
				l.errorf("php-ext.configure-options.", key, ".description : should be a string, ", php.TypeName(d), " given")
				option.Delete("description")
			}
		}
	}

	if options.Len() == 0 {
		phpExt.Delete("configure-options")
	}
}

func (l *ValidatingArrayLoader) validateLinks() error {
	for _, t := range pkg.SupportedLinkTypes() {
		linkType := t.Type
		if !l.validateArray(linkType) || !isset(l.config, linkType) {
			continue
		}

		links := subArray(l.config, linkType)

		for k, c := range links.All() {
			name := k.String()

			if configName, ok := get(l.config, "name").(string); ok && php.Strcasecmp(name, configName) == 0 {
				l.errorf(linkType, ".", name, " : a package cannot set a ", linkType, " on itself")
				links.DeleteKey(k)

				continue
			}

			msg, bad, err := HasPackageNamingError(name, true)
			if err != nil {
				return err
			}

			if bad {
				l.warnf(linkType, ".", msg)
			} else if valid, err := validLinkKey.IsMatch(name); err != nil {
				// Preg::isMatch throws. HasPackageNamingError has already
				// matched the name against a stricter pattern, so this
				// cannot happen today.
				return err
			} else if !valid {
				l.errorf(linkType, ".", name, " : invalid key, package names must be strings containing only [A-Za-z0-9_./-]")
			}

			if constraint, ok := c.(string); !ok {
				l.errorf(linkType, ".", name, " : invalid value, must be a string containing a version constraint")
				links.DeleteKey(k)
			} else if constraint != "self.version" {
				linkConstraint, err := l.versionParser.ParseConstraints(constraint)
				if err != nil {
					l.errorf(linkType, ".", name, " : invalid version constraint (", err.Error(), ")")
					links.DeleteKey(k)

					continue
				}

				l.checkConstraint(linkType, name, constraint, linkConstraint)
			}

			if linkType == "conflict" && isset(l.config, "replace") {
				replace, ok := get(l.config, "replace").(*php.Array)
				if !ok {
					return pkg.ArgumentTypeError("array_intersect_key", 1, "array", "array", get(l.config, "replace")).
						Raised("array_intersect_key", "ValidatingArrayLoader.php", 481)
				}

				if php.ArrayIntersectKey(replace, links).Len() > 0 {
					l.errorf(linkType, ".", name, " : you cannot conflict with a package that is also replaced, as replace already creates an implicit conflict rule")
					links.DeleteKey(k)
				}
			}
		}
	}

	return nil
}

// checkConstraint emits the unbound, exact and impossible constraint
// warnings.
func (l *ValidatingArrayLoader) checkConstraint(linkType, name, constraint string, linkConstraint semver.ConstraintInterface) {
	if l.flags&CheckUnboundConstraints != 0 && linkType == "require" && linkConstraint.Matches(unboundConstraint) && !pkg.IsPlatformPackage(name) {
		// check requires for unbound constraints on non-platform packages
		l.warnf(linkType, ".", name, " : unbound version constraints (", constraint, ") should be avoided")
	} else if c, ok := linkConstraint.(*semver.Constraint); ok && l.flags&CheckStrictConstraints != 0 && linkType == "require" &&
		(c.Operator() == "==" || c.Operator() == "=") && stableOrHigherDev.Matches(c) {
		// check requires for exact constraints
		l.warnf(linkType, ".", name, " : exact version constraints (", constraint, ") should be avoided if the package follows semantic versioning")
	}

	if _, ok := semver.Intervals.CompactConstraint(linkConstraint).(*semver.MatchNoneConstraint); ok {
		l.warnf(linkType, ".", name, " : this version constraint cannot possibly match anything (", constraint, ")")
	}
}

func (l *ValidatingArrayLoader) validateAutoload() {
	if l.validateArray("autoload") && isset(l.config, "autoload") {
		autoload := subArray(l.config, "autoload")

		for k, typeConfig := range autoload.All() {
			typ := k.Value()

			if !php.InArray(typ, php.StringList(autoloadTypes[:]), false) {
				l.errorf("autoload : invalid value (", k.String(), "), must be one of ", strings.Join(autoloadTypes[:], ", "))
				autoload.DeleteKey(k)
			}

			if typ != "psr-4" {
				continue
			}

			namespaces, _ := typeConfig.(*php.Array)
			if namespaces == nil {
				continue
			}

			for ns := range namespaces.All() {
				if ns.IsString() && ns.String() == "" {
					continue
				}

				if !strings.HasSuffix(ns.String(), `\`) {
					l.errorf("autoload.psr-4 : invalid value (", ns.String(), "), namespaces must end with a namespace separator, should be ", ns.String(), `\\`)
				}
			}
		}
	}

	if isset(subArray(l.config, "autoload"), "psr-4") && isset(l.config, "target-dir") {
		l.errorf("target-dir : this can not be used together with the autoload.psr-4 setting, remove target-dir to upgrade to psr-4")
		// Unset the psr-4 setting, since unsetting target-dir might
		// interfere with other settings.
		subArray(l.config, "autoload").Delete("psr-4")
	}
}

func (l *ValidatingArrayLoader) validateSourceDist() error {
	for _, srcType := range [...]string{"source", "dist"} {
		if !l.validateArray(srcType) || empty(get(l.config, srcType)) {
			continue
		}

		src := subArray(l.config, srcType)

		if !isset(src, "type") {
			l.errorf(srcType, ".type : must be present")
		}

		if !isset(src, "url") {
			l.errorf(srcType, ".url : must be present")
		}

		if srcType == "source" && !isset(src, "reference") {
			l.errorf(srcType, ".reference : must be present")
		}

		if v := get(src, "type"); v != nil {
			if _, ok := v.(string); !ok {
				l.errorf(srcType, ".type : should be a string, ", php.TypeName(v), " given")
			}
		}

		if v := get(src, "url"); v != nil {
			if _, ok := v.(string); !ok {
				l.errorf(srcType, ".url : should be a string, ", php.TypeName(v), " given")
			}
		}

		if v := get(src, "reference"); v != nil {
			switch v.(type) {
			case string, int64:
			default:
				l.errorf(srcType, ".reference : should be a string or int, ", php.TypeName(v), " given")
			}

			if mustMatch(startsWithDash, php.ToString(v)) {
				l.errorf(srcType, ".reference : must not start with a \"-\", \"", php.ToString(v), "\" given")
			}
		}

		if v := get(src, "url"); v != nil && mustMatch(startsWithDash, php.ToString(v)) {
			l.errorf(srcType, ".url : must not start with a \"-\", \"", php.ToString(v), "\" given")
		}

		// a perforce url is passed to the p4 client as P4PORT, where rsh:/jsh: endpoints
		// mean "run this command locally" (GHSA-rvx4-ffvw-m9q3)
		if url, ok := get(src, "url").(string); ok && srcType == "source" && get(src, "type") == "perforce" && !IsValidPerforcePort(url) {
			l.errorf(srcType, ".url : invalid Perforce port (\"", url, "\"), it must be of the form [tcp|ssl:][host:]port")
		}
	}

	return nil
}

func (l *ValidatingArrayLoader) validateBranchAlias() error {
	v := get(subArray(l.config, "extra"), "branch-alias")
	if v == nil {
		return nil
	}

	aliases, ok := v.(*php.Array)
	if !ok {
		l.errorf("extra.branch-alias : must be an array of versions => aliases")

		return nil
	}

	for k, t := range aliases.All() {
		sourceBranch := k.String()

		targetBranch, ok := t.(string)
		if !ok {
			l.warnf("extra.branch-alias.", sourceBranch, " : the target branch (", jsonEncode(t), ") must be a string, \"", php.TypeName(t), "\" received.")
			aliases.DeleteKey(k)

			continue
		}

		// ensure it is an alias to a -dev package
		if !strings.HasSuffix(targetBranch, "-dev") {
			l.warnf("extra.branch-alias.", sourceBranch, " : the target branch (", targetBranch, ") must end in -dev")
			aliases.DeleteKey(k)

			continue
		}

		// normalize without -dev and ensure it's a numeric branch that is parseable
		validatedTargetBranch := l.versionParser.NormalizeBranch(targetBranch[:len(targetBranch)-4])
		if !strings.HasSuffix(validatedTargetBranch, "-dev") {
			l.warnf("extra.branch-alias.", sourceBranch, " : the target branch (", targetBranch, ") must be a parseable number like 2.0-dev")
			aliases.DeleteKey(k)

			continue
		}

		// If using numeric aliases ensure the alias is a valid subversion
		if !numericAliasCompatible(l.versionParser, sourceBranch, targetBranch) {
			l.warnf("extra.branch-alias.", sourceBranch, " : the target branch (", targetBranch, ") is not a valid numeric alias for this version")
			aliases.DeleteKey(k)
		}
	}

	return nil
}

func (l *ValidatingArrayLoader) validateRegex(property, regex string, mandatory bool) (bool, error) {
	if !l.validateString(property, mandatory) {
		return false, nil
	}

	value, _ := get(l.config, property).(string)

	ok, err := php.PregIsMatch("{^"+regex+"$}u", value)
	if err != nil {
		return false, err
	}

	if !ok {
		message := property + " : invalid value (" + value + "), must match " + regex
		if mandatory {
			l.errors = append(l.errors, message)
		} else {
			l.warnings = append(l.warnings, message)
		}

		l.config.Delete(property)

		return false, nil
	}

	return true, nil
}

func (l *ValidatingArrayLoader) validateString(property string, mandatory bool) bool {
	v := get(l.config, property)

	if v != nil {
		if _, ok := v.(string); !ok {
			l.errorf(property, " : should be a string, ", php.TypeName(v), " given")
			l.config.Delete(property)

			return false
		}
	}

	if s, _ := v.(string); v == nil || php.Trim(s) == "" {
		if mandatory {
			l.errorf(property, " : must be present")
		}

		l.config.Delete(property)

		return false
	}

	return true
}

func (l *ValidatingArrayLoader) validateArray(property string) bool {
	v := get(l.config, property)

	if v != nil {
		if _, ok := v.(*php.Array); !ok {
			l.errorf(property, " : should be an array, ", php.TypeName(v), " given")
			l.config.Delete(property)

			return false
		}
	}

	if a, _ := v.(*php.Array); a == nil || a.Len() == 0 {
		// PHP reports a missing mandatory array; no caller passes $mandatory.
		l.config.Delete(property)

		return false
	}

	return true
}

func (l *ValidatingArrayLoader) validateFlatArray(property, regex string) error {
	if !l.validateArray(property) {
		return nil
	}

	a := subArray(l.config, property)

	for k, v := range a.All() {
		if _, ok := v.(string); !ok && !php.IsNumeric(v) {
			l.errorf(property, ".", k.String(), " : must be a string or int, ", php.TypeName(v), " given")
			a.DeleteKey(k)

			continue
		}

		if regex == "" {
			continue
		}

		ok, err := php.PregIsMatch("{^"+regex+"$}u", php.ToString(v))
		if err != nil {
			return err
		}

		if !ok {
			l.warnf(property, ".", k.String(), " : invalid value (", php.ToString(v), "), must match ", regex)
			a.DeleteKey(k)
		}
	}

	return nil
}

func (l *ValidatingArrayLoader) validateURL(property string) {
	if !l.validateString(property, false) {
		return
	}

	value, _ := get(l.config, property).(string)

	if ok, _ := filterURL(value, "http", "https"); !ok {
		l.warnf(property, " : invalid value (", value, "), must be an http/https URL")
		l.config.Delete(property)
	}
}

// filterURL ports ValidatingArrayLoader::filterUrl; a non-string value is
// the TypeError parse_url raises under strict types.
func filterURL(value any, schemes ...string) (bool, error) {
	if value == "" {
		return true, nil
	}

	s, ok := value.(string)
	if !ok {
		return false, pkg.ArgumentTypeError("parse_url", 1, "url", "string", value).Raised("parse_url", "ValidatingArrayLoader.php", 862)
	}

	bits, ok := util.ParseURL(s)
	if !ok || bits.Scheme == "" || bits.Scheme == "0" || bits.Host == "" || bits.Host == "0" {
		return false, nil
	}

	return slices.Contains(schemes, bits.Scheme), nil
}

var (
	reservedNames     = [...]string{"nul", "con", "prn", "aux", "com1", "com2", "com3", "com4", "com5", "com6", "com7", "com8", "com9", "lpt1", "lpt2", "lpt3", "lpt4", "lpt5", "lpt6", "lpt7", "lpt8", "lpt9"}
	validPackageName  = php.MustCompile(`{^[a-z0-9](?:[_.-]?[a-z0-9]++)*+/[a-z0-9](?:(?:[_.]|-{1,2})?[a-z0-9]++)*+$}iD`)
	jsonSuffix        = php.MustCompile(`{\.json$}`)
	uppercase         = php.MustCompile(`{[A-Z]}`)
	camelCaseBoundary = php.MustCompile(`{(?:([a-z])([A-Z])|([A-Z])([A-Z][a-z]))}`)
)

// HasPackageNamingError ports ValidatingArrayLoader::hasPackageNamingError:
// the problem with a package name (link targets with isLink), if any.
func HasPackageNamingError(name string, isLink bool) (string, bool, error) {
	if pkg.IsPlatformPackage(name) {
		return "", false, nil
	}

	ok, err := validPackageName.IsMatch(name)
	if err != nil {
		return "", false, err
	}

	if !ok {
		return name + ` is invalid, it should have a vendor name, a forward slash, and a package name. The vendor and package name can be words separated by -, . or _. The complete name should match "^[a-z0-9]([_.-]?[a-z0-9]+)*/[a-z0-9](([_.]?|-{0,2})[a-z0-9]+)*$".`, true, nil
	}

	vendor, project, _ := strings.Cut(php.Strtolower(name), "/")
	if project, _, _ = strings.Cut(project, "/"); slices.Contains(reservedNames[:], vendor) || slices.Contains(reservedNames[:], project) {
		return name + " is reserved, package and vendor names can not match any of: " + strings.Join(reservedNames[:], ", ") + ".", true, nil
	}

	if mustMatch(jsonSuffix, name) {
		return name + " is invalid, package names can not end in .json, consider renaming it or perhaps using a -json suffix instead.", true, nil
	}

	if mustMatch(uppercase, name) {
		if isLink {
			return name + " is invalid, it should not contain uppercase characters. Please use " + php.Strtolower(name) + " instead.", true, nil
		}

		suggestName := php.Strtolower(mustReplace(camelCaseBoundary, name, `\1\3-\2\4`))

		return name + " is invalid, it should not contain uppercase characters. We suggest using " + suggestName + " instead.", true, nil
	}

	return "", false, nil
}

// IsValidPerforcePort is Perforce::isValidPort, which vcs.IsValidPort
// owns; it is kept for existing callers.
func IsValidPerforcePort(url string) bool { return vcs.IsValidPort(url) }

// ValidatePackage ports ValidatingArrayLoader::validatePackage: it rejects
// names, URLs, references and binaries that could be abused, failing with
// a *pkg.SecurityError. Root packages are not checked.
func ValidatePackage(p pkg.PackageInterface) error {
	// The root package's name/metadata is locally controlled and already validated by
	// RootPackageLoader (and its "__root__" placeholder name would be a false positive here).
	// RootPackageInterface covers both RootPackage and RootAliasPackage.
	if _, ok := p.(pkg.RootPackageInterface); ok {
		return nil
	}

	// getName() is already lowercased, so the uppercase style branch never fires and only
	// structural/security failures throw. Platform packages return null here.
	if msg, bad, err := HasPackageNamingError(p.Name(), false); err != nil {
		return err
	} else if bad {
		return &pkg.SecurityError{Site: phperr.At("ValidatingArrayLoader.php", 695), Message: "Invalid package found during dependency resolution, aborting: " + msg}
	}

	// A url or reference starting with a "-" may be misinterpreted as a command-line option
	// by the VCS/download tooling, same protection as the source/dist checks done in load().
	for _, f := range [...]struct {
		field string
		value pkg.NullString
	}{
		{"source.url", p.SourceURL()},
		{"source.reference", p.SourceReference()},
		{"dist.url", p.DistURL()},
		{"dist.reference", p.DistReference()},
	} {
		if f.value.Valid && mustMatch(startsWithDash, f.value.S) {
			return &pkg.SecurityError{Site: phperr.At("ValidatingArrayLoader.php", 708), Message: p.Name() + " has an invalid " + f.field + ", it must not start with a \"-\": " + f.value.S}
		}
	}

	// A perforce source.url ends up as the p4 client's P4PORT, and a "rsh:"/"jsh:" endpoint
	// there makes the client execute the rest of the value as a local command instead of
	// connecting to a server (GHSA-rvx4-ffvw-m9q3), so only accept network endpoints.
	if sourceURL := p.SourceURL(); p.SourceType() == pkg.Str("perforce") && sourceURL.Valid && !IsValidPerforcePort(sourceURL.S) {
		return &pkg.SecurityError{Site: phperr.At("ValidatingArrayLoader.php", 717), Message: p.Name() + " has an invalid source.url, it must be a Perforce port of the form [tcp|ssl:][host:]port: " + sourceURL.S}
	}

	// Bin paths are resolved relative to the package install dir and then chmod'd (and
	// proxied) by BinaryInstaller. A ".." segment escapes that directory and lets a
	// dependency chmod/point at an arbitrary host file (GHSA-gjfg-22fp-rrxx), so reject it.
	for _, v := range p.Binaries().All() {
		bin := php.ToString(v)
		if mustMatch(binParentSegment, bin) {
			return &pkg.SecurityError{Site: phperr.At("ValidatingArrayLoader.php", 725), Message: p.Name() + " has an invalid bin " + bin + ", it must not contain \"..\" path segments"}
		}
	}

	return nil
}
