// Wiring the plugin runtime into a Composer Factory (docs/PLUGINS.md §5.2,
// §5.4, §5.5): what cmd/maestro does at startup.

package plugin

import (
	"maps"
	"os"
	"slices"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// Setup creates the process's plugin runtime for the Composer instances f
// creates and installs it in f: the plugin manager
// (Factory.CreatePluginManagerFunc), the runtime PHP listeners and scripts
// run in (Factory.ScriptRuntime), the COMPOSER_BINARY launcher
// (Factory.EnsureComposerBinary), and the InstalledVersions reloads of the
// local repositories. opts.CacheDir is required; the statics, the platform
// snapshot and the InstalledVersions data default to f.Runtime's.
//
// As bin/composer does, it sets COMPOSER_BINARY (and MAESTRO_BINARY for
// the launcher) and, when Composer would restart php without xdebug, the
// variables of the restarted process (COMPOSER_ORIGINAL_INIS,
// XDEBUG_HANDLER_SETTINGS, no COMPOSER_ALLOW_XDEBUG), which scripts see.
//
// The caller ends the run with Finish.
func Setup(f *composer.Factory, opts Options) (*Runtime, error) {
	crt := f.Runtime
	if crt == nil {
		crt = composer.NewRuntime("", nil)
		f.Runtime = crt
	}

	if opts.Statics == nil {
		opts.Statics = ComposerStatics(crt)
	}
	if opts.Snapshot == nil {
		opts.Snapshot = crt.Detector().Snapshot
	}
	if opts.InstalledVersions == nil {
		opts.InstalledVersions = crt.InstalledVersions
	}
	if opts.PlatformPHPVersion == nil {
		opts.PlatformPHPVersion = crt.PlatformPHPVersion
	}

	if err := SetProcessEnv(opts.CacheDir); err != nil {
		return nil, err
	}

	rt := New(opts)
	rt.composerFactory = f
	if err := rt.exportRestartEnv(); err != nil {
		return nil, err
	}

	f.ScriptRuntime = rt
	f.EnsureComposerBinary = rt.EnsureComposerBinary
	f.CreatePluginManagerFunc = func(out io.IO, c *composer.Composer, globalComposer *composer.PartialComposer, disablePlugins composer.DisablePlugins) (composer.PluginManager, error) {
		return NewManager(rt, out, c, globalComposer, disablePlugins)
	}
	crt.SetInstalledVersionsSink(func(versions *php.Array, repoDir string) {
		// A failure here is the PHP process ending, which the next call
		// reports.
		_ = rt.ReloadInstalledVersions(versions, repoDir)
	})

	return rt, nil
}

// ComposerStatics are Composer's statics as composer.Runtime holds them
// (Composer::$runningCommand and $runningOperation), plus
// ProcessExecutor's timeout.
func ComposerStatics(crt *composer.Runtime) map[string]rpc.Static {
	statics := DefaultStatics()
	statics["runningCommand"] = rpc.Static{
		Get: func() any {
			if v, ok := crt.RunningCommand(); ok {
				return v
			}

			return nil
		},
		Set: func(v any) {
			s, ok := v.(string)
			op, opOK := crt.RunningOperation()
			crt.SetRunningCommand(s, ok)
			// PHP set the command alone: the operation it had stays (the
			// reset of setRunningCommand() arrives as its own change).
			if opOK {
				crt.SetRunningOperation(op, true)
			}
		},
	}
	statics["runningOperation"] = rpc.Static{
		Get: func() any {
			if v, ok := crt.RunningOperation(); ok {
				return v
			}

			return nil
		},
		Set: func(v any) {
			s, ok := v.(string)
			crt.SetRunningOperation(s, ok)
		},
	}

	return statics
}

// exportRestartEnv puts the variables of bin/composer's xdebug restart in
// maestro's environment, as they are in the restarted Composer process
// (HANDOFF: scripts see them).
func (r *Runtime) exportRestartEnv() error {
	r.startMu.Lock()
	defer r.startMu.Unlock()

	if err := r.planRestart(); err != nil {
		if _, ok := err.(*platform.PHPNotFoundError); ok { //nolint:errorlint // the detector returns it as is.
			return nil
		}

		return err
	}
	if r.restart == nil {
		return nil
	}
	for _, name := range r.restart.unset {
		if err := os.Unsetenv(name); err != nil {
			return err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(r.restart.env)) {
		if err := os.Setenv(name, r.restart.env[name]); err != nil {
			return err
		}
	}

	return nil
}

// Finish ends the run with exit code code (docs/PLUGINS.md §5.2): a
// running PHP child exits with it, running its shutdown functions, and its
// final status is maestro's. Without a child it is code.
func (r *Runtime) Finish(code int) int {
	final, err := r.Shutdown(code)
	if err != nil {
		r.Close()

		return code
	}
	r.Close()

	return final
}
