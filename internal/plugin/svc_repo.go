// Repositories and the RepositoryManager (docs/PLUGINS.md §4.6): maestro's
// repositories cross as service proxies of their PHP class, whose methods
// are `repo.*`; the manager's are `rm.*`.

package plugin

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

// repositoryObject returns the object a repository crosses to PHP as (nil
// for nil).
func (r *Runtime) repositoryObject(repo pkg.Repository) any {
	if repo == nil {
		return nil
	}
	class := `Composer\Repository\RepositoryInterface`
	if c, ok := repo.(interface{ Class() string }); ok {
		class = c.Class()
	}

	return r.bridge.object(repo, func() rpc.Object { return &service{v: repo, class: class} })
}

// goRepository returns the maestro repository a PHP value stands for.
func goRepository(v any) (pkg.Repository, bool) {
	s, ok := v.(*service)
	if !ok {
		return nil, false
	}
	repo, ok := s.v.(pkg.Repository)

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
}
