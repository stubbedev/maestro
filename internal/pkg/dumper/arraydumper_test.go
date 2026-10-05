// Ports tests/Composer/Test/Package/Dumper/ArrayDumperTest.php.

package dumper_test

import (
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
)

func dump(t *testing.T, p pkg.PackageInterface) *php.Array {
	t.Helper()

	config, err := dumper.ArrayDumper{}.Dump(p)
	if err != nil {
		t.Fatal(err)
	}

	return config
}

func TestArrayDumper_RequiredInformation(t *testing.T) {
	config := dump(t, pkg.NewCompletePackage("dummy/pkg", "1.0.0.0", "1.0.0"))
	want := php.ArrayOf("name", "dummy/pkg", "version", "1.0.0", "version_normalized", "1.0.0.0", "type", "library")

	if !php.LooseEquals(want, config) {
		t.Errorf("got %v", config)
	}
}

func TestArrayDumper_RootPackage(t *testing.T) {
	p := pkg.NewRootPackage("__root__", "1.0.0.0", "1.0.0")
	p.SetMinimumStability("dev")

	if v, _ := dump(t, p).Get("minimum-stability"); v != "dev" {
		t.Errorf("got %v", v)
	}
}

func TestArrayDumper_DumpAbandoned(t *testing.T) {
	p := pkg.NewCompletePackage("dummy/pkg", "1.0.0.0", "1.0.0")
	p.SetAbandoned(true)

	if v, _ := dump(t, p).Get("abandoned"); v != true {
		t.Errorf("got %v", v)
	}
}

func TestArrayDumper_DumpAbandonedReplacement(t *testing.T) {
	p := pkg.NewCompletePackage("dummy/pkg", "1.0.0.0", "1.0.0")
	p.SetAbandoned("foo/bar")

	if v, _ := dump(t, p).Get("abandoned"); v != "foo/bar" {
		t.Errorf("got %v", v)
	}
}

// set calls $package->{'set'.ucfirst($method)}($value).
func set(t *testing.T, p *pkg.RootPackage, method string, value any) {
	t.Helper()

	arr := func() *php.Array { return value.(*pkgtest.Arr).PHP() }
	links := func() pkg.Links { return value.(*pkgtest.Arr).Links() }

	switch method {
	case "type":
		p.SetType(value.(string))
	case "ReleaseDate":
		p.SetReleaseDate(value.(time.Time), true)
	case "authors":
		p.SetAuthors(arr())
	case "homepage":
		p.SetHomepage(pkg.Str(value.(string)))
	case "description":
		p.SetDescription(pkg.Str(value.(string)))
	case "keywords":
		p.SetKeywords(arr())
	case "binaries":
		p.SetBinaries(arr())
	case "license":
		p.SetLicense(arr())
	case "autoload":
		p.SetAutoload(arr())
	case "repositories":
		p.SetRepositories(arr())
	case "scripts":
		p.SetScripts(arr())
	case "extra":
		p.SetExtra(arr())
	case "archiveExcludes":
		p.SetArchiveExcludes(arr())
	case "requires":
		p.SetRequires(links())
	case "devRequires":
		p.SetDevRequires(links())
	case "provides":
		p.SetProvides(links())
	case "replaces":
		p.SetReplaces(links())
	case "conflicts":
		p.SetConflicts(links())
	case "suggests":
		p.SetSuggests(arr())
	case "support":
		p.SetSupport(arr())
	case "funding":
		p.SetFunding(arr())
	case "transportOptions":
		p.SetTransportOptions(arr())
	case "phpExt":
		p.SetPhpExt(arr())
	default:
		t.Fatalf("no setter for %s", method)
	}
}

func phpValue(v any) any {
	if a, ok := v.(*pkgtest.Arr); ok {
		return a.PHP()
	}

	return v
}

func TestArrayDumper_Keys(t *testing.T) {
	for _, c := range pkgtest.Providers(t)["ArrayDumperTest::provideKeys"] {
		key := c.Args.String(0)
		value := c.Args.At(1)

		method := key
		if c.Args.Len() > 2 && c.Args.At(2) != nil {
			method = c.Args.String(2)
		}

		var expected any
		if c.Args.Len() > 3 && php.ToBool(phpValueOrLinks(c.Args.At(3))) {
			expected = phpValue(c.Args.At(3))
		} else {
			expected = phpValue(value)
		}

		p := pkg.NewRootPackage("__root__", "1.0.0.0", "1.0.0")
		set(t, p, method, value)

		got, _ := dump(t, p).Get(key)
		if !php.StrictEquals(expected, got) {
			t.Errorf("%s: got %v, want %v", key, got, expected)
		}
	}
}

// phpValueOrLinks converts an expected value for the ?: check.
func phpValueOrLinks(v any) any {
	if a, ok := v.(*pkgtest.Arr); ok {
		return a.Len() > 0
	}

	return v
}
