// Composer's services constructed in PHP (docs/PLUGINS.md §5.3, tier 5's
// `<area>.new`): new Config(), new AutoloadGenerator(), new
// RepositoryManager(), new InstallationManager(), new PluginManager() and
// new DownloadManager() build maestro's object, as Factory builds a
// Composer's, and PHP's object becomes its proxy (adopted), so its methods
// are maestro's.

package plugin

import (
	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// dispatcherParam is a ?EventDispatcher param: maestro's dispatcher, nil
// for null.
func dispatcherParam(a args, i int) (*eventdispatcher.EventDispatcher, error) {
	if !a.has(i) {
		return nil, nil
	}

	return param[*eventdispatcher.EventDispatcher](a, i)
}

func (r *Runtime) registerConstructors() {
	// new Config(bool $useEnvironment = true, ?string $baseDir = null).
	r.Handle("config.new", func(v any) (any, error) {
		a := argsOf("config.new", v)
		baseDir, _ := a.nullableString(2)
		obj, ok := r.configObject(config.New(a.boolean(1), baseDir)).(rpc.Object)
		if !ok {
			return nil, a.errorf("the Config cannot cross")
		}

		return nil, r.adoptAs(a, obj)
	})

	// new AutoloadGenerator(EventDispatcher $eventDispatcher, ?IOInterface $io = null).
	r.Handle("ag.new", func(v any) (any, error) {
		a := argsOf("ag.new", v)
		ed, err := param[*eventdispatcher.EventDispatcher](a, 1)
		if err != nil {
			return nil, err
		}
		out, _, err := r.ioParam(a, 2)
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, autoload.NewGenerator(ed, out))
	})

	// new RepositoryManager(IOInterface $io, Config $config, HttpDownloader
	// $httpDownloader, ?EventDispatcher $eventDispatcher = null,
	// ?ProcessExecutor $process = null): $process ?? new
	// ProcessExecutor($io), maestro's standing for a given one
	// (processExecutorOf).
	r.Handle("rm.new", func(v any) (any, error) {
		a := argsOf("rm.new", v)
		out, _, err := r.ioParam(a, 1)
		if err != nil {
			return nil, err
		}
		cfg, err := param[*config.Config](a, 2)
		if err != nil {
			return nil, err
		}
		hd, err := param[*http.HttpDownloader](a, 3)
		if err != nil {
			return nil, err
		}
		ed, err := dispatcherParam(a, 4)
		if err != nil {
			return nil, err
		}
		var dispatcher repository.EventDispatcher
		if ed != nil {
			dispatcher = ed
		}

		process, err := r.processParam(a, 5, out)
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, repository.NewRepositoryManager(out, cfg, hd, dispatcher, process))
	})

	// new InstallationManager(Loop $loop, IOInterface $io, ?EventDispatcher $eventDispatcher = null).
	r.Handle("im.new", func(v any) (any, error) {
		a := argsOf("im.new", v)
		loop, err := param[*http.Loop](a, 1)
		if err != nil {
			return nil, err
		}
		out, _, err := r.ioParam(a, 2)
		if err != nil {
			return nil, err
		}
		ed, err := dispatcherParam(a, 3)
		if err != nil {
			return nil, err
		}
		var dispatcher installer.EventDispatcher
		if ed != nil {
			dispatcher = ed
		}

		return nil, r.adopt(a, installer.NewManager(loop, out, dispatcher))
	})

	// new PluginManager(IOInterface $io, Composer $composer,
	// ?PartialComposer $globalComposer = null, $disablePlugins = false):
	// maestro's manager, with its allow-plugins rules; PHP's object gets
	// the manager's properties as maestro's managers' mirrors do.
	r.Handle("pm.new", func(v any) (any, error) {
		a := argsOf("pm.new", v)
		out, _, err := r.ioParam(a, 1)
		if err != nil {
			return nil, err
		}
		c, err := param[*composer.Composer](a, 2)
		if err != nil {
			return nil, err
		}
		var global *composer.PartialComposer
		if a.has(3) {
			if global, err = partialComposer(args{method: a.method, list: []any{a.at(3)}}); err != nil {
				return nil, err
			}
		}
		m, err := NewManager(r, out, c, global, disablePluginsArg(a, 4))
		if err != nil {
			return nil, err
		}
		if err := r.adoptAs(a, m); err != nil {
			return nil, err
		}
		m.bump()

		return nil, nil
	})

	// new DownloadManager(IOInterface $io, bool $preferSource = false,
	// ?Filesystem $filesystem = null): Filesystem is maestro's in any case.
	r.Handle("dm.new", func(v any) (any, error) {
		a := argsOf("dm.new", v)
		out, _, err := r.ioParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, downloader.NewDownloadManager(out, a.boolean(2), util.NewFilesystem(nil)))
	})
}
