// Ports tests/Composer/Test/Package/Version/VersionParserTest.php.

package pkg_test

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/internal/pkgtest"
)

func TestVersionParser_ParseNameVersionPairs(t *testing.T) {
	for _, c := range pkgtest.Providers(t)["VersionParserTest::provideParseNameVersionPairsData"] {
		pairs := c.Args.At(0).(*pkgtest.Arr).Strings()

		var want []pkg.NameVersionPair

		for _, v := range c.Args.At(1).(*pkgtest.Arr).Vals {
			entry := v.(*pkgtest.Arr)
			name, _ := entry.Get("name")
			pair := pkg.NameVersionPair{Name: name.(string)}

			if version, ok := entry.Get("version"); ok {
				pair.Version = pkg.Str(version.(string))
			}

			want = append(want, pair)
		}

		if got := pkg.NewVersionParser().ParseNameVersionPairs(pairs); !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", pairs, got, want)
		}
	}
}

func TestVersionParser_IsUpgrade(t *testing.T) {
	for _, c := range pkgtest.Providers(t)["VersionParserTest::provideIsUpgradeTests"] {
		from, to, want := c.Args.String(0), c.Args.String(1), c.Args.At(2).(bool)

		got, err := pkg.IsUpgrade(from, to)
		if err != nil || got != want {
			t.Errorf("%s -> %s: got %v %v, want %v", from, to, got, err, want)
		}
	}
}

func TestVersionParser_ParseConstraintsIsCached(t *testing.T) {
	p := pkg.NewVersionParser()

	a, err := p.ParseConstraints("^1.0 || ^2.0")
	if err != nil {
		t.Fatal(err)
	}

	b, _ := p.ParseConstraints("^1.0 || ^2.0")
	if a != b {
		t.Error("the same constraint string gave different objects")
	}

	if _, err := p.ParseConstraints("^^^"); err == nil {
		t.Error("no error for an invalid constraint")
	}
}
