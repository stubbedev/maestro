// Ports src/Composer/FilterList/FilterListEntry.php (an alias of the type
// internal/repository holds) and
// src/Composer/FilterList/FilterListEntryBuilder.php.

// Package filterlist ports Composer\FilterList: the filter lists
// (malware and custom dependency policy lists) repositories and URL
// sources advertise, and the auditing of packages against them.
package filterlist

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
)

// FilterListEntry is Composer\FilterList\FilterListEntry. It lives in
// internal/repository because repositories create entries.
type FilterListEntry = repository.FilterListEntry

// Filter is a filter result: list name => entries, in order.
type Filter = repository.NameMap[[]*FilterListEntry]

// CreateFilterListEntry ports FilterListEntry::create.
func CreateFilterListEntry(listName string, data *php.Array, parser repository.ConstraintParser) (*FilterListEntry, error) {
	return repository.CreateFilterListEntry(listName, data, parser)
}

// FilterListEntryBuilder ports Composer\FilterList\FilterListEntryBuilder:
// it builds the entries of raw JSON data grouped by list name and drops
// those not matching the package constraint map.
type FilterListEntryBuilder struct {
	versionParser repository.ConstraintParser
}

// NewFilterListEntryBuilder ports new FilterListEntryBuilder($versionParser);
// a nil parser is a new VersionParser.
func NewFilterListEntryBuilder(versionParser repository.ConstraintParser) *FilterListEntryBuilder {
	if versionParser == nil {
		versionParser = pkg.NewVersionParser()
	}

	return &FilterListEntryBuilder{versionParser: versionParser}
}

// Build ports build($rawByList, $packageConstraintMap, $defaultPackage):
// rawByList maps list names to lists of raw entries; defaultPackage ("" for
// null) names the package of entries without a "package" field (in
// per-package metadata files, where the package is implicit).
func (b *FilterListEntryBuilder) Build(rawByList *php.Array, packageConstraintMap *repository.ConstraintMap, defaultPackage string) (*Filter, error) {
	result := &Filter{}
	for k, entries := range rawByList.All() {
		list, ok := entries.(*php.Array)
		if !k.IsString() || !ok {
			continue
		}
		listName := k.String()
		for _, raw := range list.All() {
			data, ok := raw.(*php.Array)
			if !ok {
				continue
			}

			if v, _ := data.Get("constraint"); v == nil {
				continue
			}

			if v, _ := data.Get("package"); v == nil {
				if defaultPackage == "" {
					continue
				}

				data = data.Clone()
				data.Set("package", defaultPackage)
			}

			entry, err := repository.CreateFilterListEntry(listName, data, b.versionParser)
			if err != nil {
				return nil, err
			}

			constraint, ok := packageConstraintMap.Get(entry.PackageName)
			if !ok || constraint == nil {
				continue
			}

			if !entry.Constraint.Matches(constraint) {
				continue
			}

			existing, _ := result.Get(listName)
			result.Set(listName, append(existing, entry))
		}
	}

	return result, nil
}
