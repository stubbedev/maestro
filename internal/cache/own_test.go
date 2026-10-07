package cache

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
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

// TestOwnedDeclared: every path own.go declares is one of owned, once.
func TestOwnedDeclared(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "own.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var declared []string
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.CONST {
			for _, spec := range g.Specs {
				for _, v := range spec.(*ast.ValueSpec).Values {
					if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						s, _ := strconv.Unquote(lit.Value)
						declared = append(declared, s)
					}
				}
			}
		}
	}
	var paths []string
	for _, o := range owned {
		paths = append(paths, o.Path)
	}
	slices.Sort(declared)
	slices.Sort(paths)
	if !slices.Equal(declared, paths) || len(slices.Compact(slices.Clone(paths))) != len(paths) {
		t.Errorf("own.go declares %q, owned holds %q", declared, paths)
	}
}

// TestOwnedDocumented: docs/PORTING.md's "maestro's own caches" table
// lists every cache in owned, with the cache directory it is cleared
// with, and nothing else.
func TestOwnedDocumented(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(root, "docs", "PORTING.md"))
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("^\\| `([^`]+)` \\|.*\\| with `([a-z-]+)`")
	documented := map[string]string{}
	section := ""
	for line := range strings.Lines(string(data)) {
		if strings.HasPrefix(line, "#") {
			section = strings.TrimSpace(strings.TrimLeft(line, "#"))

			continue
		}
		if section != "maestro's own caches" || !strings.HasPrefix(line, "| `") {
			continue
		}
		m := row.FindStringSubmatch(line)
		if m == nil {
			t.Errorf("row without a path and a \"with `cache-...`\" column: %s", line)

			continue
		}
		documented[m[1]] = m[2]
	}

	for _, o := range owned {
		if with, ok := documented[o.Path]; !ok {
			t.Errorf("docs/PORTING.md does not document %s", o.Path)
		} else if with != o.With {
			t.Errorf("docs/PORTING.md clears %s with %s, clear-cache with %s", o.Path, with, o.With)
		}
		delete(documented, o.Path)
	}
	for p := range documented {
		t.Errorf("docs/PORTING.md documents %s, which internal/cache does not own", p)
	}
}

// TestOwnedPathsOnly: no code outside this package builds a path under
// Dir itself, so every cache maestro keeps there is one of owned.
func TestOwnedPathsOnly(t *testing.T) {
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
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || strings.HasPrefix(rel, "internal/cache/") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), "cache.Dir()") {
			t.Errorf("%s uses cache.Dir(): name its cache in internal/cache/own.go", rel)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClearOwned(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAESTRO_CACHE_DIR", dir)
	for _, o := range owned {
		p := filepath.Join(dir, filepath.FromSlash(o.Path), "entry")
		if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := ClearOwned("cache-dir"); err != nil {
		t.Fatal(err)
	}

	for _, o := range owned {
		_, err := os.Stat(filepath.Join(dir, filepath.FromSlash(o.Path)))
		if gone := os.IsNotExist(err); gone != (o.With == "cache-dir") {
			t.Errorf("%s (with %s): removed %v", o.Path, o.With, gone)
		}
	}

	// the store is pruned through internal/store, never removed here
	if err := ClearOwned("cache-files-dir"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Store()); err != nil {
		t.Errorf("store removed: %v", err)
	}
	if err := ClearOwned("cache-repo-dir"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(DecodedMetadata()); !os.IsNotExist(err) {
		t.Errorf("decoded metadata kept: %v", err)
	}
}
