package plugin

// The internals emulation of docs/PLUGINS.md §5.12 beyond the surveyed
// plugins' needs (issue #2): what a Pool from maestro holds, asynchronous
// processes of PHP code on maestro's loop, Composer's call stack in the
// exceptions crossing between maestro and PHP, and the frames
// debug_backtrace() shows.

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/semver"
)

// A Pool from maestro holds what Composer's PoolBuilder passes to new
// Pool(): the optimizer's removals by name and by kept package, and the
// security advisories' and filter lists' removals with Composer's objects,
// one per advisory or entry however many versions it removed.
func TestInternals_PoolRemovals(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	kept := loadPackage(t, `{"name": "a/a", "version": "1.2.0"}`)
	other := loadPackage(t, `{"name": "b/b", "version": "2.0.0"}`)
	constraint := func(s string) semver.ConstraintInterface {
		c, err := semver.VersionParser{}.ParseConstraints(s)
		if err != nil {
			t.Fatal(err)
		}

		return c
	}

	versions := func(pairs ...string) *resolver.VersionMap {
		m := &resolver.VersionMap{}
		for i := 0; i < len(pairs); i += 2 {
			m.Set(pairs[i], pairs[i+1])
		}

		return m
	}
	removed := &repository.NameMap[*resolver.VersionMap]{}
	removed.Set("a/a", versions("1.1.0.0", "1.1.0", "1.0.0.0", "1.0.0"))

	adv := &repository.PartialSecurityAdvisory{AdvisoryID: "PKSA-1", PackageName: "c/c", AffectedVersions: constraint("<2")}
	security := &repository.NameMap[*repository.NameMap[[]repository.Advisory]]{}
	byVersion := &repository.NameMap[[]repository.Advisory]{}
	byVersion.Set("1.0.0.0", []repository.Advisory{adv})
	byVersion.Set("1.5.0.0", []repository.Advisory{adv})
	security.Set("c/c", byVersion)

	entry := &repository.FilterListEntry{
		PackageName: "d/d", ListName: "malware", Constraint: constraint("*"),
		URL: pkg.NullString{S: "https://example.org/d", Valid: true}, Source: pkg.NullString{S: "acme", Valid: true},
	}
	filterList := &repository.NameMap[*repository.NameMap[[]*repository.FilterListEntry]]{}
	entries := &repository.NameMap[[]*repository.FilterListEntry]{}
	entries.Set("3.0.0.0", []*repository.FilterListEntry{entry})
	entries.Set("3.1.0.0", []*repository.FilterListEntry{entry})
	filterList.Set("d/d", entries)

	pool := resolver.NewPool([]pkg.PackageInterface{kept, other}, nil, &resolver.Removed{
		Versions:   removed,
		ByPackage:  map[pkg.PackageInterface]*resolver.VersionMap{kept: versions("1.1.0.0", "1.1.0")},
		Security:   security,
		FilterList: filterList,
	})

	got := evalPHP(t, rt, `
		$pool = \Closure::bind(function ($d) {
			return self::pool($d);
		}, null, \Composer\Repository\RepositorySet::class)($vars['pool']);
		$any = new \Composer\Semver\Constraint\MatchAllConstraint();
		$sec = $pool->getAllSecurityRemovedPackageVersions();
		$lists = $pool->getAllFilterListRemovedPackageVersions();
		$kept = $pool->packageById(1);

		return [
			'removed' => json_encode($pool->getAllRemovedVersions()),
			'byPackage' => json_encode($pool->getRemovedVersionsByPackage(spl_object_id($kept))),
			'security' => $pool->isSecurityRemovedPackageVersion('c/c', $any) ? 'yes' : 'no',
			'ids' => implode(',', $pool->getSecurityAdvisoryIdentifiersForPackageVersion('c/c', $any)),
			'advisoryClass' => get_class($sec['c/c']['1.0.0.0'][0]),
			'sameAdvisory' => $sec['c/c']['1.0.0.0'][0] === $sec['c/c']['1.5.0.0'][0] ? 'yes' : 'no',
			'filtered' => $pool->isFilterListRemovedPackageVersion('d/d', $any) ? 'yes' : 'no',
			'entry' => json_encode($pool->getFilterListEntryForPackageVersion('d/d', $any)),
			'entryClass' => get_class($lists['d/d']['3.0.0.0'][0]),
			'sameEntry' => $lists['d/d']['3.0.0.0'][0] === $lists['d/d']['3.1.0.0'][0] ? 'yes' : 'no',
			'notFiltered' => $pool->isFilterListRemovedPackageVersion('a/a', $any) ? 'yes' : 'no',
		];
	`, php.ArrayOf("pool", rt.poolValue(pool)))

	for key, want := range map[string]string{
		"removed":       `{"a\/a":{"1.1.0.0":"1.1.0","1.0.0.0":"1.0.0"}}`,
		"byPackage":     `{"1.1.0.0":"1.1.0"}`,
		"security":      "yes",
		"ids":           "PKSA-1",
		"advisoryClass": `Composer\Advisory\PartialSecurityAdvisory`,
		"sameAdvisory":  "yes",
		"filtered":      "yes",
		"entry":         `{"malware":"flagged as malware reported by acme (see https:\/\/example.org\/d)"}`,
		"entryClass":    `Composer\FilterList\FilterListEntry`,
		"sameEntry":     "yes",
		"notFiltered":   "no",
	} {
		if g := php.ToString(get(t, got, key)); g != want {
			t.Errorf("%s = %s, want %s", key, g, want)
		}
	}
}
