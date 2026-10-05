package policy

import (
	"encoding/json"
	"testing"
)

// Dump serialises policy values for comparisons, in the format
// tools/oracle/config/policy.php writes.
func Dump(v any) any {
	switch v := v.(type) {
	case *IgnorePackageRule:
		return []any{v.PackageName, v.Constraint.PrettyString(), v.Constraint.String(), reason(v.Reason), v.OnBlock, v.OnAudit, v.PackageNameRegex}
	case *IgnoreIDRule:
		return []any{v.ID, reason(v.Reason), v.OnBlock, v.OnAudit}
	case *IgnoreSeverityRule:
		return []any{v.Severity, reason(v.Reason), v.OnBlock, v.OnAudit}
	case *IgnoreMap:
		return pairs(v, func(rules []*IgnorePackageRule) any {
			out := make([]any, len(rules))
			for i, r := range rules {
				out[i] = Dump(r)
			}

			return out
		})
	case *Reasons:
		return pairs(v, func(r *string) any { return reason(r) })
	case *OrderedMap[*IgnoreIDRule]:
		return pairs(v, func(r *IgnoreIDRule) any { return Dump(r) })
	case *OrderedMap[*IgnoreSeverityRule]:
		return pairs(v, func(r *IgnoreSeverityRule) any { return Dump(r) })
	case IgnoreUnreachable:
		return []any{v.Audit, v.Install, v.Update}
	case ListPolicyConfig:
		return dumpList(v)
	case *PolicyConfig:
		return dumpPolicy(v)
	}
	panic("cannot dump")
}

func reason(r *string) any {
	if r == nil {
		return nil
	}

	return *r
}

func pairs[V any](m *OrderedMap[V], f func(V) any) any {
	out := []any{}
	for k, v := range m.All() {
		out = append(out, []any{k, f(v)})
	}

	return out
}

func dumpList(l ListPolicyConfig) map[string]any {
	b := l.List()
	out := map[string]any{
		"name":        b.Name,
		"block":       b.Block,
		"audit":       b.Audit,
		"ignore":      Dump(b.Ignore),
		"shouldBlock": []any{l.ShouldBlock(BlockScopeUpdate), l.ShouldBlock(BlockScopeInstall), l.ShouldBlock(BlockScopeAll)},
		"flatAudit":   Dump(b.FlatIgnoreForOperation("audit")),
		"flatBlock":   Dump(b.FlatIgnoreForOperation("block")),
		"ignoreAudit": Dump(b.IgnoreForOperation("audit")),
		"ignoreBlock": Dump(b.IgnoreForOperation("block")),
	}
	switch l := l.(type) {
	case *AdvisoriesPolicyConfig:
		out["class"] = "AdvisoriesPolicyConfig"
		out["ignoreId"] = Dump(l.IgnoreID)
		out["ignoreSeverity"] = Dump(l.IgnoreSeverity)
		out["ignoreIdAudit"] = Dump(l.IgnoreIDForOperation("audit"))
		out["ignoreIdBlock"] = Dump(l.IgnoreIDForOperation("block"))
		out["ignoreListAudit"] = Dump(l.IgnoreListForOperation("audit"))
		out["ignoreListBlock"] = Dump(l.IgnoreListForOperation("block"))
		out["ignoreSeverityAudit"] = Dump(l.IgnoreSeverityForOperation("audit"))
		out["ignoreSeverityBlock"] = Dump(l.IgnoreSeverityForOperation("block"))
	case *MalwarePolicyConfig:
		out["class"] = "MalwarePolicyConfig"
		out["blockScope"] = l.BlockScope
		src := []any{}
		for _, s := range l.IgnoreSource {
			src = append(src, s)
		}
		out["ignoreSource"] = src
	case *AbandonedPolicyConfig:
		out["class"] = "AbandonedPolicyConfig"
	case *CustomListPolicyConfig:
		out["class"] = "CustomListPolicyConfig"
		src := []any{}
		for _, s := range l.Sources {
			src = append(src, []any{s.ListName, s.URL})
		}
		out["sources"] = src
	}

	return out
}

func strs(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}

	return out
}

func dumpPolicy(p *PolicyConfig) map[string]any {
	return map[string]any{
		"enabled":            p.Enabled,
		"advisories":         dumpList(p.Advisories),
		"malware":            dumpList(p.Malware),
		"abandoned":          dumpList(p.Abandoned),
		"custom":             pairs(p.CustomLists, func(l *CustomListPolicyConfig) any { return dumpList(l) }),
		"ignoreUnreachable":  Dump(p.IgnoreUnreachable),
		"forBlockScope":      []any{p.IgnoreUnreachable.ForBlockScope(BlockScopeInstall), p.IgnoreUnreachable.ForBlockScope(BlockScopeUpdate)},
		"allLists":           strs(p.AllLists().Keys()),
		"activeAudit":        strs(p.ActiveAuditFilterListNames()),
		"activeBlockUpdate":  strs(p.ActiveBlockFilterListNames(BlockScopeUpdate)),
		"activeBlockInstall": strs(p.ActiveBlockFilterListNames(BlockScopeInstall)),
		"withSources":        strs(p.CustomListsWithSources().Keys()),
	}
}

// DumpDerived adds the with* variants of p to its dump.
func DumpDerived(p *PolicyConfig) map[string]any {
	out := dumpPolicy(p)
	out["noBlocking"] = dumpPolicy(p.WithBlockingDisabled())
	out["withAuditReport"] = p.WithAudit(AuditReport).Abandoned.Audit
	out["withAuditNull"] = p.WithAudit("").Abandoned.Audit
	out["withSeverity"] = Dump(p.WithIgnoreSeverity([]string{"low", "critical"}).Advisories.IgnoreSeverity)
	u, err := p.WithIgnoreUnreachable("audit")
	if err != nil {
		panic(err)
	}
	out["withUnreachable"] = Dump(u.IgnoreUnreachable)

	return out
}

// AssertDumpEqual fails t unless want and got serialise identically.
func AssertDumpEqual(t *testing.T, want, got any) {
	t.Helper()
	w, _ := json.Marshal(Dump(want))
	g, _ := json.Marshal(Dump(got))
	if string(w) != string(g) {
		t.Errorf("mismatch\nwant %s\n got %s", w, g)
	}
}
