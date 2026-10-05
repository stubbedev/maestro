// Package semver ports composer/semver 3.4.4: version normalization and
// constraint parsing (VersionParser), the constraint types, Intervals,
// Comparator, Semver and CompilingMatcher, with every string form and
// error message as PHP produces them. It also owns PHP's version_compare()
// (VersionCompare, VersionCompareOp), which other packages use.
//
// The PCRE patterns of VersionParser are hand-written matchers (regex.go)
// that reproduce PCRE's backtracking order and captures; the tests check
// them against regexp2 compilations of the original patterns and against
// goldens recorded from the PHP implementation (tools/oracle/semver).
//
// Static PHP classes are package variables whose methods mirror the static
// methods: Intervals.CompactConstraint, CompilingMatcher.Match, ...
// Their memoization caches are safe for concurrent use.
package semver
