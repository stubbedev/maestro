// Ports tests/Composer/Test/Package/Loader/ArrayLoaderTest.php.

package loader_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

func mustLoad(t *testing.T, l loader.LoaderInterface, config *php.Array) pkg.PackageInterface {
	t.Helper()

	p, err := l.Load(config, pkg.ClassCompletePackage)
	if err != nil {
		t.Fatal(err)
	}

	return p
}

func TestArrayLoader_SelfVersion(t *testing.T) {
	p := mustLoad(t, loader.NewArrayLoader(nil, false), php.ArrayOf("name", "A", "version", "1.2.3.4", "replace", php.ArrayOf("foo", "self.version")))

	link, _ := p.Replaces().Get("foo")
	if got := link.Constraint().String(); got != "== 1.2.3.4" {
		t.Errorf("got %q", got)
	}
}

func TestArrayLoader_TypeDefault(t *testing.T) {
	l := loader.NewArrayLoader(nil, false)

	if got := mustLoad(t, l, php.ArrayOf("name", "A", "version", "1.0")).Type(); got != "library" {
		t.Errorf("got %q", got)
	}

	if got := mustLoad(t, l, php.ArrayOf("name", "A", "version", "1.0", "type", "foo")).Type(); got != "foo" {
		t.Errorf("got %q", got)
	}
}

func TestArrayLoader_NormalizedVersionOptimization(t *testing.T) {
	l := loader.NewArrayLoader(nil, false)

	if got := mustLoad(t, l, php.ArrayOf("name", "A", "version", "1.2.3")).Version(); got != "1.2.3.0" {
		t.Errorf("got %q", got)
	}

	if got := mustLoad(t, l, php.ArrayOf("name", "A", "version", "1.2.3", "version_normalized", "1.2.3.4")).Version(); got != "1.2.3.4" {
		t.Errorf("got %q", got)
	}
}

func parseDumpCases(t *testing.T) []*php.Array {
	t.Helper()

	var out []*php.Array
	for _, c := range pkgtest.Providers(t)["ArrayLoaderTest::parseDumpProvider"] {
		out = append(out, c.Args.At(0).(*pkgtest.Arr).PHP())
	}

	return out
}

func checkParseDump(t *testing.T, l *loader.ArrayLoader, config, expected *php.Array) {
	t.Helper()

	dumped, err := dumper.ArrayDumper{}.Dump(mustLoad(t, l, config))
	if err != nil {
		t.Fatal(err)
	}

	if !php.LooseEquals(expected, dumped) {
		t.Errorf("dump %s, want %s", enc(t, dumped), enc(t, expected))
	}
}

func withoutTransportOptions(config *php.Array) *php.Array {
	expected := config.Clone()
	expected.Delete("transport-options")

	return expected
}

func TestArrayLoader_ParseDumpDefaultLoadConfig(t *testing.T) {
	for _, config := range parseDumpCases(t) {
		checkParseDump(t, loader.NewArrayLoader(nil, false), config, withoutTransportOptions(config))
	}
}

func TestArrayLoader_ParseDumpTrueLoadConfig(t *testing.T) {
	for _, config := range parseDumpCases(t) {
		checkParseDump(t, loader.NewArrayLoader(nil, true), config, config)
	}
}

func TestArrayLoader_ParseDumpFalseLoadConfig(t *testing.T) {
	for _, config := range parseDumpCases(t) {
		checkParseDump(t, loader.NewArrayLoader(nil, false), config, withoutTransportOptions(config))
	}
}

func TestArrayLoader_PackageWithBranchAlias(t *testing.T) {
	l := loader.NewArrayLoader(nil, false)

	for _, c := range []struct {
		name, version, source, target string
		alias                         bool
		pretty                        string
	}{
		{"A", "dev-master", "dev-master", "1.0.x-dev", true, "1.0.x-dev"},
		{"A", "dev-master", "dev-master", "1.0-dev", true, "1.0.x-dev"},
		{"B", "4.x-dev", "4.x-dev", "4.0.x-dev", true, "4.0.x-dev"},
		{"B", "4.x-dev", "4.x-dev", "4.0-dev", true, "4.0.x-dev"},
		{"C", "4.x-dev", "4.x-dev", "3.4.x-dev", false, "4.x-dev"},
	} {
		p := mustLoad(t, l, php.ArrayOf("name", c.name, "version", c.version,
			"extra", php.ArrayOf("branch-alias", php.ArrayOf(c.source, c.target))))

		if _, isAlias := p.(pkg.Alias); isAlias != c.alias {
			t.Errorf("%v: alias %v", c, isAlias)
		}

		if _, ok := p.(pkg.CompletePackageInterface); !ok {
			t.Errorf("%v: not complete", c)
		}

		if p.PrettyVersion() != c.pretty {
			t.Errorf("%v: pretty version %q", c, p.PrettyVersion())
		}
	}
}

func TestArrayLoader_PackageAliasingWithoutBranchAlias(t *testing.T) {
	l := loader.NewArrayLoader(nil, false)

	// non-numeric gets a default alias
	p := mustLoad(t, l, php.ArrayOf("name", "A", "version", "dev-main", "default-branch", true))
	if _, ok := p.(pkg.Alias); !ok || p.PrettyVersion() != pkg.DefaultBranchAlias {
		t.Errorf("default alias: %T %q", p, p.PrettyVersion())
	}

	// non-default branch gets no alias even if non-numeric
	p = mustLoad(t, l, php.ArrayOf("name", "A", "version", "dev-main", "default-branch", false))
	if _, ok := p.(*pkg.CompletePackage); !ok || p.PrettyVersion() != "dev-main" {
		t.Errorf("non-default: %T %q", p, p.PrettyVersion())
	}

	// default branch gets no alias if already numeric
	for _, version := range []string{"2.x-dev", "v2.x-dev"} {
		p = mustLoad(t, l, php.ArrayOf("name", "A", "version", version, "default-branch", true))
		if _, ok := p.(*pkg.CompletePackage); !ok || p.Version() != "2.9999999.9999999.9999999-dev" {
			t.Errorf("%s: %T %q", version, p, p.Version())
		}
	}
}

func TestArrayLoader_Abandoned(t *testing.T) {
	p := mustLoad(t, loader.NewArrayLoader(nil, false), php.ArrayOf("name", "A", "version", "1.2.3.4", "abandoned", "foo/bar")).(pkg.CompletePackageInterface)
	if !p.IsAbandoned() || p.ReplacementPackage() != pkg.Str("foo/bar") {
		t.Errorf("abandoned %v, replacement %v", p.IsAbandoned(), p.ReplacementPackage())
	}
}

func TestArrayLoader_NotAbandoned(t *testing.T) {
	p := mustLoad(t, loader.NewArrayLoader(nil, false), php.ArrayOf("name", "A", "version", "1.2.3.4")).(pkg.CompletePackageInterface)
	if p.IsAbandoned() {
		t.Error("abandoned")
	}
}

func TestArrayLoader_PluginApiVersionAreKeptAsDeclared(t *testing.T) {
	l := loader.NewArrayLoader(nil, false)

	for _, c := range pkgtest.Providers(t)["ArrayLoaderTest::providePluginApiVersions"] {
		apiVersion := c.Args.String(0)

		links, err := l.ParseLinks("Plugin", "9.9.9", pkg.TypeRequire, php.ArrayOf("composer-plugin-api", apiVersion))
		if err != nil {
			t.Fatal(err)
		}

		link, ok := links.Get("composer-plugin-api")
		if !ok || link.Constraint().PrettyString() != apiVersion {
			t.Errorf("%q: %v", apiVersion, link)
		}
	}
}

func TestArrayLoader_PluginApiVersionDoesSupportSelfVersion(t *testing.T) {
	links, err := loader.NewArrayLoader(nil, false).ParseLinks("Plugin", "6.6.6", pkg.TypeRequire, php.ArrayOf("composer-plugin-api", "self.version"))
	if err != nil {
		t.Fatal(err)
	}

	if link, ok := links.Get("composer-plugin-api"); !ok || link.Constraint().PrettyString() != "6.6.6" {
		t.Errorf("got %v", link)
	}
}

func TestArrayLoader_ParseLinksIntegerTarget(t *testing.T) {
	links, err := loader.NewArrayLoader(nil, false).ParseLinks("Plugin", "9.9.9", pkg.TypeRequire, php.ArrayOf("1", "dev-main"))
	if err != nil {
		t.Fatal(err)
	}

	if !links.Has("1") {
		t.Error("no key 1")
	}
}

func TestArrayLoader_ParseLinksInvalidVersion(t *testing.T) {
	_, err := loader.NewArrayLoader(nil, false).ParseLinks("Plugin", "9.9.9", pkg.TypeRequire, php.ArrayOf("composer-plugin-api", "^^^"))
	checkException(t, "parseLinks", err, "UnexpectedValueException",
		`Link constraint in Plugin requires > composer-plugin-api should be a valid version constraint, got "^^^"`)
}

func TestArrayLoader_NoneStringVersion(t *testing.T) {
	p := mustLoad(t, loader.NewArrayLoader(nil, false), php.ArrayOf("name", "acme/package", "version", 1))
	if p.PrettyVersion() != "1" {
		t.Errorf("got %q", p.PrettyVersion())
	}
}

func TestArrayLoader_NoneStringSourceDistReference(t *testing.T) {
	p := mustLoad(t, loader.NewArrayLoader(nil, false), php.ArrayOf(
		"name", "acme/package",
		"version", "dev-main",
		"source", php.ArrayOf("type", "svn", "url", "https://example.org/", "reference", 2019),
		"dist", php.ArrayOf("type", "zip", "url", "https://example.org/", "reference", 2019),
	))

	if p.SourceReference() != pkg.Str("2019") || p.DistReference() != pkg.Str("2019") {
		t.Errorf("got %v %v", p.SourceReference(), p.DistReference())
	}
}

func TestArrayLoader_BranchAliasIntegerIndex(t *testing.T) {
	_, ok, err := loader.NewArrayLoader(nil, false).GetBranchAlias(php.ArrayOf(
		"name", "acme/package",
		"version", "dev-1",
		"extra", php.ArrayOf("branch-alias", php.ArrayOf("1", "1.3-dev")),
		"dist", php.ArrayOf("type", "zip", "url", "https://example.org/"),
	))
	if err != nil || ok {
		t.Errorf("got %v %v", ok, err)
	}
}

func TestArrayLoader_PackageLinksRequire(t *testing.T) {
	p := mustLoad(t, loader.NewArrayLoader(nil, false), php.ArrayOf("name", "acme/package", "version", "dev-1", "require", php.ArrayOf("foo/bar", "1.0")))
	if link, ok := p.Requires().Get("foo/bar"); !ok || link.Constraint().PrettyString() != "1.0" {
		t.Errorf("got %v", link)
	}
}

func TestArrayLoader_PackageLinksRequireInvalid(t *testing.T) {
	p := mustLoad(t, loader.NewArrayLoader(nil, false), php.ArrayOf("name", "acme/package", "version", "dev-1",
		"require", php.ArrayOf("foo/bar", php.ArrayOf("random-string", "1.0"))))
	if p.Requires().Len() != 0 {
		t.Errorf("got %d requires", p.Requires().Len())
	}
}

func TestArrayLoader_PackageLinksReplace(t *testing.T) {
	p := mustLoad(t, loader.NewArrayLoader(nil, false), php.ArrayOf("name", "acme/package", "version", "dev-1", "replace", php.ArrayOf("coyote/package", "self.version")))
	if link, ok := p.Replaces().Get("coyote/package"); !ok || link.Constraint().PrettyString() != "dev-1" {
		t.Errorf("got %v", link)
	}
}

func TestArrayLoader_PackageLinksReplaceInvalid(t *testing.T) {
	p := mustLoad(t, loader.NewArrayLoader(nil, false), php.ArrayOf("name", "acme/package", "version", "dev-1", "replace", "coyote/package"))
	if p.Replaces().Len() != 0 {
		t.Errorf("got %d replaces", p.Replaces().Len())
	}
}

func TestArrayLoader_SupportStringValue(t *testing.T) {
	p := mustLoad(t, loader.NewArrayLoader(nil, false), php.ArrayOf("name", "acme/package", "version", "dev-1", "support", "https://example.org"))
	if n := p.(pkg.CompletePackageInterface).Support().Len(); n != 0 {
		t.Errorf("got %d", n)
	}
}

func TestArrayLoader_InvalidVersion(t *testing.T) {
	_, err := loader.NewArrayLoader(nil, false).Load(php.ArrayOf("name", "acme/package", "version", "AA"), pkg.ClassCompletePackage)
	checkException(t, "load", err, "UnexpectedValueException", `Failed to normalize version for package "acme/package": Invalid version string "AA"`)
}
