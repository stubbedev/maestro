// Packages created in PHP (docs/PLUGINS.md §5.3, "PHP-born data objects";
// phase 5): `new Package(...)`, `new CompletePackage(...)` or a plugin's
// subclass stays PHP's until it first crosses to maestro (added to a
// repository, returned from an installer, given to a setter). Then maestro
// builds its own package from the snapshot PHP sends, PHP's object becomes
// the mirror of that package (same object, so identity holds), and from
// then on its setters are maestro's.

package plugin

import (
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// adoptPackage is the rpc.MirrorFactory of the package family.
func (r *Runtime) adoptPackage(_ string, s *php.Array) (rpc.Mirror, error) {
	p, err := packageFromSnapshot(s)
	if err != nil {
		return nil, err
	}
	m := &packageMirror{r: r, p: p}
	r.bridge.object(p, func() rpc.Object { return m })

	return m, nil
}

// packageFromSnapshot builds maestro's package of a PHP package's
// properties (Maestro\Shim\Adapter\PackageAdapter::snapshot), with the
// constructor and setters Composer's class has.
func packageFromSnapshot(s *php.Array) (pkg.PackageInterface, error) {
	class, _ := s.GetString("class")
	name, _ := s.GetString("name")
	version, _ := s.GetString("version")
	prettyVersion, _ := s.GetString("prettyVersion")

	var p pkg.PackageInterface
	switch class {
	case pkg.ClassAliasPackage, pkg.ClassCompleteAliasPackage, pkg.ClassRootAliasPackage:
		target, _ := s.Get("aliasOf")
		m, ok := target.(*packageMirror)
		if !ok {
			return nil, &rpc.ProtocolError{Message: "an alias package created in PHP without a package maestro knows"}
		}
		switch class {
		case pkg.ClassRootAliasPackage:
			root, ok := m.p.(pkg.RootPackageInterface)
			if !ok {
				return nil, &rpc.ProtocolError{Message: "a RootAliasPackage of a package that is not a root package"}
			}
			p = pkg.NewRootAliasPackage(root, version, prettyVersion)
		case pkg.ClassCompleteAliasPackage:
			complete, ok := m.p.(pkg.CompletePackageInterface)
			if !ok {
				return nil, &rpc.ProtocolError{Message: "a CompleteAliasPackage of a package that is not complete"}
			}
			p = pkg.NewCompleteAliasPackage(complete, version, prettyVersion)
		default:
			p = pkg.NewAliasPackage(m.p, version, prettyVersion)
		}
		alias, _ := p.(pkg.Alias)
		if v, ok := s.Get("rootPackageAlias"); ok {
			alias.SetRootPackageAlias(php.ToBool(v))
		}
		if err := setSnapshotLinks(s, alias); err != nil {
			return nil, err
		}
	case pkg.ClassRootPackage:
		p = pkg.NewRootPackage(name, version, prettyVersion)
	case pkg.ClassCompletePackage:
		p = pkg.NewCompletePackage(name, version, prettyVersion)
	default:
		p = pkg.NewPackage(name, version, prettyVersion)
	}
	if id, ok := s.Get("id"); ok {
		p.SetID(int(php.ToInt(id)))
	}

	if pp, ok := pkg.AsPackage(p); ok {
		if err := fillPackage(pp, s); err != nil {
			return nil, err
		}
	}
	if c, ok := p.(*pkg.CompletePackage); ok {
		fillComplete(c, s)
	}
	if root, ok := p.(*pkg.RootPackage); ok {
		fillComplete(&root.CompletePackage, s)
		fillRoot(root, s)
	}

	return p, nil
}

// snapshotString is a ?string property.
func snapshotString(s *php.Array, key string) pkg.NullString {
	v, ok := s.Get(key)
	if !ok || v == nil {
		return pkg.NullString{}
	}

	return pkg.Str(php.ToString(v))
}

// snapshotArray is an array property; nil for null or absent.
func snapshotArray(s *php.Array, key string) *php.Array {
	v, _ := s.Get(key)
	a, _ := v.(*php.Array)

	return a
}

// snapshotLinks is a map of links.
func snapshotLinks(s *php.Array, key string) (pkg.Links, error) {
	var b pkg.LinksBuilder
	for k, v := range orEmptyArray(snapshotArray(s, key)).All() {
		l, ok := v.(linkValue)
		if !ok {
			return pkg.Links{}, &rpc.ProtocolError{Message: "a package's " + key + " holds something other than links"}
		}
		b.Set(k.String(), l.l)
	}

	return b.Build(), nil
}

// linkSetters are the link setters of packages and aliases.
type linkSetters interface {
	SetRequires(pkg.Links)
	SetDevRequires(pkg.Links)
	SetConflicts(pkg.Links)
	SetProvides(pkg.Links)
	SetReplaces(pkg.Links)
}

func setSnapshotLinks(s *php.Array, p any) error {
	target, ok := p.(linkSetters)
	if !ok {
		return nil
	}
	for _, f := range []struct {
		key string
		set func(pkg.Links)
	}{
		{"requires", target.SetRequires},
		{"devRequires", target.SetDevRequires},
		{"conflicts", target.SetConflicts},
		{"provides", target.SetProvides},
		{"replaces", target.SetReplaces},
	} {
		if _, ok := s.Get(f.key); !ok {
			continue
		}
		links, err := snapshotLinks(s, f.key)
		if err != nil {
			return err
		}
		f.set(links)
	}

	return nil
}

func fillPackage(p *pkg.Package, s *php.Array) error {
	if t := snapshotString(s, "type"); t.Valid {
		p.SetType(t.S)
	}
	p.SetTargetDir(snapshotString(s, "targetDir"))
	p.SetInstallationSource(snapshotString(s, "installationSource"))
	p.SetSourceType(snapshotString(s, "sourceType"))
	p.SetSourceURL(snapshotString(s, "sourceUrl"))
	p.SetSourceReference(snapshotString(s, "sourceReference"))
	p.SetSourceMirrors(snapshotArray(s, "sourceMirrors"))
	p.SetDistType(snapshotString(s, "distType"))
	p.SetDistURL(snapshotString(s, "distUrl"))
	p.SetDistReference(snapshotString(s, "distReference"))
	p.SetDistSha1Checksum(snapshotString(s, "distSha1Checksum"))
	p.SetDistMirrors(snapshotArray(s, "distMirrors"))
	if d := snapshotString(s, "releaseDate"); d.Valid {
		t, err := time.Parse(releaseDateLayout, d.S)
		if err != nil {
			return &rpc.ProtocolError{Message: "invalid release date " + d.S}
		}
		p.SetReleaseDate(t, true)
	}
	p.SetExtra(orEmptyArray(snapshotArray(s, "extra")))
	p.SetBinaries(orEmptyArray(snapshotArray(s, "binaries")))
	if n := snapshotString(s, "notificationUrl"); n.Valid {
		p.SetNotificationURL(n.S)
	}
	if err := setSnapshotLinks(s, p); err != nil {
		return err
	}
	p.SetSuggests(orEmptyArray(snapshotArray(s, "suggests")))
	p.SetAutoload(orEmptyArray(snapshotArray(s, "autoload")))
	p.SetDevAutoload(orEmptyArray(snapshotArray(s, "devAutoload")))
	p.SetIncludePaths(orEmptyArray(snapshotArray(s, "includePaths")))
	if v, ok := s.Get("isDefaultBranch"); ok {
		p.SetIsDefaultBranch(php.ToBool(v))
	}
	p.SetTransportOptions(orEmptyArray(snapshotArray(s, "transportOptions")))
	p.SetPhpExt(snapshotArray(s, "phpExt"))

	return nil
}

func fillComplete(c *pkg.CompletePackage, s *php.Array) {
	c.SetRepositories(orEmptyArray(snapshotArray(s, "repositories")))
	c.SetLicense(orEmptyArray(snapshotArray(s, "license")))
	c.SetKeywords(orEmptyArray(snapshotArray(s, "keywords")))
	c.SetAuthors(orEmptyArray(snapshotArray(s, "authors")))
	c.SetDescription(snapshotString(s, "description"))
	c.SetHomepage(snapshotString(s, "homepage"))
	c.SetScripts(orEmptyArray(snapshotArray(s, "scripts")))
	c.SetSupport(orEmptyArray(snapshotArray(s, "support")))
	c.SetFunding(orEmptyArray(snapshotArray(s, "funding")))
	if v, ok := s.Get("abandoned"); ok {
		c.SetAbandoned(v)
	}
	c.SetArchiveName(snapshotString(s, "archiveName"))
	c.SetArchiveExcludes(orEmptyArray(snapshotArray(s, "archiveExcludes")))
}

func fillRoot(r *pkg.RootPackage, s *php.Array) {
	if v, ok := s.GetString("minimumStability"); ok {
		r.SetMinimumStability(v)
	}
	if v, ok := s.Get("preferStable"); ok {
		r.SetPreferStable(php.ToBool(v))
	}
	r.SetStabilityFlags(orEmptyArray(snapshotArray(s, "stabilityFlags")))
	r.SetConfig(orEmptyArray(snapshotArray(s, "config")))
	r.SetReferences(orEmptyArray(snapshotArray(s, "references")))
	r.SetAliases(orEmptyArray(snapshotArray(s, "aliases")))
}
