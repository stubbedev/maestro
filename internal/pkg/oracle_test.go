// Differential tests against the goldens tools/oracle/pkg/pkg.php records
// by running the real Composer 2.10.3.

package pkg_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

func readPkgGolden(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	data, err := os.ReadFile("testdata/oracle/pkg.json")
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

func TestOracle_ParseNameVersionPairs(t *testing.T) {
	var cases []struct {
		Pairs  []string
		Result []map[string]string
	}

	var raw [][2]json.RawMessage
	if err := json.Unmarshal(readPkgGolden(t)["parseNameVersionPairs"], &raw); err != nil {
		t.Fatal(err)
	}

	for _, r := range raw {
		var c struct {
			Pairs  []string
			Result []map[string]string
		}

		_ = json.Unmarshal(r[0], &c.Pairs)
		_ = json.Unmarshal(r[1], &c.Result)
		cases = append(cases, c)
	}

	parser := pkg.NewVersionParser()

	for _, c := range cases {
		var want []pkg.NameVersionPair

		for _, m := range c.Result {
			pair := pkg.NameVersionPair{Name: m["name"]}
			if v, ok := m["version"]; ok {
				pair.Version = pkg.Str(v)
			}

			want = append(want, pair)
		}

		if got := parser.ParseNameVersionPairs(c.Pairs); !slices.Equal(got, want) {
			t.Errorf("%q: got %v, want %v", c.Pairs, got, want)
		}
	}
}

func TestOracle_IsUpgrade(t *testing.T) {
	var raw [][3]json.RawMessage
	if err := json.Unmarshal(readPkgGolden(t)["isUpgrade"], &raw); err != nil {
		t.Fatal(err)
	}

	for _, r := range raw {
		var (
			from, to string
			want     bool
		)

		_ = json.Unmarshal(r[0], &from)
		_ = json.Unmarshal(r[1], &to)

		got, err := pkg.IsUpgrade(from, to)
		if json.Unmarshal(r[2], &want) != nil {
			if err == nil {
				t.Errorf("%s -> %s: got %v, want %s", from, to, got, r[2])
			}

			continue
		}

		if err != nil || got != want {
			t.Errorf("%s -> %s: got %v %v, want %v", from, to, got, err, want)
		}
	}
}

func TestOracle_PackageNamesToRegexp(t *testing.T) {
	var raw [][4]json.RawMessage
	if err := json.Unmarshal(readPkgGolden(t)["regexps"], &raw); err != nil {
		t.Fatal(err)
	}

	for _, r := range raw {
		var (
			names          []string
			wrap, all, one string
		)

		_ = json.Unmarshal(r[0], &names)
		_ = json.Unmarshal(r[1], &wrap)
		_ = json.Unmarshal(r[2], &all)

		if got := pkg.PackageNamesToRegexp(names, wrap); got != all {
			t.Errorf("%q %q: got %q, want %q", names, wrap, got, all)
		}

		if json.Unmarshal(r[3], &one) == nil && len(names) > 0 {
			if got := pkg.PackageNameToRegexp(names[0], wrap); got != one {
				t.Errorf("%q %q: got %q, want %q", names[0], wrap, got, one)
			}
		}
	}
}

func TestOracle_SortPackages(t *testing.T) {
	var raw [][4]json.RawMessage
	if err := json.Unmarshal(readPkgGolden(t)["sortPackages"], &raw); err != nil {
		t.Fatal(err)
	}

	for _, r := range raw {
		var (
			graph         [][2]json.RawMessage
			weights       map[string]int
			sorted, alpha []string
		)

		_ = json.Unmarshal(r[0], &graph)
		_ = json.Unmarshal(r[1], &weights)
		_ = json.Unmarshal(r[2], &sorted)
		_ = json.Unmarshal(r[3], &alpha)

		var packages []pkg.PackageInterface

		for _, node := range graph {
			var (
				name     string
				requires []string
			)

			_ = json.Unmarshal(node[0], &name)
			_ = json.Unmarshal(node[1], &requires)

			p := pkg.NewPackage(name, "1.0.0.0", "1.0.0")

			var b pkg.LinksBuilder
			for _, req := range requires {
				b.Set(php.Strtolower(req), pkg.NewLink(p.Name(), req, semver.NewMatchAllConstraint(), pkg.TypeUnknown, pkg.NullString{}))
			}

			p.SetRequires(b.Build())
			packages = append(packages, p)
		}

		names := func(list []pkg.PackageInterface) []string {
			var out []string
			for _, p := range list {
				out = append(out, p.Name())
			}

			return out
		}

		if got := names(pkg.SortPackages(packages, weights)); !slices.Equal(got, sorted) {
			t.Errorf("sortPackages %s %v: got %q, want %q", r[0], weights, got, sorted)
		}

		if got := names(pkg.SortPackagesAlphabetically(packages)); !slices.Equal(got, alpha) {
			t.Errorf("sortPackagesAlphabetically %s: got %q, want %q", r[0], got, alpha)
		}
	}
}

func TestOracle_GetMostCurrentVersion(t *testing.T) {
	var raw [][2]json.RawMessage
	if err := json.Unmarshal(readPkgGolden(t)["getMostCurrentVersion"], &raw); err != nil {
		t.Fatal(err)
	}

	parser := pkg.NewVersionParser()

	for _, r := range raw {
		var list [][2]json.RawMessage
		_ = json.Unmarshal(r[0], &list)

		var packages []pkg.PackageInterface

		for _, entry := range list {
			var (
				version string
				def     bool
			)

			_ = json.Unmarshal(entry[0], &version)
			_ = json.Unmarshal(entry[1], &def)

			normalized, err := parser.Normalize(version)
			if err != nil {
				t.Fatal(err)
			}

			p := pkg.NewPackage("a/b", normalized, version)
			p.SetIsDefaultBranch(def)
			packages = append(packages, p)
		}

		best := pkg.GetMostCurrentVersion(packages)

		var want *int
		_ = json.Unmarshal(r[1], &want)

		switch {
		case want == nil && best != nil:
			t.Errorf("%s: got %v, want null", r[0], best)
		case want != nil && (best == nil || best != packages[*want]):
			t.Errorf("%s: got %v, want #%d", r[0], best, *want)
		}
	}
}
