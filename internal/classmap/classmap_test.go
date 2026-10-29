// Ports tests/ClassMapTest.php of composer/class-map-generator 1.8.0.

package classmap

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// classMapOf is ClassMapTest::classMapOf($classes, $ambiguousClasses).
func classMapOf(t *testing.T, classes [][2]string, ambiguousClasses map[string][]string) *ClassMap {
	t.Helper()

	m := &ClassMap{}
	for _, c := range classes {
		m.AddClass(c[0], c[1])
	}
	for class, paths := range ambiguousClasses {
		for _, path := range paths {
			m.AddAmbiguousClass(class, path)
		}
	}

	return m
}

func TestClassMap_GetAmbiguousFolders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		classes [][2]string
		want    [][]string
	}{
		{name: "empty map"},
		{
			name: "no ambiguity",
			classes: [][2]string{
				{`Foo\Bar`, "/proj/src/Foo/Bar.php"},
				{`Foo\Baz`, "/proj/src/Foo/Baz.php"},
			},
		},
		// the case this is all about: a folder renamed to a different
		// casing, which merges into a single folder once checked out on a
		// case insensitive filesystem
		{
			name: "folder differing in casing",
			classes: [][2]string{
				{`foo\Foo`, "/proj/src/foo/Foo.php"},
				{`Foo\Bar`, "/proj/src/Foo/Bar.php"},
			},
			want: [][]string{{"/proj/src/Foo", "/proj/src/foo"}},
		},
		{
			name: "three variants of the same folder",
			classes: [][2]string{
				{`A\A`, "/proj/src/Casefolding/A.php"},
				{`B\B`, "/proj/src/casefolding/B.php"},
				{`C\C`, "/proj/src/CaseFolding/C.php"},
			},
			want: [][]string{{"/proj/src/CaseFolding", "/proj/src/Casefolding", "/proj/src/casefolding"}},
		},
		// every level whose own name differs in casing is reported, as
		// renaming the parent does not make the children match each other
		{
			name: "nested differences are all reported",
			classes: [][2]string{
				{`A\B`, "/proj/src/Foo/Bar/A.php"},
				{`A\C`, "/proj/src/foo/bar/C.php"},
			},
			want: [][]string{
				{"/proj/src/Foo", "/proj/src/foo"},
				{"/proj/src/Foo/Bar", "/proj/src/foo/bar"},
			},
		},
		// a difference inherited from the parent is not repeated for
		// children matching each other
		{
			name: "ancestor difference is not repeated for identical children",
			classes: [][2]string{
				{`A\B`, "/proj/src/Foo/Bar/A.php"},
				{`A\C`, "/proj/src/foo/Bar/B.php"},
			},
			want: [][]string{{"/proj/src/Foo", "/proj/src/foo"}},
		},
		{
			name: "ancestor difference is not repeated for identical files",
			classes: [][2]string{
				{`A\A`, "/proj/src/Foo/A.php"},
				{`B\A`, "/proj/src/foo/A.php"},
			},
			want: [][]string{{"/proj/src/Foo", "/proj/src/foo"}},
		},
		{
			name: "files differing in casing under folders differing in casing",
			classes: [][2]string{
				{`A\A`, "/proj/src/Foo/A.php"},
				{`B\B`, "/proj/src/foo/a.php"},
			},
			want: [][]string{
				{"/proj/src/Foo", "/proj/src/foo"},
				{"/proj/src/Foo/A.php", "/proj/src/foo/a.php"},
			},
		},
		{
			name: "three variants under differently cased parents",
			classes: [][2]string{
				{`A\A`, "/proj/src/Foo/Bar/A.php"},
				{`B\B`, "/proj/src/foo/bar/B.php"},
				{`C\C`, "/proj/src/FOO/BAR/C.php"},
				{`D\D`, "/proj/src/Foo/bar/D.php"},
			},
			want: [][]string{
				{"/proj/src/FOO", "/proj/src/Foo", "/proj/src/foo"},
				{"/proj/src/FOO/BAR", "/proj/src/Foo/Bar", "/proj/src/Foo/bar"},
			},
		},
		// Foo\Bar and Foo\bar are the same class as far as PHP is
		// concerned, and the two files cannot co-exist on a case
		// insensitive filesystem either
		{
			name: "files differing in casing",
			classes: [][2]string{
				{`Foo\Bar`, "/proj/src/Bar.php"},
				{`Foo\bar`, "/proj/src/bar.php"},
			},
			want: [][]string{{"/proj/src/Bar.php", "/proj/src/bar.php"}},
		},
		{
			name: "folder and file with the same name",
			classes: [][2]string{
				{`A\A`, "/proj/src/Foo/A.php"},
				{`B\B`, "/proj/src/foo.php"},
			},
		},
		{
			name: "unrelated folders in separate trees",
			classes: [][2]string{
				{`A\A`, "/proj/src/Foo/A.php"},
				{`B\B`, "/proj/lib/foo/B.php"},
			},
		},
		// a namespace legitimately spread over several folders is not a
		// problem
		{
			name: "namespace spanning several folders",
			classes: [][2]string{
				{`Foo\A`, "/proj/src/A.php"},
				{`Foo\B`, "/proj/lib/B.php"},
			},
		},
		// aws/aws-sdk-php uses the Aws namespace while its aws/aws-crt-php
		// dependency uses AWS, but they live in folders which do not fold
		// into each other
		{
			name: "namespaces differing in casing in separate folders",
			classes: [][2]string{
				{`AWS\CRT\CRT`, "/proj/vendor/aws/aws-crt-php/src/AWS/CRT/CRT.php"},
				{`Aws\Sdk`, "/proj/vendor/aws/aws-sdk-php/src/Sdk.php"},
			},
		},
		{
			name: "windows directory separators",
			classes: [][2]string{
				{`A\A`, `C:\proj\src\Foo\A.php`},
				{`B\B`, `C:\proj\src\foo\B.php`},
			},
			want: [][]string{{"C:/proj/src/Foo", "C:/proj/src/foo"}},
		},
		{
			name: "mixed directory separators",
			classes: [][2]string{
				{`A\A`, `C:\proj\src\Foo\A.php`},
				{`B\B`, "C:/proj/src/foo/B.php"},
			},
			want: [][]string{{"C:/proj/src/Foo", "C:/proj/src/foo"}},
		},
		// drive letters and stream wrapper schemes are never case sensitive
		{
			name: "drive letter casing is ignored",
			classes: [][2]string{
				{`A\A`, `C:\proj\src\A.php`},
				{`B\B`, `c:\proj\src\B.php`},
			},
		},
		{
			name: "drive letter casing is kept in reported paths",
			classes: [][2]string{
				{`A\A`, `C:\proj\src\Foo\A.php`},
				{`B\B`, `c:\proj\src\foo\B.php`},
			},
			want: [][]string{{"C:/proj/src/Foo", "c:/proj/src/foo"}},
		},
		{
			name: "stream wrapper scheme casing is ignored",
			classes: [][2]string{
				{`A\A`, "phar:///proj/lib.phar/src/A.php"},
				{`B\B`, "PHAR:///proj/lib.phar/src/B.php"},
			},
		},
		{
			name: "drive letter under a stream wrapper",
			classes: [][2]string{
				{`A\A`, "phar://C:/proj/lib.phar/src/A.php"},
				{`B\B`, "phar://c:/proj/lib.phar/src/B.php"},
			},
		},
		{
			name: "relative paths",
			classes: [][2]string{
				{`A\A`, "src/Foo/A.php"},
				{`B\B`, "src/foo/B.php"},
			},
			want: [][]string{{"src/Foo", "src/foo"}},
		},
		{
			name: "several ambiguous folders",
			classes: [][2]string{
				{`A\A`, "/proj/src/Zed/A.php"},
				{`B\B`, "/proj/src/zed/B.php"},
				{`C\C`, "/proj/src/Abc/C.php"},
				{`D\D`, "/proj/src/abc/D.php"},
			},
			want: [][]string{
				{"/proj/src/Abc", "/proj/src/abc"},
				{"/proj/src/Zed", "/proj/src/zed"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := classMapOf(t, tc.classes, nil).AmbiguousFolders(nil)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.EqualFunc(got, tc.want, slices.Equal) {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassMap_GetAmbiguousFoldersIsNotAffectedByInsertionOrder(t *testing.T) {
	classes := [][2]string{
		{`A\A`, "/proj/src/Foo/Bar/A.php"},
		{`B\B`, "/proj/src/foo/B.php"},
		{`C\C`, "/proj/src/Foo/bar/C.php"},
		{`D\D`, "/proj/src/foo/Bar/D.php"},
	}
	// src/foo/Bar only differs from src/Foo/Bar by its parent so it is not
	// listed
	want := [][]string{
		{"/proj/src/Foo", "/proj/src/foo"},
		{"/proj/src/Foo/Bar", "/proj/src/Foo/bar"},
	}

	for _, order := range permutations([]int{0, 1, 2, 3}) {
		permutation := make([][2]string, len(classes))
		for i, o := range order {
			permutation[i] = classes[o]
		}

		got, err := classMapOf(t, permutation, nil).AmbiguousFolders(nil)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.EqualFunc(got, want, slices.Equal) {
			t.Fatalf("insertion order %v: got %q, want %q", order, got, want)
		}
	}
}

func permutations(items []int) [][]int {
	if len(items) <= 1 {
		return [][]int{slices.Clone(items)}
	}

	var result [][]int
	for i := range items {
		rest := slices.Concat(items[:i], items[i+1:])
		for _, p := range permutations(rest) {
			result = append(result, append([]int{items[i]}, p...))
		}
	}

	return result
}

func TestClassMap_GetAmbiguousFoldersIncludesPathsOfAmbiguousClasses(t *testing.T) {
	// a folder renamed to a different casing with stale copies left behind:
	// the same classes are found in both, so the second copy only ends up in
	// the ambiguous classes
	m := classMapOf(t, [][2]string{
		{`Foo\A`, "/proj/src/Foo/A.php"},
		{`Foo\B`, "/proj/src/Foo/B.php"},
	}, map[string][]string{
		`Foo\A`: {"/proj/src/foo/A.php"},
		`Foo\B`: {"/proj/src/foo/B.php"},
	})

	want := [][]string{{"/proj/src/Foo", "/proj/src/foo"}}

	for _, filter := range []Matcher{DefaultDuplicatesFilter, nil} {
		got, err := m.AmbiguousFolders(filter)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.EqualFunc(got, want, slices.Equal) {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func TestClassMap_GetAmbiguousFoldersFiltersTestPathsByDefault(t *testing.T) {
	m := classMapOf(t, [][2]string{
		{`A\A`, "/proj/tests/Foo/A.php"},
		{`B\B`, "/proj/tests/foo/B.php"},
	}, nil)

	got, err := m.AmbiguousFolders(DefaultDuplicatesFilter)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %q, want none", got)
	}

	got, err = m.AmbiguousFolders(nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"/proj/tests/Foo", "/proj/tests/foo"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestClassMap_GetAmbiguousFoldersOnlyFiltersFoldersWithoutUnfilteredPaths(t *testing.T) {
	// a test folder which merges with a production folder is still a problem
	// for the latter
	m := classMapOf(t, [][2]string{
		{`A\A`, "/proj/src/Foo/A.php"},
		{`B\B`, "/proj/src/foo/Tests/B.php"},
		{`C\C`, "/proj/src/foo/tests/C.php"},
	}, nil)

	got, err := m.AmbiguousFolders(DefaultDuplicatesFilter)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"/proj/src/Foo", "/proj/src/foo"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got %q, want %q", got, want)
	}

	got, err = m.AmbiguousFolders(nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{
		{"/proj/src/Foo", "/proj/src/foo"},
		{"/proj/src/foo/Tests", "/proj/src/foo/tests"},
	}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got %q, want %q", got, want)
	}

	// while folders only containing filtered paths are ignored altogether
	m = classMapOf(t, [][2]string{
		{`A\A`, "/proj/tests/Foo/A.php"},
		{`B\B`, "/proj/Tests/foo/B.php"},
	}, nil)

	got, err = m.AmbiguousFolders(DefaultDuplicatesFilter)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %q, want none", got)
	}
}

func TestClassMap_DefaultDuplicatesFilterAppliesToRelativePaths(t *testing.T) {
	m := classMapOf(t, [][2]string{
		{`A\A`, "tests/Foo/A.php"},
		{`B\B`, "tests/foo/B.php"},
	}, nil)

	got, err := m.AmbiguousFolders(DefaultDuplicatesFilter)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %q, want none", got)
	}

	got, err = m.AmbiguousFolders(nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"tests/Foo", "tests/foo"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got %q, want %q", got, want)
	}

	m = classMapOf(t, [][2]string{{"A", "src/A.php"}}, map[string][]string{"A": {"tests/A.php"}})

	classes, err := m.AmbiguousClasses(DefaultDuplicatesFilter)
	if err != nil {
		t.Fatal(err)
	}
	if len(classes) != 0 {
		t.Fatalf("got %q, want none", classes)
	}

	classes, err = m.AmbiguousClasses(nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []AmbiguousClass{{Class: "A", Paths: []string{"tests/A.php"}}}; len(classes) != len(want) || classes[0].Class != want[0].Class || !slices.Equal(classes[0].Paths, want[0].Paths) {
		t.Fatalf("got %q, want %q", classes, want)
	}
}

func TestClassMap_GetAmbiguousFoldersAcceptsACustomFilter(t *testing.T) {
	m := classMapOf(t, [][2]string{
		{`A\A`, "/proj/generated/Foo/A.php"},
		{`B\B`, "/proj/generated/foo/B.php"},
	}, nil)

	filter := php.MustCompile(`{/generated/}`)

	got, err := m.AmbiguousFolders(filter)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %q, want none", got)
	}

	got, err = m.AmbiguousFolders(DefaultDuplicatesFilter)
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"/proj/generated/Foo", "/proj/generated/foo"}}; !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestClassMap_GetAmbiguousFoldersIsRecomputedWhenClassesAreAdded(t *testing.T) {
	m := classMapOf(t, [][2]string{{`A\A`, "/proj/src/Foo/A.php"}}, nil)

	if got, err := m.AmbiguousFolders(DefaultDuplicatesFilter); err != nil || len(got) != 0 {
		t.Fatalf("got %q, %v, want none", got, err)
	}

	m.AddClass(`B\B`, "/proj/src/foo/B.php")

	if got, err := m.AmbiguousFolders(DefaultDuplicatesFilter); err != nil || !slices.EqualFunc(got, [][]string{{"/proj/src/Foo", "/proj/src/foo"}}, slices.Equal) {
		t.Fatalf("got %q, %v", got, err)
	}

	m.AddAmbiguousClass(`B\B`, "/proj/src/FOO/B.php")

	want := [][]string{{"/proj/src/FOO", "/proj/src/Foo", "/proj/src/foo"}}
	if got, err := m.AmbiguousFolders(DefaultDuplicatesFilter); err != nil || !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got %q, %v, want %q", got, err, want)
	}
	if got, err := m.AmbiguousFolders(nil); err != nil || !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("got %q, %v, want %q", got, err, want)
	}
}
