// Ports src/Composer/DependencyResolver/PolicyRemovalReason.php.

package resolver

import (
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// PolicyRemovalReason::*: why a policy removed a package version.
const (
	PolicyRemovalAbandoned  = "abandoned"
	PolicyRemovalAdvisories = "advisories"
	PolicyRemovalFilterList = "filter-list"
	PolicyRemovalCooldown   = "cooldown"
)

// CooldownRemedy is PolicyRemovalReason::COOLDOWN_REMEDY.
const CooldownRemedy = `To exempt the package from the cooldown policy, add it to the "policy.cooldown.ignore" config, or run the command with --no-cooldown for a one-off bypass.`

// CooldownRemovalInfo is the cooldown array of PolicyRemovalReason
// ({releaseDate, availableIn, source}).
type CooldownRemovalInfo struct {
	// ReleaseDate is the effective date, in DateTimeInterface::ATOM.
	ReleaseDate string
	// AvailableIn is formatTimeUntilAvailable's result.
	AvailableIn string
	// Source is "published-time" when the repository vouched for the date,
	// "time" when it came from the package.
	Source string
}

// PolicyRemovalReason is PolicyRemovalReason: why a dependency policy
// removed a package version from the pool, and how to render that.
type PolicyRemovalReason struct {
	Type          string
	PackageName   string
	PrettyVersion string
	Advisories    []repository.Advisory
	FilterEntries []*repository.FilterListEntry
	Cooldown      *CooldownRemovalInfo
}

// NewAbandonedRemovalReason is PolicyRemovalReason::abandoned().
func NewAbandonedRemovalReason(packageName, prettyVersion string) *PolicyRemovalReason {
	return &PolicyRemovalReason{Type: PolicyRemovalAbandoned, PackageName: packageName, PrettyVersion: prettyVersion}
}

// NewAdvisoriesRemovalReason is PolicyRemovalReason::advisories().
func NewAdvisoriesRemovalReason(packageName, prettyVersion string, advisories []repository.Advisory) *PolicyRemovalReason {
	return &PolicyRemovalReason{
		Type: PolicyRemovalAdvisories, PackageName: packageName, PrettyVersion: prettyVersion,
		Advisories: append([]repository.Advisory(nil), advisories...),
	}
}

// NewFilterListRemovalReason is PolicyRemovalReason::filterList().
func NewFilterListRemovalReason(packageName, prettyVersion string, entries []*repository.FilterListEntry) *PolicyRemovalReason {
	return &PolicyRemovalReason{
		Type: PolicyRemovalFilterList, PackageName: packageName, PrettyVersion: prettyVersion,
		FilterEntries: append([]*repository.FilterListEntry(nil), entries...),
	}
}

// NewCooldownRemovalReason is PolicyRemovalReason::cooldown().
func NewCooldownRemovalReason(packageName, prettyVersion, releaseDate, availableIn, source string) *PolicyRemovalReason {
	return &PolicyRemovalReason{
		Type: PolicyRemovalCooldown, PackageName: packageName, PrettyVersion: prettyVersion,
		Cooldown: &CooldownRemovalInfo{ReleaseDate: releaseDate, AvailableIn: availableIn, Source: source},
	}
}

// CombineRemovalReasons ports PolicyRemovalReason::combine: the reasons of
// several versions removed by the same policy merged into one.
func CombineRemovalReasons(reasons []*PolicyRemovalReason) (*PolicyRemovalReason, error) {
	first := reasons[0]
	combined := &PolicyRemovalReason{Type: first.Type, PackageName: first.PackageName, PrettyVersion: first.PrettyVersion}
	advisorySeen := map[string]bool{}
	entrySeen := map[*repository.FilterListEntry]bool{}
	for _, reason := range reasons {
		if reason.Type != first.Type {
			return nil, &util.LogicError{Message: "Cannot combine reasons of different policies: " + first.Type + " and " + reason.Type}
		}

		for _, advisory := range reason.Advisories {
			id := advisory.Partial().AdvisoryID
			if !advisorySeen[id] {
				advisorySeen[id] = true
				combined.Advisories = append(combined.Advisories, advisory)
			}
		}

		for _, entry := range reason.FilterEntries {
			if !entrySeen[entry] {
				entrySeen[entry] = true
				combined.FilterEntries = append(combined.FilterEntries, entry)
			}
		}

		// point at the version that becomes available soonest
		if reason.Cooldown != nil && (combined.Cooldown == nil || cooldownReleaseDate(reason) < cooldownReleaseDate(combined)) {
			combined.Cooldown = reason.Cooldown
			combined.PrettyVersion = reason.PrettyVersion
		}
	}

	return combined, nil
}

// cooldownReleaseDate is new DateTimeImmutable($reason->cooldown
// ['releaseDate']) as a Unix timestamp: the dates are ATOM strings, whose
// lexical order is not their chronological order across offsets.
func cooldownReleaseDate(reason *PolicyRemovalReason) int64 {
	date, err := time.Parse(time.RFC3339, reason.Cooldown.ReleaseDate)
	if err != nil {
		return 0
	}

	return date.Unix()
}

// Verb is getVerb: the verb that makes the description a sentence.
func (r *PolicyRemovalReason) Verb(plural bool) string {
	if r.Type == PolicyRemovalFilterList {
		if plural {
			return "were"
		}

		return "was"
	}

	if plural {
		return "are"
	}

	return "is"
}

// Description is getDescription.
func (r *PolicyRemovalReason) Description() string {
	switch r.Type {
	case PolicyRemovalAbandoned:
		return "abandoned"
	case PolicyRemovalAdvisories:
		return `affected by security advisories ("` + strings.Join(r.linkedAdvisoryIdentifiers(), `", "`) + `")`
	case PolicyRemovalFilterList:
		return strings.Join(mapValues(r.filterListDescriptions()), ", ")
	case PolicyRemovalCooldown:
		if r.Cooldown == nil {
			return `still in the cooldown period configured in "policy.cooldown"`
		}

		return version.CooldownDescription(r.Cooldown.AvailableIn, r.Cooldown.Source)
	}

	return ""
}

// Remedy is getRemedy: how to let the package through while keeping the
// policy on, or "" if there is no such config.
func (r *PolicyRemovalReason) Remedy() string {
	switch r.Type {
	case PolicyRemovalAbandoned:
		return ""
	case PolicyRemovalAdvisories:
		return r.advisoryDetailsHint() + ` To ignore the advisories, add their IDs to the "policy.advisories.ignore-id" config or add the package to "policy.advisories.ignore".`
	case PolicyRemovalFilterList:
		return "To ignore filters for this package, add the package to the " + strings.Join(r.filterListPaths("ignore"), " and ") + " config."
	case PolicyRemovalCooldown:
		return CooldownRemedy
	}

	return ""
}

// OffSwitch is getOffSwitch: the config keys that turn the policy off.
func (r *PolicyRemovalReason) OffSwitch() string {
	if r.Type == PolicyRemovalFilterList {
		return strings.Join(r.filterListPaths("block"), " and ")
	}

	return `"policy.` + r.Type + `.block"`
}

func (r *PolicyRemovalReason) filterListPaths(option string) []string {
	var paths []string
	for listName := range r.filterListDescriptions().All() {
		paths = append(paths, `"policy.`+listName+`.`+option+`"`)
	}

	return paths
}

// linkedAdvisoryIdentifiers is getLinkedAdvisoryIdentifiers: identifiers
// linked to their advisory page where one is known.
func (r *PolicyRemovalReason) linkedAdvisoryIdentifiers() []string {
	ids := make([]string, 0, len(r.Advisories))
	for _, advisory := range r.Advisories {
		partial := advisory.Partial()
		if full, ok := advisory.(*repository.SecurityAdvisory); ok && full.Link.Valid && full.Link.S != "" {
			ids = append(ids, "<href="+console.Escape(full.Link.S)+">"+partial.AdvisoryID+"</>")

			continue
		}

		if strings.HasPrefix(partial.AdvisoryID, "PKSA-") {
			ids = append(ids, "<href="+console.Escape("https://packagist.org/security-advisories/"+partial.AdvisoryID)+">"+partial.AdvisoryID+"</>")

			continue
		}

		ids = append(ids, partial.AdvisoryID)
	}

	return ids
}

// advisoryDetailsHint is getAdvisoryDetailsHint.
func (r *PolicyRemovalReason) advisoryDetailsHint() string {
	for _, advisory := range r.Advisories {
		if !strings.HasPrefix(advisory.Partial().AdvisoryID, "PKSA-") {
			return "Review the advisory details above for more information."
		}
	}

	return "Go to https://packagist.org/security-advisories/ to find advisory details."
}

// filterListDescriptions is getFilterListDescriptions: list name =>
// description.
func (r *PolicyRemovalReason) filterListDescriptions() *repository.NameMap[string] {
	lists := &repository.NameMap[[]string]{}
	for _, entry := range r.FilterEntries {
		source := ""
		if entry.Source.Valid && php.Truthy(entry.Source.S) {
			source = " reported by " + entry.Source.S
		}
		url := ""
		if entry.URL.Valid && php.Truthy(entry.URL.S) {
			url = " (see " + entry.URL.S + ")"
		}
		reason := ""
		if entry.Reason.Valid && php.Truthy(entry.Reason.S) {
			reason = " reason: " + entry.Reason.S
		}

		entries, _ := lists.Get(entry.ListName)
		lists.Set(entry.ListName, append(entries, source+url+reason))
	}

	result := &repository.NameMap[string]{}
	for listName, listEntries := range lists.All() {
		action := "filtered by "
		if listName == "malware" {
			action = "flagged as "
		}
		result.Set(listName, action+listName+strings.Join(listEntries, ", "))
	}

	return result
}

// policiesOf is the unique getType() values of reasons, in order.
func policiesOf(reasons []*PolicyRemovalReason) []string {
	var policies []string
	for _, reason := range reasons {
		if !slices.Contains(policies, reason.Type) {
			policies = append(policies, reason.Type)
		}
	}

	return policies
}
