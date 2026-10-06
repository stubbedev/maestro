// RepositorySet and VersionSelector created in PHP (docs/PLUGINS.md §4.5,
// §4.6; phase 5): both are maestro's (`reposet.*`, `selector.*`).

package plugin

import (
	"time"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
)

// constraintMapParam returns param i, a name => constraint array, as a
// ConstraintMap.
func constraintMapParam(a args, i int) (*repository.ConstraintMap, error) {
	m := &repository.ConstraintMap{}
	for name, v := range a.arrayOrEmpty(i).All() {
		c, ok := v.(constraintValue)
		if !ok {
			return nil, a.errorf("param %d holds a %T, not a constraint", i, v)
		}
		m.Set(name.String(), c.c)
	}

	return m, nil
}

// constraintMapValue is a ConstraintMap as PHP holds it.
func constraintMapValue(m *repository.ConstraintMap) *php.Array {
	out := php.NewArray()
	if m == nil {
		return out
	}
	for name, c := range m.All() {
		if c == nil {
			out.Set(name, nil)

			continue
		}
		out.Set(name, constraintValue{c})
	}

	return out
}

func (r *Runtime) registerSelectors() {
	r.Handle("reposet.new", func(v any) (any, error) {
		a := argsOf("reposet.new", v)
		requires, err := constraintMapParam(a, 5)
		if err != nil {
			return nil, err
		}
		temporary, err := constraintMapParam(a, 6)
		if err != nil {
			return nil, err
		}
		set, err := repository.NewRepositorySet(a.str(1), a.arrayOrEmpty(2), repository.RootAliasesFromArray(a.arrayOrEmpty(3)), a.arrayOrEmpty(4), requires, temporary)
		if err != nil {
			return nil, err
		}

		return nil, r.adopt(a, set)
	})
	method := func(name string, fn func(s *repository.RepositorySet, a args) (any, error)) {
		r.Handle("reposet."+name, func(v any) (any, error) {
			a := argsOf("reposet."+name, v)
			s, err := receiver[*repository.RepositorySet](a)
			if err != nil {
				return nil, err
			}

			return fn(s, a)
		})
	}
	method("allowInstalledRepositories", func(s *repository.RepositorySet, a args) (any, error) {
		s.AllowInstalledRepositories(!a.has(1) || a.boolean(1))

		return nil, nil
	})
	method("getRootRequires", func(s *repository.RepositorySet, _ args) (any, error) {
		return constraintMapValue(s.RootRequires()), nil
	})
	method("getTemporaryConstraints", func(s *repository.RepositorySet, _ args) (any, error) {
		return constraintMapValue(s.TemporaryConstraints()), nil
	})
	method("addRepository", func(s *repository.RepositorySet, a args) (any, error) {
		repo, err := r.repositoryParam(a, 1)
		if err != nil {
			return nil, err
		}

		return nil, s.AddRepository(repo)
	})
	method("findPackages", func(s *repository.RepositorySet, a args) (any, error) {
		c, err := constraintArg(a, 2)
		if err != nil {
			return nil, err
		}
		packages, err := s.FindPackages(a.str(1), c, a.integer(3))
		if err != nil {
			return nil, err
		}

		return r.packageList(packages), nil
	})
	method("getProviders", func(s *repository.RepositorySet, a args) (any, error) {
		providers, err := s.Providers(a.str(1))
		if err != nil {
			return nil, err
		}
		out := php.NewArrayCap(len(providers))
		for _, p := range providers {
			out.Set(p.Name, php.ArrayOf("name", p.Name, "description", nullable(p.Description), "type", p.Type))
		}

		return out, nil
	})
	method("isPackageAcceptable", func(s *repository.RepositorySet, a args) (any, error) {
		var names []string
		for _, n := range a.arrayOrEmpty(1).Values() {
			names = append(names, php.ToString(n))
		}

		return s.IsPackageAcceptable(names, a.str(2)), nil
	})

	method("createPool", func(s *repository.RepositorySet, a args) (any, error) {
		req, ok := unwrap(a.at(1)).(*resolver.Request)
		if !ok {
			return nil, unsupportedf("maestro does not support Composer\\Repository\\RepositorySet::createPool() with a Request created in PHP yet")
		}
		out, _, err := r.ioParam(a, 2)
		if err != nil {
			return nil, err
		}
		opts := resolver.CreatePoolOptions{IgnoredTypes: stringList(a.at(4))}
		if a.has(3) {
			ed, err := param[*eventdispatcher.EventDispatcher](a, 3)
			if err != nil {
				return nil, err
			}
			opts.EventDispatcher = ed
		}
		if a.has(5) {
			opts.AllowedTypes = stringList(a.at(5))
			if opts.AllowedTypes == nil {
				opts.AllowedTypes = []string{}
			}
		}
		pool, err := resolver.CreatePool(s, req, out, opts)
		if err != nil {
			return nil, err
		}

		return r.poolValue(pool), nil
	})
	method("createPoolWithAllPackages", func(s *repository.RepositorySet, _ args) (any, error) {
		pool, err := resolver.CreatePoolWithAllPackages(s)
		if err != nil {
			return nil, err
		}

		return r.poolValue(pool), nil
	})
	method("createPoolForPackages", func(s *repository.RepositorySet, a args) (any, error) {
		var locked *repository.LockArrayRepository
		if a.has(2) {
			l, err := param[*repository.LockArrayRepository](a, 2)
			if err != nil {
				return nil, unsupportedf("maestro does not support a LockArrayRepository created in PHP here yet")
			}
			locked = l
		}
		pool, err := resolver.CreatePoolForPackages(s, stringList(a.at(1)), locked)
		if err != nil {
			return nil, err
		}

		return r.poolValue(pool), nil
	})
	advisories := func(res repository.SecurityAdvisoriesResult, err error) (any, error) {
		if err != nil {
			return nil, err
		}
		list := php.NewArray()
		for name, advs := range res.Advisories.All() {
			items := php.NewArrayCap(len(advs))
			for _, adv := range advs {
				items.Append(advisoryValue(adv))
			}
			list.Set(name, items)
		}

		return php.ArrayOf("advisories", list, "unreachableRepos", php.StringList(res.UnreachableRepos)), nil
	}
	method("getSecurityAdvisories", func(s *repository.RepositorySet, a args) (any, error) {
		return advisories(s.GetSecurityAdvisories(stringList(a.at(1)), a.boolean(2), a.boolean(3)))
	})
	method("getMatchingSecurityAdvisories", func(s *repository.RepositorySet, a args) (any, error) {
		packages, err := packagesParam(a, 1)
		if err != nil {
			return nil, err
		}

		return advisories(s.GetMatchingSecurityAdvisories(packages, a.boolean(2), a.boolean(3)))
	})

	r.Handle("selector.new", func(v any) (any, error) {
		a := argsOf("selector.new", v)
		set, err := param[*repository.RepositorySet](a, 1)
		if err != nil {
			return nil, err
		}
		var platform []pkg.PackageInterface
		if a.has(2) {
			repo, err := param[*repository.PlatformRepository](a, 2)
			if err != nil {
				return nil, err
			}
			if platform, err = repo.Packages(); err != nil {
				return nil, err
			}
		}
		s := version.NewVersionSelector(set, platform)
		s.PHPVersion = a.str(3)

		return nil, r.adopt(a, s)
	})
	selectorMethod := func(name string, fn func(s *version.VersionSelector, a args) (any, error)) {
		r.Handle("selector."+name, func(v any) (any, error) {
			a := argsOf("selector."+name, v)
			s, err := receiver[*version.VersionSelector](a)
			if err != nil {
				return nil, err
			}

			return fn(s, a)
		})
	}
	selectorMethod("findBestCandidate", func(s *version.VersionSelector, a args) (any, error) {
		opts := version.FindBestCandidateOptions{
			PreferredStability: a.str(3),
			RepoSetFlags:       a.integer(5),
		}
		if target, ok := a.nullableString(2); ok {
			opts.TargetPackageVersion = target
		}
		f, err := r.filterFromPHP(a.at(4))
		if err != nil {
			return nil, err
		}
		opts.PlatformRequirementFilter = f
		if a.has(6) {
			out, ok, err := r.ioParam(a, 6)
			if err != nil {
				return nil, err
			}
			if ok {
				opts.IO = out
			}
		}
		// An exception of a PHP showWarnings callable ends the call, as
		// in Composer (the candidates checked after it are not).
		var callbackErr error
		switch w := a.at(7).(type) {
		case bool:
			if !w {
				opts.ShowWarnings = func(pkg.PackageInterface) bool { return false }
			}
		case nil:
		default:
			callable := w
			opts.ShowWarnings = func(p pkg.PackageInterface) bool {
				if callbackErr != nil {
					return false
				}
				v, err := r.Call("callable.invoke", php.ArrayOf("callable", callable, "args", php.ListOf(r.packageObject(p))))
				callbackErr = err

				return err == nil && php.ToBool(v)
			}
		}
		p, err := s.FindBestCandidate(a.str(1), opts)
		if callbackErr != nil {
			return nil, callbackErr
		}
		if err != nil {
			return nil, err
		}
		if p == nil {
			return false, nil
		}

		return r.packageObject(p), nil
	})
	selectorMethod("findRecommendedRequireVersion", func(s *version.VersionSelector, a args) (any, error) {
		p, err := packageParam(a, 1)
		if err != nil {
			return nil, err
		}

		return s.FindRecommendedRequireVersion(p)
	})
}

// poolValue describes a pool for the shim's PHP-local Pool: its packages
// (in id order), the unacceptable fixed or locked ones and the versions
// the pool builder removed (security and filter list removals are left
// out).
func (r *Runtime) poolValue(p *resolver.Pool) *php.Array {
	versions := func(m *repository.NameMap[*resolver.VersionMap]) *php.Array {
		out := php.NewArray()
		if m == nil {
			return out
		}
		for name, vm := range m.All() {
			v := php.NewArray()
			for version, pretty := range vm.All() {
				v.Set(version, pretty)
			}
			out.Set(name, v)
		}

		return out
	}

	return php.ArrayOf(
		"packages", r.lazyPackageList(p.Packages()),
		"unacceptable", r.lazyPackageList(p.UnacceptableFixedOrLockedPackages()),
		"removedVersions", versions(p.AllRemovedVersions()),
		"abandonedRemovedVersions", versions(p.AllAbandonedRemovedPackageVersions()),
	)
}

// advisoryValue describes an advisory for the shim's
// (Partial)SecurityAdvisory objects.
func advisoryValue(adv repository.Advisory) *php.Array {
	p := adv.Partial()
	out := php.ArrayOf("advisoryId", p.AdvisoryID, "packageName", p.PackageName, "affectedVersions", constraintValue{p.AffectedVersions})
	if full, ok := adv.(*repository.SecurityAdvisory); ok {
		out.Set("title", full.Title)
		out.Set("sources", full.Sources)
		out.Set("reportedAt", full.ReportedAt.Format(time.RFC3339))
		out.Set("cve", nullable(full.CVE))
		out.Set("link", nullable(full.Link))
		out.Set("severity", nullable(full.Severity))
	}

	return out
}
