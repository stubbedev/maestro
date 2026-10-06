package command

import (
	"errors"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

// Test hooks of DiagnoseCommand.

// DiagnoseCheckVersion runs checkVersion as Execute does (config with
// secure-http off, the command's HttpDownloader, Composer's view of php),
// so that its result can be checked without diagnose's other network
// checks.
func DiagnoseCheckVersion(c *DiagnoseCommand) (any, error) {
	cfg, err := c.factory().CreateConfig(io.NewNullIO(), "")
	if err != nil {
		return nil, err
	}
	if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("secure-http", false)), config.SourceCommand); err != nil {
		return nil, err
	}
	if c.httpDownloader, err = c.factory().CreateHttpDownloader(c.IO(), cfg, nil); err != nil {
		return nil, err
	}
	if view, _, err := c.runtime().ComposerView(); err == nil {
		c.view = view
	}

	current, ok := c.releaseBuild()
	if !ok {
		return nil, errors.New("not a release build")
	}

	return c.checkVersion(cfg, current)
}
