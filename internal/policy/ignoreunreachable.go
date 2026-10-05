// Ports src/Composer/Policy/IgnoreUnreachable.php.

package policy

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// IgnoreUnreachableScopes is IgnoreUnreachable::SCOPES. Treat it as
// read-only.
var IgnoreUnreachableScopes = [...]string{"audit", "install", "update"}

// IgnoreUnreachable is IgnoreUnreachable: for which operations unreachable
// repositories and policy sources are silently ignored.
type IgnoreUnreachable struct {
	Audit   bool
	Install bool
	Update  bool
}

// ForBlockScope ports IgnoreUnreachable::forBlockScope.
func (i IgnoreUnreachable) ForBlockScope(blockScope string) bool {
	if blockScope == BlockScopeInstall {
		return i.Install
	}

	return i.Update
}

// IgnoreUnreachableDefault is IgnoreUnreachable::default().
func IgnoreUnreachableDefault() IgnoreUnreachable { return IgnoreUnreachable{false, true, true} }

// IgnoreUnreachableAll is IgnoreUnreachable::all().
func IgnoreUnreachableAll() IgnoreUnreachable { return IgnoreUnreachable{true, true, true} }

// IgnoreUnreachableNone is IgnoreUnreachable::none().
func IgnoreUnreachableNone() IgnoreUnreachable { return IgnoreUnreachable{} }

// With ports IgnoreUnreachable::with: the listed scopes flipped to true.
func (i IgnoreUnreachable) With(scopes ...string) (IgnoreUnreachable, error) {
	if len(scopes) == 0 {
		return i, &util.InvalidArgumentError{Message: "At least one scope is required."}
	}

	for _, scope := range scopes {
		switch scope {
		case "audit":
			i.Audit = true
		case "install":
			i.Install = true
		case "update":
			i.Update = true
		default:
			return i, &util.InvalidArgumentError{Message: `Unknown scope "` + scope + `". Expected one of ` + strings.Join(IgnoreUnreachableScopes[:], ", ") + "."}
		}
	}

	return i, nil
}

// IgnoreUnreachableFromRawPolicyConfig ports
// IgnoreUnreachable::fromRawPolicyConfig.
func IgnoreUnreachableFromRawPolicyConfig(config *php.Array) IgnoreUnreachable {
	v, ok := coalesce(config, "ignore-unreachable")
	if !ok {
		return IgnoreUnreachableDefault()
	}

	if list, ok := v.(*php.Array); ok {
		values := list.Values()
		has := func(s string) bool {
			return slices.ContainsFunc(values, func(v any) bool { return v == s })
		}

		return IgnoreUnreachable{has("audit"), has("install"), has("update")}
	}

	if php.ToBool(v) {
		return IgnoreUnreachableAll()
	}

	return IgnoreUnreachableNone()
}

// IgnoreUnreachableFromRawAuditConfig ports
// IgnoreUnreachable::fromRawAuditConfig.
func IgnoreUnreachableFromRawAuditConfig(auditConfig *php.Array) IgnoreUnreachable {
	if v, ok := coalesce(auditConfig, "ignore-unreachable"); ok && php.ToBool(v) {
		return IgnoreUnreachable{Audit: true}
	}

	return IgnoreUnreachableDefault()
}
