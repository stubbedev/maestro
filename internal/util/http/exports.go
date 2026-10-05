// Exported faces of http internals for the packages above it (the VCS
// utilities in internal/util/vcs).

package http

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/util"
)

// Rawurlencode is PHP's rawurlencode.
func Rawurlencode(s string) string { return rawurlencode(s) }

// Rawurldecode is PHP's rawurldecode.
func Rawurldecode(s string) string { return rawurldecode(s) }

// StoreAuthOf converts a store-auths config value (true, false or
// "prompt") into the StoreAuth argument of AuthHelper.StoreAuth.
func StoreAuthOf(v any) StoreAuth { return storeAuthOf(v) }

// NewProcessExecutor is new ProcessExecutor($io) for an io.IO, which may
// be nil.
func NewProcessExecutor(ioi io.IO) *util.ProcessExecutor {
	if ioi == nil {
		return util.NewProcessExecutor(nil)
	}

	return util.NewProcessExecutor(utilIO{ioi})
}

// ConfigList is $config->get($key) for a list of strings (github-domains,
// github-protocols, ...); nil when the value is not an array.
func ConfigList(config Config, key string) []string { return configList(config, key) }

// Urlencode is PHP's urlencode.
func Urlencode(s string) string { return urlencode(s) }

// HTTPBuildQuery is http_build_query($pairs, "", "&") for string values,
// given as key, value, key, value, ...
func HTTPBuildQuery(pairs ...string) string { return httpBuildQuery(pairs...) }
