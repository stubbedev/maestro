// Ports src/Composer/Policy/CustomListPolicyConfig.php.

package policy

import (
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/filterlist/source"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// CustomListPolicyConfig is CustomListPolicyConfig: a user-named list,
// optionally fetched from URL sources.
type CustomListPolicyConfig struct {
	ListPolicy
	// BlockScope is one of the BlockScope* values: the scopes this list
	// blocks in.
	BlockScope string
	Sources    []*source.URLSource
}

// NewCustomListPolicyConfig is new CustomListPolicyConfig(...).
func NewCustomListPolicyConfig(name string, block bool, audit, blockScope string, ignore *IgnoreMap, sources []*source.URLSource) *CustomListPolicyConfig {
	return &CustomListPolicyConfig{
		ListPolicy: ListPolicy{Name: name, Block: block, Audit: audit, Ignore: nonNil(ignore)}, //nolint:modernize // Go cannot elide the type of a keyed embedded-field literal.
		BlockScope: blockScope,
		Sources:    sources,
	}
}

// ShouldBlock ports shouldBlock: the base rule, narrowed by the list's own
// block-scope. Custom lists opt into install-time filtering.
func (c *CustomListPolicyConfig) ShouldBlock(blockScope string) bool {
	if !c.shouldBlock(blockScope, true) {
		return false
	}

	return c.BlockScope == BlockScopeAll || c.BlockScope == blockScope
}

// WithBlockingDisabled ports withBlockingDisabled.
func (c *CustomListPolicyConfig) WithBlockingDisabled() *CustomListPolicyConfig {
	return NewCustomListPolicyConfig(c.Name, false, c.Audit, c.BlockScope, c.Ignore, c.Sources)
}

// WithAudit ports withAudit.
func (c *CustomListPolicyConfig) WithAudit(audit string) *CustomListPolicyConfig {
	return NewCustomListPolicyConfig(c.Name, c.Block, audit, c.BlockScope, c.Ignore, c.Sources)
}

// CustomListPolicyConfigFromRawConfig ports
// CustomListPolicyConfig::fromRawConfig.
func CustomListPolicyConfigFromRawConfig(listName string, listConfig any) (*CustomListPolicyConfig, error) {
	if listConfig == true {
		listConfig = php.NewArray()
	}

	raw, ok := listConfig.(*php.Array)
	if !ok {
		return CustomListPolicyConfigDisabled(listName), nil
	}

	var sources []*source.URLSource
	for _, sourceConfig := range each(arrayOf(raw, "sources")) {
		if arr, ok := sourceConfig.(*php.Array); ok {
			src, err := source.Validate(listName, arr)
			if err != nil {
				return nil, err
			}
			sources = append(sources, src)
		}
	}

	// Reject unknown values: the global config's schema check only warns, and
	// a typo would silently disable blocking
	blockScope := stringOr(raw, "block-scope", BlockScopeUpdate)
	if !slices.Contains(AvailableBlockScopes[:], blockScope) {
		encoded, err := php.JSONEncode(blockScope, 0)
		if err != nil {
			return nil, err
		}

		return nil, &util.UnexpectedValueError{Message: fmt.Sprintf(
			`Invalid block-scope for policy list "%s": expected one of "%s", got %s.`,
			listName, strings.Join(AvailableBlockScopes[:], `", "`), encoded)}
	}

	ignore, err := ParseIgnoreMap(arrayOf(raw, "ignore"))
	if err != nil {
		return nil, err
	}

	return NewCustomListPolicyConfig(
		listName,
		boolOr(raw, "block", true),
		stringOr(raw, "audit", AuditFail),
		blockScope,
		ignore,
		sources,
	), nil
}

// CustomListPolicyConfigDisabled is CustomListPolicyConfig::disabled().
func CustomListPolicyConfigDisabled(listName string) *CustomListPolicyConfig {
	return NewCustomListPolicyConfig(listName, false, AuditIgnore, BlockScopeUpdate, nil, nil)
}
