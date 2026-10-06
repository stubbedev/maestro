// The Composer object graph's service methods (docs/PLUGINS.md §4.2, §4.5,
// §4.7, §4.9, §6.6): `composer.*`, `config.*`, `factory.*`, `locker.*`,
// `im.*` and `ag.*`.

package plugin

import (
	"os"

	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/archiver"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util/http"
)

// partialComposer returns the PartialComposer part of a Composer object
// (receiver of the PartialComposer methods).
func partialComposer(a args) (*composer.PartialComposer, error) {
	switch v := serviceValue(a.at(0)).(type) {
	case *composer.Composer:
		return v.Partial(), nil
	case *composer.PartialComposer:
		return v, nil
	}

	return nil, a.errorf("param 0 is not a Composer instance maestro knows")
}

// serviceValue returns the Go object of a service param.
func serviceValue(v any) any { return unwrap(v) }

func (r *Runtime) registerComposer() {
	partial := func(method string, fn func(c *composer.PartialComposer, a args) (any, error)) {
		r.Handle("composer."+method, func(v any) (any, error) {
			a := argsOf("composer."+method, v)
			c, err := partialComposer(a)
			if err != nil {
				return nil, err
			}

			return fn(c, a)
		})
	}
	full := func(method string, fn func(c *composer.Composer, a args) (any, error)) {
		r.Handle("composer."+method, func(v any) (any, error) {
			a := argsOf("composer."+method, v)
			c, err := receiver[*composer.Composer](a)
			if err != nil {
				return nil, err
			}

			return fn(c, a)
		})
	}

	partial("getPackage", func(c *composer.PartialComposer, _ args) (any, error) { return r.value(c.Package()), nil })
	partial("getConfig", func(c *composer.PartialComposer, _ args) (any, error) { return r.value(c.Config()), nil })
	partial("getLoop", func(c *composer.PartialComposer, _ args) (any, error) { return r.value(c.Loop()), nil })
	partial("getRepositoryManager", func(c *composer.PartialComposer, _ args) (any, error) {
		return r.value(c.RepositoryManager()), nil
	})
	partial("getInstallationManager", func(c *composer.PartialComposer, _ args) (any, error) {
		return r.value(c.InstallationManager()), nil
	})
	partial("getEventDispatcher", func(c *composer.PartialComposer, _ args) (any, error) {
		return r.value(c.EventDispatcher()), nil
	})
	partial("setLoop", func(c *composer.PartialComposer, a args) (any, error) {
		loop, err := param[*http.Loop](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetLoop(loop)

		return nil, nil
	})
	partial("isGlobal", func(c *composer.PartialComposer, _ args) (any, error) { return c.IsGlobal(), nil })
	partial("setGlobal", func(c *composer.PartialComposer, _ args) (any, error) { c.SetGlobal(); return nil, nil })
	partial("setPackage", func(c *composer.PartialComposer, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		root, ok := p.(pkg.RootPackageInterface)
		if !ok {
			return nil, a.errorf("param 1 is not a root package")
		}
		c.SetPackage(root)

		return nil, nil
	})
	partial("setConfig", func(c *composer.PartialComposer, a args) (any, error) {
		cfg, err := param[*config.Config](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetConfig(cfg)

		return nil, nil
	})
	partial("setRepositoryManager", func(c *composer.PartialComposer, a args) (any, error) {
		rm, err := param[*repository.RepositoryManager](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetRepositoryManager(rm)

		return nil, nil
	})
	partial("setInstallationManager", func(c *composer.PartialComposer, a args) (any, error) {
		im, err := param[composer.InstallationManager](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetInstallationManager(im)

		return nil, nil
	})
	partial("setEventDispatcher", func(c *composer.PartialComposer, a args) (any, error) {
		ed, err := param[*eventdispatcher.EventDispatcher](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetEventDispatcher(ed)

		return nil, nil
	})
	full("getLocker", func(c *composer.Composer, _ args) (any, error) { return r.value(c.Locker()), nil })
	full("getDownloadManager", func(c *composer.Composer, _ args) (any, error) { return r.value(c.DownloadManager()), nil })
	full("getPluginManager", func(c *composer.Composer, _ args) (any, error) {
		pm := c.PluginManager()
		if pm == nil {
			return nil, nil
		}
		if m, ok := pm.(*Manager); ok {
			return m, nil
		}

		return r.serviceObject(pm, `Composer\Plugin\PluginManager`), nil
	})
	full("getAutoloadGenerator", func(c *composer.Composer, _ args) (any, error) {
		return r.value(c.AutoloadGenerator()), nil
	})
	full("getArchiveManager", func(c *composer.Composer, _ args) (any, error) { return r.value(c.ArchiveManager()), nil })
	full("setLocker", func(c *composer.Composer, a args) (any, error) {
		l, err := param[*locker.Locker](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetLocker(l)

		return nil, nil
	})
	full("setDownloadManager", func(c *composer.Composer, a args) (any, error) {
		dm, err := param[*downloader.DownloadManager](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetDownloadManager(dm)

		return nil, nil
	})
	full("setArchiveManager", func(c *composer.Composer, a args) (any, error) {
		am, err := param[*archiver.ArchiveManager](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetArchiveManager(am)

		return nil, nil
	})
	full("setAutoloadGenerator", func(c *composer.Composer, a args) (any, error) {
		g, err := param[*autoload.Generator](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetAutoloadGenerator(g)

		return nil, nil
	})
	full("setPluginManager", func(c *composer.Composer, a args) (any, error) {
		m, ok := a.at(1).(*Manager)
		if !ok {
			return nil, a.errorf("param 1 is not a plugin manager maestro knows")
		}
		c.SetPluginManager(m)

		return nil, nil
	})

	r.registerConfig()
	r.registerLocker()
	r.registerInstallationManager()
	r.registerAutoloadGenerator()

	r.registerFactory()

	// Factory's protected static directory helpers: maestro's ports, on
	// the environment PHP's putenv() reached.
	r.Handle("factory.getHomeDir", func(any) (any, error) { return config.HomeDir() })
	r.Handle("factory.getCacheDir", func(v any) (any, error) {
		return config.CacheDir(argsOf("factory.getCacheDir", v).str(0))
	})
	r.Handle("factory.getDataDir", func(v any) (any, error) {
		return config.DataDir(argsOf("factory.getDataDir", v).str(0))
	})
	r.Handle("factory.getComposerFile", func(any) (any, error) { return composer.GetComposerFile() })
	r.Handle("factory.getLockFile", func(v any) (any, error) {
		a := argsOf("factory.getLockFile", v)

		return composer.GetLockFile(a.str(0)), nil
	})
}

func (r *Runtime) registerConfig() {
	method := func(name string, fn func(c *config.Config, a args) (any, error)) {
		r.Handle("config."+name, func(v any) (any, error) {
			a := argsOf("config."+name, v)
			c, err := receiver[*config.Config](a)
			if err != nil {
				return nil, err
			}

			return fn(c, a)
		})
	}

	method("get", func(c *config.Config, a args) (any, error) { return c.Get(a.str(1), a.integer(2)) })
	method("all", func(c *config.Config, a args) (any, error) { return c.All(a.integer(1)) })
	method("raw", func(c *config.Config, _ args) (any, error) { return c.Raw(), nil })
	method("has", func(c *config.Config, a args) (any, error) { return c.Has(a.str(1)), nil })
	method("merge", func(c *config.Config, a args) (any, error) {
		source := "unknown"
		if a.has(2) {
			source = a.str(2)
		}

		return nil, c.Merge(a.arrayOrEmpty(1), source)
	})
	method("getRepositories", func(c *config.Config, _ args) (any, error) { return c.Repositories(), nil })
	method("getSourceOfValue", func(c *config.Config, a args) (any, error) { return c.SourceOfValue(a.str(1)) })
	method("getConfigSource", func(c *config.Config, _ args) (any, error) {
		return r.configSourceObject(c.ConfigSource()), nil
	})
	method("getAuthConfigSource", func(c *config.Config, _ args) (any, error) {
		return r.configSourceObject(c.AuthConfigSource()), nil
	})
	method("getLocalAuthConfigSource", func(c *config.Config, _ args) (any, error) {
		return r.configSourceObject(c.LocalAuthConfigSource()), nil
	})
	method("setConfigSource", func(c *config.Config, a args) (any, error) {
		s, err := param[config.ConfigSource](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetConfigSource(s)

		return nil, nil
	})
	method("setAuthConfigSource", func(c *config.Config, a args) (any, error) {
		s, err := param[config.ConfigSource](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetAuthConfigSource(s)

		return nil, nil
	})
	method("setLocalAuthConfigSource", func(c *config.Config, a args) (any, error) {
		s, err := param[config.ConfigSource](a, 1)
		if err != nil {
			return nil, err
		}
		c.SetLocalAuthConfigSource(s)

		return nil, nil
	})
	method("setBaseDir", func(c *config.Config, a args) (any, error) { c.SetBaseDir(a.str(1)); return nil, nil })
	method("prohibitUrlByConfig", func(c *config.Config, a args) (any, error) {
		out, _, err := r.ioParam(a, 2)
		if err != nil {
			return nil, err
		}

		return nil, c.ProhibitURLByConfig(a.str(1), out, a.arrayOrEmpty(3))
	})
}

// configSourceObject returns a config source as PHP holds it: a
// JsonConfigSource proxy (its methods come with the write APIs).
func (r *Runtime) configSourceObject(s config.ConfigSource) any {
	if s == nil || !hashable(s) {
		return nil
	}

	return r.serviceObject(s, `Composer\Config\JsonConfigSource`)
}

func (r *Runtime) registerLocker() {
	method := func(name string, fn func(l *locker.Locker, a args) (any, error)) {
		r.Handle("locker."+name, func(v any) (any, error) {
			a := argsOf("locker."+name, v)
			l, err := receiver[*locker.Locker](a)
			if err != nil {
				return nil, err
			}

			return fn(l, a)
		})
	}

	// new Locker($io, new JsonFile($path), $im, $contents) in PHP.
	r.Handle("locker.new", func(v any) (any, error) {
		a := argsOf("locker.new", v)
		out, ok, err := r.ioParam(a, 1)
		if err != nil {
			return nil, err
		}
		var fio io.IO
		if ok {
			fio = out
		}
		file, err := json.NewFile(a.str(2), nil, fio)
		if err != nil {
			return nil, err
		}
		im, err := param[repository.InstallationManager](a, 3)
		if err != nil {
			return nil, err
		}
		// $process ?? new ProcessExecutor($io)
		process, err := r.processParam(a, 5, out)
		if err != nil {
			return nil, err
		}
		l, err := locker.New(out, file, im, a.str(4), process)
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, l)
	})
	method("getJsonFile", func(l *locker.Locker, _ args) (any, error) { return l.JSONFile().Path(), nil })
	method("setLockData", func(l *locker.Locker, a args) (any, error) {
		packages, err := packagesParam(a, 1)
		if err != nil {
			return nil, err
		}
		in := locker.LockDataInput{
			Packages:          packages,
			PlatformReqs:      a.arrayOrEmpty(3),
			PlatformDevReqs:   a.arrayOrEmpty(4),
			Aliases:           a.arrayOrEmpty(5),
			MinimumStability:  a.str(6),
			StabilityFlags:    a.arrayOrEmpty(7),
			PreferStable:      a.boolean(8),
			PreferLowest:      a.boolean(9),
			PlatformOverrides: a.arrayOrEmpty(10),
		}
		if a.has(2) {
			dev, err := packagesParam(a, 2)
			if err != nil {
				return nil, err
			}
			in.DevPackages = append([]pkg.PackageInterface{}, dev...)
		}

		return l.SetLockData(in, !a.has(11) || a.boolean(11))
	})
	// updateHash($composerJson, $dataProcessor): a PHP processor runs
	// first, on the data maestro will write, so that its exception leaves
	// the lock file untouched, as in Composer.
	method("updateHash", func(l *locker.Locker, a args) (any, error) {
		var processor func(*php.Array) *php.Array
		if a.has(2) {
			contents, err := os.ReadFile(a.str(1))
			if err != nil {
				return nil, l.UpdateHash(a.str(1), nil)
			}
			data, err := l.JSONFile().Read()
			if err != nil {
				return nil, err
			}
			lockData, _ := data.(*php.Array)
			if lockData == nil {
				lockData = php.NewArray()
			}
			hash, err := locker.GetContentHash(string(contents))
			if err != nil {
				return nil, err
			}
			lockData.Set("content-hash", hash)
			v, err := r.Call("callable.invoke", php.ArrayOf("callable", a.at(2), "args", php.ListOf(lockData)))
			if err != nil {
				return nil, err
			}
			processed, _ := v.(*php.Array)
			if processed == nil {
				processed = php.NewArray()
			}
			processor = func(*php.Array) *php.Array { return processed }
		}

		return nil, l.UpdateHash(a.str(1), processor)
	})
	method("isLocked", func(l *locker.Locker, _ args) (any, error) { return l.IsLocked() })
	method("isFresh", func(l *locker.Locker, _ args) (any, error) { return l.IsFresh() })
	method("getLockData", func(l *locker.Locker, _ args) (any, error) { return l.LockData() })
	method("getLockedRepository", func(l *locker.Locker, a args) (any, error) {
		repo, err := l.LockedRepository(a.boolean(1))
		if err != nil {
			return nil, err
		}

		return r.repositoryObject(repo), nil
	})
	method("getDevPackageNames", func(l *locker.Locker, _ args) (any, error) {
		names, err := l.DevPackageNames()

		return php.StringList(names), err
	})
	method("getPlatformRequirements", func(l *locker.Locker, a args) (any, error) {
		links, err := l.PlatformRequirements(a.boolean(1))
		if err != nil {
			return nil, err
		}

		return linksValue(links), nil
	})
	method("getMinimumStability", func(l *locker.Locker, _ args) (any, error) { return l.MinimumStability() })
	method("getStabilityFlags", func(l *locker.Locker, _ args) (any, error) { return l.StabilityFlags() })
	method("getPreferStable", func(l *locker.Locker, _ args) (any, error) {
		v, ok, err := l.PreferStable()
		if err != nil || !ok {
			return nil, err
		}

		return v, nil
	})
	method("getPreferLowest", func(l *locker.Locker, _ args) (any, error) {
		v, ok, err := l.PreferLowest()
		if err != nil || !ok {
			return nil, err
		}

		return v, nil
	})
	method("getPlatformOverrides", func(l *locker.Locker, _ args) (any, error) { return l.PlatformOverrides() })
	method("getAliases", func(l *locker.Locker, _ args) (any, error) { return l.Aliases() })
	method("getPluginApi", func(l *locker.Locker, _ args) (any, error) { return l.PluginAPI() })
	method("getMissingRequirementInfo", func(l *locker.Locker, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		root, ok := p.(pkg.RootPackageInterface)
		if !ok {
			return nil, a.errorf("param 1 is not a root package")
		}
		lines, err := l.MissingRequirementInfo(root, a.boolean(2))

		return php.StringList(lines), err
	})
	r.Handle("locker.getContentHash", func(v any) (any, error) {
		a := argsOf("locker.getContentHash", v)

		return locker.GetContentHash(a.str(0))
	})
}

func (r *Runtime) registerInstallationManager() {
	method := func(name string, fn func(im composer.InstallationManager, a args) (any, error)) {
		r.Handle("im."+name, func(v any) (any, error) {
			a := argsOf("im."+name, v)
			im, err := receiver[composer.InstallationManager](a)
			if err != nil {
				return nil, err
			}

			return fn(im, a)
		})
	}

	method("getInstallPath", func(im composer.InstallationManager, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}
		path, ok, err := im.InstallPath(p)
		if err != nil || !ok {
			return nil, err
		}

		return path, nil
	})
	method("isPackageInstalled", func(im composer.InstallationManager, a args) (any, error) {
		repo, err := param[repository.InstalledRepositoryInterface](a, 1)
		if err != nil {
			return nil, err
		}
		p, err := packageParam(a, 2)
		if err != nil {
			return nil, err
		}

		return im.IsPackageInstalled(repo, p)
	})
	method("ensureBinariesPresence", func(im composer.InstallationManager, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, im.EnsureBinariesPresence(p)
	})
	method("setOutputProgress", func(im composer.InstallationManager, a args) (any, error) {
		if s, ok := im.(interface{ SetOutputProgress(bool) }); ok {
			s.SetOutputProgress(a.boolean(1))
		}

		return nil, nil
	})
	method("notifyInstalls", func(im composer.InstallationManager, a args) (any, error) {
		out, _, err := r.ioParam(a, 1)
		if err != nil {
			return nil, err
		}
		im.NotifyInstalls(out)

		return nil, nil
	})
	method("disablePlugins", func(im composer.InstallationManager, _ args) (any, error) {
		return nil, im.DisablePlugins()
	})
}

func (r *Runtime) registerAutoloadGenerator() {
	method := func(name string, fn func(g *autoload.Generator, a args) (any, error)) {
		r.Handle("ag."+name, func(v any) (any, error) {
			a := argsOf("ag."+name, v)
			g, err := receiver[*autoload.Generator](a)
			if err != nil {
				return nil, err
			}

			return fn(g, a)
		})
	}

	// The protected methods a subclass calls on itself that are maestro's
	// (the others are Composer's code in the shim).
	method("getIO", func(g *autoload.Generator, _ args) (any, error) { return r.value(g.IO()), nil })
	method("sortPackageMap", func(g *autoload.Generator, a args) (any, error) {
		entries, err := packageMapParam(a)
		if err != nil {
			return nil, err
		}

		return r.packageMapValue(autoload.SortPackageMap(entries)), nil
	})
	method("parseAutoloadsType", func(g *autoload.Generator, a args) (any, error) {
		entries, err := packageMapParam(a)
		if err != nil {
			return nil, err
		}
		root, err := packageParam(a, 3)
		if err != nil {
			return nil, err
		}
		typ := a.str(2)
		al, err := g.ParseAutoloadsType(entries, typ, root)
		if err != nil {
			return nil, err
		}
		v, _ := autoloadsValue(al).Get(typ)
		if v == nil {
			return php.NewArray(), nil
		}

		return v, nil
	})
	method("getAutoloadFile", func(_ *autoload.Generator, a args) (any, error) {
		return autoload.AutoloadFile(a.str(1), a.str(2)), nil
	})
	method("getAutoloadRealFile", func(g *autoload.Generator, a args) (any, error) {
		targetDirLoader, _ := a.nullableString(3)
		// $prependAutoloader is the code Composer writes: 'true' or
		// 'false'.
		return g.AutoloadRealFile(a.boolean(2), targetDirLoader, a.boolean(4), a.str(7), a.boolean(8), a.str(9) == "true", a.boolean(10)), nil
	})
	method("getPlatformCheck", func(g *autoload.Generator, a args) (any, error) {
		entries, err := packageMapParam(a)
		if err != nil {
			return nil, err
		}
		code, err := g.PlatformCheck(entries, a.at(2), stringList(a.at(3)))
		if err != nil || code == "" {
			return nil, err
		}

		return code, nil
	})

	// setPlatformRequirementFilter($filter): the filter crosses as its
	// description (ServiceAdapter::describeFilter()).
	method("setPlatformRequirementFilter", func(g *autoload.Generator, a args) (any, error) {
		f, err := r.filterFromPHP(a.at(1))
		if err != nil {
			return nil, err
		}
		g.SetPlatformRequirementFilter(f)

		return nil, nil
	})
	// dump($config, $localRepo, $rootPackage, $installationManager,
	// $targetDir, $scanPsrPackages, $suffix, $locker, $strictAmbiguous):
	// maestro writes the autoloader (dispatching the autoload-dump events)
	// and returns the class map's contents, from which PHP builds
	// Composer's ClassMap.
	method("dump", func(g *autoload.Generator, a args) (any, error) {
		cfg, err := param[*config.Config](a, 1)
		if err != nil {
			return nil, err
		}
		repo, err := goRepositoryParam(a, 2)
		if err != nil {
			return nil, err
		}
		localRepo, ok := repo.(repository.InstalledRepositoryInterface)
		if !ok {
			return nil, a.errorf("param 2 is not an installed repository")
		}
		p, err := packageParam(a, 3)
		if err != nil {
			return nil, err
		}
		root, ok := p.(pkg.RootPackageInterface)
		if !ok {
			return nil, a.errorf("param 3 is not a root package")
		}
		im, err := param[autoload.InstallationManager](a, 4)
		if err != nil {
			return nil, err
		}
		var l autoload.Locker
		if a.has(8) {
			if l, err = param[*locker.Locker](a, 8); err != nil {
				return nil, err
			}
		}
		suffix, _ := a.nullableString(7)
		adapted := &installedRepoForAutoload{repo: localRepo}
		classMap, err := g.Dump(cfg, adapted, root, im, a.str(5), a.boolean(6), suffix, l, a.boolean(9))
		if err == nil {
			err = adapted.err
		}
		if err != nil {
			return nil, err
		}

		return classMapValue(classMap)
	})

	method("setDevMode", func(g *autoload.Generator, a args) (any, error) { g.SetDevMode(a.boolean(1)); return nil, nil })
	method("setClassMapAuthoritative", func(g *autoload.Generator, a args) (any, error) {
		g.SetClassMapAuthoritative(a.boolean(1))
		return nil, nil
	})
	method("setApcu", func(g *autoload.Generator, a args) (any, error) {
		var prefix *string
		if s, ok := a.nullableString(2); ok {
			prefix = &s
		}
		g.SetApcu(a.boolean(1), prefix)

		return nil, nil
	})
	method("setRunScripts", func(g *autoload.Generator, a args) (any, error) { g.SetRunScripts(a.boolean(1)); return nil, nil })
	method("setDryRun", func(g *autoload.Generator, a args) (any, error) { g.SetDryRun(a.boolean(1)); return nil, nil })
	method("buildPackageMap", func(g *autoload.Generator, a args) (any, error) {
		im, err := param[composer.InstallationManager](a, 1)
		if err != nil {
			return nil, err
		}
		root, err := packageParam(a, 2)
		if err != nil {
			return nil, err
		}
		packages, err := packagesParam(a, 3)
		if err != nil {
			return nil, err
		}
		entries, err := g.BuildPackageMap(im, root, packages)
		if err != nil {
			return nil, err
		}

		return r.packageMapValue(entries), nil
	})
	method("parseAutoloads", func(g *autoload.Generator, a args) (any, error) {
		entries, err := packageMapParam(a)
		if err != nil {
			return nil, err
		}
		root, err := packageParam(a, 2)
		if err != nil {
			return nil, err
		}
		filter := autoload.NoDevFilter
		switch v := a.at(3).(type) {
		case bool:
			if v {
				filter = autoload.LegacyDevFilter
			}
		case *php.Array:
			var names []string
			for _, n := range v.Values() {
				names = append(names, php.ToString(n))
			}
			filter = autoload.DevPackageNames(names)
		}
		autoloads, err := g.ParseAutoloads(entries, root, filter)
		if err != nil {
			return nil, err
		}

		return autoloadsValue(autoloads), nil
	})
	method("classMap", func(g *autoload.Generator, a args) (any, error) {
		var dirs, excluded []string
		for _, v := range a.arrayOrEmpty(1).Values() {
			dirs = append(dirs, php.ToString(v))
		}
		for _, v := range a.arrayOrEmpty(2).Values() {
			excluded = append(excluded, php.ToString(v))
		}
		loader, err := g.CreateLoader(&autoload.Autoloads{
			PSR0:                php.NewArray(),
			PSR4:                php.NewArray(),
			Classmap:            dirs,
			Files:               php.NewArray(),
			ExcludeFromClassmap: excluded,
		}, "")
		if err != nil {
			return nil, err
		}

		return loader.ClassMap, nil
	})
}

// packageMapValue is a package map as PHP holds it: [[package, path], ...]
// with null for a package that is not installed.
func (r *Runtime) packageMapValue(entries []autoload.PackageMapEntry) *php.Array {
	list := php.NewArrayCap(len(entries))
	for _, e := range entries {
		var path any
		if e.Installed {
			path = e.InstallPath
		}
		list.Append(php.ListOf(r.packageObject(e.Package), path))
	}

	return list
}

// packageMapParam returns param 1, a package map.
func packageMapParam(a args) ([]autoload.PackageMapEntry, error) {
	list := a.arrayOrEmpty(1)
	entries := make([]autoload.PackageMapEntry, 0, list.Len())
	for _, item := range list.Values() {
		pair, ok := item.(*php.Array)
		if !ok || pair.Len() < 2 {
			return nil, a.errorf("param 1 is not a package map")
		}
		pv, _ := pair.Get(0)
		m, ok := pv.(*packageMirror)
		if !ok {
			return nil, a.errorf("param 1 holds a %T, not a package maestro knows", pv)
		}
		path, _ := pair.Get(1)
		entries = append(entries, autoload.PackageMapEntry{Package: m.p, InstallPath: php.ToString(path), Installed: path != nil})
	}

	return entries, nil
}

// autoloadsValue is parseAutoloads' result as PHP holds it.
func autoloadsValue(a *autoload.Autoloads) *php.Array {
	return php.ArrayOf(
		"psr-0", orEmptyArray(a.PSR0),
		"psr-4", orEmptyArray(a.PSR4),
		"classmap", php.StringList(a.Classmap),
		"files", orEmptyArray(a.Files),
		"exclude-from-classmap", php.StringList(a.ExcludeFromClassmap),
	)
}

// disablePluginsArg is a `bool|'local'|'global' $disablePlugins` param.
func disablePluginsArg(a args, i int) composer.DisablePlugins {
	switch v := a.at(i).(type) {
	case string:
		switch v {
		case "local":
			return composer.PluginsDisabledLocal
		case "global":
			return composer.PluginsDisabledGlobal
		}
	}
	if a.boolean(i) {
		return composer.PluginsDisabled
	}

	return composer.PluginsEnabled
}

// localConfigArg is a `string|array|null $localConfig` param as the
// Factory takes it.
func localConfigArg(a args, i int) any {
	switch v := a.at(i).(type) {
	case string, *php.Array:
		return v
	}

	return nil
}

// ioArg is the IO param (the first of the Factory methods) as maestro
// uses it; maestro's null IO for null.
func (r *Runtime) ioArg(a args) (io.IO, error) {
	out, ok, err := r.ioParam(a, 0)
	if err != nil {
		return nil, err
	}
	if !ok {
		return io.NewNullIO(), nil
	}

	return out, nil
}

// registerFactory registers the Factory methods that create Composer
// instances (docs/PLUGINS.md §5.11): maestro builds them, re-entrantly,
// loading their plugins in this same PHP process.
func (r *Runtime) registerFactory() {
	r.Handle("factory.createComposer", func(v any) (any, error) {
		a := argsOf("factory.createComposer", v)
		out, err := r.ioArg(a)
		if err != nil {
			return nil, err
		}
		cwd, _ := a.nullableString(3)
		fullLoad := !a.has(4) || a.boolean(4)
		if !fullLoad {
			c, err := r.factory().CreatePartialComposer(out, localConfigArg(a, 1), disablePluginsArg(a, 2), cwd, a.boolean(5))
			if err != nil {
				return nil, err
			}

			return r.value(c), nil
		}
		c, err := r.factory().CreateComposer(out, localConfigArg(a, 1), disablePluginsArg(a, 2), cwd, a.boolean(5))
		if err != nil {
			return nil, err
		}

		return r.value(c), nil
	})
	r.Handle("factory.create", func(v any) (any, error) {
		a := argsOf("factory.create", v)
		out, err := r.ioArg(a)
		if err != nil {
			return nil, err
		}
		c, err := r.factory().Create(out, localConfigArg(a, 1), disablePluginsArg(a, 2), a.boolean(3))
		if err != nil {
			return nil, err
		}

		return r.value(c), nil
	})
	r.Handle("factory.createGlobal", func(v any) (any, error) {
		a := argsOf("factory.createGlobal", v)
		out, err := r.ioArg(a)
		if err != nil {
			return nil, err
		}
		c, err := r.factory().CreateGlobal(out, a.boolean(1), a.boolean(2))
		if err != nil || c == nil {
			return nil, err
		}

		return r.value(c), nil
	})
	r.Handle("factory.createConfig", func(v any) (any, error) {
		a := argsOf("factory.createConfig", v)
		out, err := r.ioArg(a)
		if err != nil {
			return nil, err
		}
		cwd, _ := a.nullableString(1)
		cfg, err := r.factory().CreateConfig(out, cwd)
		if err != nil {
			return nil, err
		}

		return r.value(cfg), nil
	})
	// $factory->createDownloadManager($io, $config, $httpDownloader,
	// $process, $eventDispatcher): maestro's manager with its downloaders,
	// as Factory sets one up, on maestro's executor standing for $process
	// (processExecutorOf).
	r.Handle("factory.createDownloadManager", func(v any) (any, error) {
		a := argsOf("factory.createDownloadManager", v)
		out, err := r.ioArg(a)
		if err != nil {
			return nil, err
		}
		cfg, err := param[*config.Config](a, 1)
		if err != nil {
			return nil, err
		}
		hd, err := param[*http.HttpDownloader](a, 2)
		if err != nil {
			return nil, err
		}
		ed, err := dispatcherParam(a, 4)
		if err != nil {
			return nil, err
		}
		process, err := r.processParam(a, 3, out)
		if err != nil {
			return nil, err
		}
		dm, err := r.factory().CreateDownloadManager(out, cfg, hd, process, ed)
		if err != nil {
			return nil, err
		}

		return r.value(dm), nil
	})
	// $factory->createArchiveManager($config, $dm, $loop).
	r.Handle("factory.createArchiveManager", func(v any) (any, error) {
		a := argsOf("factory.createArchiveManager", v)
		cfg, err := param[*config.Config](a, 0)
		if err != nil {
			return nil, err
		}
		dm, err := param[*downloader.DownloadManager](a, 1)
		if err != nil {
			return nil, err
		}
		loop, err := param[*http.Loop](a, 2)
		if err != nil {
			return nil, err
		}

		return r.value(r.factory().CreateArchiveManager(cfg, dm, loop)), nil
	})
}

// installedRepoForAutoload is the local repository as AutoloadGenerator
// reads it, whose CanonicalPackages cannot fail: the error is kept and
// returned after the dump (as composer.GeneratorAdapter does).
type installedRepoForAutoload struct {
	repo repository.InstalledRepositoryInterface
	err  error
}

func (r *installedRepoForAutoload) DevPackageNames() []string { return r.repo.DevPackageNames() }

func (r *installedRepoForAutoload) CanonicalPackages() []pkg.PackageInterface {
	packages, err := r.repo.CanonicalPackages()
	if err != nil && r.err == nil {
		r.err = err
	}

	return packages
}

// classMapValue is what PHP builds Composer's ClassMap of a dump from:
// its map, its ambiguous classes (every path, as addAmbiguousClass()
// recorded them) and its PSR violations by path, in order.
func classMapValue(m *classmap.ClassMap) (*php.Array, error) {
	classes := php.NewArray()
	for class, path := range m.Map() {
		classes.Set(class, path)
	}
	ambiguous := php.NewArray()
	list, err := m.AmbiguousClasses(nil)
	if err != nil {
		return nil, err
	}
	for _, c := range list {
		ambiguous.Set(c.Class, php.StringList(c.Paths))
	}
	violations := php.NewArray()
	for path, vs := range m.RawPsrViolations() {
		entries := php.NewArrayCap(len(vs))
		for _, v := range vs {
			entries.Append(php.ArrayOf("warning", v.Warning, "className", v.ClassName))
		}
		violations.Set(path, entries)
	}

	return php.ArrayOf("map", classes, "ambiguous", ambiguous, "psrViolations", violations), nil
}
