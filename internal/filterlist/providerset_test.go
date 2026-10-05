// Ports tests/Composer/Test/FilterList/FilterListProvider/FilterListProviderSetTest.php
// (MIT, testdata/LICENSE-composer).

package filterlist_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

func TestFilterListProviderSet_GetMatchingFilterListsOnlyReturnsConfiguredLists(t *testing.T) {
	repo, err := repository.NewPackageRepository(php.ArrayOf(
		"package", php.NewArray(),
		"filter", php.ArrayOf(
			"malware", php.ListOf(php.ArrayOf("package", "acme/package", "constraint", "1.0", "reason", "malware")),
			"typosquatting", php.ListOf(php.ArrayOf("package", "acme/package", "constraint", "1.0", "reason", "typosquatting")),
		),
	))
	if err != nil {
		t.Fatal(err)
	}

	set, err := filterlist.NewFilterListProviderSet([]repository.RepositoryInterface{repo}, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := set.GetMatchingFilterLists([]pkg.PackageInterface{pkg.NewPackage("acme/package", "1.0.0.0", "1.0")}, []string{"malware"}, false)
	if err != nil {
		t.Fatal(err)
	}

	if result.Filter.Has("typosquatting") {
		t.Error("typosquatting present")
	}
	malware, _ := result.Filter.Get("malware")
	if len(malware) != 1 || malware[0].Reason.S != "malware" {
		t.Errorf("malware = %+v", malware)
	}
}

// unreachableProvider is the anonymous ArrayRepository subclass whose
// hasFilter() throws a TransportException.
type unreachableProvider struct {
	*repository.ArrayRepository
	message string
}

func (r *unreachableProvider) HasFilter() (bool, error) {
	return false, util.NewTransportError(r.message, 0)
}

func (r *unreachableProvider) Filter(*repository.ConstraintMap, []string) (*filterlist.Filter, error) {
	return &filterlist.Filter{}, nil
}

func (r *unreachableProvider) FilterLists() ([]string, error) { return nil, nil }

func createUnreachableFilterListProvider(t *testing.T, message string) *unreachableProvider {
	t.Helper()

	repo, err := repository.NewArrayRepository(nil)
	if err != nil {
		t.Fatal(err)
	}

	return &unreachableProvider{ArrayRepository: repo, message: message}
}

func TestFilterListProviderSet_UnreachableRepositoryDuringConstructionIsReportedWhenIgnored(t *testing.T) {
	repo := createUnreachableFilterListProvider(t, "repo.example.com could not be reached")

	set, err := filterlist.NewFilterListProviderSet([]repository.RepositoryInterface{repo}, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := set.GetMatchingFilterLists([]pkg.PackageInterface{pkg.NewPackage("acme/package", "1.0.0.0", "1.0")}, []string{"malware"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Filter.Len() != 0 {
		t.Errorf("filter = %v", result.Filter.Keys())
	}
	if !slices.Equal(result.UnreachableRepos, []string{"repo.example.com could not be reached"}) {
		t.Errorf("unreachableRepos = %q", result.UnreachableRepos)
	}
}

func TestFilterListProviderSet_UnreachableRepositoryDuringConstructionIsThrownWhenNotIgnored(t *testing.T) {
	repo := createUnreachableFilterListProvider(t, "repo.example.com could not be reached")

	set, err := filterlist.NewFilterListProviderSet([]repository.RepositoryInterface{repo}, nil)
	if err != nil {
		t.Fatal(err)
	}

	_, err = set.GetMatchingFilterLists([]pkg.PackageInterface{pkg.NewPackage("acme/package", "1.0.0.0", "1.0")}, []string{"malware"}, false)
	var transport *util.TransportError
	if !errors.As(err, &transport) || transport.Message != "repo.example.com could not be reached" {
		t.Errorf("err = %v", err)
	}
}
