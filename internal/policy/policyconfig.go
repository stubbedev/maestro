// Ports src/Composer/Policy/PolicyConfig.php.

package policy

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// ConfigReader is the part of Composer\Config PolicyConfig reads;
// *config.Config implements it.
type ConfigReader interface {
	Get(key string, flags int) (any, error)
}

// ReservedNames is PolicyConfig::RESERVED_NAMES: names reserved for
// built-in lists, which repositories must not advertise. Treat it as
// read-only.
var ReservedNames = [...]string{AdvisoriesName, AbandonedName}

// BuiltinListNames is PolicyConfig::BUILTIN_LIST_NAMES. Treat it as
// read-only.
var BuiltinListNames = [...]string{AdvisoriesName, AbandonedName, MalwareName}

// FutureReservedPrefixes is PolicyConfig::FUTURE_RESERVED_PREFIXES. Treat
// it as read-only.
var FutureReservedPrefixes = [...]string{"ignore"}

// FutureReservedNames is PolicyConfig::FUTURE_RESERVED_NAMES. Treat it as
// read-only.
var FutureReservedNames = [...]string{
	"package", "packages",
	"license", "licence", "licenses", "licences",
	"support", "maintenance", "security",
	"minimum-release-age",
}

// PolicyConfig is PolicyConfig: config.policy (with config.audit as its
// backwards-compatible fallback) parsed into its lists.
type PolicyConfig struct {
	// Enabled is the main switch: false disables all policy enforcement.
	Enabled     bool
	Advisories  *AdvisoriesPolicyConfig
	Malware     *MalwarePolicyConfig
	Abandoned   *AbandonedPolicyConfig
	CustomLists *OrderedMap[*CustomListPolicyConfig]
	// IgnoreUnreachable tells for which operations unreachable
	// repositories and policy sources are silently ignored.
	IgnoreUnreachable IgnoreUnreachable
}

// FutureReservedListNameError ports
// PolicyConfig::getFutureReservedListNameError: why listName collides with
// a future-reserved identifier, or "" (null) when it does not.
func FutureReservedListNameError(listName string) string {
	for _, prefix := range FutureReservedPrefixes {
		if strings.HasPrefix(listName, prefix) {
			return fmt.Sprintf(`"%s" starts with reserved prefix "%s".`, listName, prefix)
		}
	}

	if slices.Contains(FutureReservedNames[:], listName) {
		return fmt.Sprintf(`"%s" is reserved for future use.`, listName)
	}

	return ""
}

// assertCustomListNameAllowed ports
// PolicyConfig::assertCustomListNameAllowed.
func assertCustomListNameAllowed(listName string) error {
	if slices.Contains(ReservedNames[:], listName) {
		return &util.UnexpectedValueError{Site: phperr.At("PolicyConfig.php", 139), Message: fmt.Sprintf(
			`Invalid custom dependency policy name "%s": this name is reserved for a built-in dependency policy.`, listName)}
	}

	if msg := FutureReservedListNameError(listName); msg != "" {
		return &util.UnexpectedValueError{Site: phperr.At("PolicyConfig.php", 147), Message: "Invalid custom dependency policy name: " + msg}
	}

	return nil
}

// FromConfig ports PolicyConfig::fromConfig.
func FromConfig(config ConfigReader) (*PolicyConfig, error) {
	policyRaw, err := config.Get("policy", 0)
	if err != nil {
		return nil, phperr.Call(err, `Composer\Config->get`, "PolicyConfig.php", 156)
	}
	auditRaw, err := config.Get("audit", 0)
	if err != nil {
		return nil, phperr.Call(err, `Composer\Config->get`, "PolicyConfig.php", 157)
	}

	if policyRaw == false {
		return &PolicyConfig{
			Enabled:           false,
			Advisories:        AdvisoriesPolicyConfigDisabled(),
			Malware:           MalwarePolicyConfigDisabled(),
			Abandoned:         AbandonedPolicyConfigDisabled(),
			CustomLists:       &OrderedMap[*CustomListPolicyConfig]{},
			IgnoreUnreachable: IgnoreUnreachableAll(),
		}, nil
	}

	policyConfig, _ := policyRaw.(*php.Array)
	auditConfig, _ := auditRaw.(*php.Array)

	advisories, err := AdvisoriesPolicyConfigFromRawConfig(policyConfig, auditConfig)
	if err != nil {
		return nil, err
	}
	malware, err := MalwarePolicyConfigFromRawConfig(policyConfig)
	if err != nil {
		return nil, err
	}
	abandoned, err := AbandonedPolicyConfigFromRawConfig(policyConfig, auditConfig)
	if err != nil {
		return nil, err
	}

	customLists := &OrderedMap[*CustomListPolicyConfig]{}
	for k, listConfig := range each(policyConfig) {
		listName := k.String()
		if k.IsString() && (slices.Contains(BuiltinListNames[:], listName) || slices.Contains(NonListKeys[:], listName)) {
			continue
		}

		if err := assertCustomListNameAllowed(listName); err != nil {
			return nil, err
		}

		list, err := CustomListPolicyConfigFromRawConfig(listName, listConfig)
		if err != nil {
			return nil, err
		}
		customLists.Set(listName, list)
	}

	ignoreUnreachable := IgnoreUnreachableDefault()
	if _, ok := coalesce(policyConfig, "ignore-unreachable"); ok {
		ignoreUnreachable = IgnoreUnreachableFromRawPolicyConfig(policyConfig)
	} else if _, ok := coalesce(auditConfig, "ignore-unreachable"); ok {
		ignoreUnreachable = IgnoreUnreachableFromRawAuditConfig(auditConfig)
	}

	block, set, err := util.GetBoolEnv("COMPOSER_POLICY_ADVISORIES_BLOCK")
	if err != nil {
		return nil, err
	}
	if set {
		advisories = NewAdvisoriesPolicyConfig(block, advisories.Audit, advisories.Ignore, advisories.IgnoreID, advisories.IgnoreSeverity)
	}

	block, set, err = util.GetBoolEnv("COMPOSER_POLICY_MALWARE_BLOCK")
	if err != nil {
		return nil, err
	}
	if set {
		malware = NewMalwarePolicyConfig(block, malware.Audit, malware.BlockScope, malware.Ignore, malware.IgnoreSource)
	}

	// COMPOSER_POLICY_ABANDONED_BLOCK is the canonical name following the
	// COMPOSER_POLICY_<LIST>_BLOCK pattern; COMPOSER_SECURITY_BLOCKING_ABANDONED
	// is the legacy alias and only applies when the canonical var is unset.
	block, set, err = util.GetBoolEnv("COMPOSER_POLICY_ABANDONED_BLOCK")
	if err != nil {
		return nil, err
	}
	if !set {
		block, set, err = util.GetBoolEnv("COMPOSER_SECURITY_BLOCKING_ABANDONED")
		if err != nil {
			return nil, err
		}
	}
	if set {
		abandoned = NewAbandonedPolicyConfig(block, abandoned.Audit, abandoned.Ignore)
	}

	if auditAbandonedEnv, ok := util.GetEnv("COMPOSER_AUDIT_ABANDONED"); ok {
		if !slices.Contains(Audits[:], auditAbandonedEnv) {
			return nil, &util.RuntimeError{Site: phperr.At("PolicyConfig.php", 234), Message: "Invalid value for COMPOSER_AUDIT_ABANDONED: " + auditAbandonedEnv +
				". Expected one of " + strings.Join(Audits[:], ", ") + "."}
		}
		abandoned = NewAbandonedPolicyConfig(abandoned.Block, auditAbandonedEnv, abandoned.Ignore)
	}

	return &PolicyConfig{
		Enabled:           true,
		Advisories:        advisories,
		Malware:           malware,
		Abandoned:         abandoned,
		CustomLists:       customLists,
		IgnoreUnreachable: ignoreUnreachable,
	}, nil
}

// AllLists ports getAllLists: the built-in lists, then the custom ones.
func (p *PolicyConfig) AllLists() *OrderedMap[ListPolicyConfig] {
	all := &OrderedMap[ListPolicyConfig]{entries: make([]Entry[ListPolicyConfig], 0, 3+p.CustomLists.Len())}
	all.Set(AdvisoriesName, p.Advisories)
	all.Set(MalwareName, p.Malware)
	all.Set(AbandonedName, p.Abandoned)
	for name, list := range p.CustomLists.All() {
		all.Set(name, list)
	}

	return all
}

// ActiveAuditFilterLists ports getActiveAuditFilterLists: the filter lists
// active for composer audit reporting.
func (p *PolicyConfig) ActiveAuditFilterLists() *OrderedMap[ListPolicyConfig] {
	lists := &OrderedMap[ListPolicyConfig]{}
	for name, list := range p.filterableLists() {
		if list.List().Audit != AuditIgnore {
			lists.Set(name, list)
		}
	}

	return lists
}

// ActiveBlockFilterLists ports getActiveBlockFilterLists: the filter lists
// blocking during the given BlockScope*.
func (p *PolicyConfig) ActiveBlockFilterLists(blockScope string) *OrderedMap[ListPolicyConfig] {
	lists := &OrderedMap[ListPolicyConfig]{}
	for name, list := range p.filterableLists() {
		if list.ShouldBlock(blockScope) {
			lists.Set(name, list)
		}
	}

	return lists
}

// ActiveAuditFilterListNames ports getActiveAuditFilterListNames.
func (p *PolicyConfig) ActiveAuditFilterListNames() []string {
	return p.ActiveAuditFilterLists().Keys()
}

// ActiveBlockFilterListNames ports getActiveBlockFilterListNames.
func (p *PolicyConfig) ActiveBlockFilterListNames(blockScope string) []string {
	return p.ActiveBlockFilterLists(blockScope).Keys()
}

// filterableLists ports filterableLists: every list except advisories and
// abandoned, which have dedicated code paths.
func (p *PolicyConfig) filterableLists() func(yield func(string, ListPolicyConfig) bool) {
	return func(yield func(string, ListPolicyConfig) bool) {
		if !yield(MalwareName, p.Malware) {
			return
		}
		for name, list := range p.CustomLists.All() {
			if !yield(name, list) {
				return
			}
		}
	}
}

// CustomListsWithSources ports getCustomListsWithSources.
func (p *PolicyConfig) CustomListsWithSources() *OrderedMap[*CustomListPolicyConfig] {
	lists := &OrderedMap[*CustomListPolicyConfig]{}
	for name, list := range p.CustomLists.All() {
		if len(list.Sources) > 0 {
			lists.Set(name, list)
		}
	}

	return lists
}

// with returns a copy of p.
func (p *PolicyConfig) with() *PolicyConfig {
	c := *p

	return &c
}

// WithBlockingDisabled ports withBlockingDisabled (--no-blocking,
// --no-security-blocking, COMPOSER_NO_BLOCKING, COMPOSER_NO_SECURITY_BLOCKING).
func (p *PolicyConfig) WithBlockingDisabled() *PolicyConfig {
	c := p.with()
	c.Advisories = p.Advisories.WithBlockingDisabled()
	c.Malware = p.Malware.WithBlockingDisabled()
	c.Abandoned = p.Abandoned.WithBlockingDisabled()
	c.CustomLists = &OrderedMap[*CustomListPolicyConfig]{entries: make([]Entry[*CustomListPolicyConfig], 0, p.CustomLists.Len())}
	for name, list := range p.CustomLists.All() {
		c.CustomLists.Set(name, list.WithBlockingDisabled())
	}

	return c
}

// WithIgnoreUnreachable ports withIgnoreUnreachable: the listed scopes
// (at least one) flipped to true.
func (p *PolicyConfig) WithIgnoreUnreachable(scopes ...string) (*PolicyConfig, error) {
	ignoreUnreachable, err := p.IgnoreUnreachable.With(scopes...)
	if err != nil {
		return nil, err
	}
	c := p.with()
	c.IgnoreUnreachable = ignoreUnreachable

	return c, nil
}

// WithIgnoreSeverity ports withIgnoreSeverity.
func (p *PolicyConfig) WithIgnoreSeverity(severities []string) *PolicyConfig {
	c := p.with()
	c.Advisories = p.Advisories.WithIgnoreSeverity(severities)

	return c
}

// WithAudit ports withAudit: abandoned, when not "" (null), overrides the
// audit setting of the abandoned list.
func (p *PolicyConfig) WithAudit(abandoned string) *PolicyConfig {
	if abandoned == "" {
		abandoned = p.Abandoned.Audit
	}
	c := p.with()
	c.Abandoned = p.Abandoned.WithAudit(abandoned)

	return c
}
