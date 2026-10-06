// Command maestro is Composer, natively: a drop-in replacement for the
// composer command, ported from Composer 2.10.3 to Go. It ports
// bin/composer; installed (or symlinked) as `composer` it behaves the
// same, as its name never changes its behaviour.
//
// Version: `maestro --version` prints exactly Composer's first line
// ("Composer version 2.10.3 2026-08-27 13:34:23"), so tools parsing
// `composer --version` keep working, followed on stderr by Composer's PHP
// line and one maestro line ("maestro version X"). Signals: like PHP
// without pcntl handlers, SIGINT/SIGTERM/SIGHUP end the process unless a
// step installed Composer's SignalHandler (process execution, the
// installation manager), which then handles them as Composer does.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/plugin"
)

// version is stamped by the build; see package.nix.
var version = "dev"

func main() {
	stop := startProfiling()
	code := run()
	stop()
	os.Exit(code)
}

// startProfiling starts the profiles the maestro_profile build asks for
// (profile.go) and returns what stops them; a release build profiles
// nothing.
var startProfiling = func() func() { return func() {} }

func run() int {
	rt := composer.NewRuntime(version, nil)
	rt.InstallProcessGlobals()

	// bin/composer's aborts on the PHP Composer would run on. Without php
	// maestro still runs (plugins and scripts then fail when they need it).
	if view, _, err := rt.ComposerView(); err == nil {
		if err := view.CheckBinComposer(); err != nil {
			if ue, ok := errors.AsType[*platform.UnsupportedPHPError](err); ok {
				fmt.Fprint(os.Stdout, ue.Message+php.EOL)

				return 1
			}
		}
	}

	factory := &composer.Factory{Runtime: rt}

	// The plugin runtime (docs/PLUGINS.md): plugins, PHP scripts, and
	// Platform::putEnv('COMPOSER_BINARY', realpath($_SERVER['argv'][0])),
	// the launcher that runs this maestro (D13). PHP starts only when a
	// plugin or script needs it.
	var plugins *plugin.Runtime
	if cfg, err := config.CreateConfig(io.NewNullIO(), ""); err == nil {
		if cacheDir, err := cfg.Get("cache-dir", 0); err == nil {
			if dir, ok := cacheDir.(string); ok && dir != "" {
				if pr, err := plugin.Setup(factory, plugin.Options{CacheDir: dir}); err == nil {
					plugins = pr
				}
			}
		}
	}

	// run the command application
	app := command.NewApplication(factory)
	app.SetAutoExit(false)
	code, err := app.Run(nil, nil)
	if err != nil {
		// SetCatchExceptions(false) is never called, so run() rendered
		// every exception itself; nothing reaches here but a failure to
		// render.
		if plugins != nil {
			plugins.Close()
		}

		return 1
	}
	if code > 255 {
		code = 255
	}

	// PHP exits with the code (its shutdown functions run), and its final
	// status is maestro's.
	if plugins != nil {
		code = plugins.Finish(code)
	}

	return code
}
