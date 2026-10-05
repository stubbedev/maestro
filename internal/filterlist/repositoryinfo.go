// Ports src/Composer/FilterList/ComposerRepositoryFilterInformation.php.

package filterlist

import (
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/policy"
)

// ComposerRepositoryFilterInformation ports
// Composer\FilterList\ComposerRepositoryFilterInformation: the "filter"
// section of a Composer repository's packages.json.
type ComposerRepositoryFilterInformation struct {
	Metadata bool
	Lists    []string
	// SummaryURL and APIURL are "" for null.
	SummaryURL string
	APIURL     string
}

// ComposerRepositoryFilterInformationFromData ports
// ComposerRepositoryFilterInformation::fromData; canonicalizeURL, when
// not nil, is applied to summary-url and api-url.
func ComposerRepositoryFilterInformationFromData(data *php.Array, canonicalizeURL func(string) string) *ComposerRepositoryFilterInformation {
	var lists []string
	if raw, ok := data.GetArray("lists"); ok {
		for name, config := range raw.All() {
			c, ok := config.(*php.Array)
			if !name.IsString() || !ok {
				continue
			}
			if enabled, _ := c.Get("enabled"); !php.ToBool(enabled) {
				continue
			}
			lists = append(lists, name.String())
		}
	}

	// Repos must not advertise built-in list names or names that collide with
	// future-reserved identifiers; drop them silently so they cannot shadow
	// Composer's own advisory/abandoned handling or claim a future reserved slot.
	lists = slices.DeleteFunc(lists, func(name string) bool {
		if slices.Contains(policy.ReservedNames[:], name) || slices.Contains(policy.FutureReservedNames[:], name) {
			return true
		}

		for _, prefix := range policy.FutureReservedPrefixes {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}

		return false
	})

	url := func(key string) string {
		s, ok := data.GetString(key)
		if !ok || s == "" {
			return ""
		}
		if canonicalizeURL != nil {
			return canonicalizeURL(s)
		}

		return s
	}

	metadata, _ := data.Get("metadata")

	return &ComposerRepositoryFilterInformation{
		Metadata:   php.ToBool(metadata),
		Lists:      lists,
		SummaryURL: url("summary-url"),
		APIURL:     url("api-url"),
	}
}
