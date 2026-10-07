package pkg_test

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

func link(source, target, pretty string) *pkg.Link {
	c, err := pkg.NewVersionParser().ParseConstraints(pretty)
	if err != nil {
		panic(err)
	}

	return pkg.NewLink(source, target, c, pkg.TypeRequire, pkg.Str(pretty))
}

func keys(l pkg.Links) []string {
	var out []string
	for k := range l.All() {
		out = append(out, k)
	}

	return out
}

func TestClass(t *testing.T) {
	p := pkg.NewPackage("a/b", "1.0.0.0", "1.0.0")
	c := pkg.NewCompletePackage("a/b", "1.0.0.0", "1.0.0")
	r := pkg.NewRootPackage("a/b", "1.0.0.0", "1.0.0")

	for _, x := range []struct {
		p    pkg.PackageInterface
		want string
	}{
		{p, pkg.ClassPackage},
		{c, pkg.ClassCompletePackage},
		{r, pkg.ClassRootPackage},
		{pkg.NewAliasPackage(p, "2.0.0.0", "2.0.0"), pkg.ClassAliasPackage},
		{pkg.NewCompleteAliasPackage(c, "2.0.0.0", "2.0.0"), pkg.ClassCompleteAliasPackage},
		{pkg.NewRootAliasPackage(r, "2.0.0.0", "2.0.0"), pkg.ClassRootAliasPackage},
	} {
		if got := x.p.PHPClass(); got != x.want {
			t.Errorf("%T: %q", x.p, got)
		}
	}

	if got, ok := pkg.AsPackage(r); !ok || got != &r.Package {
		t.Error("AsPackage(root)")
	}

	if _, ok := pkg.AsCompletePackage(p); ok {
		t.Error("a Package is not a CompletePackage")
	}
}

func TestRevChangesOnEverySetter(t *testing.T) {
	r := pkg.NewRootPackage("a/b", "1.0.0.0", "1.0.0")
	alias := pkg.NewRootAliasPackage(r, "2.0.0.0", "2.0.0")

	for _, set := range []func(){
		func() { r.SetType("library") },
		func() { r.SetExtra(php.NewArray()) },
		func() { r.SetDistURL(pkg.Str("https://x")) },
		func() { r.SetRequires(pkg.Links{}) },
		func() { r.SetDescription(pkg.Str("d")) },
		func() { r.SetAbandoned(true) },
		func() { r.SetMinimumStability("dev") },
		func() { r.SetStabilityFlags(php.NewArray()) },
		func() { r.ReplaceVersion("1.0.1.0", "1.0.1") },
		func() { r.SetID(5) },
		func() { alias.SetRootPackageAlias(true) },
		func() { alias.SetDevRequires(pkg.Links{}) },
		func() { alias.SetSourceReference(pkg.Str("abc")) },
	} {
		before, aliasBefore := r.Rev(), alias.Rev()
		set()

		if r.Rev() == before && alias.Rev() == aliasBefore {
			t.Error("a setter did not change Rev")
		}

		if alias.Rev() < aliasBefore {
			t.Error("the alias Rev went down")
		}
	}
}

func TestClone(t *testing.T) {
	r := pkg.NewRootPackage("a/b", "1.0.0.0", "1.0.0")
	r.SetID(3)

	if err := r.SetRepository(&repo{"x"}); err != nil {
		t.Fatal(err)
	}

	c := pkg.Clone(r).(*pkg.RootPackage)
	if c == r || c.ID() != -1 || c.Repository() != nil || c.Name() != "a/b" {
		t.Errorf("clone %+v", c)
	}

	alias := pkg.NewRootAliasPackage(r, "2.0.0.0", "2.0.0")
	ca := pkg.Clone(alias).(*pkg.RootAliasPackage)

	if ca.AliasOf() == pkg.PackageInterface(r) {
		t.Error("RootAliasPackage::__clone clones the aliased package")
	}

	ca.SetMinimumStability("beta")

	if r.MinimumStability() == "beta" {
		t.Error("the clone shares the root package")
	}

	plain := pkg.NewCompletePackage("c/d", "1.0.0.0", "1.0.0")
	cp := pkg.Clone(pkg.NewCompleteAliasPackage(plain, "2.0.0.0", "2.0.0")).(*pkg.CompleteAliasPackage)

	if cp.AliasOf() != pkg.PackageInterface(plain) {
		t.Error("an alias clone shares the aliased package")
	}
}

func TestEqualsLooksThroughAliases(t *testing.T) {
	r := pkg.NewRootPackage("a/b", "1.0.0.0", "1.0.0")
	alias := pkg.NewRootAliasPackage(r, "2.0.0.0", "2.0.0")
	other := pkg.NewRootPackage("a/b", "1.0.0.0", "1.0.0")

	if !r.Equals(alias) || !alias.Equals(r) || r.Equals(other) || !r.Equals(r) {
		t.Error("equals")
	}
}

func TestSetRequiresConvertsLists(t *testing.T) {
	p := pkg.NewPackage("a/b", "1.0.0.0", "1.0.0")
	p.SetRequires(pkg.LinkList(link("a/b", "c/d", "^1.0"), link("a/b", "e/f", "^2.0")))

	if got := keys(p.Requires()); len(got) != 2 || got[0] != "c/d" || got[1] != "e/f" {
		t.Errorf("keys %q", got)
	}
}

func TestLinks(t *testing.T) {
	a, b, a2 := link("x/y", "a/a", "^1.0"), link("x/y", "b/b", "^1.0"), link("x/y", "a/a", "^2.0")

	l := pkg.LinksOf(a, b, a2)
	if got := keys(l); len(got) != 2 || l.At(0) != a2 {
		t.Errorf("LinksOf: %q %v", got, l.At(0))
	}

	l = l.With("zz", a)
	if got := keys(l); len(got) != 3 || got[2] != "zz" || l.At(2) != a {
		t.Errorf("With: %q", got)
	}

	l = l.Without("a/a")
	if got := keys(l); len(got) != 2 || got[0] != "b/b" || l.Has("a/a") {
		t.Errorf("Without: %q", got)
	}

	if got, ok := l.Get("zz"); !ok || got != a {
		t.Error("Get")
	}

	var b2 pkg.LinksBuilder
	for i := range 40 {
		b2.Set(php.ToString(int64(i%20)), a)
	}

	if built := b2.Build(); built.Len() != 20 {
		t.Errorf("builder kept %d keys", built.Len())
	}
}

func TestAliasSelfVersionLinks(t *testing.T) {
	p := pkg.NewCompletePackage("a/b", "dev-main", "dev-main")
	p.SetReplaces(pkg.LinksOf(
		pkg.NewLink("a/b", "c/d", semver.NewConstraintOp(semver.OpEQ, "dev-main"), pkg.TypeReplace, pkg.Str("self.version")),
		pkg.NewLink("a/b", "e/f", semver.NewMatchAllConstraint(), pkg.TypeReplace, pkg.Str("*")),
	))
	p.SetRequires(pkg.LinksOf(pkg.NewLink("a/b", "g/h", semver.NewConstraintOp(semver.OpEQ, "dev-main"), pkg.TypeRequire, pkg.Str("self.version"))))

	alias := pkg.NewCompleteAliasPackage(p, pkg.DefaultBranchAlias, pkg.DefaultBranchAlias)

	// replaces keep the original link and add one for the alias version,
	// appended with an int key as array_merge does
	if got := keys(alias.Replaces()); len(got) != 3 || got[2] != "0" {
		t.Fatalf("replaces %q", got)
	}

	if l := alias.Replaces().At(2); l.Constraint().String() != "== "+pkg.DefaultBranchAlias || l.Constraint().PrettyString() != "dev-main" {
		t.Errorf("replace %v / %q", l, l.Constraint().PrettyString())
	}

	if l, _ := alias.Requires().Get("g/h"); l.Constraint().String() != "== "+pkg.DefaultBranchAlias || !alias.HasSelfVersionRequires() {
		t.Errorf("require %v", l)
	}

	if names := alias.Names(true); len(names) != 3 || names[2] != "e/f" {
		t.Errorf("names %q", names)
	}
}

func TestPrettyConstraintMissing(t *testing.T) {
	l := pkg.NewLink("a", "b", semver.NewMatchAllConstraint(), pkg.TypeRequire, pkg.NullString{})

	_, err := l.PrettyConstraint()

	var uv *util.UnexpectedValueError
	if !errors.As(err, &uv) || err.Error() != "Link a requires b (*) has been misconfigured and had no prettyConstraint given." {
		t.Errorf("got %v", err)
	}

	if d := pkg.NewLink("a", "b", semver.NewMatchAllConstraint(), "custom", pkg.NullString{}).Description(); d != "custom" {
		t.Errorf("description %q", d)
	}
}

func TestSetSourceDistReferences(t *testing.T) {
	p := pkg.NewPackage("a/b", "dev-main", "dev-main")
	p.SetDistURL(pkg.Str("https://api.github.com/repos/a/b/zipball/0123456789abcdef0123456789abcdef01234567"))
	p.SetSourceDistReferences("fedcba9876543210fedcba9876543210fedcba98")

	if p.DistURL() != pkg.Str("https://api.github.com/repos/a/b/zipball/fedcba9876543210fedcba9876543210fedcba98") ||
		p.DistReference() != pkg.Str("fedcba9876543210fedcba9876543210fedcba98") {
		t.Errorf("got %v %v", p.DistURL(), p.DistReference())
	}

	q := pkg.NewPackage("a/b", "dev-main", "dev-main")
	q.SetDistURL(pkg.Str("https://example.org/a.zip"))
	q.SetSourceDistReferences("abc")

	if q.DistReference().Valid || q.SourceReference() != pkg.Str("abc") {
		t.Errorf("got %v %v", q.DistReference(), q.SourceReference())
	}
}

func TestFullPrettyVersionInvalidMode(t *testing.T) {
	defer func() {
		var uv *util.UnexpectedValueError
		if err, ok := recover().(error); !ok || !errors.As(err, &uv) || err.Error() != "Display mode 7 is not supported" {
			t.Errorf("recovered %v", err)
		}
	}()

	p := pkg.NewPackage("a/b", "dev-main", "dev-main")
	p.SetSourceType(pkg.Str("git"))
	p.FullPrettyVersion(true, 7)
}

func TestGetViewSourceURL(t *testing.T) {
	p := pkg.NewCompletePackage("a/b", "1.0.0.0", "1.0.0")
	p.SetSourceURL(pkg.Str("https://src"))
	p.SetHomepage(pkg.Str("https://home"))

	if pkg.GetViewSourceURL(p) != pkg.Str("https://src") || pkg.GetViewSourceOrHomepageURL(p) != pkg.Str("https://src") {
		t.Error("source url")
	}

	p.SetSupport(php.ArrayOf("source", "https://support"))

	if pkg.GetViewSourceURL(p) != pkg.Str("https://support") {
		t.Error("support source")
	}

	q := pkg.NewCompletePackage("a/b", "1.0.0.0", "1.0.0")
	q.SetHomepage(pkg.Str(""))

	if got := pkg.GetViewSourceOrHomepageURL(q); got.Valid {
		t.Errorf("got %v", got)
	}
}
