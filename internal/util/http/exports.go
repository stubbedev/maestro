// Exported faces of http internals for the packages above it (the VCS
// utilities in internal/util/vcs).

package http

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// Rawurlencode forwards to php.Rawurlencode, which owns it.
func Rawurlencode(s string) string { return php.Rawurlencode(s) }

// Rawurldecode forwards to php.Rawurldecode, which owns it.
func Rawurldecode(s string) string { return php.Rawurldecode(s) }

// StoreAuthOf converts a store-auths config value (true, false or
// "prompt") into the StoreAuth argument of AuthHelper.StoreAuth.
func StoreAuthOf(v any) StoreAuth { return storeAuthOf(v) }

// NewProcessExecutor is new ProcessExecutor($io) for an io.IO, which may
// be nil.
func NewProcessExecutor(ioi io.IO) *util.ProcessExecutor {
	if ioi == nil { // not a typed nil util.IO
		return util.NewProcessExecutor(nil)
	}

	return util.NewProcessExecutor(ioi)
}

// ConfigList is $config->get($key) for a list of strings (github-domains,
// github-protocols, ...); nil when the value is not an array.
func ConfigList(config Config, key string) []string { return configList(config, key) }

// Urlencode forwards to php.Urlencode, which owns it.
func Urlencode(s string) string { return php.Urlencode(s) }

// HTTPBuildQuery forwards to php.HTTPBuildQuery, which owns it.
func HTTPBuildQuery(pairs ...string) string { return php.HTTPBuildQuery(pairs...) }
