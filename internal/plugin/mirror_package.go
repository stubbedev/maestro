// The package family's mirrors (docs/PLUGINS.md §4.5, §5.3, §6.4) and
// their methods (`pkg.*`, §6.6): PHP's Composer\Package\* objects hold
// the fields of maestro's packages, by Composer's property names; a
// setter called in PHP runs maestro's setter, whose change reaches PHP
// with the reply.

package plugin

import (
	"sync/atomic"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// packageBase is the shim base class of the package family.
const packageBase = `Composer\Package\BasePackage`

// releaseDateLayout is how a release date crosses: PHP's
// DateTime::format('Y-m-d\TH:i:s.uP'), which `new \DateTime()` parses
// back.
const releaseDateLayout = "2006-01-02T15:04:05.000000-07:00"

// packageMirror is a maestro package as PHP mirrors it.
type packageMirror struct {
	r *Runtime
	p pkg.PackageInterface
	// lazy is set while PHP holds only the core fields (docs/PLUGINS.md
	// §5.3 "Lazy snapshot tiers"): PRE_POOL_CREATE's package lists send
	// them, PHP asks for the rest (`pkg.load`) when a getter needs it.
	lazy atomic.Bool
}

// PHPOpaque implements php.Opaque.
func (*packageMirror) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (m *packageMirror) PHPClass() string { return m.p.Class() }

// MirrorBase implements rpc.Mirror.
func (*packageMirror) MirrorBase() string { return packageBase }

// Rev implements rpc.Mirror.
func (m *packageMirror) Rev() uint64 { return m.p.Rev() }

// ApplyMirror implements rpc.Mirror. The shim changes packages through
// their setters (`pkg.*`), never by syncing fields.
func (m *packageMirror) ApplyMirror(*php.Array) error {
	return &rpc.ProtocolError{Message: "package fields are not synced from PHP"}
}

// MirrorSnapshot implements rpc.Mirror: every property of the package's
// class (Maestro\Shim\Adapter\PackageAdapter).
func (m *packageMirror) MirrorSnapshot() (*php.Array, error) {
	p := m.p
	if m.lazy.Load() {
		return php.ArrayOf(
			"id", int64(p.ID()),
			"name", p.Name(),
			"prettyName", p.PrettyName(),
			"version", p.Version(),
			"prettyVersion", p.PrettyVersion(),
			"type", nullable(rawType(p)),
			"stability", p.Stability(),
			"dev", p.IsDev(),
			"lazy", true,
		), nil
	}

	s := php.NewArrayCap(64)
	s.Set("id", int64(p.ID()))
	s.Set("name", p.Name())
	s.Set("prettyName", p.PrettyName())

	if a, ok := p.(pkg.Alias); ok {
		s.Set("version", a.Version())
		s.Set("prettyVersion", a.PrettyVersion())
		s.Set("dev", a.IsDev())
		s.Set("rootPackageAlias", a.IsRootPackageAlias())
		s.Set("stability", a.Stability())
		s.Set("hasSelfVersionRequires", a.HasSelfVersionRequires())
		s.Set("aliasOf", m.r.packageObject(a.AliasOf()))
		setLinks(s, p)

		return s, nil
	}

	s.Set("type", nullable(rawType(p)))
	s.Set("targetDir", nullable(p.TargetDir()))
	s.Set("installationSource", nullable(p.InstallationSource()))
	s.Set("sourceType", nullable(p.SourceType()))
	s.Set("sourceUrl", nullable(p.SourceURL()))
	s.Set("sourceReference", nullable(p.SourceReference()))
	s.Set("sourceMirrors", nullArray(p.SourceMirrors()))
	s.Set("distType", nullable(p.DistType()))
	s.Set("distUrl", nullable(p.DistURL()))
	s.Set("distReference", nullable(p.DistReference()))
	s.Set("distSha1Checksum", nullable(p.DistSha1Checksum()))
	s.Set("distMirrors", nullArray(p.DistMirrors()))
	s.Set("version", p.Version())
	s.Set("prettyVersion", p.PrettyVersion())
	if t, ok := p.ReleaseDate(); ok {
		s.Set("releaseDate", t.Format(releaseDateLayout))
	} else {
		s.Set("releaseDate", nil)
	}
	s.Set("extra", p.Extra())
	s.Set("binaries", p.Binaries())
	s.Set("dev", p.IsDev())
	s.Set("stability", p.Stability())
	s.Set("notificationUrl", nullable(p.NotificationURL()))
	setLinks(s, p)
	s.Set("suggests", p.Suggests())
	s.Set("autoload", p.Autoload())
	s.Set("devAutoload", p.DevAutoload())
	s.Set("includePaths", p.IncludePaths())
	s.Set("isDefaultBranch", p.IsDefaultBranch())
	s.Set("transportOptions", p.TransportOptions())
	s.Set("phpExt", nullArray(p.PhpExt()))

	if c, ok := p.(pkg.CompletePackageInterface); ok {
		s.Set("repositories", c.Repositories())
		s.Set("license", c.License())
		s.Set("keywords", c.Keywords())
		s.Set("authors", c.Authors())
		s.Set("description", nullable(c.Description()))
		s.Set("homepage", nullable(c.Homepage()))
		s.Set("scripts", c.Scripts())
		s.Set("support", c.Support())
		s.Set("funding", c.Funding())
		s.Set("abandoned", c.Abandoned())
		s.Set("archiveName", nullable(c.ArchiveName()))
		s.Set("archiveExcludes", c.ArchiveExcludes())
	}

	if r, ok := p.(pkg.RootPackageInterface); ok {
		s.Set("minimumStability", r.MinimumStability())
		s.Set("preferStable", r.PreferStable())
		s.Set("stabilityFlags", r.StabilityFlags())
		s.Set("config", r.Config())
		s.Set("references", r.References())
		s.Set("aliases", r.Aliases())
	}

	return s, nil
}

func setLinks(s *php.Array, p pkg.PackageInterface) {
	s.Set("requires", linksValue(p.Requires()))
	s.Set("devRequires", linksValue(p.DevRequires()))
	s.Set("conflicts", linksValue(p.Conflicts()))
	s.Set("provides", linksValue(p.Provides()))
	s.Set("replaces", linksValue(p.Replaces()))
}

// rawType is the $type property: null unless set.
func rawType(p pkg.PackageInterface) pkg.NullString {
	if pp, ok := pkg.AsPackage(p); ok {
		return pp.RawType()
	}

	return pkg.Str(p.Type())
}

// nullable is a ?string as PHP holds it.
func nullable(s pkg.NullString) any {
	if !s.Valid {
		return nil
	}

	return s.S
}

// nullArray is a ?array: a nil *php.Array is null.
func nullArray(a *php.Array) any {
	if a == nil {
		return nil
	}

	return a
}

// packageObject returns the object a package crosses to PHP as (nil for
// nil).
func (r *Runtime) packageObject(p pkg.PackageInterface) any {
	if p == nil {
		return nil
	}

	return r.bridge.object(p, func() rpc.Object { return &packageMirror{r: r, p: p} })
}

// lazyPackageList is packageList for the package lists of PRE_POOL_CREATE
// (docs/PLUGINS.md §5.3): a package PHP does not know yet gets its core
// fields only (not a root package or an alias, whose fields are their
// own).
func (r *Runtime) lazyPackageList(packages []pkg.PackageInterface) *php.Array {
	a := php.NewArrayCap(len(packages))
	for _, p := range packages {
		a.Append(r.lazyPackage(p))
	}

	return a
}

// lazyPackage is one package of lazyPackageList (nil for nil).
func (r *Runtime) lazyPackage(p pkg.PackageInterface) any {
	if p == nil {
		return nil
	}
	_, root := p.(pkg.RootPackageInterface)
	_, alias := p.(pkg.Alias)

	return r.bridge.object(p, func() rpc.Object {
		m := &packageMirror{r: r, p: p}
		m.lazy.Store(!root && !alias)

		return m
	})
}

// packageList returns packages as a PHP list of package objects.
func (r *Runtime) packageList(packages []pkg.PackageInterface) *php.Array {
	a := php.NewArrayCap(len(packages))
	for _, p := range packages {
		a.Append(r.packageObject(p))
	}

	return a
}

// packageParam returns param i as a maestro package.
func packageParam(a args, i int) (pkg.PackageInterface, error) {
	if m, ok := a.at(i).(*packageMirror); ok {
		return m.p, nil
	}

	return nil, a.errorf("param %d is not a package maestro knows (a %T)", i, a.at(i))
}

// packagesParam returns param i, a list of packages.
func packagesParam(a args, i int) ([]pkg.PackageInterface, error) {
	list := a.arrayOrEmpty(i)
	out := make([]pkg.PackageInterface, 0, list.Len())
	for _, v := range list.Values() {
		m, ok := v.(*packageMirror)
		if !ok {
			return nil, a.errorf("param %d holds a %T, not a package maestro knows", i, v)
		}
		out = append(out, m.p)
	}

	return out, nil
}

// linksParam returns param i, a map of links, as pkg.Links (keys kept).
func linksParam(a args, i int) (pkg.Links, error) {
	var b pkg.LinksBuilder
	for k, v := range a.arrayOrEmpty(i).All() {
		l, ok := v.(linkValue)
		if !ok {
			return pkg.Links{}, a.errorf("param %d holds a %T, not a Link", i, v)
		}
		b.Set(k.String(), l.l)
	}

	return b.Build(), nil
}

func nullString(a args, i int) pkg.NullString {
	s, ok := a.nullableString(i)
	if !ok {
		return pkg.NullString{}
	}

	return pkg.Str(s)
}

// registerPackages registers the `pkg.*` methods.
func (r *Runtime) registerPackages() {
	r.RegisterMirrorFactory(packageBase, r.adoptPackage)

	// setter registers a setter of the packages implementing T.
	type setterFunc func(p pkg.PackageInterface, a args) error
	setter := func(method string, fn setterFunc) {
		r.Handle("pkg."+method, func(v any) (any, error) {
			a := argsOf("pkg."+method, v)
			p, err := packageParam(a, 0)
			if err != nil {
				return nil, err
			}

			return nil, fn(p, a)
		})
	}
	asPackage := func(p pkg.PackageInterface, a args) (*pkg.Package, error) {
		pp, ok := pkg.AsPackage(p)
		if !ok {
			return nil, a.errorf("%s has no such method", p.Class())
		}

		return pp, nil
	}
	pkgSetter := func(method string, fn func(pp *pkg.Package, a args) error) {
		setter(method, func(p pkg.PackageInterface, a args) error {
			pp, err := asPackage(p, a)
			if err != nil {
				return err
			}

			return fn(pp, a)
		})
	}
	completeSetter := func(method string, fn func(c pkg.CompletePackageInterface, a args)) {
		setter(method, func(p pkg.PackageInterface, a args) error {
			c, ok := p.(pkg.CompletePackageInterface)
			if !ok {
				return a.errorf("%s has no such method", p.Class())
			}
			fn(c, a)

			return nil
		})
	}
	rootSetter := func(method string, fn func(rp pkg.RootPackageInterface, a args) error) {
		setter(method, func(p pkg.PackageInterface, a args) error {
			rp, ok := p.(pkg.RootPackageInterface)
			if !ok {
				return a.errorf("%s has no such method", p.Class())
			}

			return fn(rp, a)
		})
	}

	setter("setId", func(p pkg.PackageInterface, a args) error { p.SetID(a.integer(1)); return nil })
	setter("setRepository", func(p pkg.PackageInterface, a args) error {
		repo, ok := goRepository(a.at(1))
		if !ok {
			return a.errorf("maestro cannot add its package %s to a repository created in PHP", p.PrettyName())
		}

		return p.SetRepository(repo)
	})
	// The fields of a package PHP got with its core fields only.
	r.Handle("pkg.load", func(v any) (any, error) {
		a := argsOf("pkg.load", v)
		m, ok := a.at(0).(*packageMirror)
		if !ok {
			return nil, a.errorf("param 0 is not a package maestro knows (a %T)", a.at(0))
		}
		m.lazy.Store(false)

		return m.MirrorSnapshot()
	})
	r.Handle("pkg.getRepository", func(v any) (any, error) {
		a := argsOf("pkg.getRepository", v)
		p, err := packageParam(a, 0)
		if err != nil {
			return nil, err
		}

		return r.repositoryObject(p.Repository()), nil
	})
	r.Handle("pkg.getSourceUrls", func(v any) (any, error) {
		a := argsOf("pkg.getSourceUrls", v)
		p, err := packageParam(a, 0)
		if err != nil {
			return nil, err
		}

		return php.StringList(p.SourceURLs()), nil
	})
	r.Handle("pkg.getDistUrls", func(v any) (any, error) {
		a := argsOf("pkg.getDistUrls", v)
		p, err := packageParam(a, 0)
		if err != nil {
			return nil, err
		}

		return php.StringList(p.DistURLs()), nil
	})
	// Package::getUrls of a package created in PHP: url, mirrors, ref,
	// type, urlType, name, version, prettyVersion.
	r.Handle("pkg.urls", func(v any) (any, error) {
		a := argsOf("pkg.urls", v)
		p := pkg.NewPackage(a.str(5), a.str(6), a.str(7))
		if a.str(4) == "dist" {
			p.SetDistURL(nullString(a, 0))
			p.SetDistMirrors(a.array(1))
			p.SetDistReference(nullString(a, 2))
			p.SetDistType(nullString(a, 3))

			return php.StringList(p.DistURLs()), nil
		}
		p.SetSourceURL(nullString(a, 0))
		p.SetSourceMirrors(a.array(1))
		p.SetSourceReference(nullString(a, 2))
		p.SetSourceType(nullString(a, 3))

		return php.StringList(p.SourceURLs()), nil
	})

	// PackageInterface setters (aliases forward them to their package).
	setter("setInstallationSource", func(p pkg.PackageInterface, a args) error {
		p.SetInstallationSource(nullString(a, 1))
		return nil
	})
	setter("setSourceReference", func(p pkg.PackageInterface, a args) error {
		p.SetSourceReference(nullString(a, 1))
		return nil
	})
	setter("setSourceMirrors", func(p pkg.PackageInterface, a args) error {
		p.SetSourceMirrors(a.array(1))
		return nil
	})
	setter("setDistReference", func(p pkg.PackageInterface, a args) error {
		p.SetDistReference(nullString(a, 1))
		return nil
	})
	setter("setDistMirrors", func(p pkg.PackageInterface, a args) error {
		p.SetDistMirrors(a.array(1))
		return nil
	})
	setter("setTransportOptions", func(p pkg.PackageInterface, a args) error {
		p.SetTransportOptions(a.arrayOrEmpty(1))
		return nil
	})
	setter("setDistUrl", func(p pkg.PackageInterface, a args) error {
		p.SetDistURL(nullString(a, 1))
		return nil
	})
	setter("setDistType", func(p pkg.PackageInterface, a args) error {
		p.SetDistType(nullString(a, 1))
		return nil
	})
	setter("setSourceDistReferences", func(p pkg.PackageInterface, a args) error {
		p.SetSourceDistReferences(a.str(1))
		return nil
	})

	// Package's own setters.
	pkgSetter("setType", func(p *pkg.Package, a args) error { p.SetType(a.str(1)); return nil })
	pkgSetter("setTargetDir", func(p *pkg.Package, a args) error { p.SetTargetDir(nullString(a, 1)); return nil })
	pkgSetter("setSourceType", func(p *pkg.Package, a args) error { p.SetSourceType(nullString(a, 1)); return nil })
	pkgSetter("setSourceUrl", func(p *pkg.Package, a args) error { p.SetSourceURL(nullString(a, 1)); return nil })
	pkgSetter("setDistSha1Checksum", func(p *pkg.Package, a args) error {
		p.SetDistSha1Checksum(nullString(a, 1))
		return nil
	})
	pkgSetter("setReleaseDate", func(p *pkg.Package, a args) error {
		if !a.has(1) {
			p.SetReleaseDate(time.Time{}, false)
			return nil
		}
		t, ok := a.at(1).(dateValue)
		if !ok {
			return a.errorf("param 1 is not a date")
		}
		p.SetReleaseDate(t.t, true)

		return nil
	})
	pkgSetter("setIncludePaths", func(p *pkg.Package, a args) error { p.SetIncludePaths(a.arrayOrEmpty(1)); return nil })
	pkgSetter("setPhpExt", func(p *pkg.Package, a args) error { p.SetPhpExt(a.array(1)); return nil })
	pkgSetter("setNotificationUrl", func(p *pkg.Package, a args) error { p.SetNotificationURL(a.str(1)); return nil })
	pkgSetter("setIsDefaultBranch", func(p *pkg.Package, a args) error { p.SetIsDefaultBranch(a.boolean(1)); return nil })
	pkgSetter("setBinaries", func(p *pkg.Package, a args) error { p.SetBinaries(a.arrayOrEmpty(1)); return nil })
	pkgSetter("replaceVersion", func(p *pkg.Package, a args) error { p.ReplaceVersion(a.str(1), a.str(2)); return nil })

	// Setters RootPackageInterface also has (RootAliasPackage forwards
	// them); on other packages they are Package's.
	for _, method := range []string{"setRequires", "setDevRequires", "setConflicts", "setProvides", "setReplaces"} {
		setter(method, func(p pkg.PackageInterface, a args) error {
			links, err := linksParam(a, 1)
			if err != nil {
				return err
			}
			// The shim's Package::setRequires throws for a list itself; a
			// RootAliasPackage hands its links over as given, and PHP's
			// sets its own links before its aliased package's setter
			// throws the \ErrorException.
			if err := pkg.LinksListError(method, links); err != nil {
				if ra, ok := p.(*pkg.RootAliasPackage); ok {
					ra.SetOwnLinks(method, links)
				}

				return err
			}
			var target interface {
				SetRequires(pkg.Links)
				SetDevRequires(pkg.Links)
				SetConflicts(pkg.Links)
				SetProvides(pkg.Links)
				SetReplaces(pkg.Links)
			}
			if rp, ok := p.(pkg.RootPackageInterface); ok {
				target = rp
			} else if pp, ok := pkg.AsPackage(p); ok {
				target = pp
			} else {
				return a.errorf("%s has no such method", p.Class())
			}
			switch method {
			case "setRequires":
				target.SetRequires(links)
			case "setDevRequires":
				target.SetDevRequires(links)
			case "setConflicts":
				target.SetConflicts(links)
			case "setProvides":
				target.SetProvides(links)
			case "setReplaces":
				target.SetReplaces(links)
			}

			return nil
		})
	}
	type arraySetters interface {
		SetSuggests(*php.Array)
		SetAutoload(*php.Array)
		SetDevAutoload(*php.Array)
		SetExtra(*php.Array)
	}
	for _, method := range []string{"setSuggests", "setAutoload", "setDevAutoload", "setExtra"} {
		setter(method, func(p pkg.PackageInterface, a args) error {
			var target arraySetters
			if rp, ok := p.(pkg.RootPackageInterface); ok {
				target = rp
			} else if pp, ok := pkg.AsPackage(p); ok {
				target = pp
			} else {
				return a.errorf("%s has no such method", p.Class())
			}
			v := a.arrayOrEmpty(1)
			switch method {
			case "setSuggests":
				target.SetSuggests(v)
			case "setAutoload":
				target.SetAutoload(v)
			case "setDevAutoload":
				target.SetDevAutoload(v)
			case "setExtra":
				target.SetExtra(v)
			}

			return nil
		})
	}

	// CompletePackageInterface setters.
	completeSetter("setScripts", func(c pkg.CompletePackageInterface, a args) { c.SetScripts(a.arrayOrEmpty(1)) })
	completeSetter("setRepositories", func(c pkg.CompletePackageInterface, a args) { c.SetRepositories(a.arrayOrEmpty(1)) })
	completeSetter("setLicense", func(c pkg.CompletePackageInterface, a args) { c.SetLicense(a.arrayOrEmpty(1)) })
	completeSetter("setKeywords", func(c pkg.CompletePackageInterface, a args) { c.SetKeywords(a.arrayOrEmpty(1)) })
	completeSetter("setAuthors", func(c pkg.CompletePackageInterface, a args) { c.SetAuthors(a.arrayOrEmpty(1)) })
	completeSetter("setDescription", func(c pkg.CompletePackageInterface, a args) { c.SetDescription(nullString(a, 1)) })
	completeSetter("setHomepage", func(c pkg.CompletePackageInterface, a args) { c.SetHomepage(nullString(a, 1)) })
	completeSetter("setSupport", func(c pkg.CompletePackageInterface, a args) { c.SetSupport(a.arrayOrEmpty(1)) })
	completeSetter("setFunding", func(c pkg.CompletePackageInterface, a args) { c.SetFunding(a.arrayOrEmpty(1)) })
	completeSetter("setAbandoned", func(c pkg.CompletePackageInterface, a args) { c.SetAbandoned(a.at(1)) })
	completeSetter("setArchiveName", func(c pkg.CompletePackageInterface, a args) { c.SetArchiveName(nullString(a, 1)) })
	completeSetter("setArchiveExcludes", func(c pkg.CompletePackageInterface, a args) {
		c.SetArchiveExcludes(a.arrayOrEmpty(1))
	})

	// RootPackageInterface setters.
	rootSetter("setMinimumStability", func(p pkg.RootPackageInterface, a args) error {
		p.SetMinimumStability(a.str(1))
		return nil
	})
	rootSetter("setStabilityFlags", func(p pkg.RootPackageInterface, a args) error {
		p.SetStabilityFlags(a.arrayOrEmpty(1))
		return nil
	})
	rootSetter("setPreferStable", func(p pkg.RootPackageInterface, a args) error {
		p.SetPreferStable(a.boolean(1))
		return nil
	})
	rootSetter("setConfig", func(p pkg.RootPackageInterface, a args) error { p.SetConfig(a.arrayOrEmpty(1)); return nil })
	rootSetter("setReferences", func(p pkg.RootPackageInterface, a args) error {
		p.SetReferences(a.arrayOrEmpty(1))
		return nil
	})
	rootSetter("setAliases", func(p pkg.RootPackageInterface, a args) error { p.SetAliases(a.arrayOrEmpty(1)); return nil })

	// AliasPackage's own setter.
	setter("setRootPackageAlias", func(p pkg.PackageInterface, a args) error {
		alias, ok := p.(pkg.Alias)
		if !ok {
			return a.errorf("%s has no such method", p.Class())
		}
		alias.SetRootPackageAlias(a.boolean(1))

		return nil
	})
}
