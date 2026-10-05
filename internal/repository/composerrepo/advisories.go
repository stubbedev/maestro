// Ports src/Composer/Repository/ComposerRepository.php: the security
// advisories API (AdvisoryProviderInterface) and the filter lists
// (FilterListProviderInterface).

package composerrepo

import (
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// HasSecurityAdvisories ports hasSecurityAdvisories.
func (r *ComposerRepository) HasSecurityAdvisories() (bool, error) {
	if _, _, err := r.loadRootServerFile(600); err != nil {
		return false, err
	}

	return r.securityAdvisoryConfig != nil && (r.securityAdvisoryConfig.metadata || r.securityAdvisoryConfig.apiURL != ""), nil
}

// withoutUnavailable drops the names the repository's available-packages
// / available-package-patterns directives leave out.
func (r *ComposerRepository) withoutUnavailable(packageConstraintMap *repository.ConstraintMap) (*repository.ConstraintMap, error) {
	packageConstraintMap = packageConstraintMap.Clone()
	if r.hasAvailablePackageList {
		for name := range packageConstraintMap.Clone().All() {
			contains, err := r.lazyProvidersRepoContains(php.Strtolower(name))
			if err != nil {
				return nil, err
			}
			if !contains {
				packageConstraintMap.Delete(name)
			}
		}
	}

	return packageConstraintMap, nil
}

// metadataNames are the lowercased names of a constraint map the
// metadata files are fetched for: no platform packages, no root package.
func metadataNames(packageConstraintMap *repository.ConstraintMap) []string {
	var names []string
	for name := range packageConstraintMap.All() {
		name = php.Strtolower(name)

		// skip platform packages, root package and composer-plugin-api
		if pkg.IsPlatformPackage(name) || name == "__root__" {
			continue
		}
		names = append(names, name)
	}

	return names
}

// SecurityAdvisories ports getSecurityAdvisories.
func (r *ComposerRepository) SecurityAdvisories(packageConstraintMap *repository.ConstraintMap, allowPartialAdvisories bool) (repository.AdvisoryResult, error) {
	if _, _, err := r.loadRootServerFile(600); err != nil {
		return repository.AdvisoryResult{}, err
	}
	advisories := &repository.NameMap[[]repository.Advisory]{}
	if r.securityAdvisoryConfig == nil {
		return repository.AdvisoryResult{Advisories: advisories}, nil
	}

	namesFound := &repository.NameMap[struct{}]{}
	apiURL := r.securityAdvisoryConfig.apiURL

	// respect available-package-patterns / available-packages directives from the repo
	packageConstraintMap, err := r.withoutUnavailable(packageConstraintMap)
	if err != nil {
		return repository.AdvisoryResult{}, err
	}

	parser := pkg.NewVersionParser()
	create := func(data any, name string) (repository.Advisory, error) {
		dataArray, ok := data.(*php.Array)
		if !ok {
			return nil, &pkg.TypeError{Message: "Composer\\Advisory\\PartialSecurityAdvisory::create(): Argument #2 ($data) must be of type array, " + php.TypeName(data) + " given"}
		}
		advisory, err := repository.CreatePartialSecurityAdvisory(name, dataArray, parser)
		if err != nil {
			return nil, err
		}
		if _, full := advisory.(*repository.SecurityAdvisory); !allowPartialAdvisories && !full {
			return nil, &util.RuntimeError{Site: phperr.At("ComposerRepository.php", 727), Message: "Advisory for " + name + " could not be loaded as a full advisory from " + r.RepoName() + "\n" + php.VarExport(dataArray)}
		}
		constraint, ok := packageConstraintMap.Get(name)
		if !ok {
			return nil, &util.ErrorException{Site: phperr.At("ComposerRepository.php", 729), Message: `Undefined array key "` + name + `"`}
		}
		if constraint == nil {
			return nil, &pkg.TypeError{Message: "Composer\\Semver\\Constraint\\ConstraintInterface::matches(): Argument #1 ($provider) must be of type Composer\\Semver\\Constraint\\ConstraintInterface, null given"}
		}
		if !advisory.Partial().AffectedVersions.Matches(constraint) {
			return nil, nil
		}

		return advisory, nil
	}
	createAll := func(list *php.Array, name string) ([]repository.Advisory, error) {
		var out []repository.Advisory
		for _, data := range list.All() {
			advisory, err := create(data, name)
			if err != nil {
				return nil, err
			}
			if advisory != nil {
				out = append(out, advisory)
			}
		}

		return out, nil
	}

	if r.securityAdvisoryConfig.metadata && (allowPartialAdvisories || apiURL == "") {
		names := metadataNames(packageConstraintMap)
		downloads, err := r.startCachedAsyncDownloads(names, names)
		if err != nil {
			return repository.AdvisoryResult{}, err
		}
		r.waitDownloads(downloads)

		var firstErr error
		for i, d := range downloads {
			name := names[i]
			err := func() error {
				response, _, err := r.finishCachedDownload(d)
				if err != nil {
					return err
				}

				list, ok := get(response, "security-advisories").(*php.Array)
				if !ok {
					return nil
				}

				namesFound.Set(name, struct{}{})
				if list.Len() > 0 {
					created, err := createAll(list, name)
					if err != nil {
						return err
					}
					advisories.Set(name, created)
				}
				packageConstraintMap.Delete(name)

				return nil
			}()
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
		if firstErr != nil {
			return repository.AdvisoryResult{}, firstErr
		}
	}

	if apiURL != "" && packageConstraintMap.Len() > 0 {
		pairs := make([]string, 0, 2*packageConstraintMap.Len())
		for i, name := range packageConstraintMap.Keys() {
			pairs = append(pairs, "packages["+strconv.Itoa(i)+"]", name)
		}
		options := filterlist.PostOptions(r.options, "Content-type: application/x-www-form-urlencoded", php.HTTPBuildQuery(pairs...))

		advisoryData, err := r.getJSON(apiURL, options)
		if err != nil {
			return repository.AdvisoryResult{}, err
		}
		warned := false
		for k, raw := range asArray(get(advisoryData, "advisories")).All() {
			name := k.String()
			if v, _ := packageConstraintMap.Get(name); v == nil {
				if !warned {
					r.io.WriteError("<warning>"+r.RepoName()+" returned names which were not requested in response to the security-advisories API. "+name+" was not requested but is present in the response. Requested names were: "+strings.Join(packageConstraintMap.Keys(), ", ")+"</warning>", true, io.Normal)
					warned = true
				}

				continue
			}
			if list := asArray(raw); list.Len() > 0 {
				created, err := createAll(list, name)
				if err != nil {
					return repository.AdvisoryResult{}, err
				}
				advisories.Set(name, created)
			}
			namesFound.Set(name, struct{}{})
		}
	}

	nonEmpty := &repository.NameMap[[]repository.Advisory]{}
	for name, list := range advisories.All() {
		if len(list) > 0 {
			nonEmpty.Set(name, list)
		}
	}

	return repository.AdvisoryResult{NamesFound: namesFound.Keys(), Advisories: nonEmpty}, nil
}

// waitDownloads waits for the requests of startCachedAsyncDownloads.
func (r *ComposerRepository) waitDownloads(downloads []*cachedDownload) {
	fetches := make([]*asyncFetch, len(downloads))
	for i, d := range downloads {
		fetches[i] = d.fetch
	}
	r.waitFetches(fetches)
}

// HasFilter ports hasFilter.
func (r *ComposerRepository) HasFilter() (bool, error) {
	if r.userFilterDisabled {
		return false, nil
	}

	if _, _, err := r.loadRootServerFile(600); err != nil {
		return false, err
	}

	return r.filterConfig != nil && r.filterConfig.Metadata, nil
}

// Filter ports getFilter: the filter entries of the configured lists for
// the packages.
func (r *ComposerRepository) Filter(packageConstraintMap *repository.ConstraintMap, configuredLists []string) (*repository.NameMap[[]*repository.FilterListEntry], error) {
	if _, _, err := r.loadRootServerFile(600); err != nil {
		return nil, err
	}

	// respect available-package-patterns / available-packages directives from the repo
	packageConstraintMap, err := r.withoutUnavailable(packageConstraintMap)
	if err != nil {
		return nil, err
	}

	// api-url returns the matched filter entries directly: skip the summary + per-package
	// metadata path entirely. As with summary-url below, fall through to the cached metadata
	// path if package metadata was already pulled in this run.
	if r.filterConfig != nil && r.filterConfig.APIURL != "" && len(r.freshMetadataUrls) == 0 {
		response, err := r.filterAPI().PostPurls(r.filterConfig.APIURL, packageConstraintMap, configuredLists)
		if err != nil {
			return nil, err
		}
		decoded, err := response.DecodeJSON()
		if err != nil {
			return nil, err
		}
		if _, err := http.OutputWarnings(r.io, r.url, decoded); err != nil {
			return nil, err
		}
		filter, ok := get(asArrayOrNil(decoded), "filter").(*php.Array)
		if !ok {
			return nil, transportErrorAt(phperr.At("ComposerRepository.php", 847), "Filter api-url "+r.filterConfig.APIURL+" returned an unexpected response for "+r.RepoName(), 0)
		}

		return r.entryBuilder().Build(filter, packageConstraintMap, "")
	}

	// skip the summary fetch if we already pulled package metadata in this run; per-package
	// calls below will short-circuit on the cache, so the summary would be wasted work.
	if r.filterConfig != nil && r.filterConfig.SummaryURL != "" && len(r.freshMetadataUrls) == 0 {
		summary, err := r.loadFilterSummary()
		if err != nil {
			return nil, err
		}

		candidates := map[string]struct{}{}
		for _, listName := range configuredLists {
			packages, ok := summary.Get(listName)
			if !ok {
				continue
			}

			for packageName, summaryConstraint := range packages.All() {
				constraint, ok := packageConstraintMap.Get(packageName)
				if !ok || constraint == nil {
					continue
				}

				if _, matchAll := constraint.(*semver.MatchAllConstraint); !matchAll {
					parsed, err := r.versionParser.ParseConstraints(summaryConstraint)
					if err != nil {
						return nil, err
					}
					if !parsed.Matches(constraint) {
						continue
					}
				}

				candidates[packageName] = struct{}{}
			}
		}
		for name := range packageConstraintMap.Clone().All() {
			if _, ok := candidates[name]; !ok {
				packageConstraintMap.Delete(name)
			}
		}
	}

	entryBuilder := r.entryBuilder()
	filter := &repository.NameMap[[]*repository.FilterListEntry]{}
	names := metadataNames(packageConstraintMap)
	downloads, err := r.startCachedAsyncDownloads(names, names)
	if err != nil {
		return nil, err
	}
	r.waitDownloads(downloads)

	var firstErr error
	for i, d := range downloads {
		err := func() error {
			response, _, err := r.finishCachedDownload(d)
			if err != nil {
				return err
			}

			raw, ok := get(response, "filter").(*php.Array)
			if !ok {
				return nil
			}

			built, err := entryBuilder.Build(raw, packageConstraintMap, names[i])
			if err != nil {
				return err
			}
			for listName, entries := range built.All() {
				existing, _ := filter.Get(listName)
				filter.Set(listName, append(existing, entries...))
			}

			return nil
		}()
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return nil, firstErr
	}

	return filter, nil
}

// filterAPI ports getFilterApiClient.
func (r *ComposerRepository) filterAPI() *filterlist.FilterListApiClient {
	if r.filterAPIClient == nil {
		r.filterAPIClient = filterlist.NewFilterListApiClient(r.httpDownloader, r.options)
	}

	return r.filterAPIClient
}

// entryBuilder ports getFilterEntryBuilder.
func (r *ComposerRepository) entryBuilder() *filterlist.FilterListEntryBuilder {
	if r.filterEntryBuilder == nil {
		r.filterEntryBuilder = filterlist.NewFilterListEntryBuilder(r.versionParser)
	}

	return r.filterEntryBuilder
}

// loadFilterSummary ports loadFilterSummary: list name => package name =>
// constraint.
func (r *ComposerRepository) loadFilterSummary() (*repository.NameMap[*repository.NameMap[string]], error) {
	summary := &repository.NameMap[*repository.NameMap[string]]{}
	if r.filterConfig == nil || r.filterConfig.SummaryURL == "" {
		return summary, nil
	}

	const cacheKey = "filter-summary.json"
	var (
		contents     *php.Array
		lastModified string
	)
	cached, ok, err := r.cache.Read(cacheKey)
	if err != nil {
		return nil, err
	}
	if ok {
		if contents = decodeArray(cached); contents != nil {
			lastModified = php.ToString(get(contents, "last-modified"))
		}
	}

	f, err := r.asyncFetchFile(r.filterConfig.SummaryURL, cacheKey, lastModified)
	if err != nil {
		return nil, err
	}
	r.waitFetches([]*asyncFetch{f})
	res, err := r.finishFetch(f)
	if err != nil {
		return nil, err
	}
	data := res.data
	if res.fresh {
		data = contents
	}

	lists, ok := get(data, "filter").(*php.Array)
	if !ok {
		return nil, transportErrorAt(phperr.At("ComposerRepository.php", 962), "Filter summary URL "+r.filterConfig.SummaryURL+" returned 404 for "+r.RepoName(), 404)
	}

	for k, raw := range lists.All() {
		packages, isArray := raw.(*php.Array)
		if !k.IsString() || !isArray {
			listName := ""
			if k.IsString() {
				listName = k.String()
			}

			return nil, &util.UnexpectedValueError{Site: phperr.At("ComposerRepository.php", 968), Message: "Invalid filter summary received from " + r.RepoName() + `: list "` + listName + `" must map to an object of package => constraint`}
		}
		listName := k.String()

		for packageName, rawConstraint := range packages.All() {
			constraint, isString := rawConstraint.(string)
			if !packageName.IsString() || !isString {
				return nil, &util.UnexpectedValueError{Site: phperr.At("ComposerRepository.php", 973), Message: "Invalid filter summary received from " + r.RepoName() + `: list "` + listName + `" entries must be strings`}
			}
			list, ok := summary.Get(listName)
			if !ok {
				list = &repository.NameMap[string]{}
				summary.Set(listName, list)
			}
			list.Set(php.Strtolower(packageName.String()), constraint)
		}
	}

	return summary, nil
}

// FilterLists ports getFilterLists.
func (r *ComposerRepository) FilterLists() ([]string, error) {
	if r.userFilterDisabled {
		return nil, nil
	}

	if _, _, err := r.loadRootServerFile(600); err != nil {
		return nil, err
	}

	if r.filterConfig == nil {
		return nil, nil
	}

	if len(r.userFilterSkipped) == 0 {
		return r.filterConfig.Lists, nil
	}

	var lists []string
	for _, listName := range r.filterConfig.Lists {
		if _, skipped := r.userFilterSkipped[listName]; !skipped {
			lists = append(lists, listName)
		}
	}

	return lists, nil
}
