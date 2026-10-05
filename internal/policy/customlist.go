// Ports src/Composer/Policy/CustomListPolicyConfig.php.

package policy

import (
	"github.com/stubbedev/maestro/internal/filterlist/source"
	"github.com/stubbedev/maestro/internal/php"
)

// CustomListPolicyConfig is CustomListPolicyConfig: a user-named list,
// optionally fetched from URL sources.
type CustomListPolicyConfig struct {
	ListPolicy
	Sources []*source.URLSource
}

// NewCustomListPolicyConfig is new CustomListPolicyConfig(...).
func NewCustomListPolicyConfig(name string, block bool, audit string, ignore *IgnoreMap, sources []*source.URLSource) *CustomListPolicyConfig {
	return &CustomListPolicyConfig{
		Name: name, Block: block, Audit: audit, Ignore: nonNil(ignore),
		Sources: sources,
	}
}

// ShouldBlock ports shouldBlock.
func (c *CustomListPolicyConfig) ShouldBlock(blockScope string) bool {
	return c.shouldBlock(blockScope, false)
}

// WithBlockingDisabled ports withBlockingDisabled.
func (c *CustomListPolicyConfig) WithBlockingDisabled() *CustomListPolicyConfig {
	return NewCustomListPolicyConfig(c.Name, false, c.Audit, c.Ignore, c.Sources)
}

// WithAudit ports withAudit.
func (c *CustomListPolicyConfig) WithAudit(audit string) *CustomListPolicyConfig {
	return NewCustomListPolicyConfig(c.Name, c.Block, audit, c.Ignore, c.Sources)
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

	ignore, err := ParseIgnoreMap(arrayOf(raw, "ignore"))
	if err != nil {
		return nil, err
	}

	return NewCustomListPolicyConfig(
		listName,
		boolOr(raw, "block", true),
		stringOr(raw, "audit", AuditFail),
		ignore,
		sources,
	), nil
}

// CustomListPolicyConfigDisabled is CustomListPolicyConfig::disabled().
func CustomListPolicyConfigDisabled(listName string) *CustomListPolicyConfig {
	return NewCustomListPolicyConfig(listName, false, AuditIgnore, nil, nil)
}
