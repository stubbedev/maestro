// Ports src/Composer/FilterList/FilterListProvider/FilterListProviderSet.php
// and src/Composer/FilterList/FilterListProvider/UrlSourceFilterListProvider.php.
//
// They stay in this package rather than a FilterListProvider subpackage:
// FilterListAuditor takes the provider set, and the providers use the
// API client and entry builder.

package filterlist

import (
	"errors"
	"slices"

	"github.com/stubbedev/maestro/internal/filterlist/source"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// MatchingFilterLists is FilterListProviderSet::getMatchingFilterLists'
// result.
type MatchingFilterLists struct {
	// Filter maps list names, sorted, to the matching entries.
	Filter           *Filter
	UnreachableRepos []string
}

// ProviderSet is what FilterListAuditor and the Auditor use of a
// FilterListProviderSet; *FilterListProviderSet implements it.
type ProviderSet interface {
	GetMatchingFilterLists(packages []pkg.PackageInterface, configuredLists []string, ignoreUnreachable bool) (MatchingFilterLists, error)
}

// FilterListProviderSet ports
// Composer\FilterList\FilterListProvider\FilterListProviderSet.
type FilterListProviderSet struct {
	providers                 []repository.FilterListProvider
	unreachableRepoExceptions []*util.TransportError
}

var _ ProviderSet = (*FilterListProviderSet)(nil)

// NewFilterListProviderSet ports new FilterListProviderSet($repositories,
// $sources): the sources, then the repositories that have a filter. A
// repository whose hasFilter() fails with a TransportException is
// remembered as unreachable; other errors are returned.
func NewFilterListProviderSet(repositories []repository.RepositoryInterface, sources []repository.FilterListProvider) (*FilterListProviderSet, error) {
	providers := slices.Clone(sources)
	var unreachableRepoExceptions []*util.TransportError
	for _, repo := range repositories {
		provider, ok := repo.(repository.FilterListProvider)
		if !ok {
			continue
		}
		has, err := provider.HasFilter()
		if err != nil {
			var transport *util.TransportError
			if !errors.As(err, &transport) {
				return nil, err
			}
			unreachableRepoExceptions = append(unreachableRepoExceptions, transport)

			continue
		}
		if has {
			providers = append(providers, provider)
		}
	}

	return &FilterListProviderSet{providers: providers, unreachableRepoExceptions: unreachableRepoExceptions}, nil
}

// CreateFilterListProviderSet ports FilterListProviderSet::create: the
// URL sources of the custom lists, then the repositories.
func CreateFilterListProviderSet(config *policy.PolicyConfig, repositories []repository.RepositoryInterface, httpDownloader http.Getter) (*FilterListProviderSet, error) {
	var sources []repository.FilterListProvider
	for _, listConfig := range config.CustomListsWithSources().All() {
		for _, s := range listConfig.Sources {
			sources = append(sources, NewUrlSourceFilterListProvider(httpDownloader, s))
		}
	}

	return NewFilterListProviderSet(repositories, sources)
}

// GetMatchingFilterLists ports getMatchingFilterLists: the entries of the
// configured lists matching the versions of the packages (root aliases
// left out).
func (s *FilterListProviderSet) GetMatchingFilterLists(packages []pkg.PackageInterface, configuredLists []string, ignoreUnreachable bool) (MatchingFilterLists, error) {
	var unreachableRepos []string
	filters, err := s.filterListEntriesForConstraints(repository.PackageVersionsConstraintMap(packages), configuredLists, ignoreUnreachable, &unreachableRepos)
	if err != nil {
		return MatchingFilterLists{}, err
	}

	return MatchingFilterLists{Filter: filters, UnreachableRepos: unreachableRepos}, nil
}

func (s *FilterListProviderSet) filterListEntriesForConstraints(packageConstraintMap *repository.ConstraintMap, configuredLists []string, ignoreUnreachable bool, unreachableRepos *[]string) (*Filter, error) {
	for _, e := range s.unreachableRepoExceptions {
		if !ignoreUnreachable {
			return nil, e
		}

		*unreachableRepos = append(*unreachableRepos, e.Error())
	}

	filters := &Filter{}
	for _, provider := range s.providers {
		providerLists, err := provider.FilterLists()
		if err != nil {
			return nil, err
		}
		var relevantLists []string
		for _, name := range configuredLists {
			if slices.Contains(providerLists, name) {
				relevantLists = append(relevantLists, name)
			}
		}
		if len(relevantLists) == 0 {
			continue
		}

		repoFilter, err := provider.Filter(packageConstraintMap, relevantLists)
		if err != nil {
			var transport *util.TransportError
			if !ignoreUnreachable || !errors.As(err, &transport) {
				return nil, err
			}
			*unreachableRepos = append(*unreachableRepos, transport.Error())

			continue
		}

		for listName, entries := range repoFilter.All() {
			if !slices.Contains(configuredLists, listName) || !slices.Contains(providerLists, listName) {
				continue
			}

			for _, entry := range entries {
				constraint, ok := packageConstraintMap.Get(entry.PackageName)
				if !ok || constraint == nil {
					continue
				}

				if !entry.Constraint.Matches(constraint) {
					continue
				}

				list, _ := filters.Get(listName)
				filters.Set(listName, append(list, entry))
			}
		}
	}

	return ksort(filters), nil
}

// UrlSourceFilterListProvider ports
// Composer\FilterList\FilterListProvider\UrlSourceFilterListProvider: a
// filter list served by a URL source of a custom policy list.
type UrlSourceFilterListProvider struct {
	apiClient    *FilterListApiClient
	entryBuilder *FilterListEntryBuilder
	source       *source.URLSource
}

var _ repository.FilterListProvider = (*UrlSourceFilterListProvider)(nil)

// NewUrlSourceFilterListProvider ports new
// UrlSourceFilterListProvider($httpDownloader, $source).
func NewUrlSourceFilterListProvider(httpDownloader http.Getter, src *source.URLSource) *UrlSourceFilterListProvider {
	return &UrlSourceFilterListProvider{
		apiClient:    NewFilterListApiClient(httpDownloader, nil),
		entryBuilder: NewFilterListEntryBuilder(nil),
		source:       src,
	}
}

// HasFilter ports hasFilter.
func (p *UrlSourceFilterListProvider) HasFilter() (bool, error) { return true, nil }

// Filter ports getFilter.
func (p *UrlSourceFilterListProvider) Filter(packageConstraintMap *repository.ConstraintMap, _ []string) (*Filter, error) {
	response, err := p.apiClient.PostPurls(p.source.URL, packageConstraintMap, []string{p.source.ListName})
	if err != nil {
		return nil, err
	}

	decoded, err := response.DecodeJSON()
	if err != nil {
		return nil, err
	}
	var entries any = php.NewArray()
	if d, ok := decoded.(*php.Array); ok {
		if f, ok := d.GetArray("filter"); ok {
			entries = f
		}
	}

	// The remote returns a flat list of entries; this provider is bound to a single list name.
	rawByList := php.ArrayOf(p.source.ListName, entries)

	return p.entryBuilder.Build(rawByList, packageConstraintMap, "")
}

// FilterLists ports getFilterLists.
func (p *UrlSourceFilterListProvider) FilterLists() ([]string, error) {
	return []string{p.source.ListName}, nil
}
