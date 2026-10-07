package switches

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// root is the module root, relative to this package's directory.
const root = "../.."

// runtimeVars are the MAESTRO_ variables that are not switches: maestro's
// own configuration, the plugin shim's channel and install.sh's inputs.
var runtimeVars = []string{
	"MAESTRO_CACHE_DIR",
	"MAESTRO_PACKAGE_IMPORT_METHOD",
	"MAESTRO_BINARY",
	"MAESTRO_IPC",
	"MAESTRO_IPC_TOKEN",
	"MAESTRO_VERSION",
	"MAESTRO_INSTALL_DIR",
}

var name = regexp.MustCompile(`MAESTRO_[A-Z0-9_]*[A-Z0-9]`)

// declared returns the values of the constants switches.go declares.
func declared(t *testing.T) map[string]bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "switches.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		if spec, ok := n.(*ast.ValueSpec); ok {
			for _, v := range spec.Values {
				if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s, _ := strconv.Unquote(lit.Value)
					names[s] = true
				}
			}
		}

		return true
	})
	if len(names) == 0 {
		t.Fatal("switches.go declares no switches")
	}

	return names
}

// TestDocumented: docs/PORTING.md's "Test switches" and "Profiling"
// tables list every switch, and nothing else.
func TestDocumented(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(root, "docs", "PORTING.md"))
	if err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	section := ""
	for line := range strings.Lines(string(data)) {
		if strings.HasPrefix(line, "#") {
			section = strings.TrimSpace(strings.TrimLeft(line, "#"))

			continue
		}
		if section != "Test switches" && section != "Profiling" || !strings.HasPrefix(line, "| ") {
			continue
		}
		first, _, _ := strings.Cut(line[2:], " | ")
		for _, n := range name.FindAllString(first, -1) {
			documented[n] = true
		}
	}

	want := declared(t)
	for _, n := range slices.Sorted(maps.Keys(want)) {
		if !documented[n] {
			t.Errorf("docs/PORTING.md does not document %s in \"Test switches\" or \"Profiling\"", n)
		}
	}
	for _, n := range slices.Sorted(maps.Keys(documented)) {
		if !want[n] {
			t.Errorf("docs/PORTING.md documents %s, which internal/switches does not declare", n)
		}
	}
}

// TestDocsNameNoStaleVariable: every MAESTRO_ variable the documentation
// names is a switch or one of runtimeVars, and each of runtimeVars is
// still read somewhere.
func TestDocsNameNoStaleVariable(t *testing.T) {
	known := declared(t)
	for _, n := range runtimeVars {
		known[n] = true
	}

	docs, _ := filepath.Glob(filepath.Join(root, "docs", "*.md"))
	docs = append(docs, filepath.Join(root, "README.md"))
	for _, doc := range docs {
		data, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range name.FindAllString(string(data), -1) {
			if !known[n] {
				t.Errorf("%s names %s, which nothing reads", filepath.Base(doc), n)
				known[n] = true
			}
		}
	}

	var sources strings.Builder
	walkSources(t, func(path string, data []byte) {
		if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".sh") {
			sources.Write(data)
		}
	})
	for _, n := range runtimeVars {
		if !strings.Contains(sources.String(), n) {
			t.Errorf("%s is listed as a runtime variable but no source reads it", n)
		}
	}
}

// envRead is a read of a MAESTRO_ variable by its literal name.
var envRead = regexp.MustCompile(`(?:Getenv|LookupEnv)\("(MAESTRO_\w+)"\)`)

// TestReadByConstant: code reads a switch through its constant, and every
// other MAESTRO_ variable read by name is a runtime variable or one the
// same test file sets for itself (a helper process, a variable a test
// sends PHP). A new switch therefore has to be declared here, which
// TestDocumented ties to the documentation.
func TestReadByConstant(t *testing.T) {
	switches := declared(t)
	walkSources(t, func(path string, data []byte) {
		if !strings.HasSuffix(path, ".go") {
			return
		}
		src := string(data)
		for _, m := range envRead.FindAllStringSubmatch(src, -1) {
			n := m[1]
			reads := len(regexp.MustCompile(`(?:Getenv|LookupEnv)\("`+n+`"\)`).FindAllStringIndex(src, -1))
			switch {
			case switches[n]:
				t.Errorf("%s reads %s by name: use internal/switches' constant", path, n)
			case slices.Contains(runtimeVars, n):
			case strings.HasSuffix(path, "_test.go") &&
				(strings.Contains(src, `"`+n+`=`) || strings.Count(src, `"`+n+`"`) > reads):
			default:
				t.Errorf("%s reads %s, which is no switch, no runtime variable and not set in the file: declare it in internal/switches and document it", path, n)
			}
		}
	})
}

// walkSources calls fn with every file of the module outside hidden
// directories and testdata, its path relative to the module root.
func walkSources(t *testing.T, fn func(path string, data []byte)) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata") {
				return filepath.SkipDir
			}

			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if filepath.ToSlash(rel) == "internal/switches/switches_test.go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(filepath.ToSlash(rel), data)

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
