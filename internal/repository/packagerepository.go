// Ports src/Composer/Repository/PackageRepository.php.

package repository

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util"
)

// PackageRepository ports Composer\Repository\PackageRepository: packages
// defined inline in a "package" repository, with optional
// "security-advisories" and "filter" data.
type PackageRepository struct {
	ArrayRepository
	config             []any
	securityAdvisories *php.Array
	filter             *php.Array
}

var (
	_ AdvisoryProvider   = (*PackageRepository)(nil)
	_ FilterListProvider = (*PackageRepository)(nil)
)

// NewPackageRepository ports new PackageRepository($config).
func NewPackageRepository(config *php.Array) (*PackageRepository, error) {
	r := &PackageRepository{}
	r.bind(r, r)

	packageConfig, _ := config.Get("package")
	// make sure we have an array of package definitions
	if definitions, ok := packageConfig.(*php.Array); ok {
		if key, _, ok := definitions.First(); ok && php.IsNumeric(key.Value()) {
			r.config = definitions.Values()
		} else {
			r.config = []any{definitions}
		}
	} else {
		r.config = []any{packageConfig}
	}

	r.securityAdvisories = arrayOrEmpty(config, "security-advisories")
	r.filter = arrayOrEmpty(config, "filter")

	return r, nil
}

// arrayOrEmpty is $config[$key] ?? [] for an array setting.
func arrayOrEmpty(config *php.Array, key string) *php.Array {
	if a, ok := config.GetArray(key); ok {
		return a
	}

	return php.NewArray()
}

// Class returns the PHP class name.
func (r *PackageRepository) Class() string { return `Composer\Repository\PackageRepository` }

// RepoName ports PackageRepository::getRepoName.
func (r *PackageRepository) RepoName() string {
	name := r.ArrayRepository.RepoName()
	if rest, ok := strings.CutPrefix(name, "array "); ok {
		return "package " + rest
	}

	return name
}

// initialize ports PackageRepository::initialize: the package definitions
// are validated and loaded.
func (r *PackageRepository) initialize() error {
	r.baseInitialize()

	validating := loader.NewValidatingArrayLoader(loader.NewArrayLoader(nil, true), nil, 0)
	for _, definition := range r.config {
		var p pkg.PackageInterface
		data, ok := definition.(*php.Array)
		var err error
		if ok {
			p, err = validating.Load(data, pkg.ClassCompletePackage)
		} else {
			err = pkg.ArgumentTypeError(`Composer\Package\Loader\ValidatingArrayLoader::load`, 1, "config", "array", definition).
				Called(`Composer\Package\Loader\ValidatingArrayLoader->load`, phperr.At("ValidatingArrayLoader.php", 67), "PackageRepository.php", 68)
		}
		if err != nil && isPHPError(err) {
			// catch (\Exception) does not catch PHP's \Error classes
			return err
		}
		if err != nil {
			encoded, _ := php.JSONEncode(definition, 0)

			return &InvalidRepositoryError{Site: phperr.At("PackageRepository.php", 70), Message: "A repository of type \"package\" contains an invalid package definition: " + err.Error() + "\n\nInvalid package definition:\n" + encoded}
		}

		if err := r.hooks.addPackage(p); err != nil {
			return err
		}
	}

	return nil
}

// HasSecurityAdvisories ports PackageRepository::hasSecurityAdvisories.
func (r *PackageRepository) HasSecurityAdvisories() (bool, error) {
	return r.securityAdvisories.Len() > 0, nil
}

// SecurityAdvisories ports PackageRepository::getSecurityAdvisories.
func (r *PackageRepository) SecurityAdvisories(packageConstraintMap *ConstraintMap, allowPartial bool) (AdvisoryResult, error) {
	parser := pkg.NewVersionParser()

	advisories := &NameMap[[]Advisory]{}
	var namesFound []string
	for key, packageAdvisories := range r.securityAdvisories.All() {
		packageName := key.String()
		constraint, _ := packageConstraintMap.Get(packageName)
		if constraint == nil {
			continue
		}
		list, ok := packageAdvisories.(*php.Array)
		if !ok {
			return AdvisoryResult{}, pkg.ArgumentTypeError("array_map", 2, "array", "array", packageAdvisories).
				Raised("array_map", "PackageRepository.php", 94)
		}
		var matching []Advisory
		for _, data := range list.All() {
			advisoryData, ok := data.(*php.Array)
			if !ok {
				// the closure array_map() calls: no "called in", and
				// the closure's line is the site
				return AdvisoryResult{}, pkg.ArgumentTypeError(`Composer\Repository\PackageRepository::{closure:Composer\Repository\PackageRepository::getSecurityAdvisories():94}`, 1, "data", "array", data).
					Raised("", "PackageRepository.php", 94)
			}
			advisory, err := CreatePartialSecurityAdvisory(packageName, advisoryData, parser)
			if err != nil {
				return AdvisoryResult{}, err
			}
			if _, full := advisory.(*SecurityAdvisory); !allowPartial && !full {
				return AdvisoryResult{}, &util.RuntimeError{Site: phperr.At("PackageRepository.php", 97), Message: "Advisory for " + packageName + " could not be loaded as a full advisory from " + r.RepoName() + php.EOL + php.VarExport(advisoryData)}
			}

			if advisory.Partial().AffectedVersions.Matches(constraint) {
				matching = append(matching, advisory)
			}
		}
		namesFound = append(namesFound, packageName)
		if len(matching) > 0 {
			advisories.Set(packageName, matching)
		}
	}

	return AdvisoryResult{NamesFound: namesFound, Advisories: advisories}, nil
}

// HasFilter ports PackageRepository::hasFilter.
func (r *PackageRepository) HasFilter() (bool, error) { return r.filter.Len() > 0, nil }

// Filter ports PackageRepository::getFilter.
func (r *PackageRepository) Filter(packageConstraintMap *ConstraintMap, _ []string) (*NameMap[[]*FilterListEntry], error) {
	parser := pkg.NewVersionParser()

	filter := &NameMap[[]*FilterListEntry]{}
	for key, listEntries := range r.filter.All() {
		listName := key.String()
		entries, ok := listEntries.(*php.Array)
		if !ok {
			return nil, &util.ErrorException{Site: phperr.At("PackageRepository.php", 123), Message: "foreach() argument must be of type array|object, " + php.TypeName(listEntries) + " given"}
		}
		for _, data := range entries.All() {
			entryData, ok := data.(*php.Array)
			if !ok {
				return nil, pkg.ArgumentTypeError(`Composer\FilterList\FilterListEntry::create`, 2, "data", "array", data).
					Called(`Composer\FilterList\FilterListEntry::create`, phperr.At("FilterListEntry.php", 80), "PackageRepository.php", 124)
			}
			entry, err := CreateFilterListEntry(listName, entryData, parser)
			if err != nil {
				return nil, err
			}
			if c, _ := packageConstraintMap.Get(entry.PackageName); c != nil {
				list, _ := filter.Get(listName)
				filter.Set(listName, append(list, entry))
			}
		}
	}

	return filter, nil
}

// FilterLists ports PackageRepository::getFilterLists.
func (r *PackageRepository) FilterLists() ([]string, error) {
	lists := make([]string, 0, r.filter.Len())
	for key := range r.filter.All() {
		lists = append(lists, key.String())
	}

	return lists, nil
}
