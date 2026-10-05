// Helpers shared by group B's commands (show, outdated, depends, ...):
// access to the Application's process-wide composer.Runtime and Factory,
// and `new PlatformRepository([], $overrides)`.

package command

import (
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util/http"
)

// processRuntime is the process' composer.Runtime: the Application's, or a
// new one for a command used without an Application.
func (c *BaseCommand) processRuntime() *composer.Runtime {
	if app := c.application(); app != nil && app.Runtime() != nil {
		return app.Runtime()
	}

	return composer.NewRuntime("", nil)
}

// processFactory is the Application's Factory (static Factory calls).
func (c *BaseCommand) processFactory() *composer.Factory {
	if app := c.application(); app != nil && app.Factory() != nil {
		return app.Factory()
	}

	return &composer.Factory{Runtime: c.processRuntime()}
}

// newPlatformRepository is `new PlatformRepository([], $overrides)`
// (overrides nil is []).
func (c *BaseCommand) newPlatformRepository(overrides *php.Array) (*repository.PlatformRepository, error) {
	opts, err := c.processRuntime().PlatformOptions(http.NewProcessExecutor(c.IO()))
	if err != nil {
		return nil, err
	}
	if overrides == nil {
		overrides = php.NewArray()
	}

	return repository.NewPlatformRepository(nil, overrides, opts)
}
