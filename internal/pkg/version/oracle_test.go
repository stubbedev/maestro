// Differential tests against the goldens tools/oracle/pkg/version.php
// records by running the real Composer 2.10.3.

package version_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
)

func readVersionGolden(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	data, err := os.ReadFile("testdata/oracle/version.json")
	if err != nil {
		t.Fatal(err)
	}

	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}

	return out
}

// result decodes a string result or an {"e": [class, message]} exception.
func result(raw json.RawMessage) (value string, exception []string) {
	var e struct {
		E []string `json:"e"`
	}

	if json.Unmarshal(raw, &value) == nil {
		return value, nil
	}

	_ = json.Unmarshal(raw, &e)

	return "", e.E
}

func TestOracle_BumpRequirement(t *testing.T) {
	var cases [][4]json.RawMessage
	if err := json.Unmarshal(readVersionGolden(t)["bump"], &cases); err != nil {
		t.Fatal(err)
	}

	parser := pkg.NewVersionParser()

	for _, c := range cases {
		var (
			constraint, prettyVersion string
			alias                     *string
		)

		_ = json.Unmarshal(c[0], &constraint)
		_ = json.Unmarshal(c[1], &prettyVersion)
		_ = json.Unmarshal(c[2], &alias)
		want, exception := result(c[3])

		normalized, err := parser.Normalize(prettyVersion)
		if err != nil {
			t.Fatal(err)
		}

		p := pkg.NewPackage("foo/bar", normalized, prettyVersion)
		if alias != nil {
			p.SetExtra(php.ArrayOf("branch-alias", php.ArrayOf(prettyVersion, *alias)))
		}

		got, err := func() (string, error) {
			parsed, err := parser.ParseConstraints(constraint)
			if err != nil {
				return "", err
			}

			return version.VersionBumper{}.BumpRequirement(parsed, p)
		}()

		switch {
		case exception != nil:
			if err == nil || err.Error() != exception[1] {
				t.Errorf("%s %s: got %q %v, want %v", constraint, prettyVersion, got, err, exception)
			}
		case err != nil || got != want:
			t.Errorf("%s %s (alias %v): got %q %v, want %q", constraint, prettyVersion, alias, got, err, want)
		}
	}
}

func TestOracle_FindRecommendedRequireVersion(t *testing.T) {
	golden := readVersionGolden(t)

	var phpVersion string
	_ = json.Unmarshal(golden["php"], &phpVersion)

	var cases [][4]json.RawMessage
	if err := json.Unmarshal(golden["recommended"], &cases); err != nil {
		t.Fatal(err)
	}

	parser := pkg.NewVersionParser()
	selector := version.NewVersionSelector(&repositorySetMock{t: t}, nil)
	selector.PHPVersion = phpVersion

	for _, c := range cases {
		var (
			name, prettyVersion string
			alias               *string
		)

		_ = json.Unmarshal(c[0], &name)
		_ = json.Unmarshal(c[1], &prettyVersion)
		_ = json.Unmarshal(c[2], &alias)
		want, exception := result(c[3])

		normalized, err := parser.Normalize(prettyVersion)
		if err != nil {
			t.Fatal(err)
		}

		p := pkg.NewPackage(name, normalized, prettyVersion)
		if alias != nil {
			p.SetExtra(php.ArrayOf("branch-alias", php.ArrayOf(prettyVersion, *alias)))
		}

		got, err := selector.FindRecommendedRequireVersion(p)
		if exception != nil {
			if err == nil || err.Error() != exception[1] {
				t.Errorf("%s %s: got %q %v, want %v", name, prettyVersion, got, err, exception)
			}

			continue
		}

		if err != nil || got != want {
			t.Errorf("%s %s (alias %v): got %q %v, want %q", name, prettyVersion, alias, got, err, want)
		}
	}
}

func TestOracle_FindBestCandidate(t *testing.T) {
	var cases [][3]json.RawMessage
	if err := json.Unmarshal(readVersionGolden(t)["best"], &cases); err != nil {
		t.Fatal(err)
	}

	parser := pkg.NewVersionParser()

	for _, c := range cases {
		var (
			list      [][2]json.RawMessage
			stability string
			want      *[2]json.RawMessage
		)

		_ = json.Unmarshal(c[0], &list)
		_ = json.Unmarshal(c[1], &stability)
		_ = json.Unmarshal(c[2], &want)

		var packages []pkg.PackageInterface

		for _, entry := range list {
			var (
				v            string
				defaultAlias bool
			)

			_ = json.Unmarshal(entry[0], &v)
			_ = json.Unmarshal(entry[1], &defaultAlias)

			normalized, err := parser.Normalize(v)
			if err != nil {
				t.Fatal(err)
			}

			var p pkg.PackageInterface = pkg.NewCompletePackage("foo/bar", normalized, v)
			if defaultAlias {
				p = pkg.NewAliasPackage(p, pkg.DefaultBranchAlias, pkg.DefaultBranchAlias)
			}

			packages = append(packages, p)
		}

		repo := &repositorySetMock{t: t, results: [][]pkg.PackageInterface{packages}}
		got := best(t, version.NewVersionSelector(repo, nil), "foo/bar", version.FindBestCandidateOptions{PreferredStability: stability})

		if want == nil {
			if got != nil {
				t.Errorf("%s %s: got %v, want none", c[0], stability, got)
			}

			continue
		}

		var (
			index int
			same  bool
		)

		_ = json.Unmarshal(want[0], &index)
		_ = json.Unmarshal(want[1], &same)

		expected := packages[index]
		if !same {
			expected = expected.(pkg.Alias).AliasOf()
		}

		if got != expected {
			t.Errorf("%s %s: got %v, want %v", c[0], stability, got, expected)
		}
	}
}
