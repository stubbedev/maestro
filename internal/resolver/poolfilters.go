// Ports src/Composer/DependencyResolver/SecurityAdvisoryPoolFilter.php and
// FilterListPoolFilter.php.

package resolver

import (
	"slices"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util/http"
)

// SecurityAdvisoryPoolFilter ports
// Composer\DependencyResolver\SecurityAdvisoryPoolFilter: it removes the
// package versions affected by security advisories, and abandoned ones,
// as the policy configures.
type SecurityAdvisoryPoolFilter struct {
	auditor      advisory.Auditor
	policyConfig *policy.PolicyConfig
	io           io.IO
}

// NewSecurityAdvisoryPoolFilter is new SecurityAdvisoryPoolFilter($auditor,
// $policyConfig, $io).
func NewSecurityAdvisoryPoolFilter(auditor advisory.Auditor, policyConfig *policy.PolicyConfig, out io.IO) *SecurityAdvisoryPoolFilter {
	return &SecurityAdvisoryPoolFilter{auditor: auditor, policyConfig: policyConfig, io: out}
}

// Filter ports filter.
func (f *SecurityAdvisoryPoolFilter) Filter(pool *Pool, repositories []repository.RepositoryInterface, request *Request) (*Pool, error) {
	advisories := f.policyConfig.Advisories
	abandoned := f.policyConfig.Abandoned
	if !advisories.Block {
		return pool, nil
	}

	repoSet, err := repository.NewRepositorySet("stable", nil, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	for _, repo := range repositories {
		if err := repoSet.AddRepository(repo); err != nil {
			return nil, err
		}
	}

	var packagesForAdvisories []pkg.PackageInterface
	for _, p := range pool.Packages() {
		if _, isRoot := p.(pkg.RootPackageInterface); !isRoot && !repository.IsPlatformPackage(p.Name()) && !request.IsLockedPackage(p) {
			packagesForAdvisories = append(packagesForAdvisories, p)
		}
	}

	ignoreListForBlocking := advisories.IgnoreListForOperation("block")
	ignoreUnreachableUpdate := f.policyConfig.IgnoreUnreachable.Update
	allAdvisories, err := repoSet.GetMatchingSecurityAdvisories(packagesForAdvisories, true, ignoreUnreachableUpdate)
	if err != nil {
		return nil, err
	}
	if f.auditor.NeedsCompleteAdvisoryLoad(allAdvisories.Advisories, ignoreListForBlocking) {
		if allAdvisories, err = repoSet.GetMatchingSecurityAdvisories(packagesForAdvisories, false, ignoreUnreachableUpdate); err != nil {
			return nil, err
		}
	}

	if ignoreUnreachableUpdate && len(allAdvisories.UnreachableRepos) > 0 {
		f.io.WriteError("<warning>Security advisory data could not be fetched from some repositories (ignored per policy.ignore-unreachable); matches may be incomplete:</warning>", true, io.Normal)
		for _, repo := range allAdvisories.UnreachableRepos {
			f.io.WriteError("  - "+repo, true, io.Normal)
		}
	}

	advisoryMap, _ := f.auditor.ProcessAdvisories(allAdvisories.Advisories, ignoreListForBlocking, advisories.IgnoreSeverityForOperation("block"))
	ignoreAbandonedForBlocking := abandoned.FlatIgnoreForOperation("block")

	var packages []pkg.PackageInterface
	securityRemovedVersions := &repository.NameMap[*repository.NameMap[[]repository.Advisory]]{}
	abandonedRemovedVersions := &repository.NameMap[*VersionMap]{}
	for _, p := range pool.Packages() {
		if abandoned.Block {
			filtered, err := f.auditor.FilterAbandonedPackages([]pkg.PackageInterface{p}, ignoreAbandonedForBlocking)
			if err != nil {
				return nil, err
			}
			if len(filtered) != 0 {
				for _, packageName := range p.Names(false) {
					versions, ok := abandonedRemovedVersions.Get(packageName)
					if !ok {
						versions = &VersionMap{}
						abandonedRemovedVersions.Set(packageName, versions)
					}
					versions.Set(p.Version(), p.PrettyVersion())
				}

				continue
			}
		}

		matchingAdvisories := matchingAdvisories(p, advisoryMap)
		if len(matchingAdvisories) > 0 {
			for _, packageName := range p.Names(false) {
				versions, ok := securityRemovedVersions.Get(packageName)
				if !ok {
					versions = &repository.NameMap[[]repository.Advisory]{}
					securityRemovedVersions.Set(packageName, versions)
				}
				versions.Set(p.Version(), matchingAdvisories)
			}

			continue
		}

		packages = append(packages, p)
	}

	return NewPool(packages, pool.UnacceptableFixedOrLockedPackages(), &Removed{
		Versions:  pool.AllRemovedVersions(),
		ByPackage: pool.AllRemovedVersionsByPackage(),
		Security:  securityRemovedVersions,
		Abandoned: abandonedRemovedVersions,
	}), nil
}

// matchingAdvisories ports getMatchingAdvisories.
func matchingAdvisories(p pkg.PackageInterface, advisoryMap *repository.NameMap[[]repository.Advisory]) []repository.Advisory {
	if p.IsDev() {
		return nil
	}

	var matching []repository.Advisory
	for _, packageName := range p.Names(false) {
		advisories, ok := advisoryMap.Get(packageName)
		if !ok {
			continue
		}

		packageConstraint := semver.NewConstraintOp(semver.OpEQ, p.Version())
		for _, a := range advisories {
			if a.Partial().AffectedVersions.Matches(packageConstraint) {
				matching = append(matching, a)
			}
		}
	}

	return matching
}

// FilterListPoolFilter ports Composer\DependencyResolver\FilterListPoolFilter:
// it removes the package versions the policy's filter lists (malware and
// custom lists) block.
type FilterListPoolFilter struct {
	policyConfig      *policy.PolicyConfig
	filterListAuditor filterlist.FilterListAuditor
	httpDownloader    http.Getter
	blockScope        string
	repositories      []repository.RepositoryInterface
	io                io.IO
}

// NewFilterListPoolFilter is new FilterListPoolFilter($policyConfig,
// $filterListAuditor, $httpDownloader, $blockScope, $repositories, $io).
func NewFilterListPoolFilter(policyConfig *policy.PolicyConfig, filterListAuditor filterlist.FilterListAuditor, httpDownloader http.Getter, blockScope string, repositories []repository.RepositoryInterface, out io.IO) *FilterListPoolFilter {
	return &FilterListPoolFilter{
		policyConfig:      policyConfig,
		filterListAuditor: filterListAuditor,
		httpDownloader:    httpDownloader,
		blockScope:        blockScope,
		repositories:      repositories,
		io:                out,
	}
}

// Filter ports filter.
func (f *FilterListPoolFilter) Filter(pool *Pool, request *Request) (*Pool, error) {
	// During UPDATE scope, packages that are locked are about to be installed
	// and must also be checked against install-scope filter lists (e.g. malware).
	checkLockedAgainstInstall := f.blockScope == policy.BlockScopeUpdate

	configuredScopeListNames := f.policyConfig.ActiveBlockFilterListNames(f.blockScope)
	var installScopeListNames []string
	if checkLockedAgainstInstall {
		installScopeListNames = f.policyConfig.ActiveBlockFilterListNames(policy.BlockScopeInstall)
	}
	unionListNames := uniqueStrings(append(slices.Clone(configuredScopeListNames), installScopeListNames...))
	if len(unionListNames) == 0 {
		return pool, nil
	}

	// One fetch per provider for the union of both scopes' active lists.
	// Conservative ignoreUnreachable: only suppress transport errors when
	// both scopes opt in — otherwise the stricter scope wins.
	ignoreUnreachable := f.policyConfig.IgnoreUnreachable.ForBlockScope(f.blockScope)
	if checkLockedAgainstInstall {
		ignoreUnreachable = ignoreUnreachable && f.policyConfig.IgnoreUnreachable.ForBlockScope(policy.BlockScopeInstall)
	}

	providerSet, err := filterlist.CreateFilterListProviderSet(f.policyConfig, f.repositories, f.httpDownloader)
	if err != nil {
		return nil, err
	}
	var packagesForFilter []pkg.PackageInterface
	for _, p := range pool.Packages() {
		if isFilterable(p) {
			packagesForFilter = append(packagesForFilter, p)
		}
	}
	fetchResult, err := f.filterListAuditor.CollectFilterLists(packagesForFilter, providerSet, unionListNames, ignoreUnreachable)
	if err != nil {
		return nil, err
	}
	unionMap := fetchResult.Filter
	if len(fetchResult.UnreachableRepos) > 0 {
		f.io.WriteError("<warning>Filter list data could not be fetched from some sources (ignored per policy.ignore-unreachable); matches may be incomplete:</warning>", true, io.Normal)
		for _, repo := range fetchResult.UnreachableRepos {
			f.io.WriteError("  - "+repo, true, io.Normal)
		}
	}

	if unionMap.Len() == 0 {
		return pool, nil
	}

	configuredScopeMap := filterMapByListNames(unionMap, configuredScopeListNames)
	installScopeMap := &filterlist.FilterListMap{}
	var lockedNameVersionMap map[string]map[string]bool
	if checkLockedAgainstInstall {
		installScopeMap = filterMapByListNames(unionMap, installScopeListNames)
		if lockedNameVersionMap, err = buildLockedNameVersionMap(request); err != nil {
			return nil, err
		}
	}

	var packages []pkg.PackageInterface
	filterListRemovedVersions := &repository.NameMap[*repository.NameMap[[]*repository.FilterListEntry]]{}
	for _, p := range pool.Packages() {
		if !isFilterable(p) {
			packages = append(packages, p)

			continue
		}

		var matchingEntries []*repository.FilterListEntry
		if checkLockedAgainstInstall && isLockedEquivalent(p, request, lockedNameVersionMap) {
			matchingEntries, err = f.filterListAuditor.GetMatchingBlockEntries(p, installScopeMap, f.policyConfig, policy.BlockScopeInstall)
		} else {
			matchingEntries, err = f.filterListAuditor.GetMatchingBlockEntries(p, configuredScopeMap, f.policyConfig, f.blockScope)
		}
		if err != nil {
			return nil, err
		}

		if len(matchingEntries) > 0 {
			for _, packageName := range p.Names(false) {
				versions, ok := filterListRemovedVersions.Get(packageName)
				if !ok {
					versions = &repository.NameMap[[]*repository.FilterListEntry]{}
					filterListRemovedVersions.Set(packageName, versions)
				}
				versions.Set(p.Version(), matchingEntries)
			}

			continue
		}

		packages = append(packages, p)
	}

	return NewPool(packages, pool.UnacceptableFixedOrLockedPackages(), &Removed{
		Versions:   pool.AllRemovedVersions(),
		ByPackage:  pool.AllRemovedVersionsByPackage(),
		Security:   pool.AllSecurityRemovedPackageVersions(),
		Abandoned:  pool.AllAbandonedRemovedPackageVersions(),
		FilterList: filterListRemovedVersions,
	}), nil
}

// filterMapByListNames ports filterMapByListNames.
func filterMapByListNames(m *filterlist.FilterListMap, listNames []string) *filterlist.FilterListMap {
	filtered := &filterlist.FilterListMap{}
	if len(listNames) == 0 {
		return filtered
	}

	for packageName, entriesByList := range m.All() {
		for listName, entries := range entriesByList.All() {
			if !slices.Contains(listNames, listName) {
				continue
			}
			byList, ok := filtered.Get(packageName)
			if !ok {
				byList = &filterlist.Filter{}
				filtered.Set(packageName, byList)
			}
			byList.Set(listName, entries)
		}
	}

	return filtered
}

// isFilterable ports isFilterable.
func isFilterable(p pkg.PackageInterface) bool {
	_, isRoot := p.(pkg.RootPackageInterface)

	return !isRoot && !repository.IsPlatformPackage(p.Name())
}

// buildLockedNameVersionMap ports buildLockedNameVersionMap.
func buildLockedNameVersionMap(request *Request) (map[string]map[string]bool, error) {
	m := map[string]map[string]bool{}
	if lockedRepository := request.LockedRepository(); lockedRepository != nil {
		packages, err := lockedRepository.Packages()
		if err != nil {
			return nil, err
		}
		for _, lockedPackage := range packages {
			if m[lockedPackage.Name()] == nil {
				m[lockedPackage.Name()] = map[string]bool{}
			}
			m[lockedPackage.Name()][lockedPackage.Version()] = true
		}
	}

	return m, nil
}

// isLockedEquivalent ports isLockedEquivalent.
func isLockedEquivalent(p pkg.PackageInterface, request *Request, lockedNameVersionMap map[string]map[string]bool) bool {
	if request.IsLockedPackage(p) {
		return true
	}

	return lockedNameVersionMap[p.Name()][p.Version()]
}
