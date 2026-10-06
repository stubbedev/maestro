// Repositories and the RepositoryManager (docs/PLUGINS.md §4.6): maestro's
// repositories cross as service proxies of their PHP class, whose methods
// are `repo.*`; the manager's are `rm.*`.

package plugin

import (
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/repository/composerrepo"
	rvcs "github.com/stubbedev/maestro/internal/repository/vcs"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// repositoryObject returns the object a repository crosses to PHP as (nil
// for nil).
func (r *Runtime) repositoryObject(repo pkg.Repository) any {
	if repo == nil {
		return nil
	}
	if p, ok := repo.(*proxyRepository); ok {
		return p.obj
	}
	class := `Composer\Repository\RepositoryInterface`
	if c, ok := repo.(interface{ Class() string }); ok {
		class = c.Class()
	}

	return r.bridge.object(repo, func() rpc.Object { return &service{v: repo, class: class} })
}

// goRepository returns the maestro repository a PHP value stands for.
func goRepository(v any) (pkg.Repository, bool) {
	g, ok := v.(goObject)
	if !ok {
		return nil, false
	}
	repo, ok := g.goValue().(pkg.Repository)

	return repo, ok
}

// constraintArg is a $constraint param: a ConstraintInterface or a string
// to parse; nil for null.
func constraintArg(a args, i int) (semver.ConstraintInterface, error) { //nolint:unparam // the param's position is the method's.
	switch v := a.at(i).(type) {
	case nil:
		return nil, nil
	case constraintValue:
		return v.c, nil
	case string:
		return repository.ParseConstraint(v)
	}

	return nil, a.errorf("param %d is a %T, not a constraint", i, a.at(i))
}

// searchResults are search() results as PHP arrays.
func searchResults(results []repository.SearchResult) *php.Array {
	list := php.NewArrayCap(len(results))
	for _, res := range results {
		if res.Raw != nil {
			list.Append(res.Raw)

			continue
		}
		item := php.ArrayOf("name", res.Name, "description", nullable(res.Description))
		if res.Abandoned != nil {
			item.Set("abandoned", res.Abandoned)
		}
		if res.URL.Valid {
			item.Set("url", res.URL.S)
		}
		list.Append(item)
	}

	return list
}

// registerRepositories registers the `repo.*` and `rm.*` methods.
func (r *Runtime) registerRepositories() {
	repoMethod := func(method string, fn func(repo repository.RepositoryInterface, a args) (any, error)) {
		r.Handle("repo."+method, func(v any) (any, error) {
			a := argsOf("repo."+method, v)
			repo, err := receiver[repository.RepositoryInterface](a)
			if err != nil {
				return nil, err
			}

			return fn(repo, a)
		})
	}
	writableMethod := func(method string, fn func(repo repository.WritableRepository, a args) (any, error)) {
		repoMethod(method, func(repo repository.RepositoryInterface, a args) (any, error) {
			w, ok := repo.(repository.WritableRepository)
			if !ok {
				return nil, a.errorf("%s is not writable", repo.Class())
			}

			return fn(w, a)
		})
	}
	installedMethod := func(method string, fn func(repo repository.InstalledRepositoryInterface, a args) (any, error)) {
		repoMethod(method, func(repo repository.RepositoryInterface, a args) (any, error) {
			w, ok := repo.(repository.InstalledRepositoryInterface)
			if !ok {
				return nil, a.errorf("%s is not an installed repository", repo.Class())
			}

			return fn(w, a)
		})
	}

	repoMethod("getRepoName", func(repo repository.RepositoryInterface, _ args) (any, error) {
		return repo.RepoName(), nil
	})
	repoMethod("findPackage", func(repo repository.RepositoryInterface, a args) (any, error) {
		c, err := constraintArg(a, 2)
		if err != nil {
			return nil, err
		}
		p, err := repo.FindPackage(php.Strtolower(a.str(1)), c)
		if err != nil {
			return nil, err
		}

		return r.packageObject(p), nil
	})
	repoMethod("findPackages", func(repo repository.RepositoryInterface, a args) (any, error) {
		c, err := constraintArg(a, 2)
		if err != nil {
			return nil, err
		}
		packages, err := repo.FindPackages(php.Strtolower(a.str(1)), c)
		if err != nil {
			return nil, err
		}

		return r.packageList(packages), nil
	})
	repoMethod("search", func(repo repository.RepositoryInterface, a args) (any, error) {
		typ, _ := a.nullableString(3)
		results, err := repo.Search(a.str(1), a.integer(2), typ)
		if err != nil {
			return nil, err
		}

		return searchResults(results), nil
	})
	repoMethod("loadPackages", func(repo repository.RepositoryInterface, a args) (any, error) {
		nameMap := &repository.ConstraintMap{}
		for name, v := range a.arrayOrEmpty(1).All() {
			var c semver.ConstraintInterface
			if v != nil {
				cc, ok := v.(constraintValue)
				if !ok {
					return nil, a.errorf("param 1 holds a %T, not a constraint", v)
				}
				c = cc.c
			}
			nameMap.Set(name.String(), c)
		}
		already := repository.AlreadyLoaded{}
		for name, versions := range a.arrayOrEmpty(4).All() {
			list, _ := versions.(*php.Array)
			if list == nil {
				continue
			}
			byVersion := map[string]pkg.PackageInterface{}
			for version, pv := range list.All() {
				var p pkg.PackageInterface
				if m, ok := pv.(*packageMirror); ok {
					p = m.p
				}
				byVersion[version.String()] = p
			}
			already[name.String()] = byVersion
		}
		res, err := repo.LoadPackages(nameMap, a.arrayOrEmpty(2), a.arrayOrEmpty(3), already)
		if err != nil {
			return nil, err
		}

		return php.ArrayOf("namesFound", php.StringList(res.NamesFound), "packages", r.packageList(res.Packages)), nil
	})
	repoMethod("hasPackage", func(repo repository.RepositoryInterface, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return repo.HasPackage(p)
	})
	repoMethod("getProviders", func(repo repository.RepositoryInterface, a args) (any, error) {
		providers, err := repo.Providers(a.str(1))
		if err != nil {
			return nil, err
		}
		out := php.NewArrayCap(len(providers))
		for _, p := range providers {
			out.Set(p.Name, php.ArrayOf("name", p.Name, "description", nullable(p.Description), "type", p.Type))
		}

		return out, nil
	})
	repoMethod("getPackages", func(repo repository.RepositoryInterface, _ args) (any, error) {
		packages, err := repo.Packages()
		if err != nil {
			return nil, err
		}

		return r.packageList(packages), nil
	})
	repoMethod("count", func(repo repository.RepositoryInterface, _ args) (any, error) {
		n, err := repo.Count()

		return int64(n), err
	})
	writableMethod("addPackage", func(repo repository.WritableRepository, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, repo.AddPackage(p)
	})
	writableMethod("removePackage", func(repo repository.WritableRepository, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, repo.RemovePackage(p)
	})
	writableMethod("getCanonicalPackages", func(repo repository.WritableRepository, _ args) (any, error) {
		packages, err := repo.CanonicalPackages()
		if err != nil {
			return nil, err
		}

		return r.packageList(packages), nil
	})
	writableMethod("setDevPackageNames", func(repo repository.WritableRepository, a args) (any, error) {
		var names []string
		for _, v := range a.arrayOrEmpty(1).Values() {
			names = append(names, php.ToString(v))
		}
		repo.SetDevPackageNames(names)

		return nil, nil
	})
	writableMethod("getDevPackageNames", func(repo repository.WritableRepository, _ args) (any, error) {
		return php.StringList(repo.DevPackageNames()), nil
	})
	writableMethod("write", func(repo repository.WritableRepository, a args) (any, error) {
		im, err := param[repository.InstallationManager](a, 2)
		if err != nil {
			return nil, err
		}

		return nil, repo.Write(a.boolean(1), im)
	})
	writableMethod("reload", func(repo repository.WritableRepository, _ args) (any, error) {
		return nil, repo.Reload()
	})
	installedMethod("getDevMode", func(repo repository.InstalledRepositoryInterface, _ args) (any, error) {
		devMode, ok := repo.DevMode()
		if !ok {
			return nil, nil
		}

		return devMode, nil
	})
	installedMethod("isFresh", func(repo repository.InstalledRepositoryInterface, _ args) (any, error) {
		return repo.IsFresh()
	})
	r.Handle("repo.safelyLoadInstalledVersions", func(v any) (any, error) {
		a := argsOf("repo.safelyLoadInstalledVersions", v)
		data, ok, err := repository.SafelyLoadInstalledVersionsChecked(a.str(0))
		if err != nil || !ok {
			return nil, err
		}

		return data, nil
	})

	rmMethod := func(method string, fn func(rm *repository.RepositoryManager, a args) (any, error)) {
		r.Handle("rm."+method, func(v any) (any, error) {
			a := argsOf("rm."+method, v)
			rm, err := receiver[*repository.RepositoryManager](a)
			if err != nil {
				return nil, err
			}

			return fn(rm, a)
		})
	}
	rmMethod("getLocalRepository", func(rm *repository.RepositoryManager, _ args) (any, error) {
		local := rm.LocalRepository()
		if local == nil {
			return nil, nil
		}

		return r.repositoryObject(local), nil
	})
	rmMethod("getRepositories", func(rm *repository.RepositoryManager, _ args) (any, error) {
		repos := rm.Repositories()
		list := php.NewArrayCap(len(repos))
		for _, repo := range repos {
			list.Append(r.repositoryObject(repo))
		}

		return list, nil
	})
	rmMethod("findPackage", func(rm *repository.RepositoryManager, a args) (any, error) {
		c, err := constraintArg(a, 2)
		if err != nil {
			return nil, err
		}
		p, err := rm.FindPackage(a.str(1), c)
		if err != nil {
			return nil, err
		}

		return r.packageObject(p), nil
	})
	rmMethod("findPackages", func(rm *repository.RepositoryManager, a args) (any, error) {
		c, err := constraintArg(a, 2)
		if err != nil {
			return nil, err
		}
		packages, err := rm.FindPackages(a.str(1), c)
		if err != nil {
			return nil, err
		}

		return r.packageList(packages), nil
	})
	rmMethod("createRepository", func(rm *repository.RepositoryManager, a args) (any, error) {
		name, _ := a.nullableString(3)
		repo, err := rm.CreateRepository(a.str(1), a.arrayOrEmpty(2), name)
		if err != nil {
			return nil, err
		}

		return r.repositoryObject(repo), nil
	})
	rmMethod("addRepository", func(rm *repository.RepositoryManager, a args) (any, error) {
		repo, err := r.repositoryParam(a, 1)
		if err != nil {
			return nil, err
		}
		rm.AddRepository(repo)

		return nil, nil
	})
	rmMethod("prependRepository", func(rm *repository.RepositoryManager, a args) (any, error) {
		repo, err := r.repositoryParam(a, 1)
		if err != nil {
			return nil, err
		}
		rm.PrependRepository(repo)

		return nil, nil
	})
	rmMethod("setLocalRepository", func(rm *repository.RepositoryManager, a args) (any, error) {
		repo, err := goRepositoryParam(a, 1)
		if err != nil {
			return nil, err
		}
		local, ok := repo.(repository.InstalledRepositoryInterface)
		if !ok {
			return nil, a.errorf("param 1 is not an installed repository")
		}
		rm.SetLocalRepository(local)

		return nil, nil
	})
	rmMethod("setRepositoryClass", func(rm *repository.RepositoryManager, a args) (any, error) {
		rm.SetRepositoryClass(a.str(1), r.phpRepositoryConstructor(a.str(2)))

		return nil, nil
	})
	rmMethod("getHttpDownloader", func(rm *repository.RepositoryManager, _ args) (any, error) {
		return r.value(rm.HTTPDownloader()), nil
	})

	r.registerRepositoriesPhase5()
	r.registerFilesystemRepositories()
	r.registerRepositoryFactory()
}

// goRepositoryParam returns param i, a repository of maestro's. A
// repository created in PHP stays PHP's: maestro cannot use it.
func goRepositoryParam(a args, i int) (repository.RepositoryInterface, error) {
	if repo, ok := goRepository(a.at(i)); ok {
		if ri, ok := repo.(repository.RepositoryInterface); ok {
			return ri, nil
		}
	}
	if o, ok := a.at(i).(*rpc.PHPObject); ok {
		return nil, unsupportedf("maestro does not support giving it a %s created in PHP yet", o.Class)
	}

	return nil, a.errorf("param %d is not a repository maestro knows (a %T)", i, a.at(i))
}

// dependentsValue is getDependents()'s result as PHP holds it: [package,
// link, dependents or false] entries.
func (r *Runtime) dependentsValue(deps []repository.Dependent) *php.Array {
	out := php.NewArrayCap(len(deps))
	for _, d := range deps {
		var sub any = false
		if !d.Cut {
			sub = r.dependentsValue(d.Dependents)
		}
		out.Append(php.ListOf(r.packageObject(d.Package), linkValue{d.Link}, sub))
	}

	return out
}

// registerRepositoriesPhase5 registers the repository methods of
// docs/PLUGINS.md's phase 5: platform repositories created in PHP and the
// composite repositories of maestro's.
func (r *Runtime) registerRepositoriesPhase5() {
	repoMethod := func(method string, fn func(repo repository.RepositoryInterface, a args) (any, error)) {
		r.Handle("repo."+method, func(v any) (any, error) {
			a := argsOf("repo."+method, v)
			repo, err := receiver[repository.RepositoryInterface](a)
			if err != nil {
				return nil, err
			}

			return fn(repo, a)
		})
	}

	// new PlatformRepository($packages, $overrides): maestro detects the
	// platform, as the constructor does.
	r.Handle("repo.newPlatform", func(v any) (any, error) {
		a := argsOf("repo.newPlatform", v)
		packages, err := packagesParam(a, 1)
		if err != nil {
			return nil, err
		}
		var opts repository.PlatformOptions
		if f := r.composerFactory; f != nil && f.Runtime != nil {
			if opts, err = f.Runtime.PlatformOptions(util.NewProcessExecutor(r.bootIO)); err != nil {
				return nil, err
			}
		}
		repo, err := repository.NewPlatformRepository(packages, a.arrayOrEmpty(2), opts)
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, repo)
	})
	platformMethod := func(method string, fn func(repo *repository.PlatformRepository, a args) (any, error)) {
		repoMethod(method, func(repo repository.RepositoryInterface, a args) (any, error) {
			p, ok := repo.(*repository.PlatformRepository)
			if !ok {
				return nil, a.errorf("%s is not a platform repository", repo.Class())
			}

			return fn(p, a)
		})
	}
	platformMethod("getDisabledPackages", func(repo *repository.PlatformRepository, _ args) (any, error) {
		out := php.NewArray()
		for name, p := range repo.DisabledPackages().All() {
			out.Set(name, r.packageObject(p))
		}

		return out, nil
	})
	platformMethod("isPlatformPackageDisabled", func(repo *repository.PlatformRepository, a args) (any, error) {
		return repo.IsPlatformPackageDisabled(a.str(1)), nil
	})

	type composite interface {
		Repositories() []repository.RepositoryInterface
		AddRepository(repository.RepositoryInterface) error
	}
	compositeMethod := func(method string, fn func(repo composite, a args) (any, error)) {
		repoMethod(method, func(repo repository.RepositoryInterface, a args) (any, error) {
			c, ok := repo.(composite)
			if !ok {
				return nil, a.errorf("%s is not a composite repository", repo.Class())
			}

			return fn(c, a)
		})
	}
	compositeMethod("getRepositories", func(repo composite, _ args) (any, error) {
		repos := repo.Repositories()
		list := php.NewArrayCap(len(repos))
		for _, inner := range repos {
			list.Append(r.repositoryObject(inner))
		}

		return list, nil
	})
	compositeMethod("addRepository", func(repo composite, a args) (any, error) {
		inner, err := goRepositoryParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, repo.AddRepository(inner)
	})
	installedMethod := func(method string, fn func(repo *repository.InstalledRepository, a args) (any, error)) {
		repoMethod(method, func(repo repository.RepositoryInterface, a args) (any, error) {
			c, ok := repo.(*repository.InstalledRepository)
			if !ok {
				return nil, a.errorf("%s is not an InstalledRepository", repo.Class())
			}

			return fn(c, a)
		})
	}
	installedMethod("findPackagesWithReplacersAndProviders", func(repo *repository.InstalledRepository, a args) (any, error) {
		c, err := constraintArg(a, 2)
		if err != nil {
			return nil, err
		}
		packages, err := repo.FindPackagesWithReplacersAndProviders(php.Strtolower(a.str(1)), c)
		if err != nil {
			return nil, err
		}

		return r.packageList(packages), nil
	})
	installedMethod("getDependents", func(repo *repository.InstalledRepository, a args) (any, error) {
		var needles []string
		if list, ok := a.at(1).(*php.Array); ok {
			for _, n := range list.Values() {
				needles = append(needles, php.ToString(n))
			}
		} else {
			needles = []string{a.str(1)}
		}
		c, err := constraintArg(a, 2)
		if err != nil {
			return nil, err
		}
		if a.has(5) {
			return nil, unsupportedf("maestro does not support Composer\\Repository\\InstalledRepository::getDependents() with \\$packagesFound in plugins yet")
		}
		deps, err := repo.GetDependents(needles, c, a.boolean(3), !a.has(4) || a.boolean(4))
		if err != nil {
			return nil, err
		}

		return r.dependentsValue(deps), nil
	})
}

// registerFilesystemRepositories registers `repo.newFilesystem`: new
// (Installed)FilesystemRepository(new JsonFile($path), $dumpVersions,
// $rootPackage) in PHP is maestro's repository of that file.
func (r *Runtime) registerFilesystemRepositories() {
	r.Handle("repo.newFilesystem", func(v any) (any, error) {
		a := argsOf("repo.newFilesystem", v)
		out, ok, err := r.ioParam(a, 2)
		if err != nil {
			return nil, err
		}
		var fio io.IO
		if ok {
			fio = out
		}
		file, err := json.NewFile(a.str(1), nil, fio)
		if err != nil {
			return nil, err
		}
		var root pkg.RootPackageInterface
		if a.has(4) {
			p, err := packageParam(a, 4)
			if err != nil {
				return nil, err
			}
			if root, ok = p.(pkg.RootPackageInterface); !ok {
				return nil, a.errorf("param 4 is not a root package")
			}
		}
		sink := func(versions *php.Array) {
			// A failure is PHP ending, which the next call reports.
			_ = r.ReloadInstalledVersions(versions, util.Dirname(a.str(1)))
		}
		if a.boolean(5) {
			repo, err := repository.NewInstalledFilesystemRepository(file, a.boolean(3), root)
			if err != nil {
				return nil, err
			}
			repo.SetInstalledVersionsSink(sink)

			return nil, r.adopt(a, repo)
		}
		repo, err := repository.NewFilesystemRepository(file, a.boolean(3), root)
		if err != nil {
			return nil, err
		}
		repo.SetInstalledVersionsSink(sink)

		return nil, r.adopt(a, repo)
	})
}

// registerRepositoryFactory registers what RepositoryFactory and
// Factory::createHttpDownloader need of maestro (`repofactory.*`,
// `factory.createHttpDownloader`).
func (r *Runtime) registerRepositoryFactory() {
	ioAndConfig := func(a args) (io.IO, *config.Config, error) {
		out, _, err := r.ioParam(a, 0)
		if err != nil {
			return nil, nil, err
		}
		cfg, err := param[*config.Config](a, 1)

		return out, cfg, err
	}
	factory := func() *composer.Factory {
		if r.composerFactory != nil {
			return r.composerFactory
		}

		return &composer.Factory{}
	}

	r.Handle("factory.createHttpDownloader", func(v any) (any, error) {
		a := argsOf("factory.createHttpDownloader", v)
		out, cfg, err := ioAndConfig(a)
		if err != nil {
			return nil, err
		}
		h, err := factory().CreateHttpDownloader(out, cfg, a.array(2))
		if err != nil {
			return nil, err
		}

		return r.value(h), nil
	})
	r.Handle("repofactory.configFromString", func(v any) (any, error) {
		a := argsOf("repofactory.configFromString", v)
		out, cfg, err := ioAndConfig(a)
		if err != nil {
			return nil, err
		}
		h, err := factory().CreateHttpDownloader(out, cfg, nil)
		if err != nil {
			return nil, err
		}

		return repository.ConfigFromString(a.str(2), a.boolean(3), h)
	})
	// RepositoryFactory::manager(): a manager of every repository type,
	// with a new asynchronous ProcessExecutor.
	r.Handle("repofactory.manager", func(v any) (any, error) {
		a := argsOf("repofactory.manager", v)
		out, cfg, err := ioAndConfig(a)
		if err != nil {
			return nil, err
		}
		h, err := param[*http.HttpDownloader](a, 2)
		if err != nil {
			return nil, err
		}
		var dispatcher repository.EventDispatcher
		if a.has(3) {
			ed, err := param[*eventdispatcher.EventDispatcher](a, 3)
			if err != nil {
				return nil, err
			}
			dispatcher = ed
		}
		process := util.NewProcessExecutor(out)
		process.EnableAsync()
		rm := repository.Manager(out, cfg, h, dispatcher, process, repository.ExternalTypes{
			Composer: composerrepo.Constructor,
			VCS:      rvcs.NewRepository,
		})

		return r.value(rm), nil
	})
}
