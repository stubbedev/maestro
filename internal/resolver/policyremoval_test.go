package resolver

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

// Ports tests/Composer/Test/DependencyResolver/PolicyRemovalReasonTest.php.

func TestPolicyRemovalReason_CombinePicksChronologicallyEarliestCooldownAcrossOffsets(t *testing.T) {
	// Lexically "2026-01-12T00:30..." < "2026-01-12T01:00...", but 1.0.0's
	// instant (23:00Z the day before) is the earlier one, so a string compare
	// would wrongly pick 2.0.0 as the soonest-available version
	combined, err := CombineRemovalReasons([]*PolicyRemovalReason{
		NewCooldownRemovalReason("vendor/pkg", "1.0.0", "2026-01-12T01:00:00+02:00", "5 days", "time"),
		NewCooldownRemovalReason("vendor/pkg", "2.0.0", "2026-01-12T00:30:00+00:00", "6 days", "time"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if combined.PrettyVersion != "1.0.0" || combined.Cooldown == nil || combined.Cooldown.AvailableIn != "5 days" {
		t.Fatalf("combined %+v", combined)
	}
}

func TestPolicyRemovalReason_CombineMergesAdvisoriesOfAllVersions(t *testing.T) {
	shared := &repository.PartialSecurityAdvisory{AdvisoryID: "PKSA-aaaa-aaaa-aaaa", PackageName: "vendor/pkg", AffectedVersions: semver.NewConstraintOp(semver.OpLT, "2.0.0.0")}
	combined, err := CombineRemovalReasons([]*PolicyRemovalReason{
		NewAdvisoriesRemovalReason("vendor/pkg", "1.0.0", []repository.Advisory{
			shared,
			&repository.PartialSecurityAdvisory{AdvisoryID: "CVE-2026-1", PackageName: "vendor/pkg", AffectedVersions: semver.NewConstraintOp(semver.OpLT, "1.1.0.0")},
		}),
		NewAdvisoriesRemovalReason("vendor/pkg", "1.5.0", []repository.Advisory{shared}),
	})
	if err != nil {
		t.Fatal(err)
	}

	ids := make([]string, len(combined.Advisories))
	for i, advisory := range combined.Advisories {
		ids[i] = advisory.Partial().AdvisoryID
	}
	if len(ids) != 2 || ids[0] != "PKSA-aaaa-aaaa-aaaa" || ids[1] != "CVE-2026-1" {
		t.Fatalf("advisories %v", ids)
	}
	if !strings.Contains(combined.Remedy(), "Review the advisory details above") {
		t.Errorf("remedy %q", combined.Remedy())
	}
}

func TestPolicyRemovalReason_CombineRejectsMixedPolicies(t *testing.T) {
	_, err := CombineRemovalReasons([]*PolicyRemovalReason{
		NewAbandonedRemovalReason("vendor/pkg", "1.0.0"),
		NewCooldownRemovalReason("vendor/pkg", "2.0.0", "2026-01-12T00:30:00+00:00", "6 days", "time"),
	})
	if err == nil {
		t.Fatal("no error")
	}
}

func TestPolicyRemovalReason_FilterListRendersEachList(t *testing.T) {
	reason := NewFilterListRemovalReason("vendor/pkg", "1.0.0", []*repository.FilterListEntry{
		{
			PackageName: "vendor/pkg", Constraint: semver.NewMatchAllConstraint(), ListName: "malware",
			URL: pkg.Str("https://example.org/m"), Reason: pkg.Str("looks suspicious"), ID: pkg.Str("PKG-1"), Source: pkg.Str("aikido"),
		},
		{PackageName: "vendor/pkg", Constraint: semver.NewMatchAllConstraint(), ListName: "internal"},
	})

	if got := reason.Verb(true); got != "were" {
		t.Errorf("verb %q", got)
	}
	if got, want := reason.Description(), "flagged as malware reported by aikido (see https://example.org/m) reason: looks suspicious, filtered by internal"; got != want {
		t.Errorf("description %q, want %q", got, want)
	}
	if got, want := reason.Remedy(), `To ignore filters for this package, add the package to the "policy.malware.ignore" and "policy.internal.ignore" config.`; got != want {
		t.Errorf("remedy %q, want %q", got, want)
	}
	if got, want := reason.OffSwitch(), `"policy.malware.block" and "policy.internal.block"`; got != want {
		t.Errorf("off switch %q, want %q", got, want)
	}
}

func TestPolicyRemovalReason_CooldownMentionsWeakerTimeSource(t *testing.T) {
	verified := NewCooldownRemovalReason("vendor/pkg", "1.0.0", "2026-01-12T00:30:00+00:00", "5 days", "published-time")
	unverified := NewCooldownRemovalReason("vendor/pkg", "1.0.0", "2026-01-12T00:30:00+00:00", "5 days", "time")

	if got, want := verified.Description(), `still in the cooldown period configured in "policy.cooldown" (available in 5 days)`; got != want {
		t.Errorf("description %q, want %q", got, want)
	}
	if !strings.Contains(unverified.Description(), "based on the package-supplied time field") {
		t.Errorf("description %q", unverified.Description())
	}
	if got, want := verified.OffSwitch(), `"policy.cooldown.block"`; got != want {
		t.Errorf("off switch %q, want %q", got, want)
	}
}
