// Package filter ports Composer\Filter\PlatformRequirementFilter: the filters
// deciding which platform requirements --ignore-platform-req(s) ignores.
//
// The interfaces the consumers check for (`instanceof`) are in
// internal/pkg/version, below the resolver and the autoload generator:
// version.PlatformRequirementFilter, version.IgnoreAllPlatformRequirementFilter
// and version.IgnoreListPlatformRequirementFilter.
package filter

// Ports src/Composer/Filter/PlatformRequirementFilter/*.php.

import (
	"fmt"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// PlatformRequirementFilter is PlatformRequirementFilterInterface.
type PlatformRequirementFilter interface {
	version.PlatformRequirementFilter
	IsUpperBoundIgnored(req string) bool
}

// IgnoreAll is IgnoreAllPlatformRequirementFilter.
type IgnoreAll struct{}

// IsIgnored ports isIgnored.
func (IgnoreAll) IsIgnored(req string) bool { return pkg.IsPlatformPackage(req) }

// IsUpperBoundIgnored ports isUpperBoundIgnored.
func (f IgnoreAll) IsUpperBoundIgnored(req string) bool { return f.IsIgnored(req) }

// IgnoresAllPlatformRequirements marks the class for `instanceof` checks
// (version.IgnoreAllPlatformRequirementFilter).
func (IgnoreAll) IgnoresAllPlatformRequirements() {}

// IgnoreNothing is IgnoreNothingPlatformRequirementFilter.
type IgnoreNothing struct{}

// IsIgnored ports isIgnored.
func (IgnoreNothing) IsIgnored(string) bool { return false }

// IsUpperBoundIgnored ports isUpperBoundIgnored.
func (IgnoreNothing) IsUpperBoundIgnored(string) bool { return false }

// IgnoreList is IgnoreListPlatformRequirementFilter.
type IgnoreList struct {
	ignoreRegex           *php.Regexp
	ignoreUpperBoundRegex *php.Regexp
}

// NewIgnoreList ports new IgnoreListPlatformRequirementFilter($reqList):
// entries ending in "+" only ignore the upper bound of the requirement.
func NewIgnoreList(reqList []string) *IgnoreList {
	var ignoreAll, ignoreUpperBound []string
	for _, req := range reqList {
		if req != "" && req[len(req)-1] == '+' {
			ignoreUpperBound = append(ignoreUpperBound, req[:len(req)-1])
		} else {
			ignoreAll = append(ignoreAll, req)
		}
	}

	return &IgnoreList{
		ignoreRegex:           php.MustCompile(pkg.PackageNamesToRegexp(ignoreAll, "{^(?:%s)$}iD")),
		ignoreUpperBoundRegex: php.MustCompile(pkg.PackageNamesToRegexp(ignoreUpperBound, "{^(?:%s)$}iD")),
	}
}

// isMatch is Preg::isMatch on a pattern built from preg_quote'd names, which
// cannot hit the engine's limits.
func isMatch(re *php.Regexp, subject string) bool {
	ok, err := re.IsMatch(subject)

	return err == nil && ok
}

// IsIgnored ports isIgnored.
func (f *IgnoreList) IsIgnored(req string) bool {
	if !pkg.IsPlatformPackage(req) {
		return false
	}

	return isMatch(f.ignoreRegex, req)
}

// IsUpperBoundIgnored ports isUpperBoundIgnored.
func (f *IgnoreList) IsUpperBoundIgnored(req string) bool {
	if !pkg.IsPlatformPackage(req) {
		return false
	}

	return f.IsIgnored(req) || isMatch(f.ignoreUpperBoundRegex, req)
}

// FilterConstraint ports filterConstraint($req, $constraint) (with
// $allowUpperBoundOverride true; with false it returns the constraint
// unchanged): an ignored upper bound is replaced by ">= its end".
func (f *IgnoreList) FilterConstraint(req string, constraint semver.ConstraintInterface) semver.ConstraintInterface {
	if !pkg.IsPlatformPackage(req) {
		return constraint
	}

	if !isMatch(f.ignoreUpperBoundRegex, req) {
		return constraint
	}

	if isMatch(f.ignoreRegex, req) {
		return semver.NewMatchAllConstraint()
	}

	intervals := semver.Intervals.Get(constraint)
	if n := len(intervals.Numeric); n > 0 {
		last := intervals.Numeric[n-1]
		if last.End().String() != semver.IntervalUntilPositiveInfinity().String() {
			multi, err := semver.NewMultiConstraint([]semver.ConstraintInterface{constraint, semver.NewConstraintOp(semver.OpGE, last.End().Version())}, false)
			if err == nil {
				constraint = multi
			}
		}
	}

	return constraint
}

// FromBoolOrList ports PlatformRequirementFilterFactory::fromBoolOrList:
// true ignores all platform requirements, false none, a []string those
// listed.
func FromBoolOrList(boolOrList any) (PlatformRequirementFilter, error) {
	switch v := boolOrList.(type) {
	case bool:
		if v {
			return IgnoreAll{}, nil
		}

		return IgnoreNothing{}, nil
	case []string:
		return NewIgnoreList(v), nil
	case *php.Array:
		list := make([]string, 0, v.Len())
		for _, item := range v.Values() {
			list = append(list, php.ToString(item))
		}

		return NewIgnoreList(list), nil
	}

	return nil, &util.InvalidArgumentError{Message: fmt.Sprintf("PlatformRequirementFilter: Unknown $boolOrList parameter %s. Please report at https://github.com/composer/composer/issues/new.", php.TypeName(boolOrList))}
}

// FromIgnoreOptions is the value Composer's commands pass to fromBoolOrList:
// `true === $input->getOption('ignore-platform-reqs') ?: ($input->getOption('ignore-platform-req') ?: false)`.
func FromIgnoreOptions(ignoreAll bool, ignoreList []string) PlatformRequirementFilter {
	if ignoreAll {
		return IgnoreAll{}
	}
	if len(ignoreList) > 0 {
		return NewIgnoreList(ignoreList)
	}

	return IgnoreNothing{}
}

// IgnoreAllFilter ports PlatformRequirementFilterFactory::ignoreAll.
func IgnoreAllFilter() PlatformRequirementFilter { return IgnoreAll{} }

// IgnoreNothingFilter ports PlatformRequirementFilterFactory::ignoreNothing.
func IgnoreNothingFilter() PlatformRequirementFilter { return IgnoreNothing{} }
