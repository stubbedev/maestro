// Ports tests/Composer/Test/FilterList/FilterListEntryBuilderTest.php
// (MIT, testdata/LICENSE-composer).

package filterlist_test

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/repository"
)

func build(t *testing.T, rawByList *php.Array, packageConstraintMap *repository.ConstraintMap, defaultPackage string) *filterlist.Filter {
	t.Helper()

	result, err := filterlist.NewFilterListEntryBuilder(nil).Build(rawByList, packageConstraintMap, defaultPackage)
	if err != nil {
		t.Fatal(err)
	}

	return result
}

func TestFilterListEntryBuilder_BuildFiltersByConstraintMatch(t *testing.T) {
	rawByList := php.ArrayOf("malware", php.ListOf(
		php.ArrayOf("package", "vendor/foo", "constraint", "^1.0", "url", "https://example.org/foo", "reason", "bad", "id", "A"),
		php.ArrayOf("package", "vendor/foo", "constraint", "^9.0", "url", nil, "reason", "ignored", "id", "B"),
		php.ArrayOf("package", "vendor/unknown", "constraint", "*", "url", nil, "reason", "ignored", "id", "C"),
	))

	result := build(t, rawByList, constraintMap("vendor/foo", eq("1.2.3.0")), "")

	malware, ok := result.Get("malware")
	if !ok || len(malware) != 1 {
		t.Fatalf("malware = %v", malware)
	}
	if malware[0].PackageName != "vendor/foo" || malware[0].ID.S != "A" {
		t.Errorf("entry = %+v", malware[0])
	}
}

func TestFilterListEntryBuilder_BuildFillsInDefaultPackageWhenMissing(t *testing.T) {
	// Per-package metadata files omit the "package" field — the builder uses defaultPackage.
	rawByList := php.ArrayOf("malware", php.ListOf(
		php.ArrayOf("constraint", "^1.0", "url", "https://example.org/foo", "reason", "bad", "id", "PKFE-001"),
	))

	result := build(t, rawByList, constraintMap("vendor/foo", eq("1.2.3.0")), "vendor/foo")

	malware, _ := result.Get("malware")
	if len(malware) != 1 || malware[0].PackageName != "vendor/foo" {
		t.Errorf("malware = %v", malware)
	}
}

func TestFilterListEntryBuilder_BuildPrefersExplicitPackageOverDefault(t *testing.T) {
	rawByList := php.ArrayOf("malware", php.ListOf(
		php.ArrayOf("package", "vendor/foo", "constraint", "*", "id", "X"),
	))

	// defaultPackage points at a different package; the explicit package field must win.
	result := build(t, rawByList, constraintMap("vendor/foo", eq("1.0.0.0")), "vendor/bar")

	malware, _ := result.Get("malware")
	if len(malware) != 1 || malware[0].PackageName != "vendor/foo" {
		t.Errorf("malware = %v", malware)
	}
}

func TestFilterListEntryBuilder_BuildIgnoresMalformedListShapes(t *testing.T) {
	rawByList := php.ArrayOf(
		"malware", "not-an-array",
		5, php.ListOf(php.ArrayOf("package", "vendor/foo", "constraint", "*")),
		"typosquatting", php.ListOf(
			"not-an-entry",
			php.ArrayOf("package", "vendor/foo", "constraint", "*"),
		),
	)

	result := build(t, rawByList, constraintMap("vendor/foo", eq("1.0.0.0")), "")

	if keys := result.Keys(); !slices.Equal(keys, []string{"typosquatting"}) {
		t.Errorf("keys = %q", keys)
	}
	if list, _ := result.Get("typosquatting"); len(list) != 1 {
		t.Errorf("typosquatting = %v", list)
	}
}

func TestFilterListEntryBuilder_BuildReturnsEmptyForEmptyInput(t *testing.T) {
	if result := build(t, php.NewArray(), constraintMap("vendor/foo", eq("1.0.0.0")), ""); result.Len() != 0 {
		t.Errorf("result has %d lists", result.Len())
	}
}
