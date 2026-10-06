// Ports src/Composer/Package/Loader/RootPackageLoader.php.

package loader

import (
	"strings"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// RootConfig is the part of Composer\Config RootPackageLoader uses.
type RootConfig interface {
	// Repositories ports Config::getRepositories.
	Repositories() *php.Array
}

// RepositoryManager is the part of Composer\Repository\RepositoryManager
// (with RepositoryFactory) RootPackageLoader uses.
type RepositoryManager interface {
	// AddDefaultRepositories adds each of
	// RepositoryFactory::defaultRepos(null, $config, $manager) to the
	// manager.
	AddDefaultRepositories() error
}

// VersionData is the array VersionGuesser::guessVersion returns.
type VersionData struct {
	Version              string
	PrettyVersion        string
	Commit               pkg.NullString
	FeatureVersion       pkg.NullString
	FeaturePrettyVersion pkg.NullString
}

// VersionGuesser is the part of Composer\Package\Version\VersionGuesser
// RootPackageLoader uses; pkg/version implements it.
type VersionGuesser interface {
	// GuessVersion ports guessVersion; nil is null.
	GuessVersion(packageConfig *php.Array, path string) (*VersionData, error)
	// RootVersionFromEnv ports getRootVersionFromEnv.
	RootVersionFromEnv() (string, error)
}

// RootPackageLoader ports Composer\Package\Loader\RootPackageLoader: it
// loads the root package from composer.json, guessing its version and
// collecting aliases, stability flags and references from its requires.
type RootPackageLoader struct {
	*ArrayLoader

	manager        RepositoryManager
	config         RootConfig
	versionGuesser VersionGuesser
	io             io.IO
}

// NewRootPackageLoader ports RootPackageLoader::__construct. PHP builds a
// VersionGuesser when none is given; here versionGuesser is required.
// parser and io may be nil.
func NewRootPackageLoader(manager RepositoryManager, config RootConfig, parser *pkg.VersionParser, versionGuesser VersionGuesser, io io.IO) *RootPackageLoader {
	return &RootPackageLoader{
		ArrayLoader:    NewArrayLoader(parser, false),
		manager:        manager,
		config:         config,
		versionGuesser: versionGuesser,
		io:             io,
	}
}

// Load ports RootPackageLoader::load with the current directory.
func (l *RootPackageLoader) Load(config *php.Array, class string) (pkg.PackageInterface, error) {
	cwd, err := util.GetCwd(true)
	if err != nil {
		return nil, err
	}

	return l.LoadIn(config, class, cwd)
}

// LoadIn ports RootPackageLoader::load: the root package of config, whose
// version is guessed from the VCS checkout in cwd when config has none.
func (l *RootPackageLoader) LoadIn(config *php.Array, class, cwd string) (pkg.PackageInterface, error) {
	config = config.Clone()

	if !isset(config, "name") {
		config.Set("name", "__root__")
	} else {
		name, ok := get(config, "name").(string)
		if !ok {
			return nil, pkg.ArgumentTypeError(`Composer\Package\Loader\ValidatingArrayLoader::hasPackageNamingError`, 1, "name", "string", get(config, "name"))
		}

		if msg, bad, err := HasPackageNamingError(name, false); err != nil {
			return nil, err
		} else if bad {
			return nil, &util.RuntimeError{Message: "Your package name " + msg}
		}
	}

	autoVersioned, err := l.setVersion(config, cwd)
	if err != nil {
		return nil, err
	}

	p, err := l.ArrayLoader.Load(config, class)
	if err != nil {
		return nil, err
	}

	var realPackage *pkg.RootPackage

	switch p := p.(type) {
	case *pkg.RootAliasPackage:
		realPackage, _ = p.AliasOf().(*pkg.RootPackage)
	case *pkg.RootPackage:
		realPackage = p
	}

	if realPackage == nil {
		return nil, &util.LogicError{Message: `Expecting a Composer\Package\RootPackage at this point`}
	}

	if autoVersioned {
		realPackage.ReplaceVersion(realPackage.Version(), pkg.DefaultPrettyVersion)
	}

	if ms := get(config, "minimum-stability"); ms != nil {
		// normalizeStability($stability) is untyped (composer/semver
		// declares no strict_types) and starts with (string) $stability:
		// a scalar is cast, an array is PHP's "Array to string
		// conversion" warning, which Composer's ErrorHandler throws
		if _, ok := ms.(*php.Array); ok {
			err := &util.ErrorException{Message: "Array to string conversion"}

			return nil, err
		}

		normalized, err := semver.NormalizeStability(php.ToString(ms))
		if err != nil {
			return nil, err
		}

		realPackage.SetMinimumStability(normalized)
	}

	if err := l.setRequireData(config, realPackage); err != nil {
		return nil, err
	}

	if pv := get(config, "prefer-stable"); pv != nil {
		realPackage.SetPreferStable(php.ToBool(pv))
	}

	if c := get(config, "config"); c != nil {
		a, err := arrayArg(`Composer\Package\RootPackage::setConfig`, "config", "array", c)
		if err != nil {
			return nil, err
		}

		realPackage.SetConfig(a)
	}

	if err := l.manager.AddDefaultRepositories(); err != nil {
		return nil, err
	}

	realPackage.SetRepositories(l.config.Repositories())

	return p, nil
}

// setVersion fills in the version when config has none, reporting whether
// it fell back to 1.0.0.
func (l *RootPackageLoader) setVersion(config *php.Array, cwd string) (bool, error) {
	if isset(config, "version") {
		return false, nil
	}

	var commit pkg.NullString

	// override with env var if available
	if env, _ := util.GetEnv("COMPOSER_ROOT_VERSION"); php.ToBool(env) {
		version, err := l.versionGuesser.RootVersionFromEnv()
		if err != nil {
			return false, err
		}

		config.Set("version", version)
	} else {
		versionData, err := l.versionGuesser.GuessVersion(config, cwd)
		if err != nil {
			return false, err
		}

		if versionData != nil {
			config.Set("version", versionData.PrettyVersion)
			config.Set("version_normalized", versionData.Version)
			commit = versionData.Commit
		}
	}

	autoVersioned := false

	if !isset(config, "version") {
		name, _ := get(config, "name").(string)
		typ, _ := get(config, "type").(string)

		if l.io != nil && name != "__root__" && typ != "project" {
			l.io.Warning("Composer could not detect the root package ("+name+") version, defaulting to '1.0.0'. See https://getcomposer.org/root-version", nil)
		}

		config.Set("version", "1.0.0")

		autoVersioned = true
	}

	if php.ToBool(commit.Value()) {
		config.Set("source", php.ArrayOf("type", "", "url", "", "reference", commit.S))
		config.Set("dist", php.ArrayOf("type", "", "url", "", "reference", commit.S))
	}

	return autoVersioned, nil
}

// setRequireData sets the aliases, stability flags and references taken
// from the root requirements, and checks the link names.
func (l *RootPackageLoader) setRequireData(config *php.Array, realPackage *pkg.RootPackage) error {
	aliases := php.NewArray()
	stabilityFlags := php.NewArray()
	references := php.NewArray()

	for _, linkType := range [...]string{"require", "require-dev"} {
		if !isset(config, linkType) {
			continue
		}

		t, _ := pkg.SupportedLinkType(linkType)
		links := php.NewArray()

		for link := range pkg.LinksByMethod(realPackage, t.Method).Values() {
			links.Set(link.Target(), link.Constraint().PrettyString())
		}

		var err error
		if aliases, err = l.extractAliases(links, aliases); err != nil {
			return err
		}

		if stabilityFlags, err = ExtractStabilityFlags(links, realPackage.MinimumStability(), stabilityFlags); err != nil {
			return err
		}

		if references, err = ExtractReferences(links, references); err != nil {
			return err
		}

		if name := get(config, "name"); links.Has(name) {
			return &util.RuntimeError{Message: "Root package '" + php.ToString(name) + "' cannot require itself in its composer.json" + php.EOL +
				"Did you accidentally name your root package after an external package?"}
		}
	}

	for _, t := range pkg.SupportedLinkTypes() {
		links := subArray(config, t.Type)
		if links == nil {
			continue
		}

		for k := range links.All() {
			if k.IsInt() {
				return pkg.ArgumentTypeError(`Composer\Package\Loader\ValidatingArrayLoader::hasPackageNamingError`, 1, "name", "string", k.Value())
			}

			if msg, bad, err := HasPackageNamingError(k.String(), true); err != nil {
				return err
			} else if bad {
				return &util.RuntimeError{Message: t.Type + "." + msg}
			}
		}
	}

	realPackage.SetAliases(aliases)
	realPackage.SetStabilityFlags(stabilityFlags)
	realPackage.SetReferences(references)

	return nil
}

var (
	inlineAlias          = php.MustCompile(`{(?:^|\| *|, *)([^,\s#|]+)(?:#[^ ]+)? +as +([^,\s|]+)(?:$| *\|| *,)}`)
	orSplit              = php.MustCompile(`{\s*\|\|?\s*}`)
	andSplit             = php.MustCompile(`{(?<!^|as|[=>< ,]) *(?<!-)[, ](?!-) *(?!,|as|$)}`)
	explicitStability    = php.MustCompile(`{^[^@]*?@(stable|RC|beta|alpha|dev)$}i`)
	stripInlineAlias     = php.MustCompile(`{^([^,\s@]+) as .+$}`)
	singleVersion        = php.MustCompile(`{^[^,\s@]+$}`)
	referenceInConstrant = php.MustCompile(`{^[^,\s@]+?#([a-f0-9]+)$}`)
)

// extractAliases ports RootPackageLoader::extractAliases: the inline
// aliases ("1.0.x-dev as 1.2.0") of requires (name => constraint),
// appended to aliases.
func (l *RootPackageLoader) extractAliases(requires, aliases *php.Array) (*php.Array, error) {
	for k, v := range requires.All() {
		reqName := k.String()
		reqVersion := php.ToString(v)

		match, err := inlineAlias.MatchStrictGroups(reqVersion)
		if err != nil {
			return nil, err
		}

		if match != nil {
			version, err := l.versionParser.NormalizeWithFullVersion(match.Get(1), reqVersion)
			if err != nil {
				return nil, err
			}

			aliasNormalized, err := l.versionParser.NormalizeWithFullVersion(match.Get(2), reqVersion)
			if err != nil {
				return nil, err
			}

			aliases.Append(php.ArrayOf(
				"package", php.Strtolower(reqName),
				"version", version,
				"alias", match.Get(2),
				"alias_normalized", aliasNormalized,
			))
		} else if strings.Contains(reqVersion, " as ") {
			return nil, &util.UnexpectedValueError{Message: "Invalid alias definition in \"" + reqName + "\": \"" + reqVersion +
				"\". Aliases should be in the form \"exact-version as other-exact-version\"."}
		}
	}

	return aliases, nil
}

// ExtractStabilityFlags ports RootPackageLoader::extractStabilityFlags:
// the stability flags (name => BasePackage::STABILITY_*) of requires
// (name => constraint), from explicit @flags or unstable versions, merged
// into a copy of stabilityFlags.
func ExtractStabilityFlags(requires *php.Array, minimumStability string, stabilityFlags *php.Array) (*php.Array, error) {
	stabilityFlags = stabilityFlags.Clone()
	minimum, hasMinimum := pkg.StabilityValue(minimumStability)

	flagAbove := func(name string, stability int) bool {
		v, ok := stabilityFlags.Get(name)

		return ok && v != nil && php.ToInt(v) > int64(stability)
	}

	for k, v := range requires.All() {
		reqName := k.String()

		// extract all sub-constraints in case it is an OR/AND multi-constraint
		var constraints []string

		orParts, err := orSplit.Split(php.Trim(php.ToString(v)), -1, 0)
		if err != nil {
			return nil, err
		}

		for _, orConstraint := range orParts {
			andParts, err := andSplit.Split(orConstraint, -1, 0)
			if err != nil {
				return nil, err
			}

			constraints = append(constraints, andParts...)
		}

		// parse explicit stability flags to the most unstable
		matched := false

		for _, constraint := range constraints {
			match, err := explicitStability.MatchStrictGroups(constraint)
			if err != nil {
				return nil, err
			}

			if match == nil {
				continue
			}

			name := php.Strtolower(reqName)
			normalized, err := semver.NormalizeStability(match.Get(1))
			if err != nil {
				return nil, err
			}

			stability, _ := pkg.StabilityValue(normalized)

			if flagAbove(name, stability) {
				continue
			}

			stabilityFlags.Set(name, stability)

			matched = true
		}

		if matched {
			continue
		}

		for _, constraint := range constraints {
			// infer flags for requirements that have an explicit -dev or -beta version specified but only
			// for those that are more unstable than the minimumStability or existing flags
			// Both patterns backtrack over a long constraint (".+$" before a
			// newline, a class run before "$"), and Preg::* throws.
			reqVersion, _, err := stripInlineAlias.Replace(constraint, "$1", -1)
			if err != nil {
				return nil, err
			}
			if ok, err := singleVersion.IsMatch(reqVersion); err != nil {
				return nil, err
			} else if !ok {
				continue
			}

			stabilityName := semver.ParseStability(reqVersion)
			if stabilityName == semver.StabilityStable {
				continue
			}

			name := php.Strtolower(reqName)
			stability, _ := pkg.StabilityValue(stabilityName)

			if flagAbove(name, stability) || (hasMinimum && minimum > stability) {
				continue
			}

			stabilityFlags.Set(name, stability)
		}
	}

	return stabilityFlags, nil
}

// ExtractReferences ports RootPackageLoader::extractReferences: the
// commit references ("dev-main#abc123") of requires (name => constraint),
// merged into a copy of references.
func ExtractReferences(requires, references *php.Array) (*php.Array, error) {
	references = references.Clone()

	for k, v := range requires.All() {
		reqVersion, _, err := stripInlineAlias.Replace(php.ToString(v), "$1", -1)
		if err != nil {
			return nil, err
		}

		match, err := referenceInConstrant.MatchStrictGroups(reqVersion)
		if err != nil {
			return nil, err
		}

		if match != nil && semver.ParseStability(reqVersion) == semver.StabilityDev {
			references.Set(php.Strtolower(k.String()), match.Get(1))
		}
	}

	return references, nil
}
