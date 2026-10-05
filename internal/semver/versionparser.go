// Ports src/VersionParser.php.

package semver

import "strings"

// VersionParser ports Composer\Semver\VersionParser. It holds no state;
// Composer's own VersionParser embeds it.
type VersionParser struct{}

// Stability names, as parseStability() returns them.
const (
	StabilityStable = "stable"
	StabilityRC     = "RC"
	StabilityBeta   = "beta"
	StabilityAlpha  = "alpha"
	StabilityDev    = "dev"
)

// ParseStability ports VersionParser::parseStability(): the stability of a
// version.
func ParseStability(version string) string {
	version = stripReference(version)

	if strings.HasPrefix(version, "dev-") || strings.HasSuffix(version, "-dev") {
		return StabilityDev
	}

	// PHP lowercases the version and matches case-insensitively, which is
	// the same as matching the original case-insensitively and comparing
	// the captures case-insensitively.
	var match modCaps
	matchStabilityModifier(version, &match)

	if match.dev.nonEmpty() {
		return StabilityDev
	}

	if match.word.nonEmpty() {
		word := match.word.text(version)
		switch {
		case strings.EqualFold(word, "beta") || strings.EqualFold(word, "b"):
			return StabilityBeta
		case strings.EqualFold(word, "alpha") || strings.EqualFold(word, "a"):
			return StabilityAlpha
		case strings.EqualFold(word, "rc"):
			return StabilityRC
		}
	}

	return StabilityStable
}

// NormalizeStability ports VersionParser::normalizeStability().
func NormalizeStability(stability string) (string, error) {
	stability = asciiLower(stability)

	switch stability {
	case "stable", "beta", "alpha", "dev":
		return stability, nil
	case "rc":
		return StabilityRC, nil
	}

	return "", &InvalidArgumentError{Message: "Invalid stability string \"" + stability +
		"\", expected one of stable, RC, beta, alpha or dev"}
}

// Normalize ports normalize($version): it normalizes a version string to be
// able to perform comparisons on it. Errors are *UnexpectedValueError.
func (p VersionParser) Normalize(version string) (string, error) {
	version = phpTrim(version)

	return p.normalize(version, version)
}

// NormalizeWithFullVersion ports normalize($version, $fullVersion), where
// fullVersion is the complete version string, used to give more context
// in the error.
func (p VersionParser) NormalizeWithFullVersion(version, fullVersion string) (string, error) {
	return p.normalize(phpTrim(version), fullVersion)
}

func (p VersionParser) normalize(version, fullVersion string) (string, error) {
	origVersion := version

	// strip off aliasing
	if source, ok := matchAlias(version); ok {
		version = source
	}

	// strip off stability flag
	if n := matchStabilityFlag(version); n > 0 {
		version = version[:len(version)-n]
	}

	// normalize master/trunk/default branches to dev-name for BC with 1.x as these used to be valid constraints
	if version == "master" || version == "trunk" || version == "default" {
		version = "dev-" + version
	}

	// if requirement is branch-like, use full name
	if hasPrefixLit(version, "dev-", true) {
		if strings.HasPrefix(version, "dev-") {
			return version, nil
		}

		return "dev-" + version[4:], nil
	}

	// strip off build metadata
	if base, ok := matchBuildMetadata(version); ok {
		version = base
	}

	// match classical versioning
	var classical classicalCaps
	if matchClassical(version, &classical) {
		b := make([]byte, 0, len(version)+16)
		b = append(b, classical.major.text(version)...)
		for _, minor := range classical.minor {
			if minor.isSet() {
				b = append(b, version[minor.start-1:minor.end]...)
			} else {
				b = append(b, ".0"...)
			}
		}

		return appendModifiers(b, version, &classical.mod), nil
	}

	// match date(time) based versioning
	var date dateCaps
	if matchDate(version, &date) {
		b := make([]byte, 0, len(version)+16)
		for _, c := range []byte(date.date.text(version)) {
			if !isDigit(c) {
				c = '.'
			}
			b = append(b, c)
		}

		return appendModifiers(b, version, &date.mod), nil
	}

	// match dev branches
	if branch, ok := matchDevSuffix(version); ok {
		// a branch ending with -dev is only valid if it is numeric
		// if it gets prefixed with dev- it means the branch name should
		// have had a dev- prefix already when passed to normalize
		if normalized := p.NormalizeBranch(branch); !strings.Contains(normalized, "dev-") {
			return normalized, nil
		}
	}

	extraMessage := ""
	if matchAliasOf(fullVersion, version) {
		extraMessage = " in \"" + fullVersion + "\", the alias must be an exact version"
	} else if matchAliasSourceOf(fullVersion, version) {
		extraMessage = " in \"" + fullVersion + "\", the alias source must be an exact version, if it is a branch name you should prefix it with dev-"
	}

	return "", &UnexpectedValueError{Message: "Invalid version string \"" + origVersion + "\"" + extraMessage}
}

// appendModifiers adds the version modifiers of a matched version to the
// normalized version in b.
func appendModifiers(b []byte, version string, mod *modCaps) string {
	if mod.word.nonEmpty() {
		word := mod.word.text(version)
		if word == "stable" {
			return string(b)
		}
		b = append(b, '-')
		b = append(b, expandStability(word)...)
		b = append(b, strings.TrimLeft(mod.nums.text(version), ".-")...)
	}

	if mod.dev.nonEmpty() {
		b = append(b, "-dev"...)
	}

	return string(b)
}

// ParseNumericAliasPrefix ports parseNumericAliasPrefix(): the numeric
// prefix of an alias (e.g. "2.1." for 2.1.x-dev), suitable for version
// comparison; ok is false when the alias is not in numeric format.
func (VersionParser) ParseNumericAliasPrefix(branch string) (prefix string, ok bool) {
	end, ok := matchNumericAliasPrefix(branch, 0)
	if !ok {
		return "", false
	}

	return branch[:end] + ".", true
}

// NormalizeBranch ports normalizeBranch(): it normalizes a branch name to
// be able to perform comparisons on it.
func (VersionParser) NormalizeBranch(name string) string {
	name = phpTrim(name)

	var groups branchCaps
	if !matchBranch(name, &groups) {
		return "dev-" + name
	}

	// Groups after the last one that matched are absent from $matches,
	// earlier unmatched ones are "".
	last := 0
	for i, g := range groups {
		if g.isSet() {
			last = i
		}
	}
	b := make([]byte, 0, len(name)+32)
	for i, g := range groups {
		part := ".x"
		if i <= last {
			part = g.text(name)
		}
		// str_replace(array('*', 'X'), 'x', ...), then str_replace('x', '9999999', ...)
		for j := range len(part) {
			if c := part[j]; isXStar(c) {
				b = append(b, "9999999"...)
			} else {
				b = append(b, c)
			}
		}
	}

	return string(append(b, "-dev"...))
}

// NormalizeDefaultBranch ports normalizeDefaultBranch(): it normalizes a
// default branch name (i.e. master on git) to 9999999-dev.
//
// Deprecated: Composer 2 does not normalize any branch names to
// 9999999-dev anymore.
func (VersionParser) NormalizeDefaultBranch(name string) string {
	if name == "dev-master" || name == "dev-default" || name == "dev-trunk" {
		return "9999999-dev"
	}

	return name
}

// ParseConstraints ports parseConstraints(): it parses a constraint string
// into MultiConstraint and/or Constraint objects. Errors are
// *UnexpectedValueError.
func (p VersionParser) ParseConstraints(constraints string) (ConstraintInterface, error) {
	prettyConstraint := constraints

	orConstraints := splitOr(phpTrim(constraints))
	orGroups := make([]ConstraintInterface, 0, len(orConstraints))

	for _, orConstraint := range orConstraints {
		andConstraints := splitAnd(orConstraint)
		var constraintObjects []ConstraintInterface
		for _, andConstraint := range andConstraints {
			parsed, n, err := p.parseConstraint(andConstraint)
			if err != nil {
				return nil, err
			}
			constraintObjects = append(constraintObjects, parsed[:n]...)
		}

		if len(constraintObjects) == 1 {
			orGroups = append(orGroups, constraintObjects[0])
		} else {
			orGroups = append(orGroups, newMultiConstraint(constraintObjects, true))
		}
	}

	parsedConstraint := CreateMultiConstraint(orGroups, false)

	parsedConstraint.SetPrettyString(prettyConstraint)

	return parsedConstraint, nil
}

// parseConstraint ports parseConstraint(): one or two constraints.
func (p VersionParser) parseConstraint(constraint string) ([2]ConstraintInterface, int, error) {
	var none [2]ConstraintInterface

	// strip off aliasing
	if source, ok := matchAlias(constraint); ok {
		constraint = source
	}

	// strip @stability flags, and keep it for later use
	stabilityModifier := ""
	if rest, stability, ok := matchStabilitySuffix(constraint); ok {
		constraint = rest
		if rest == "" {
			constraint = "*"
		}
		if stability != "stable" {
			stabilityModifier = stability
		}
	}

	// get rid of #refs as those are used by composer only
	if ref, ok := matchDevReference(constraint); ok {
		constraint = ref
	}

	if ok, hasGroup := matchWildcard(constraint); ok {
		if hasGroup {
			return [2]ConstraintInterface{NewConstraintOp(OpGE, zeroVersion)}, 1, nil
		}

		return [2]ConstraintInterface{NewMatchAllConstraint()}, 1, nil
	}

	// Tilde Range
	//
	// Like wildcard constraints, unsuffixed tilde constraints say that they must be greater than the previous
	// version, to ensure that unstable instances of the current version are allowed. However, if a stability
	// suffix is added to the constraint, then a >= match on the current version is used instead.
	var matches vrCaps
	if matchTilde(constraint, &matches) {
		if strings.HasPrefix(constraint, "~>") {
			return none, 0, &UnexpectedValueError{Message: "Could not parse version constraint " + constraint + ": " +
				"Invalid operator \"~>\", you probably meant to use the \"~\" operator"}
		}

		// Work out which position in the version we are operating at
		position := 1
		for i := 3; i > 0; i-- {
			if matches.num[i].nonEmpty() {
				position = i + 1

				break
			}
		}

		// when matching 2.x-dev or 3.0.x-dev we have to shift the second or third number, despite no second/third number matching above
		if matches.xDev.nonEmpty() {
			position++
		}

		lowVersion, err := p.Normalize(constraint[1:] + stabilitySuffix(&matches))
		if err != nil {
			return none, 0, err
		}

		// For upper bound, we increment the position of one more significance,
		// but highPosition = 0 would be illegal
		highPosition := max(1, position-1)
		highVersion, _ := manipulateVersionString(matches.numbers(constraint), highPosition, 1)

		return [2]ConstraintInterface{
			NewConstraintOp(OpGE, lowVersion),
			NewConstraintOp(OpLT, highVersion+"-dev"),
		}, 2, nil
	}

	// Caret Range
	//
	// Allows changes that do not modify the left-most non-zero digit in the [major, minor, patch] tuple.
	// In other words, this allows patch and minor updates for versions 1.0.0 and above, patch updates for
	// versions 0.X >=0.1.0, and no updates for versions 0.0.X
	if matchCaret(constraint, &matches) {
		// Work out which position in the version we are operating at
		m := matches.numbers(constraint)
		position := 3
		if m[1] != "0" || m[2] == "" {
			position = 1
		} else if m[2] != "0" || m[3] == "" {
			position = 2
		}

		lowVersion, err := p.Normalize(constraint[1:] + stabilitySuffix(&matches))
		if err != nil {
			return none, 0, err
		}

		// For upper bound, we increment the position of one more significance,
		// but highPosition = 0 would be illegal
		highVersion, _ := manipulateVersionString(m, position, 1)

		return [2]ConstraintInterface{
			NewConstraintOp(OpGE, lowVersion),
			NewConstraintOp(OpLT, highVersion+"-dev"),
		}, 2, nil
	}

	// X Range
	//
	// Any of X, x, or * may be used to "stand in" for one of the numeric values in the [major, minor, patch] tuple.
	// A partial version range is treated as an X-Range, so the special character is in fact optional.
	var xRange xRangeCaps
	if matchXRange(constraint, &xRange) {
		position := 1
		if xRange[2].nonEmpty() {
			position = 3
		} else if xRange[1].nonEmpty() {
			position = 2
		}

		m := [5]string{"", xRange[0].text(constraint), xRange[1].text(constraint), xRange[2].text(constraint)}
		lowVersion, _ := manipulateVersionString(m, position, 0)
		highVersion, _ := manipulateVersionString(m, position, 1)
		lowVersion += "-dev"
		highVersion += "-dev"

		if lowVersion == zeroVersion {
			return [2]ConstraintInterface{NewConstraintOp(OpLT, highVersion)}, 1, nil
		}

		return [2]ConstraintInterface{
			NewConstraintOp(OpGE, lowVersion),
			NewConstraintOp(OpLT, highVersion),
		}, 2, nil
	}

	// Hyphen Range
	//
	// Specifies an inclusive set. If a partial version is provided as the first version in the inclusive range,
	// then the missing pieces are replaced with zeroes. If a partial version is provided as the second version in
	// the inclusive range, then all versions that start with the supplied parts of the tuple are accepted, but
	// nothing that would be greater than the provided tuple parts.
	var hyphen hyphenCaps
	if matchHyphen(constraint, &hyphen) {
		from, to := constraint[:hyphen.fromEnd], constraint[hyphen.toStart:hyphen.toEnd]

		// Calculate the stability suffix
		lowStabilitySuffix := stabilitySuffix(&hyphen.from)

		lowVersion, err := p.Normalize(from)
		if err != nil {
			return none, 0, err
		}
		lowerBound := NewConstraintOp(OpGE, lowVersion+lowStabilitySuffix)

		// $empty() is empty() except that "0" is not empty.
		highMatch := hyphen.to.numbers(constraint)
		var upperBound *Constraint
		if (highMatch[2] != "" && highMatch[3] != "") || hyphen.to.mod.word.nonEmpty() ||
			hyphen.to.mod.dev.nonEmpty() || hyphen.to.xDev.nonEmpty() {
			highVersion, err := p.Normalize(to)
			if err != nil {
				return none, 0, err
			}
			upperBound = NewConstraintOp(OpLE, highVersion)
		} else {
			// validate to version
			if _, err := p.Normalize(to); err != nil {
				return none, 0, err
			}

			position := 2
			if highMatch[2] == "" {
				position = 1
			}
			highVersion, _ := manipulateVersionString(highMatch, position, 1)
			upperBound = NewConstraintOp(OpLT, highVersion+"-dev")
		}

		return [2]ConstraintInterface{lowerBound, upperBound}, 2, nil
	}

	// Basic Comparators
	operator, versionString := matchBasicComparator(constraint)
	version, err := p.Normalize(versionString)
	if err != nil {
		// recover from an invalid constraint like foobar-dev which should be dev-foobar
		// except if the constraint uses a known operator, in which case it must be a parse error
		if !strings.HasSuffix(versionString, "-dev") || !matchSimpleDevChars(versionString) {
			return none, 0, &UnexpectedValueError{Message: "Could not parse version constraint " + constraint + ": " + err.Error()}
		}
		version, _ = p.Normalize("dev-" + versionString[:len(versionString)-4])
	}

	op := operator
	if op == "" {
		op = StrOpEQAlt
	}

	if op != StrOpEQ && op != StrOpEQAlt && stabilityModifier != "" && ParseStability(version) == StabilityStable {
		version += "-" + stabilityModifier
	} else if op == StrOpLT || op == StrOpGE {
		if !matchDashModifier(asciiLower(versionString)) && !strings.HasPrefix(versionString, "dev-") {
			version += "-dev"
		}
	}

	c, _ := NewConstraint(op, version)

	return [2]ConstraintInterface{c}, 1, nil
}

// numbers returns $matches[0..4] of a $versionRegex match: "" and the four
// numeric groups ("" when unmatched).
func (c *vrCaps) numbers(s string) [5]string {
	return [5]string{"", c.num[0].text(s), c.num[1].text(s), c.num[2].text(s), c.num[3].text(s)}
}

// stabilitySuffix is the "-dev" added to the lower bound of a range whose
// version has no stability of its own.
func stabilitySuffix(c *vrCaps) string {
	if !c.mod.word.nonEmpty() && !c.mod.dev.nonEmpty() && !c.xDev.nonEmpty() {
		return "-dev"
	}

	return ""
}

// manipulateVersionString ports manipulateVersionString(): it increments,
// decrements, or simply pads a version number. matches[1..4] are the
// version parts; position (1-4) is the part to change. ok is false where
// PHP returns null, on a carry overflow.
func manipulateVersionString(matches [5]string, position, increment int) (string, bool) {
	const pad = "0"
	for i := 4; i > 0; i-- {
		if i > position {
			matches[i] = pad
		} else if i == position && increment != 0 {
			var negative bool
			matches[i], negative = phpAddInt(matches[i], increment)
			// If $matches[$i] was 0, carry the decrement
			if negative {
				matches[i] = pad
				position--

				// Return null on a carry overflow
				if i == 1 {
					return "", false
				}
			}
		}
	}

	return matches[1] + "." + matches[2] + "." + matches[3] + "." + matches[4], true
}

// expandStability ports expandStability(): it expands a shorthand
// stability string to the long version.
func expandStability(stability string) string {
	stability = asciiLower(stability)

	switch stability {
	case "a":
		return StabilityAlpha
	case "b":
		return StabilityBeta
	case "p", "pl":
		return "patch"
	case "rc":
		return StabilityRC
	default:
		return stability
	}
}
