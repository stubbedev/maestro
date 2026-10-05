package resolver

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/DependencyResolver/RuleTest.php,
// RuleSetTest.php and RuleSetIteratorTest.php.

func rootRequire(name string, constraint semver.ConstraintInterface) *RootRequire {
	return &RootRequire{PackageName: name, Constraint: constraint}
}

func emptyRootRequire() *RootRequire { return rootRequire("", semver.NewMatchAllConstraint()) }

// testGetHash compares Composer's xxh3-based hash with PHP's own; the
// hash is internal here, so the port checks what it is used for: equal
// literals hash alike, others do not.
func TestRule_GetHash(t *testing.T) {
	rule := NewGenericRule([]int32{123}, RuleRootRequire, emptyRootRequire())
	same := NewGenericRule([]int32{123}, RuleRootRequire, emptyRootRequire())
	other := NewGenericRule([]int32{124}, RuleRootRequire, emptyRootRequire())

	if rule.hash() != same.hash() || rule.hash() == other.hash() {
		t.Fatal("hash does not identify the literals")
	}
}

func TestRule_EqualsForRulesWithDifferentHashes(t *testing.T) {
	rule := NewGenericRule([]int32{1, 2}, RuleRootRequire, emptyRootRequire())
	rule2 := NewGenericRule([]int32{1, 3}, RuleRootRequire, emptyRootRequire())

	if rule.Equals(rule2) {
		t.Fatal("rules equal")
	}
}

func TestRule_EqualsForRulesWithDifferLiteralsQuantity(t *testing.T) {
	rule := NewGenericRule([]int32{1, 12}, RuleRootRequire, emptyRootRequire())
	rule2 := NewGenericRule([]int32{1}, RuleRootRequire, emptyRootRequire())

	if rule.Equals(rule2) {
		t.Fatal("rules equal")
	}
}

func TestRule_EqualsForRulesWithSameLiterals(t *testing.T) {
	rule := NewGenericRule([]int32{1, 12}, RuleRootRequire, emptyRootRequire())
	rule2 := NewGenericRule([]int32{1, 12}, RuleRootRequire, emptyRootRequire())

	if !rule.Equals(rule2) {
		t.Fatal("rules differ")
	}
}

func TestRule_SetAndGetType(t *testing.T) {
	rule := NewGenericRule(nil, RuleRootRequire, emptyRootRequire())
	rule.SetType(TypeRequest)

	if rule.Type() != TypeRequest {
		t.Fatalf("type %d", rule.Type())
	}
}

func TestRule_Enable(t *testing.T) {
	rule := NewGenericRule(nil, RuleRootRequire, emptyRootRequire())
	if err := rule.Disable(); err != nil {
		t.Fatal(err)
	}
	rule.Enable()

	if !rule.IsEnabled() || rule.IsDisabled() {
		t.Fatal("rule not enabled")
	}
}

func TestRule_Disable(t *testing.T) {
	rule := NewGenericRule(nil, RuleRootRequire, emptyRootRequire())
	rule.Enable()
	if err := rule.Disable(); err != nil {
		t.Fatal(err)
	}

	if !rule.IsDisabled() || rule.IsEnabled() {
		t.Fatal("rule not disabled")
	}
}

func TestRule_IsAssertions(t *testing.T) {
	rule := NewGenericRule([]int32{1, 12}, RuleRootRequire, emptyRootRequire())
	rule2 := NewGenericRule([]int32{1}, RuleRootRequire, emptyRootRequire())

	if rule.IsAssertion() || !rule2.IsAssertion() {
		t.Fatal("wrong assertion state")
	}
}

func TestRule_PrettyString(t *testing.T) {
	p1 := getPackage(t, "foo", "2.1")
	p2 := getPackage(t, "baz", "1.1")
	pool := NewPool([]pkg.PackageInterface{p1, p2}, nil, nil)

	rule := NewGenericRule([]int32{int32(p1.ID()), -int32(p2.ID())}, RulePackageRequires, newLink("baz", "foo", matchAll("*"), pkg.TypeUnknown))

	got, err := rule.PrettyString(&PrettyContext{RepositorySet: newRepositorySet(t, "stable"), Request: NewRequest(nil), Pool: pool})
	if err != nil {
		t.Fatal(err)
	}
	if want := "baz 1.1 relates to foo * -> satisfiable by foo[2.1]."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRule_GetRequiredPackageForLockedFilterListRemovedRule(t *testing.T) {
	p := getPackage(t, "vendor/malware", "1.0")
	rule := NewGenericRule(nil, RuleLockedFilterListRemoved, p)

	if name, ok := rule.RequiredPackage(); !ok || name != "vendor/malware" {
		t.Fatalf("got %q %v", name, ok)
	}
}

func TestRule_PrettyStringForLockedFilterListRemovedRule(t *testing.T) {
	p := getPackage(t, "vendor/malware", "1.0")
	pool := NewPool([]pkg.PackageInterface{p}, nil, nil)
	rule := NewGenericRule(nil, RuleLockedFilterListRemoved, p)

	got, err := rule.PrettyString(&PrettyContext{RepositorySet: newRepositorySet(t, "stable"), Request: NewRequest(nil), Pool: pool})
	if err != nil {
		t.Fatal(err)
	}
	if want := "vendor/malware 1.0 was removed by a dependency policy (e.g. malware) and cannot be installed."; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func mustAdd(t *testing.T, ruleSet *RuleSet, rule *Rule, typ int) {
	t.Helper()
	if err := ruleSet.Add(rule, typ); err != nil {
		t.Fatal(err)
	}
}

func TestRuleSet_Add(t *testing.T) {
	rules := map[int][]*Rule{
		TypePackage: nil,
		TypeRequest: {
			NewGenericRule([]int32{1}, RuleRootRequire, emptyRootRequire()),
			NewGenericRule([]int32{2}, RuleRootRequire, emptyRootRequire()),
		},
		TypeLearned: {
			NewGenericRule(nil, RuleLearned, 1),
		},
	}

	ruleSet := NewRuleSet()

	mustAdd(t, ruleSet, rules[TypeRequest][0], TypeRequest)
	mustAdd(t, ruleSet, rules[TypeLearned][0], TypeLearned)
	mustAdd(t, ruleSet, rules[TypeRequest][1], TypeRequest)

	got := ruleSet.Rules()
	if len(got) != len(rules) {
		t.Fatalf("got %d types", len(got))
	}
	for typ, want := range rules {
		if !slices.Equal(got[typ], want) {
			t.Fatalf("type %d: got %v, want %v", typ, got[typ], want)
		}
	}
}

func TestRuleSet_AddIgnoresDuplicates(t *testing.T) {
	rules := []*Rule{
		NewGenericRule(nil, RuleRootRequire, emptyRootRequire()),
		NewGenericRule(nil, RuleRootRequire, emptyRootRequire()),
		NewGenericRule(nil, RuleRootRequire, emptyRootRequire()),
	}

	ruleSet := NewRuleSet()
	for _, rule := range rules {
		mustAdd(t, ruleSet, rule, TypeRequest)
	}

	if n := ruleSet.IteratorFor(TypeRequest).Count(); n != 1 {
		t.Fatalf("count %d", n)
	}
}

func TestRuleSet_AddWhenTypeIsNotRecognized(t *testing.T) {
	ruleSet := NewRuleSet()

	err := ruleSet.Add(NewGenericRule(nil, RuleRootRequire, emptyRootRequire()), 7)
	if _, ok := errors.AsType[*OutOfBoundsError](err); !ok {
		t.Fatalf("got %v", err)
	}
}

func TestRuleSet_Count(t *testing.T) {
	ruleSet := NewRuleSet()

	mustAdd(t, ruleSet, NewGenericRule([]int32{1}, RuleRootRequire, emptyRootRequire()), TypeRequest)
	mustAdd(t, ruleSet, NewGenericRule([]int32{2}, RuleRootRequire, emptyRootRequire()), TypeRequest)

	if ruleSet.Count() != 2 {
		t.Fatalf("count %d", ruleSet.Count())
	}
}

func TestRuleSet_RuleByID(t *testing.T) {
	ruleSet := NewRuleSet()
	rule := NewGenericRule(nil, RuleRootRequire, emptyRootRequire())

	mustAdd(t, ruleSet, rule, TypeRequest)

	if ruleSet.RuleByID[0] != rule {
		t.Fatal("wrong rule")
	}
}

func TestRuleSet_GetIterator(t *testing.T) {
	ruleSet := NewRuleSet()

	rule1 := NewGenericRule([]int32{1}, RuleRootRequire, emptyRootRequire())
	rule2 := NewGenericRule([]int32{2}, RuleRootRequire, emptyRootRequire())
	mustAdd(t, ruleSet, rule1, TypeRequest)
	mustAdd(t, ruleSet, rule2, TypeLearned)

	iterator := ruleSet.Iterator()

	if iterator.Current() != rule1 {
		t.Fatal("first rule")
	}
	iterator.Next()
	if iterator.Current() != rule2 {
		t.Fatal("second rule")
	}
}

func TestRuleSet_GetIteratorFor(t *testing.T) {
	ruleSet := NewRuleSet()
	rule1 := NewGenericRule([]int32{1}, RuleRootRequire, emptyRootRequire())
	rule2 := NewGenericRule([]int32{2}, RuleRootRequire, emptyRootRequire())

	mustAdd(t, ruleSet, rule1, TypeRequest)
	mustAdd(t, ruleSet, rule2, TypeLearned)

	if ruleSet.IteratorFor(TypeLearned).Current() != rule2 {
		t.Fatal("wrong rule")
	}
}

func TestRuleSet_GetIteratorWithout(t *testing.T) {
	ruleSet := NewRuleSet()
	rule1 := NewGenericRule([]int32{1}, RuleRootRequire, emptyRootRequire())
	rule2 := NewGenericRule([]int32{2}, RuleRootRequire, emptyRootRequire())

	mustAdd(t, ruleSet, rule1, TypeRequest)
	mustAdd(t, ruleSet, rule2, TypeLearned)

	if ruleSet.IteratorWithout(TypeRequest).Current() != rule2 {
		t.Fatal("wrong rule")
	}
}

func TestRuleSet_PrettyString(t *testing.T) {
	p := getPackage(t, "foo", "2.1")
	pool := NewPool([]pkg.PackageInterface{p}, nil, nil)

	ruleSet := NewRuleSet()
	rule := NewGenericRule([]int32{int32(p.ID())}, RuleRootRequire, rootRequire("foo/bar", semver.NewMatchNoneConstraint()))

	mustAdd(t, ruleSet, rule, TypeRequest)

	got, err := ruleSet.PrettyString(&PrettyContext{RepositorySet: newRepositorySet(t, "stable"), Request: NewRequest(nil), Pool: pool})
	if err != nil {
		t.Fatal(err)
	}
	if want := "REQUEST : No package found to satisfy root composer.json require foo/bar"; !strings.Contains(got, want) {
		t.Fatalf("%q does not contain %q", got, want)
	}
}

func ruleSetIteratorRules() map[int][]*Rule {
	return map[int][]*Rule{
		TypeRequest: {
			NewGenericRule(nil, RuleRootRequire, emptyRootRequire()),
			NewGenericRule(nil, RuleRootRequire, emptyRootRequire()),
		},
		TypeLearned: {
			NewGenericRule(nil, RuleLearned, 1),
		},
		TypePackage: nil,
	}
}

func TestRuleSetIterator_Foreach(t *testing.T) {
	rules := ruleSetIteratorRules()
	var result []*Rule
	for it := NewRuleSetIterator(rules); it.Valid(); it.Next() {
		result = append(result, it.Current())
	}

	expected := []*Rule{rules[TypeRequest][0], rules[TypeRequest][1], rules[TypeLearned][0]}
	if !slices.Equal(result, expected) {
		t.Fatalf("got %v, want %v", result, expected)
	}
}

func TestRuleSetIterator_Keys(t *testing.T) {
	var result []int
	for it := NewRuleSetIterator(ruleSetIteratorRules()); it.Valid(); it.Next() {
		result = append(result, it.Key())
	}

	if expected := []int{TypeRequest, TypeRequest, TypeLearned}; !slices.Equal(result, expected) {
		t.Fatalf("got %v, want %v", result, expected)
	}
}

// Ports tests/Composer/Test/DependencyResolver/PoolTest.php.

func TestPool_Pool(t *testing.T) {
	p := getPackage(t, "foo", "1")
	pool := NewPool([]pkg.PackageInterface{p}, nil, nil)

	for range 2 {
		if got := pool.WhatProvides("foo", nil); !slices.Equal(got, []pkg.PackageInterface{p}) {
			t.Fatalf("got %v", got)
		}
	}
}

func TestPool_WhatProvidesPackageWithConstraint(t *testing.T) {
	firstPackage := getPackage(t, "foo", "1")
	secondPackage := getPackage(t, "foo", "2")
	pool := NewPool([]pkg.PackageInterface{firstPackage, secondPackage}, nil, nil)

	if got := pool.WhatProvides("foo", nil); !slices.Equal(got, []pkg.PackageInterface{firstPackage, secondPackage}) {
		t.Fatalf("got %v", got)
	}
	if got := pool.WhatProvides("foo", getVersionConstraint(t, "==", "2")); !slices.Equal(got, []pkg.PackageInterface{secondPackage}) {
		t.Fatalf("got %v", got)
	}
}

func TestPool_PackageByID(t *testing.T) {
	p := getPackage(t, "foo", "1")
	pool := NewPool([]pkg.PackageInterface{p}, nil, nil)

	if pool.PackageByID(1) != pkg.PackageInterface(p) {
		t.Fatal("wrong package")
	}
}

func TestPool_WhatProvidesWhenPackageCannotBeFound(t *testing.T) {
	pool := NewPool(nil, nil, nil)

	if got := pool.WhatProvides("foo", nil); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

// Ports tests/Composer/Test/DependencyResolver/RequestTest.php.

func TestRequest_RequestInstall(t *testing.T) {
	newArrayRepository(t, getPackage(t, "foo", "1"), getPackage(t, "bar", "1"), getPackage(t, "foobar", "1"))

	request := NewRequest(nil)
	requireName(t, request, "foo", nil)

	requires := request.Requires()
	c, ok := requires.Get("foo")
	if requires.Len() != 1 || !ok {
		t.Fatalf("requires %v", requires.Keys())
	}
	if _, isMatchAll := c.(*semver.MatchAllConstraint); !isMatchAll {
		t.Fatalf("constraint %T", c)
	}
}

func TestRequest_RequestInstallSamePackageFromDifferentRepositories(t *testing.T) {
	newArrayRepository(t, getPackage(t, "foo", "1"))
	newArrayRepository(t, getPackage(t, "foo", "1"))

	request := NewRequest(nil)
	constraint := getVersionConstraint(t, "=", "1")
	requireName(t, request, "foo", constraint)

	requires := request.Requires()
	if c, ok := requires.Get("foo"); requires.Len() != 1 || !ok || c != semver.ConstraintInterface(constraint) {
		t.Fatalf("requires %v", requires.Keys())
	}
}

// Ports tests/Composer/Test/DependencyResolver/ProblemTest.php.

func TestProblem_GetMissingLockedPackageReasonForFilterListRemovedPackage(t *testing.T) {
	p := getPackage(t, "vendor/malware", "1.0.0")
	entry := &repository.FilterListEntry{
		PackageName: "vendor/malware",
		Constraint:  semver.NewMatchAllConstraint(),
		ListName:    "malware",
		URL:         pkg.Str("https://example.org/malware/vendor-malware"),
		Reason:      pkg.Str("looks suspicious"),
		ID:          pkg.Str("PKG-1"),
		Source:      pkg.Str("aikido"),
	}
	versions := &repository.NameMap[[]*repository.FilterListEntry]{}
	versions.Set("1.0.0.0", []*repository.FilterListEntry{entry})
	removed := &repository.NameMap[*repository.NameMap[[]*repository.FilterListEntry]]{}
	removed.Set("vendor/malware", versions)
	pool := NewPool(nil, nil, &Removed{FilterList: removed})

	reason, err := MissingLockedPackageReason(pool, p)
	if err != nil {
		t.Fatal(err)
	}

	if reason[0] != "- Package vendor/malware 1.0.0 (in the lock file) " {
		t.Fatalf("prefix %q", reason[0])
	}
	for _, want := range []string{
		"flagged as malware reported by aikido",
		"https://example.org/malware/vendor-malware",
		"reason: looks suspicious",
		`"policy.malware.ignore"`,
		`"policy.malware.block"`,
	} {
		if !strings.Contains(reason[1], want) {
			t.Errorf("%q does not contain %q", reason[1], want)
		}
	}
}
