// Ports src/Composer/FilterList/FilterListAuditor.php.

package filterlist

import (
	"slices"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

// FilterListMap is array<string, array<string, list<FilterListEntry>>>:
// package name => list name => entries.
type FilterListMap = repository.NameMap[*Filter]

// CollectedFilterLists is FilterListAuditor::collectFilterLists' result.
type CollectedFilterLists struct {
	// Filter maps package names, sorted, to their entries by list.
	Filter           *FilterListMap
	UnreachableRepos []string
}

// FilterListAuditor ports Composer\FilterList\FilterListAuditor.
type FilterListAuditor struct{}

// CollectFilterLists ports collectFilterLists: the entries matching the
// packages from the providers, grouped by package and list.
func (FilterListAuditor) CollectFilterLists(packages []pkg.PackageInterface, providerSet ProviderSet, configuredLists []string, ignoreUnreachable bool) (CollectedFilterLists, error) {
	result, err := providerSet.GetMatchingFilterLists(packages, configuredLists, ignoreUnreachable)
	if err != nil {
		return CollectedFilterLists{}, err
	}

	filterListMap := &FilterListMap{}
	for _, entries := range result.Filter.All() {
		for _, entry := range entries {
			byList, ok := filterListMap.Get(entry.PackageName)
			if !ok {
				byList = &Filter{}
				filterListMap.Set(entry.PackageName, byList)
			}
			list, _ := byList.Get(entry.ListName)
			byList.Set(entry.ListName, append(list, entry))
		}
	}

	return CollectedFilterLists{Filter: ksort(filterListMap), UnreachableRepos: result.UnreachableRepos}, nil
}

// ksort returns m with its keys sorted as PHP's ksort() sorts them.
func ksort[V any](m *repository.NameMap[V]) *repository.NameMap[V] {
	keys := m.Keys()
	php.SortSlice(keys, func(a, b string) int { return php.Compare(php.StrKey(a).Value(), php.StrKey(b).Value()) })
	sorted := &repository.NameMap[V]{}
	for _, k := range keys {
		v, _ := m.Get(k)
		sorted.Set(k, v)
	}

	return sorted
}

// GetMatchingAuditEntries ports getMatchingAuditEntries. The error is the
// PcreException Preg::isMatch throws on the ignore patterns.
func (a FilterListAuditor) GetMatchingAuditEntries(p pkg.PackageInterface, filterListMap *FilterListMap, policyConfig *policy.PolicyConfig) ([]*FilterListEntry, error) {
	return a.matchingEntries(p, filterListMap, policyConfig.ActiveAuditFilterLists(), "audit")
}

// GetMatchingBlockEntries ports getMatchingBlockEntries; blockScope is
// one of the policy.BlockScope* values.
func (a FilterListAuditor) GetMatchingBlockEntries(p pkg.PackageInterface, filterListMap *FilterListMap, policyConfig *policy.PolicyConfig, blockScope string) ([]*FilterListEntry, error) {
	return a.matchingEntries(p, filterListMap, policyConfig.ActiveBlockFilterLists(blockScope), "block")
}

func (a FilterListAuditor) matchingEntries(p pkg.PackageInterface, filterListMap *FilterListMap, activeListConfigs *policy.OrderedMap[policy.ListPolicyConfig], operation string) ([]*FilterListEntry, error) {
	if _, ok := p.(pkg.RootPackageInterface); ok || filterListMap.Len() == 0 {
		return nil, nil
	}

	if malware, ok := activeListConfigs.Get(policy.MalwareName); ok {
		if malwareConfig, ok := malware.(*policy.MalwarePolicyConfig); ok {
			filterListMap = applyMalwareIgnoreSource(filterListMap, malwareConfig)
		}
	}

	var matchingEntries []*FilterListEntry
	var allPackageNames []string
	for _, activeListConfig := range activeListConfigs.All() {
		allPackageNames = append(allPackageNames, activeListConfig.List().IgnoreForOperation(operation).Keys()...)
	}
	allIgnoredPackageNamesRegex := pkg.PackageNamesToRegexp(allPackageNames, "{^(?:%s)$}iD")
	packageConstraint := semver.NewConstraintOp(semver.OpEQ, p.Version())

	for _, packageName := range p.Names(false) {
		byList, ok := filterListMap.Get(packageName)
		if !ok {
			continue
		}

		// array_intersect_key($filterListMap[$packageName], $activeListConfigs)
		type listEntries struct {
			name    string
			entries []*FilterListEntry
		}
		var packageEntries []listEntries
		for listName, entries := range byList.All() {
			if activeListConfigs.Has(listName) {
				packageEntries = append(packageEntries, listEntries{listName, entries})
			}
		}

		ignored, err := php.PregIsMatch(allIgnoredPackageNamesRegex, packageName)
		if err != nil {
			return nil, err
		}
		if ignored {
			var ignoreErr error
			packageEntries = slices.DeleteFunc(packageEntries, func(e listEntries) bool {
				if ignoreErr != nil {
					return false
				}
				listConfig, _ := activeListConfigs.Get(e.name)
				var isIgnored bool
				isIgnored, ignoreErr = isPackageIgnored(packageName, packageConstraint, listConfig, operation)

				return isIgnored
			})
			if ignoreErr != nil {
				return nil, ignoreErr
			}
		}

		for _, e := range packageEntries {
			for _, entry := range e.entries {
				if entry.Constraint.Matches(packageConstraint) {
					matchingEntries = append(matchingEntries, entry)
				}
			}
		}
	}

	return matchingEntries, nil
}

func isPackageIgnored(packageName string, packageConstraint semver.ConstraintInterface, listConfig policy.ListPolicyConfig, operation string) (bool, error) {
	for _, ignorePackageRules := range listConfig.List().IgnoreForOperation(operation).All() {
		for _, ignorePackageRule := range ignorePackageRules {
			matched, err := php.PregIsMatch(ignorePackageRule.PackageNameRegex, packageName)
			if err != nil {
				return false, err
			}
			if matched && ignorePackageRule.Constraint.Matches(packageConstraint) {
				return true, nil
			}
		}
	}

	return false, nil
}

// applyMalwareIgnoreSource ports applyMalwareIgnoreSource: it drops the
// malware-list entries whose source is listed in
// policy.malware.ignore-source. filterListMap is not modified.
func applyMalwareIgnoreSource(filterListMap *FilterListMap, malwarePolicyConfig *policy.MalwarePolicyConfig) *FilterListMap {
	ignoreSource := malwarePolicyConfig.IgnoreSource
	if len(ignoreSource) == 0 {
		return filterListMap
	}

	result := filterListMap.Clone()
	for packageName, entries := range filterListMap.All() {
		malwareEntries, ok := entries.Get(policy.MalwareName)
		if !ok {
			continue
		}

		var packageEntries []*FilterListEntry
		for _, malwareEntry := range malwareEntries {
			if !malwareEntry.Source.Valid || !slices.Contains(ignoreSource, malwareEntry.Source.S) {
				packageEntries = append(packageEntries, malwareEntry)
			}
		}

		byList := entries.Clone()
		if len(packageEntries) > 0 {
			byList.Set(policy.MalwareName, packageEntries)
			result.Set(packageName, byList)

			continue
		}

		byList.Delete(policy.MalwareName)
		if byList.Len() == 0 {
			result.Delete(packageName)
		} else {
			result.Set(packageName, byList)
		}
	}

	return result
}
