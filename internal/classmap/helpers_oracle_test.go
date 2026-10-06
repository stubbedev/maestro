package classmap

import (
	"encoding/json"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type helpersOracle struct {
	NormalizePath      [][2]string               `json:"normalizePath"`
	IsAbsolutePath     [][2]json.RawMessage      `json:"isAbsolutePath"`
	CollapseSeparators [][2]string               `json:"collapseSeparators"`
	Extension          [][2]string               `json:"extension"`
	DuplicatesFilter   [][2]json.RawMessage      `json:"duplicatesFilter"`
	DotPath            [][2]json.RawMessage      `json:"dotPath"`
	ExcludePattern     [][3]json.RawMessage      `json:"excludePattern"`
	Fnmatch            [][3]json.RawMessage      `json:"fnmatch"`
	ClassMap           map[string]classMapOracle `json:"classMap"`
}

type classMapOracle struct {
	Map        [][2]string `json:"map"`
	Raw        []rawOracle `json:"raw"`
	Violations []string    `json:"violations"`
	Ambiguous  []classList `json:"ambiguous"`
	Filtered   []classList `json:"ambiguousFiltered"`
	Count      int         `json:"count"`
}

type rawOracle struct {
	Path       string
	Violations []PsrViolation
}

func (r *rawOracle) UnmarshalJSON(b []byte) error {
	var v []struct {
		Warning   string `json:"warning"`
		ClassName string `json:"className"`
	}
	if err := json.Unmarshal(b, &[]any{&r.Path, &v}); err != nil {
		return err
	}
	for _, x := range v {
		r.Violations = append(r.Violations, PsrViolation(x))
	}

	return nil
}

func loadHelpers(t *testing.T) helpersOracle {
	t.Helper()
	data, err := os.ReadFile("testdata/oracle/helpers.json")
	if err != nil {
		t.Fatal(err)
	}
	var h helpersOracle
	if err := json.Unmarshal(data, &h); err != nil {
		t.Fatal(err)
	}

	return h
}

func unmarshal[T any](t *testing.T, raw json.RawMessage) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}

	return v
}

func TestOracleHelpers(t *testing.T) {
	h := loadHelpers(t)
	for _, c := range h.NormalizePath {
		if got := normalizePath(c[0]); got != c[1] {
			t.Errorf("normalizePath(%q) = %q, want %q", c[0], got, c[1])
		}
	}
	for _, c := range h.IsAbsolutePath {
		in, want := unmarshal[string](t, c[0]), unmarshal[bool](t, c[1])
		if got := isAbsolutePath(in); got != want {
			t.Errorf("isAbsolutePath(%q) = %v", in, got)
		}
	}
	for _, c := range h.CollapseSeparators {
		if got := collapseSeparators(c[0]); got != c[1] {
			t.Errorf("collapseSeparators(%q) = %q, want %q", c[0], got, c[1])
		}
	}
	g := NewGenerator(nil)
	for _, c := range h.Extension {
		// The golden ran on Linux; pathinfo() splits at backslashes too
		// on Windows.
		if runtime.GOOS == "windows" && strings.Contains(c[0], `\`) {
			continue
		}
		g.extensions = []string{c[1]}
		if !g.hasExtension(c[0]) {
			t.Errorf("extension of %q is not %q", c[0], c[1])
		}
	}
	for _, c := range h.DuplicatesFilter {
		in, want := unmarshal[string](t, c[0]), unmarshal[bool](t, c[1])
		if got := matchesDuplicates(in); got != want {
			t.Errorf("duplicates filter on %q = %v", in, got)
		}
	}
	for _, c := range h.DotPath {
		in, want := unmarshal[string](t, c[0]), unmarshal[bool](t, c[1])
		if got := isDotPath(in); got != want {
			t.Errorf("isDotPath(%q) = %v", in, got)
		}
	}
	for _, c := range h.ExcludePattern {
		dirs, in, want := unmarshal[[]string](t, c[0]), unmarshal[string](t, c[1]), unmarshal[bool](t, c[2])
		e := newFinderExclusions(dirs)
		if got := e.matchesPattern(in); got != want {
			t.Errorf("exclude %v on %q = %v", dirs, in, got)
		}
	}
	if len(h.Fnmatch) < 500 {
		t.Fatalf("only %d fnmatch cases", len(h.Fnmatch))
	}
	for _, c := range h.Fnmatch {
		p, n, want := unmarshal[string](t, c[0]), unmarshal[string](t, c[1]), unmarshal[bool](t, c[2])
		if got := fnmatch(p, n); got != want {
			t.Errorf("fnmatch(%q, %q) = %v", p, n, got)
		}
	}
}

func TestOracleClassMap(t *testing.T) {
	h := loadHelpers(t)
	var cm ClassMap
	cm.AddPsrViolation("w1", "C1", "/p/a.php")
	cm.AddPsrViolation("w2", "C2", `\p\b.php/`)
	cm.AddPsrViolation("w3", "C3", "/p/a.php")
	cm.AddPsrViolation("w4", "C4", "/q/c.php")
	cm.AddClass("Zed", "/p/b.php")
	cm.AddPsrViolation("w5", "C5", "/p/b.php")
	cm.AddClass("Alpha", "/x/alpha.php")
	cm.AddClass("Zed", "/x/zed.php")
	cm.AddClass(`Beta\X`, "/x/b.php")
	cm.AddClass("beta", "/x/lower.php")
	cm.AddClass("Beta", "/x/upper.php")
	cm.AddClass("_u", "/x/u.php")
	cm.AddAmbiguousClass("Alpha", "/y/alpha.php")
	cm.AddAmbiguousClass("Zed", "/tests/zed.php")
	cm.AddAmbiguousClass("Alpha", "/y/Fixtures/alpha.php")
	check := func(state string, full bool) {
		want := h.ClassMap[state]
		got := classMapOracle{Violations: cm.PsrViolations(), Count: cm.Count()}
		for class, path := range cm.Map() {
			got.Map = append(got.Map, [2]string{class, path})
		}
		for path, v := range cm.RawPsrViolations() {
			got.Raw = append(got.Raw, rawOracle{path, v})
		}
		if full {
			for _, a := range ambiguousOf(t, &cm, nil) {
				got.Ambiguous = append(got.Ambiguous, classList(a))
			}
			for _, a := range ambiguousOf(t, &cm, DefaultDuplicatesFilter) {
				got.Filtered = append(got.Filtered, classList(a))
			}
		} else {
			got.Count = want.Count
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s:\n got %+v\nwant %+v", state, got, want)
		}
	}
	check("before", false)
	cm.ClearPsrViolationsByPath("/p/")
	cm.AddPsrViolation("w6", "C6", "/p/a.php")
	cm.Sort()
	check("after", true)

	if _, err := cm.ClassPath("Nope"); err == nil || err.Error() != "Class Nope is not present in the map" {
		t.Errorf("ClassPath of a missing class: %v", err)
	}
	if p, err := cm.ClassPath("Beta"); err != nil || p != "/x/upper.php" {
		t.Errorf("ClassPath(Beta) = %q, %v", p, err)
	}
}
