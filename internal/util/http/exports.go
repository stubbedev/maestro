// Exported faces of http internals for the packages above it (the VCS
// utilities in internal/util/vcs).

package http

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/util"
)

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
