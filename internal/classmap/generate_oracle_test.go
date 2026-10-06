package classmap

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

type generateScenario struct {
	Name   string               `json:"name"`
	Cwd    string               `json:"cwd"`
	Dedupe bool                 `json:"dedupe"`
	Tree   [][3]json.RawMessage `json:"tree"`
	Steps  []struct {
		Path         string   `json:"path"`
		Type         string   `json:"type"`
		Namespace    *string  `json:"namespace"`
		Excluded     *string  `json:"excluded"`
		ExcludedDirs []string `json:"excludedDirs"`
	} `json:"steps"`
	Result generateResult `json:"result"`
}

type generateResult struct {
	Error *struct {
		Class   string `json:"class"`
		Message string `json:"message"`
	} `json:"error"`
	Map               [][2]string `json:"map"`
	Ambiguous         []classList `json:"ambiguous"`
	AmbiguousFiltered []classList `json:"ambiguousFiltered"`
	Violations        []string    `json:"violations"`
	RawViolations     []rawList   `json:"rawViolations"`
	Digest            string      `json:"digest"`
}

// classList is a [class, [paths]] pair.
type classList struct {
	Class string
	Paths []string
}

func (c *classList) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &[]any{&c.Class, &c.Paths})
}

func (c classList) MarshalJSON() ([]byte, error) { return json.Marshal([]any{c.Class, c.Paths}) }

// rawList is a [path, [[warning, className]]] pair.
type rawList struct {
	Path       string
	Violations [][2]string
}

func (r *rawList) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &[]any{&r.Path, &r.Violations})
}

func (r rawList) MarshalJSON() ([]byte, error) { return json.Marshal([]any{r.Path, r.Violations}) }

// pcreMatcher runs a '{...}' PHP regex that Go's regexp also understands.
type pcreMatcher struct{ re *regexp.Regexp }

func (m pcreMatcher) IsMatch(s string) (bool, error) { return m.re.MatchString(s), nil }

func mustPCRE(t *testing.T, pattern string) Matcher {
	t.Helper()
	if !strings.HasPrefix(pattern, "{") || !strings.HasSuffix(pattern, "}") {
		t.Fatalf("unsupported pattern %s", pattern)
	}

	return pcreMatcher{regexp.MustCompile(pattern[1 : len(pattern)-1])}
}

func TestOracleGenerate(t *testing.T) {
	checkGenerate(t, "testdata/oracle/generate.json", false)
}

// TestOracleGenerateLive reruns the scenarios with PHP on this machine
// (MAESTRO_ORACLE_LIVE=1) and compares the results exactly, in order: the
// directory order the Finder sees decides which file wins for a duplicate
// class, and it is only stable on the same file system.
func TestOracleGenerateLive(t *testing.T) {
	if os.Getenv("MAESTRO_ORACLE_LIVE") == "" {
		t.Skip("set MAESTRO_ORACLE_LIVE=1 to run against php")
	}
	out := filepath.Join(t.TempDir(), "generate.json")
	if b, err := exec.Command("php", "../../tools/oracle/classmap/generate.php", out, "--full").CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	checkGenerate(t, out, true)
}

func checkGenerate(t *testing.T, golden string, exact bool) {
	data, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	var scenarios []generateScenario
	if err := json.Unmarshal(data, &scenarios); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dirs := map[string]string{"testdata": filepath.Join(wd, "testdata"), "ref": filepath.Join(wd, "../../.ref/composer")}
	for _, s := range scenarios {
		t.Run(s.Name, func(t *testing.T) {
			dir := dirs[s.Cwd]
			if s.Cwd == "tree" {
				if os.Getuid() == 0 {
					t.Skip("permissions do not apply to root")
				}
				if runtime.GOOS == "windows" && treeHasModes(s.Tree) {
					t.Skip("file modes do not take read access away on Windows")
				}
				dir = buildTree(t, s.Tree)
			}
			cwd, err := filepath.EvalSymlinks(dir)
			if err != nil {
				t.Skip(err)
			}
			t.Chdir(cwd)
			got := runScenario(t, s, cwd)
			want := s.Result
			if want.Digest != "" {
				if d := digest(got); d != want.Digest {
					t.Errorf("digest %s, want %s", d, want.Digest)
				}

				return
			}
			if !exact {
				got, want = normalizeResult(got), normalizeResult(want)
				if s.Cwd == "tree" && s.Dedupe {
					got, want = resolveAliases(t, got, cwd), resolveAliases(t, want, cwd)
				}
				if s.Cwd == "tree" && want.Error != nil {
					got, want = errorOnly(got), errorOnly(want)
				}
			}
			// Round trip through JSON, as the golden went through it.
			gotJSON, _ := json.Marshal(got)
			got = generateResult{}
			if err := json.Unmarshal(gotJSON, &got); err != nil {
				t.Fatal(err)
			}
			compareResults(t, canonical(got), canonical(want))
		})
	}
}

func runScenario(t *testing.T, s generateScenario, cwd string) generateResult {
	t.Helper()
	// The golden ran on Linux. On Windows the working directory shows up
	// with backslashes and, normalized, with slashes, and the Finder joins
	// the paths it finds with backslashes, which pathOf turns into slashes.
	strip := func(p string) string {
		return strings.ReplaceAll(strings.ReplaceAll(p, cwd, "<cwd>"), filepath.ToSlash(cwd), "<cwd>")
	}
	pathOf := func(p string) string { return strip(filepath.ToSlash(p)) }
	g := NewGenerator([]string{"php", "inc", "hh"})
	if s.Dedupe {
		g.AvoidDuplicateScans(nil)
	}
	var res generateResult
	for _, step := range s.Steps {
		var excluded Matcher
		if step.Excluded != nil {
			excluded = mustPCRE(t, *step.Excluded)
		}
		typ := map[string]AutoloadType{"classmap": Classmap, "psr-0": PSR0, "psr-4": PSR4}[step.Type]
		namespace := ""
		if step.Namespace != nil {
			namespace = *step.Namespace
		}
		err := g.ScanPaths(strings.ReplaceAll(step.Path, "<cwd>", cwd), excluded, typ, namespace, step.ExcludedDirs)
		if err != nil {
			var e *Exception
			if !errors.As(err, &e) {
				t.Fatal(err)
			}
			res.Error = &struct {
				Class   string `json:"class"`
				Message string `json:"message"`
			}{e.Class, strip(e.Message)}

			break
		}
	}
	cm := g.ClassMap()
	for class, path := range cm.Map() {
		res.Map = append(res.Map, [2]string{class, pathOf(path)})
	}
	stripAll := func(list []AmbiguousClass) []classList {
		var out []classList
		for _, a := range list {
			paths := make([]string, len(a.Paths))
			for i, p := range a.Paths {
				paths[i] = pathOf(p)
			}
			out = append(out, classList{a.Class, paths})
		}

		return out
	}
	res.Ambiguous = stripAll(ambiguousOf(t, cm, nil))
	res.AmbiguousFiltered = stripAll(ambiguousOf(t, cm, DefaultDuplicatesFilter))
	for _, v := range cm.PsrViolations() {
		res.Violations = append(res.Violations, strip(v))
	}
	for path, violations := range cm.RawPsrViolations() {
		r := rawList{Path: strip(path)}
		for _, v := range violations {
			r.Violations = append(r.Violations, [2]string{strip(v.Warning), v.ClassName})
		}
		res.RawViolations = append(res.RawViolations, r)
	}

	return res
}

// normalizeResult removes what depends on the directory order: the order of
// everything, and which of the files declaring an ambiguous class wins.
func normalizeResult(r generateResult) generateResult {
	ambiguous := map[string][]string{}
	for _, a := range r.Ambiguous {
		ambiguous[a.Class] = a.Paths
	}
	out := generateResult{Error: r.Error, Violations: slices.Clone(r.Violations)}
	for _, e := range r.Map {
		if paths, ok := ambiguous[e[0]]; ok {
			all := append([]string{e[1]}, paths...)
			slices.Sort(all)
			out.Ambiguous = append(out.Ambiguous, classList{e[0], all})
			e[1] = "*"
		}
		out.Map = append(out.Map, e)
	}
	slices.SortFunc(out.Map, func(a, b [2]string) int { return strings.Compare(a[0], b[0]) })
	slices.SortFunc(out.Ambiguous, func(a, b classList) int { return strings.Compare(a.Class, b.Class) })
	slices.Sort(out.Violations)
	out.RawViolations = slices.Clone(r.RawViolations)
	slices.SortFunc(out.RawViolations, func(a, b rawList) int { return strings.Compare(a.Path, b.Path) })

	return out
}

// resolveAliases replaces each path in the map by the file it resolves to.
// The tree reaches some files through several paths (a symlinked directory,
// a symlinked file); with avoidDuplicateScans the generator keeps whichever
// path the Finder visits first and skips the others by realpath, so the
// path recorded depends on the readdir order of the file system the tree
// was built on (hash order on ext4, name order on APFS, creation order on
// tmpfs). maestro reads directories in readdir order as Composer does;
// TestOracleGenerateLive checks the exact paths against php on one file
// system. Here only the file each class came from is compared.
func resolveAliases(t *testing.T, r generateResult, cwd string) generateResult {
	t.Helper()
	r.Map = slices.Clone(r.Map)
	for i, e := range r.Map {
		if e[1] == "*" || !strings.HasPrefix(e[1], "<cwd>/") {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(cwd, strings.TrimPrefix(e[1], "<cwd>/")))
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(cwd, real)
		if err != nil {
			t.Fatal(err)
		}
		r.Map[i][1] = "<cwd>/" + filepath.ToSlash(rel)
	}

	return r
}

// errorOnly keeps only the error of a result. When a scan of the tree fails
// (an unreadable directory or file, a broken symlink), what was collected
// before the failure is whatever the Finder visited before the bad entry,
// which depends on the readdir order of the file system; Composer never
// shows that partial map, as the exception ends the command.
func errorOnly(r generateResult) generateResult {
	return generateResult{Error: r.Error}
}

// canonical turns empty lists into nil ones, as PHP's [] and Go's nil both
// mean none.
func canonical(r generateResult) generateResult {
	if len(r.Map) == 0 {
		r.Map = nil
	}
	if len(r.Ambiguous) == 0 {
		r.Ambiguous = nil
	}
	if len(r.AmbiguousFiltered) == 0 {
		r.AmbiguousFiltered = nil
	}
	if len(r.Violations) == 0 {
		r.Violations = nil
	}
	if len(r.RawViolations) == 0 {
		r.RawViolations = nil
	}

	return r
}

func compareResults(t *testing.T, got, want generateResult) {
	t.Helper()
	fields := []struct {
		name      string
		got, want any
	}{
		{"error", got.Error, want.Error},
		{"map", got.Map, want.Map},
		{"ambiguous", got.Ambiguous, want.Ambiguous},
		{"ambiguousFiltered", got.AmbiguousFiltered, want.AmbiguousFiltered},
		{"violations", got.Violations, want.Violations},
		{"rawViolations", got.RawViolations, want.RawViolations},
	}
	for _, f := range fields {
		if !reflect.DeepEqual(f.got, f.want) {
			g, _ := json.Marshal(f.got)
			w, _ := json.Marshal(f.want)
			t.Errorf("%s differs\n got %s\nwant %s", f.name, g, w)
		}
	}
}

// treeHasModes reports whether a tree of buildTree changes file modes.
func treeHasModes(tree [][3]json.RawMessage) bool {
	for _, e := range tree {
		var kind string
		_ = json.Unmarshal(e[1], &kind)
		if kind != "file" && kind != "link" {
			return true
		}
	}

	return false
}

// buildTree is buildTree() of generate.php, in a temporary directory.
func buildTree(t *testing.T, tree [][3]json.RawMessage) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, _ error) error {
			if d != nil && d.IsDir() {
				_ = os.Chmod(path, 0o755)
			}

			return nil
		})
	})
	for _, e := range tree {
		var path, kind string
		_ = json.Unmarshal(e[0], &path)
		_ = json.Unmarshal(e[1], &kind)
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		var err error
		switch kind {
		case "file", "link":
			var content string
			_ = json.Unmarshal(e[2], &content)
			if kind == "file" {
				err = os.WriteFile(full, []byte(content), 0o644)
			} else {
				err = os.Symlink(content, full)
			}
		default:
			var mode uint32
			_ = json.Unmarshal(e[2], &mode)
			err = os.Chmod(full, os.FileMode(mode))
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// digest is digest() of generate.php.
func digest(r generateResult) string {
	n := normalizeResult(r)
	var lines, ambiguousLines []string
	if n.Error != nil {
		lines = append(lines, "error\t"+n.Error.Class+"\t"+n.Error.Message)
	}
	for _, a := range n.Ambiguous {
		ambiguousLines = append(ambiguousLines, "ambiguous\t"+a.Class+"\t"+strings.Join(a.Paths, "\t"))
	}
	for _, e := range n.Map {
		lines = append(lines, "map\t"+e[0]+"\t"+e[1])
	}
	for _, v := range n.Violations {
		lines = append(lines, "violation\t"+v)
	}
	for _, r := range n.RawViolations {
		parts := []string{"raw", r.Path}
		for _, v := range r.Violations {
			parts = append(parts, v[0], v[1])
		}
		lines = append(lines, strings.Join(parts, "\t"))
	}
	sum := md5.Sum([]byte(strings.Join(append(lines, ambiguousLines...), "\n")))

	return hex.EncodeToString(sum[:])
}
