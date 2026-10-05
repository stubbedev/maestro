// Ports src/Composer/Package/Version/VersionParser.php. It lives in pkg
// rather than pkg/version because the package classes and the loaders
// need it, and pkg/version depends on the loaders.

package pkg

import (
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
)

// DefaultBranchAlias is VersionParser::DEFAULT_BRANCH_ALIAS.
const DefaultBranchAlias = "9999999-dev"

// VersionParser ports Composer\Package\Version\VersionParser, Composer's
// subclass of the semver VersionParser.
//
// PHP caches parsed constraints in a static property shared by the whole
// process; here the cache belongs to the parser, so share one parser to
// share parsed constraints. It is safe for concurrent use.
type VersionParser struct {
	semver.VersionParser

	constraints sync.Map // string => semver.ConstraintInterface
}

// NewVersionParser returns a VersionParser with an empty constraint
// cache.
func NewVersionParser() *VersionParser { return &VersionParser{} }

// ParseConstraints ports VersionParser::parseConstraints: the semver
// parse, cached by constraint string. Every caller of the same string gets
// the same constraint object.
func (p *VersionParser) ParseConstraints(constraints string) (semver.ConstraintInterface, error) {
	if c, ok := p.constraints.Load(constraints); ok {
		cached, _ := c.(semver.ConstraintInterface)

		return cached, nil
	}

	c, err := p.VersionParser.ParseConstraints(constraints)
	if err != nil {
		return nil, err
	}

	actual, _ := p.constraints.LoadOrStore(constraints, c)
	shared, _ := actual.(semver.ConstraintInterface)

	return shared, nil
}

// NameVersionPair is an entry of parseNameVersionPairs' result:
// ['name' => ..., 'version' => ...], Version being null when the entry
// has no 'version' key.
type NameVersionPair struct {
	Name    string
	Version NullString
}

var (
	nameVersionSeparator = php.MustCompile(`{^([^=: ]+)[=: ](.*)$}`)
	wildcardNextToName   = php.MustCompile(`{(?<=[a-z0-9_/-])\*|\*(?=[a-z0-9_/-])}i`)
)

// ParseNameVersionPairs ports VersionParser::parseNameVersionPairs: it
// parses "name version", "name:version" and "name=version" arguments, and
// "name" followed by a separate version argument.
func (p *VersionParser) ParseNameVersionPairs(pairs []string) []NameVersionPair {
	result := make([]NameVersionPair, 0, len(pairs))

	for i := 0; i < len(pairs); i++ {
		pair, _, err := nameVersionSeparator.Replace(php.Trim(pairs[i]), "$1 $2", -1)
		if err != nil {
			panic(err)
		}

		if !strings.Contains(pair, " ") && i+1 < len(pairs) && !strings.Contains(pairs[i+1], "/") &&
			!mustMatch(wildcardNextToName, pairs[i+1]) && !IsPlatformPackage(pairs[i+1]) {
			pair += " " + pairs[i+1]
			i++
		}

		if strings.IndexByte(pair, ' ') > 0 {
			name, version, _ := strings.Cut(pair, " ")
			result = append(result, NameVersionPair{Name: name, Version: Str(version)})
		} else {
			result = append(result, NameVersionPair{Name: pair})
		}
	}

	return result
}

// IsUpgrade ports VersionParser::isUpgrade: whether going from one
// normalized version to the other is an upgrade (dev versions always are).
func IsUpgrade(normalizedFrom, normalizedTo string) (bool, error) {
	if normalizedFrom == normalizedTo {
		return true, nil
	}

	if isDefaultBranchName(normalizedFrom) {
		normalizedFrom = DefaultBranchAlias
	}

	if isDefaultBranchName(normalizedTo) {
		normalizedTo = DefaultBranchAlias
	}

	if strings.HasPrefix(normalizedFrom, "dev-") || strings.HasPrefix(normalizedTo, "dev-") {
		return true, nil
	}

	sorted, err := semver.Semver.Sort([]string{normalizedTo, normalizedFrom})
	if err != nil {
		return false, err
	}

	return sorted[0] == normalizedFrom, nil
}

func isDefaultBranchName(v string) bool {
	return v == "dev-master" || v == "dev-trunk" || v == "dev-default"
}

// PlatformPackageRegex is PlatformRepository::PLATFORM_PACKAGE_REGEX.
const PlatformPackageRegex = `{^(?:php(?:-64bit|-ipv6|-zts|-debug)?|hhvm|(?:ext|lib)-[a-z0-9](?:[_.-]?[a-z0-9]+)*|composer(?:-(?:plugin|runtime)-api)?)$}iD`

// PlatformPackageRegexp is PlatformPackageRegex compiled, for callers that
// must see the PcreException Preg::isMatch throws when the engine gives up
// (JsonManipulator::sortPackages); IsPlatformPackage cannot fail.
var PlatformPackageRegexp = php.MustCompile(PlatformPackageRegex)

// IsPlatformPackage ports PlatformRepository::isPlatformPackage: whether
// name matches PlatformPackageRegex, matched by hand, as the solver calls
// it for every link.
func IsPlatformPackage(name string) bool {
	if len(name) > 4 && (equalFoldASCII(name[:4], "ext-") || equalFoldASCII(name[:4], "lib-")) {
		return isPlatformSuffix(name[4:])
	}

	for _, n := range [...]string{
		"php", "php-64bit", "php-ipv6", "php-zts", "php-debug", "hhvm",
		"composer", "composer-plugin-api", "composer-runtime-api",
	} {
		if equalFoldASCII(name, n) {
			return true
		}
	}

	return false
}

// equalFoldASCII compares s with the lowercase ASCII string lower, ignoring
// ASCII case only (PCRE's caseless matching without the u modifier).
func equalFoldASCII(s, lower string) bool {
	if len(s) != len(lower) {
		return false
	}

	for i := range len(s) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}

		if c != lower[i] {
			return false
		}
	}

	return true
}

// isPlatformSuffix matches [a-z0-9](?:[_.-]?[a-z0-9]+)* case-insensitively:
// alphanumerics separated by single _ . or - characters.
func isPlatformSuffix(s string) bool {
	prevSep := true // a leading separator is not allowed

	for i := range len(s) {
		switch c := s[i]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			prevSep = false
		case c == '_' || c == '.' || c == '-':
			if prevSep {
				return false
			}

			prevSep = true
		default:
			return false
		}
	}

	return !prevSep
}
