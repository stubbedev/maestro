// Ports src/Composer/Package/Dumper/ArrayDumper.php.

package dumper

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// RFC3339 is PHP's DATE_RFC3339 ("Y-m-d\TH:i:sP") as a Go layout.
const RFC3339 = "2006-01-02T15:04:05-07:00"

// ArrayDumper ports Composer\Package\Dumper\ArrayDumper: a package as the
// array composer.lock and the repositories store.
type ArrayDumper struct{}

// Dump ports ArrayDumper::dump. Nested arrays of the result are shared
// with the package. It fails where PHP throws: a link without a pretty
// constraint.
func (ArrayDumper) Dump(p pkg.PackageInterface) (*php.Array, error) {
	data := php.NewArrayCap(24)
	data.Set("name", p.PrettyName())
	data.Set("version", p.PrettyVersion())
	data.Set("version_normalized", p.Version())

	if targetDir := p.TargetDir(); targetDir.Valid {
		data.Set("target-dir", targetDir.S)
	}

	if sourceType := p.SourceType(); sourceType.Valid {
		source := php.NewArrayCap(4)
		source.Set("type", sourceType.S)
		source.Set("url", p.SourceURL().Value())

		if ref := p.SourceReference(); ref.Valid {
			source.Set("reference", ref.S)
		}

		if mirrors := p.SourceMirrors(); mirrors != nil && mirrors.Len() > 0 {
			source.Set("mirrors", mirrors)
		}

		data.Set("source", source)
	}

	if distType := p.DistType(); distType.Valid {
		dist := php.NewArrayCap(5)
		dist.Set("type", distType.S)
		dist.Set("url", p.DistURL().Value())

		if ref := p.DistReference(); ref.Valid {
			dist.Set("reference", ref.S)
		}

		if sum := p.DistSha1Checksum(); sum.Valid {
			dist.Set("shasum", sum.S)
		}

		if mirrors := p.DistMirrors(); mirrors != nil && mirrors.Len() > 0 {
			dist.Set("mirrors", mirrors)
		}

		data.Set("dist", dist)
	}

	for _, t := range pkg.SupportedLinkTypes() {
		links := pkg.LinksByMethod(p, t.Method)
		if links.Len() == 0 {
			continue
		}

		m := php.NewArrayCap(links.Len())
		for link := range links.Values() {
			pc, err := link.PrettyConstraint()
			if err != nil {
				return nil, err
			}

			m.Set(link.Target(), pc)
		}

		php.Ksort(m, php.SortRegular)
		data.Set(t.Type, m)
	}

	if suggests := p.Suggests(); suggests.Len() > 0 {
		suggests = suggests.Clone()
		php.Ksort(suggests, php.SortRegular)
		data.Set("suggest", suggests)
	}

	if date, ok := p.ReleaseDate(); ok {
		data.Set("time", date.Format(RFC3339))
	}

	if p.IsDefaultBranch() {
		data.Set("default-branch", true)
	}

	dumpValue(data, "bin", p.Binaries())
	data.Set("type", p.Type())
	dumpValue(data, "extra", p.Extra())
	dumpValue(data, "installation-source", p.InstallationSource().Value())
	dumpValue(data, "autoload", p.Autoload())
	dumpValue(data, "autoload-dev", p.DevAutoload())
	dumpValue(data, "notification-url", p.NotificationURL().Value())
	dumpValue(data, "include-path", p.IncludePaths())
	dumpValue(data, "php-ext", p.PhpExt())

	if c, ok := p.(pkg.CompletePackageInterface); ok {
		archiveName := c.ArchiveName()
		archiveExcludes := c.ArchiveExcludes()

		if php.ToBool(archiveName.Value()) || archiveExcludes.Len() > 0 {
			archive := php.NewArrayCap(2)
			if php.ToBool(archiveName.Value()) {
				archive.Set("name", archiveName.S)
			}

			if archiveExcludes.Len() > 0 {
				archive.Set("exclude", archiveExcludes)
			}

			data.Set("archive", archive)
		}

		dumpValue(data, "scripts", c.Scripts())
		dumpValue(data, "license", c.License())
		dumpValue(data, "authors", c.Authors())
		dumpValue(data, "description", c.Description().Value())
		dumpValue(data, "homepage", c.Homepage().Value())

		if keywords := c.Keywords(); keywords.Len() > 0 {
			keywords = keywords.Clone()
			php.Sort(keywords, php.SortRegular)
			data.Set("keywords", keywords)
		}

		dumpValue(data, "repositories", c.Repositories())
		dumpValue(data, "support", c.Support())
		dumpValue(data, "funding", c.Funding())

		if c.IsAbandoned() {
			if r := c.ReplacementPackage(); php.ToBool(r.Value()) {
				data.Set("abandoned", r.S)
			} else {
				data.Set("abandoned", true)
			}
		}
	}

	if r, ok := p.(pkg.RootPackageInterface); ok {
		if minimumStability := r.MinimumStability(); minimumStability != "" {
			data.Set("minimum-stability", minimumStability)
		}
	}

	if options := p.TransportOptions(); options.Len() > 0 {
		data.Set("transport-options", options)
	}

	return data, nil
}

// dumpValue ports ArrayDumper::dumpValues for one key: set unless null or
// an empty array.
func dumpValue(data *php.Array, key string, value any) {
	switch v := value.(type) {
	case nil:
		return
	case *php.Array:
		if v == nil || v.Len() == 0 {
			return
		}
	}

	data.Set(key, value)
}
