package resolver

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/DependencyResolver/PoolTest.php.

func TestPool_GetPolicyRemovedVersionsFiltersByConstraintInVersionOrder(t *testing.T) {
	v1 := NewAbandonedRemovalReason("vendor/pkg", "1.0.0")
	v2 := NewAbandonedRemovalReason("vendor/pkg", "2.0.0")
	versions := &repository.NameMap[*PolicyRemovalReason]{}
	versions.Set("2.0.0.0", v2)
	versions.Set("1.0.0.0", v1)
	versions.Set("3.0.0.0", NewAbandonedRemovalReason("vendor/pkg", "3.0.0"))
	removed := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	removed.Set("vendor/pkg", versions)
	pool := NewPool(nil, nil, &Removed{Policy: removed})

	got := pool.PolicyRemovedVersions("vendor/pkg", semver.NewConstraintOp(semver.OpLT, "3.0.0.0"))
	if keys := got.Keys(); len(keys) != 2 || keys[0] != "1.0.0.0" || keys[1] != "2.0.0.0" {
		t.Fatalf("versions %v", keys)
	}
	if first, _ := got.Get("1.0.0.0"); first != v1 {
		t.Error("wrong reason for 1.0.0.0")
	}
	if len(pool.PolicyRemovedVersions("vendor/pkg", nil).Keys()) != 3 {
		t.Error("wrong count")
	}
	if len(pool.PolicyRemovedVersions("vendor/other", nil).Keys()) != 0 {
		t.Error("other package has versions")
	}
	if pool.PolicyRemovalReason("vendor/pkg", "2.0.0.0") != v2 {
		t.Error("wrong reason")
	}
	if pool.PolicyRemovalReason("vendor/pkg", "4.0.0.0") != nil {
		t.Error("reason for unknown version")
	}
}

func TestPool_RecordPolicyRemovalKeepsThePackagesOwnReasonOverAReplacers(t *testing.T) {
	replacer := getPackage(t, "vendor/pkg", "2.0.0")
	replacer.SetReplaces(pkg.LinksOf(pkg.NewLink("vendor/pkg", "vendor/replaced", semver.NewConstraintOp(semver.OpEQ, "2.0.0.0"), pkg.TypeReplace, pkg.Str("self.version"))))
	replaced := getPackage(t, "vendor/replaced", "2.0.0")

	removed := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	replacerReason := NewAbandonedRemovalReason("vendor/pkg", "2.0.0")
	ownReason := NewAbandonedRemovalReason("vendor/replaced", "2.0.0")
	RecordPolicyRemoval(removed, replacer, replacerReason)
	RecordPolicyRemoval(removed, replaced, ownReason)
	RecordPolicyRemoval(removed, replacer, NewAbandonedRemovalReason("vendor/pkg", "2.0.0"))

	if versions, _ := removed.Get("vendor/pkg"); versions == nil {
		t.Fatal("no versions for vendor/pkg")
	} else if reason, _ := versions.Get("2.0.0.0"); reason != replacerReason {
		t.Error("replacer's own reason replaced")
	}
	if versions, _ := removed.Get("vendor/replaced"); versions == nil {
		t.Fatal("no versions for vendor/replaced")
	} else if reason, _ := versions.Get("2.0.0.0"); reason != ownReason {
		t.Error("replaced package's own reason hidden")
	}
}

// Ports tests/Composer/Test/DependencyResolver/TransactionTest.php.

func TestTransaction_RejectsInvalidPresentPackage(t *testing.T) {
	if _, err := NewTransaction([]pkg.PackageInterface{getPackage(t, "a/../../b", "1.0.0")}, nil); !errors.As(err, new(*pkg.SecurityError)) {
		t.Fatalf("got %v", err)
	}
}

func TestTransaction_RejectsInvalidResultPackage(t *testing.T) {
	if _, err := NewTransaction(nil, []pkg.PackageInterface{getPackage(t, "a/../../b", "1.0.0")}); !errors.As(err, new(*pkg.SecurityError)) {
		t.Fatalf("got %v", err)
	}
}

// Ports the new cases of tests/Composer/Test/DependencyResolver/ProblemTest.php.

func TestProblem_GetMissingPackageReasonForCooldownRemovedPackage(t *testing.T) {
	p := getPackage(t, "vendor/pkg", "2.0.0")

	removedVersions := &repository.NameMap[*VersionMap]{}
	versions := &VersionMap{}
	versions.Set("2.0.0.0", "2.0.0")
	removedVersions.Set("vendor/pkg", versions)
	policyRemoved := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	policyVersions := &repository.NameMap[*PolicyRemovalReason]{}
	policyVersions.Set("2.0.0.0", NewCooldownRemovalReason("vendor/pkg", "2.0.0", "2026-01-10T12:00:00+00:00", "5 days", "published-time"))
	policyRemoved.Set("vendor/pkg", policyVersions)

	pool := NewPool(nil, nil, &Removed{Versions: removedVersions, Policy: policyRemoved})
	set := newRepositorySet(t, "stable")
	addRepository(t, set, newArrayRepository(t, p))

	constraint := semver.CreateMultiConstraint([]semver.ConstraintInterface{
		semver.NewConstraintOp(semver.OpGE, "1.0.0.0"),
		semver.NewConstraintOp(semver.OpLT, "3.0.0.0"),
	}, true)

	message := missingPackageMessage(t, set, pool, "vendor/pkg", constraint)

	for _, want := range []string{
		"vendor/pkg[2.0.0]",
		"still in the cooldown period",
		`"policy.cooldown"`,
		"available in 5 days",
		`"policy.cooldown.ignore"`,
		`"policy.cooldown.block"`,
	} {
		if !strings.Contains(message, want) {
			t.Errorf("%q does not contain %q", message, want)
		}
	}
	// authoritative publication timestamp -> no fallback caveat
	if strings.Contains(message, "package-supplied time field") {
		t.Errorf("%q mentions the fallback", message)
	}
}

func TestProblem_GetMissingPackageReasonForCooldownRemovedPackageWithTimeFallback(t *testing.T) {
	p := getPackage(t, "vendor/pkg", "2.0.0")

	removedVersions := &repository.NameMap[*VersionMap]{}
	versions := &VersionMap{}
	versions.Set("2.0.0.0", "2.0.0")
	removedVersions.Set("vendor/pkg", versions)
	policyRemoved := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	policyVersions := &repository.NameMap[*PolicyRemovalReason]{}
	policyVersions.Set("2.0.0.0", NewCooldownRemovalReason("vendor/pkg", "2.0.0", "2026-01-10T12:00:00+00:00", "5 days", "time"))
	policyRemoved.Set("vendor/pkg", policyVersions)

	pool := NewPool(nil, nil, &Removed{Versions: removedVersions, Policy: policyRemoved})
	set := newRepositorySet(t, "stable")
	addRepository(t, set, newArrayRepository(t, p))

	message := missingPackageMessage(t, set, pool, "vendor/pkg", semver.CreateMultiConstraint([]semver.ConstraintInterface{
		semver.NewConstraintOp(semver.OpGE, "1.0.0.0"),
		semver.NewConstraintOp(semver.OpLT, "3.0.0.0"),
	}, true))

	if !strings.Contains(message, "still in the cooldown period") {
		t.Errorf("%q", message)
	}
	// fell back to the author-controlled `time` field -> caveat is shown
	if !strings.Contains(message, "package-supplied time field") {
		t.Errorf("%q", message)
	}
}

func missingPackageMessage(t *testing.T, set *repository.RepositorySet, pool *Pool, name string, constraint semver.ConstraintInterface) string {
	t.Helper()
	ctx := &PrettyContext{RepositorySet: set, Request: NewRequest(nil), Pool: pool}
	reason, err := ctx.MissingPackageReason(name, constraint)
	if err != nil {
		t.Fatal(err)
	}

	return reason[0] + reason[1]
}

func TestProblem_GetMissingPackageReasonPointsAtCooldownWithheldFixNextToAdvisories(t *testing.T) {
	vulnerable := getPackage(t, "vendor/pkg", "1.0.0")
	fix := getPackage(t, "vendor/pkg", "1.0.1")
	advisory := &repository.PartialSecurityAdvisory{
		AdvisoryID: "PKSA-1234-abcd-1234", PackageName: "vendor/pkg",
		AffectedVersions: semver.NewConstraintOp(semver.OpLT, "1.0.1.0"),
	}

	policyRemoved := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	policyVersions := &repository.NameMap[*PolicyRemovalReason]{}
	policyVersions.Set("1.0.0.0", NewAdvisoriesRemovalReason("vendor/pkg", "1.0.0", []repository.Advisory{advisory}))
	policyVersions.Set("1.0.1.0", NewCooldownRemovalReason("vendor/pkg", "1.0.1", "2026-01-10T12:00:00+00:00", "5 days", "published-time"))
	policyRemoved.Set("vendor/pkg", policyVersions)

	pool := NewPool(nil, nil, &Removed{Policy: policyRemoved})
	set := newRepositorySet(t, "stable")
	addRepository(t, set, newArrayRepository(t, vulnerable, fix))

	message := missingPackageMessage(t, set, pool, "vendor/pkg", semver.CreateMultiConstraint([]semver.ConstraintInterface{
		semver.NewConstraintOp(semver.OpGE, "1.0.0.0"),
		semver.NewConstraintOp(semver.OpLT, "2.0.0.0"),
	}, true))

	// each version gets its own line with the policy that removed it, and the
	// remedies follow once
	if want := "found vendor/pkg[1.0.0, 1.0.1] but these were not loaded, because:\n      - 1.0.0: affected by security advisories (\""; !strings.Contains(message, want) {
		t.Errorf("%q does not contain %q", message, want)
	}
	if !strings.Contains(message, "PKSA-1234-abcd-1234") {
		t.Errorf("%q", message)
	}
	want := "\n      - 1.0.1: still in the cooldown period configured in \"policy.cooldown\" (available in 5 days)\n      Go to https://packagist.org/security-advisories/ to find advisory details. To ignore the advisories, add their IDs to the \"policy.advisories.ignore-id\" config or add the package to \"policy.advisories.ignore\". To exempt the package from the cooldown policy, add it to the \"policy.cooldown.ignore\" config, or run the command with --no-cooldown for a one-off bypass. To turn a policy off entirely, you can set \"policy.advisories.block\" or \"policy.cooldown.block\" to false."
	if !strings.Contains(message, want) {
		t.Errorf("%q does not contain %q", message, want)
	}
}

func TestProblem_GetMissingPackageReasonKeepsOneSentenceWhenOnlyTheCooldownWithheldVersions(t *testing.T) {
	policyRemoved := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	policyVersions := &repository.NameMap[*PolicyRemovalReason]{}
	policyVersions.Set("1.0.1.0", NewCooldownRemovalReason("vendor/pkg", "1.0.1", "2026-01-10T12:00:00+00:00", "1 day", "published-time"))
	policyVersions.Set("1.0.2.0", NewCooldownRemovalReason("vendor/pkg", "1.0.2", "2026-01-12T12:00:00+00:00", "3 days", "published-time"))
	policyRemoved.Set("vendor/pkg", policyVersions)

	pool := NewPool(nil, nil, &Removed{Policy: policyRemoved})
	set := newRepositorySet(t, "stable")
	addRepository(t, set, newArrayRepository(t, getPackage(t, "vendor/pkg", "1.0.1"), getPackage(t, "vendor/pkg", "1.0.2")))

	message := missingPackageMessage(t, set, pool, "vendor/pkg", semver.NewConstraintOp(semver.OpGE, "1.0.0.0"))

	// the sentence points at the version that becomes available soonest
	if want := `found vendor/pkg[1.0.1, 1.0.2] but these were not loaded, because they are still in the cooldown period configured in "policy.cooldown" (available in 1 day).`; !strings.Contains(message, want) {
		t.Errorf("%q does not contain %q", message, want)
	}
	if strings.Contains(message, "\n      - ") {
		t.Errorf("%q lists versions", message)
	}
}

func TestProblem_GetMissingPackageReasonListsFilterListAndCooldownRemovalsSeparately(t *testing.T) {
	flagged := getPackage(t, "vendor/pkg", "1.0.0")
	withheld := getPackage(t, "vendor/pkg", "1.0.1")
	entry := &repository.FilterListEntry{
		PackageName: "vendor/pkg", Constraint: semver.NewMatchAllConstraint(), ListName: "malware",
		URL: pkg.Str("https://example.org/malware/vendor-pkg"), Reason: pkg.Str("looks suspicious"), ID: pkg.Str("PKG-1"),
	}

	policyRemoved := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	policyVersions := &repository.NameMap[*PolicyRemovalReason]{}
	policyVersions.Set("1.0.0.0", NewFilterListRemovalReason("vendor/pkg", "1.0.0", []*repository.FilterListEntry{entry}))
	policyVersions.Set("1.0.1.0", NewCooldownRemovalReason("vendor/pkg", "1.0.1", "2026-01-10T12:00:00+00:00", "5 days", "time"))
	policyRemoved.Set("vendor/pkg", policyVersions)

	pool := NewPool(nil, nil, &Removed{Policy: policyRemoved})
	set := newRepositorySet(t, "stable")
	addRepository(t, set, newArrayRepository(t, flagged, withheld))

	message := missingPackageMessage(t, set, pool, "vendor/pkg", semver.CreateMultiConstraint([]semver.ConstraintInterface{
		semver.NewConstraintOp(semver.OpGE, "1.0.0.0"),
		semver.NewConstraintOp(semver.OpLT, "2.0.0.0"),
	}, true))

	if want := "found vendor/pkg[1.0.0, 1.0.1] but these were not loaded, because:\n      - 1.0.0: flagged as malware"; !strings.Contains(message, want) {
		t.Errorf("%q does not contain %q", message, want)
	}
	if !strings.Contains(message, "reason: looks suspicious") {
		t.Errorf("%q", message)
	}
	want := "\n      - 1.0.1: still in the cooldown period configured in \"policy.cooldown\" (available in 5 days, based on the package-supplied time field as the repository provides no published-time)\n      To ignore filters for this package, add the package to the \"policy.malware.ignore\" config. To exempt the package from the cooldown policy, add it to the \"policy.cooldown.ignore\" config, or run the command with --no-cooldown for a one-off bypass. To turn a policy off entirely, you can set \"policy.malware.block\" or \"policy.cooldown.block\" to false."
	if !strings.Contains(message, want) {
		t.Errorf("%q does not contain %q", message, want)
	}
}

func TestProblem_ConflictAlternativeCondensesTheVersionList(t *testing.T) {
	conflicter := getPackage(t, "acme/conflicter", "1.0.0")
	lib := getPackage(t, "acme/lib", "1.0.3")
	policyRemoved := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	policyVersions := &repository.NameMap[*PolicyRemovalReason]{}
	for _, version := range []string{"1.0.0", "1.0.1", "1.0.2"} {
		policyVersions.Set(version+".0", NewCooldownRemovalReason("acme/lib", version, "2026-01-10T12:00:00+00:00", "5 days", "published-time"))
	}
	policyRemoved.Set("acme/lib", policyVersions)

	pool := NewPool([]pkg.PackageInterface{conflicter, lib}, nil, &Removed{Policy: policyRemoved})

	problem := NewProblem()
	problem.AddRule(NewGenericRule([]int32{int32(conflicter.ID())}, RuleRootRequire, &RootRequire{PackageName: "acme/conflicter", Constraint: semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")}))
	problem.AddRule(NewGenericRule([]int32{int32(lib.ID())}, RuleRootRequire, &RootRequire{PackageName: "acme/lib", Constraint: semver.CreateMultiConstraint([]semver.ConstraintInterface{
		semver.NewConstraintOp(semver.OpGE, "1.0.0.0"),
		semver.NewConstraintOp(semver.OpLT, "2.0.0.0"),
	}, true)}))
	problem.AddRule(NewGenericRule(
		[]int32{int32(-conflicter.ID()), int32(-lib.ID())},
		RulePackageConflict,
		pkg.NewLink("acme/conflicter", "acme/lib", semver.NewConstraintOp(semver.OpGE, "1.0.3.0"), pkg.TypeConflict, pkg.NullString{}),
	))

	message, err := problem.PrettyString(&PrettyContext{RepositorySet: newRepositorySet(t, "stable"), Request: NewRequest(nil), Pool: pool})
	if err != nil {
		t.Fatal(err)
	}

	if want := "acme/lib[1.0.0, ..., 1.0.2] match the requirements and are not excluded by the conflicts above"; !strings.Contains(message, want) {
		t.Errorf("%q does not contain %q", message, want)
	}
}

func TestProblem_ConflictAlternativeForASingleVersion(t *testing.T) {
	conflicter := getPackage(t, "acme/conflicter", "1.0.0")
	lib := getPackage(t, "acme/lib", "1.0.3")
	policyRemoved := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	policyVersions := &repository.NameMap[*PolicyRemovalReason]{}
	policyVersions.Set("1.0.0.0", NewCooldownRemovalReason("acme/lib", "1.0.0", "2026-01-10T12:00:00+00:00", "5 days", "published-time"))
	policyRemoved.Set("acme/lib", policyVersions)

	pool := NewPool([]pkg.PackageInterface{conflicter, lib}, nil, &Removed{Policy: policyRemoved})

	problem := NewProblem()
	problem.AddRule(NewGenericRule([]int32{int32(conflicter.ID())}, RuleRootRequire, &RootRequire{PackageName: "acme/conflicter", Constraint: semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")}))
	problem.AddRule(NewGenericRule([]int32{int32(lib.ID())}, RuleRootRequire, &RootRequire{PackageName: "acme/lib", Constraint: semver.CreateMultiConstraint([]semver.ConstraintInterface{
		semver.NewConstraintOp(semver.OpGE, "1.0.0.0"),
		semver.NewConstraintOp(semver.OpLT, "2.0.0.0"),
	}, true)}))
	problem.AddRule(NewGenericRule(
		[]int32{int32(-conflicter.ID()), int32(-lib.ID())},
		RulePackageConflict,
		pkg.NewLink("acme/conflicter", "acme/lib", semver.NewConstraintOp(semver.OpGE, "1.0.3.0"), pkg.TypeConflict, pkg.NullString{}),
	))

	message, err := problem.PrettyString(&PrettyContext{RepositorySet: newRepositorySet(t, "stable"), Request: NewRequest(nil), Pool: pool})
	if err != nil {
		t.Fatal(err)
	}

	if want := "acme/lib[1.0.0] matches the requirements and is not excluded by the conflicts above"; !strings.Contains(message, want) {
		t.Errorf("%q does not contain %q", message, want)
	}
}

func TestProblem_ConflictAlternativeLinksCompleteAdvisories(t *testing.T) {
	conflicter := getPackage(t, "acme/conflicter", "1.0.0")
	lib := getPackage(t, "acme/lib", "1.0.1")
	policyRemoved := &repository.NameMap[*repository.NameMap[*PolicyRemovalReason]]{}
	policyVersions := &repository.NameMap[*PolicyRemovalReason]{}
	policyVersions.Set("1.0.0.0", NewAdvisoriesRemovalReason("acme/lib", "1.0.0", []repository.Advisory{
		&repository.PartialSecurityAdvisory{AdvisoryID: "test-advisory-1", PackageName: "acme/lib", AffectedVersions: semver.NewConstraintOp(semver.OpLT, "1.0.1.0")},
	}))
	policyRemoved.Set("acme/lib", policyVersions)

	pool := NewPool([]pkg.PackageInterface{conflicter, lib}, nil, &Removed{Policy: policyRemoved})
	set := newRepositorySet(t, "stable")
	addRepository(t, set, newPackageRepository(t, php.ArrayOf(
		"package", php.ListOf(php.ArrayOf("name", "acme/lib", "version", "1.0.0")),
		"security-advisories", php.ArrayOf("acme/lib", php.ListOf(php.ArrayOf(
			"advisoryId", "test-advisory-1",
			"packageName", "acme/lib",
			"title", "Security Advisory",
			"link", "https://example.org/advisories/1",
			"affectedVersions", "<1.0.1",
			"reportedAt", "2024-04-30 12:37:47",
			"sources", php.ListOf(php.ArrayOf("name", "Security Advisory", "remoteId", "test")),
		))),
	)))

	problem := NewProblem()
	problem.AddRule(NewGenericRule([]int32{int32(conflicter.ID())}, RuleRootRequire, &RootRequire{PackageName: "acme/conflicter", Constraint: semver.NewConstraintOp(semver.OpEQ, "1.0.0.0")}))
	problem.AddRule(NewGenericRule([]int32{int32(lib.ID())}, RuleRootRequire, &RootRequire{PackageName: "acme/lib", Constraint: semver.NewConstraintOp(semver.OpGE, "1.0.0.0")}))
	problem.AddRule(NewGenericRule(
		[]int32{int32(-conflicter.ID()), int32(-lib.ID())},
		RulePackageConflict,
		pkg.NewLink("acme/conflicter", "acme/lib", semver.NewConstraintOp(semver.OpGE, "1.0.1.0"), pkg.TypeConflict, pkg.NullString{}),
	))

	message, err := problem.PrettyString(&PrettyContext{RepositorySet: set, Request: NewRequest(nil), Pool: pool})
	if err != nil {
		t.Fatal(err)
	}

	// the pool only knew the advisory ID, the complete advisory provides the
	// link
	if want := `affected by security advisories ("<href=https://example.org/advisories/1>test-advisory-1</>")`; !strings.Contains(message, want) {
		t.Errorf("%q does not contain %q", message, want)
	}
}
