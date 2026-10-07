// Package loading and dumping (docs/PLUGINS.md §4.5, tier 5):
// ArrayLoader's methods (`loader.*`) load maestro's packages, which come
// back as mirrors; ArrayDumper::dump is `dumper.dump`.

package plugin

import (
	"sync"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// versionParsers is the VersionParser the loaders of PHP's ArrayLoaders
// share (its parsed-constraint cache is Composer's static one).
type versionParsers struct {
	once sync.Once
	p    *pkg.VersionParser
}

func (v *versionParsers) get() *pkg.VersionParser {
	v.once.Do(func() { v.p = pkg.NewVersionParser() })

	return v.p
}

func (r *Runtime) registerLoaders() {
	r.Handle("loader.load", func(v any) (any, error) {
		a := argsOf("loader.load", v)
		p, err := loader.NewArrayLoader(r.parsers.get(), a.boolean(2)).Load(a.arrayOrEmpty(0), a.str(1))
		if err != nil {
			return nil, err
		}

		return r.packageObject(p), nil
	})
	r.Handle("loader.loadPackages", func(v any) (any, error) {
		a := argsOf("loader.loadPackages", v)
		var versions []*php.Array
		for _, item := range a.arrayOrEmpty(0).Values() {
			config, ok := item.(*php.Array)
			if !ok {
				return nil, a.errorf("param 0 holds a %T, not a package's array", item)
			}
			versions = append(versions, config)
		}
		packages, err := loader.NewArrayLoader(r.parsers.get(), a.boolean(1)).LoadPackages(versions)
		if err != nil {
			return nil, err
		}

		return r.packageList(packages), nil
	})
	r.Handle("loader.parseLinks", func(v any) (any, error) {
		a := argsOf("loader.parseLinks", v)
		links, err := loader.NewArrayLoader(r.parsers.get(), false).ParseLinks(a.str(0), a.str(1), a.str(2), a.arrayOrEmpty(3))
		if err != nil {
			return nil, err
		}

		return linksValue(links), nil
	})
	r.Handle("loader.getBranchAlias", func(v any) (any, error) {
		a := argsOf("loader.getBranchAlias", v)
		alias, ok, err := loader.NewArrayLoader(r.parsers.get(), false).GetBranchAlias(a.arrayOrEmpty(0))
		if err != nil || !ok {
			return nil, err
		}

		return alias, nil
	})
	r.Handle("dumper.dump", func(v any) (any, error) {
		a := argsOf("dumper.dump", v)
		p, err := packageParam(a, 0)
		if err != nil {
			return nil, err
		}

		return dumper.ArrayDumper{}.Dump(p)
	})
}
