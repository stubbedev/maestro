// The view of Composer\Config that IOInterface::loadConfiguration takes.

package config

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

// ForIO adapts c to io.Config, for IOInterface::loadConfiguration: Get
// without flags, and Merge, whose TypeError (only raised for wrongly typed
// settings) the IO has no use for. A Get error (a setting that cannot be
// resolved) reads as null.
func (c *Config) ForIO() io.Config { return ioConfig{c} }

type ioConfig struct{ c *Config }

func (a ioConfig) Get(key string) any {
	v, err := a.c.Get(key, 0)
	if err != nil {
		return nil
	}

	return v
}

func (a ioConfig) Merge(config *php.Array, source string) { _ = a.c.Merge(config, source) }

// Config returns the Config a value of ForIO adapts (for an IO written in
// PHP, whose loadConfiguration() takes the Config itself).
func (a ioConfig) Config() *Config { return a.c }
