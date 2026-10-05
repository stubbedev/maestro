// The view of Composer\Config that the HTTP layer and the VCS utilities
// (internal/util/http, internal/util/vcs) take.

package config

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http"
)

// ForHTTP adapts c to http.Config: Get without flags (a Get error, for a
// setting that cannot be resolved, reads as null, as in ForIO), and the
// config sources as http.ConfigSource (nil for null).
func (c *Config) ForHTTP() http.Config { return httpConfig{c} }

type httpConfig struct{ c *Config }

func (a httpConfig) Get(key string) any { return ioConfig(a).Get(key) }

func (a httpConfig) ProhibitURLByConfig(url string, out io.IO, repoOptions *php.Array) error {
	return a.c.ProhibitURLByConfig(url, out, repoOptions)
}

func (a httpConfig) ConfigSource() http.ConfigSource { return httpSource(a.c.ConfigSource()) }

func (a httpConfig) AuthConfigSource() http.ConfigSource {
	return httpSource(a.c.AuthConfigSource())
}

func (a httpConfig) LocalAuthConfigSource() http.ConfigSource {
	return httpSource(a.c.LocalAuthConfigSource())
}

// httpSource converts s, keeping a nil source nil.
func httpSource(s ConfigSource) http.ConfigSource {
	if s == nil {
		return nil
	}

	return s
}
