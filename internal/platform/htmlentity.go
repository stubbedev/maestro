// html_entity_decode() is owned by internal/php.

package platform

import "github.com/stubbedev/maestro/internal/php"

// HTMLEntityDecode forwards to php.HTMLEntityDecode, which owns it.
func HTMLEntityDecode(s string) string { return php.HTMLEntityDecode(s) }
