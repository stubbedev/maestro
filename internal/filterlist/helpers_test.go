package filterlist_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/policy"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

var versionParser = pkg.NewVersionParser()

// eq is new Constraint('=', $version).
func eq(version string) semver.ConstraintInterface {
	return semver.NewConstraintOp(semver.OpEQ, version)
}

// constraintMap builds an array<string, ConstraintInterface> from pairs.
func constraintMap(kv ...any) *repository.ConstraintMap {
	m := &repository.ConstraintMap{}
	for i := 0; i < len(kv); i += 2 {
		m.Set(kv[i].(string), kv[i+1].(semver.ConstraintInterface))
	}

	return m
}

// createEntry is FilterListEntry::create($listName, $data, self::getVersionParser()).
func createEntry(t *testing.T, listName string, data *php.Array) *filterlist.FilterListEntry {
	t.Helper()

	e, err := filterlist.CreateFilterListEntry(listName, data, versionParser)
	if err != nil {
		t.Fatal(err)
	}

	return e
}

// policyConfig is PolicyConfig::fromConfig of a Config merged with
// ['config' => ['policy' => $policy]].
func policyConfig(t *testing.T, policyData *php.Array) *policy.PolicyConfig {
	t.Helper()

	c := config.New(false, "")
	if err := c.Merge(php.ArrayOf("config", php.ArrayOf("policy", policyData)), "test"); err != nil {
		t.Fatal(err)
	}
	p, err := policy.FromConfig(c)
	if err != nil {
		t.Fatal(err)
	}

	return p
}
