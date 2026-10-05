// Ports tests/Composer/Test/Package/BasePackageTest.php,
// CompletePackageTest.php and RootAliasPackageTest.php.

package pkg_test

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

type repo struct{ name string }

func (r *repo) RepoName() string { return r.name }

func TestBasePackage_SetSameRepository(t *testing.T) {
	p := pkg.NewPackage("foo", "1.0.0.0", "1.0.0")
	r := &repo{"r"}

	if err := p.SetRepository(r); err != nil {
		t.Fatal(err)
	}

	if err := p.SetRepository(r); err != nil {
		t.Errorf("Set against the same repository is allowed: %v", err)
	}
}

func TestBasePackage_SetAnotherRepository(t *testing.T) {
	p := pkg.NewPackage("foo", "1.0.0.0", "1.0.0")
	if err := p.SetRepository(&repo{"a"}); err != nil {
		t.Fatal(err)
	}

	err := p.SetRepository(&repo{"b"})

	var logic *util.LogicError
	if !errors.As(err, &logic) || err.Error() != `Package "foo" cannot be added to repository "b" as it is already in repository "a".` {
		t.Errorf("got %v", err)
	}
}

func TestBasePackage_FormatVersionForDevPackage(t *testing.T) {
	for _, c := range pkgtest.Providers(t)["BasePackageTest::provideFormattedVersions"] {
		sourceReference, _ := c.Args.Get("sourceReference")
		truncate, _ := c.Args.Get("truncate")
		expected, _ := c.Args.Get("expected")

		p := pkg.NewPackage("foo", "dev-master", "PrettyVersion")
		p.SetSourceType(pkg.Str("git"))
		p.SetSourceReference(pkg.Str(sourceReference.(string)))

		if got := p.FullPrettyVersion(truncate.(bool), pkg.DisplaySourceRefIfDev); got != expected {
			t.Errorf("%v: got %q, want %q", sourceReference, got, expected)
		}
	}
}

func TestBasePackage_PackageNamesToRegexp(t *testing.T) {
	for _, c := range pkgtest.Providers(t)["BasePackageTest::dataPackageNamesToRegexp"] {
		// The provider lists all cases in one flat row; PHPUnit only runs
		// the first three values.
		names := c.Args.At(0).(*pkgtest.Arr).Strings()
		if got := pkg.PackageNamesToRegexp(names, c.Args.String(1)); got != c.Args.String(2) {
			t.Errorf("%q: got %q, want %q", names, got, c.Args.String(2))
		}
	}
}

func TestBasePackage_PackageNameToRegexpForms(t *testing.T) {
	for _, c := range []struct {
		names      []string
		wrap, want string
	}{
		{[]string{"php"}, "{^%s$}i", "{^php$}i"},
		{[]string{"*"}, "{^%s$}i", "{^.*$}i"},
		{[]string{"foo", "bar"}, "§%s§", "§foo|bar§"},
		{[]string{"vendor/*-bundle"}, "{^(?:%s)$}iD", `{^(?:vendor/.*\-bundle)$}iD`},
	} {
		if got := pkg.PackageNamesToRegexp(c.names, c.wrap); got != c.want {
			t.Errorf("%q: got %q, want %q", c.names, got, c.want)
		}
	}
}

func TestCompletePackage_NamingVersioningMarshalling(t *testing.T) {
	parser := pkg.NewVersionParser()

	for _, c := range pkgtest.Providers(t)["CompletePackageTest::providerVersioningSchemes"] {
		name, version := c.Args.String(0), c.Args.String(1)

		normVersion, err := parser.Normalize(version)
		if err != nil {
			t.Fatal(err)
		}

		p := pkg.NewPackage(name, normVersion, version)

		// testPackageHasExpectedNamingSemantics
		if p.Name() != php.Strtolower(name) {
			t.Errorf("name %q", p.Name())
		}

		// testPackageHasExpectedVersioningSemantics
		if p.PrettyVersion() != version || p.Version() != normVersion {
			t.Errorf("versions %q %q", p.PrettyVersion(), p.Version())
		}

		// testPackageHasExpectedMarshallingSemantics
		if p.String() != php.Strtolower(name)+"-"+normVersion {
			t.Errorf("string %q", p.String())
		}
	}
}

func TestCompletePackage_GetTargetDir(t *testing.T) {
	p := pkg.NewPackage("a", "1.0.0.0", "1.0")
	if p.TargetDir().Valid {
		t.Error("target dir set")
	}

	for _, c := range [][2]string{
		{"./../foo/", "foo/"},
		{"foo/../../../bar/", "foo/bar/"},
		{"../..", ""},
		{"..", ""},
		{"/..", ""},
		{"/foo/..", "foo/"},
		{"/foo/..//bar", "foo/bar"},
	} {
		p.SetTargetDir(pkg.Str(c[0]))
		if got := p.TargetDir(); got != pkg.Str(c[1]) {
			t.Errorf("%q: got %v, want %q", c[0], got, c[1])
		}
	}
}

func selfVersionLinks(linkType string) pkg.Links {
	return pkg.LinkList(pkg.NewLink("a", "b", semver.NewMatchAllConstraint(), linkType, pkg.Str("self.version")))
}

func TestRootAliasPackage_UpdateLinks(t *testing.T) {
	for _, c := range []struct {
		typ string
		set func(*pkg.RootAliasPackage, pkg.Links)
		get func(pkg.PackageInterface) pkg.Links
	}{
		{pkg.TypeRequire, (*pkg.RootAliasPackage).SetRequires, pkg.PackageInterface.Requires},
		{pkg.TypeDevRequire, (*pkg.RootAliasPackage).SetDevRequires, pkg.PackageInterface.DevRequires},
		{pkg.TypeConflict, (*pkg.RootAliasPackage).SetConflicts, pkg.PackageInterface.Conflicts},
		{pkg.TypeProvide, (*pkg.RootAliasPackage).SetProvides, pkg.PackageInterface.Provides},
		{pkg.TypeReplace, (*pkg.RootAliasPackage).SetReplaces, pkg.PackageInterface.Replaces},
	} {
		root := pkg.NewRootPackage("something/something", "1.0.0.0", "1.0.0")
		alias := pkg.NewRootAliasPackage(root, "1.0", "1.0.0.0")

		if c.get(alias).Len() != 0 {
			t.Errorf("%s: not empty", c.typ)
		}

		links := selfVersionLinks(c.typ)
		c.set(alias, links)

		if c.get(alias).Len() == 0 {
			t.Errorf("%s: alias links empty", c.typ)
		}

		// the root package receives the links too (as a map, PHP warns
		// about the list)
		if got := c.get(root); got.Len() != 1 || got.At(0) != links.At(0) {
			t.Errorf("%s: root links %v", c.typ, got)
		}
	}
}
