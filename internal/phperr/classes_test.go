package phperr

import (
	"encoding/json"
	"errors"
	"fmt"
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

// The class table is PHP's: every class exists, with the parent PHP
// reports, and implements the interfaces the table knows of exactly when
// PHP says it does (testdata/oracle/classes.json, tools/oracle/phperr).
func TestClasses_MatchPHP(t *testing.T) {
	data, err := os.ReadFile("testdata/oracle/classes.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden map[string]struct {
		Parent     *string  `json:"parent"`
		Interfaces []string `json:"interfaces"`
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden) != len(parents) {
		t.Errorf("the oracle has %d classes, the table %d: rerun it", len(golden), len(parents))
	}
	known := map[string]bool{}
	for _, interfaces := range implements {
		for _, i := range interfaces {
			known[i] = true
		}
	}
	for class, parent := range parents {
		want, ok := golden[class]
		if !ok {
			t.Errorf("%s: PHP has no such class (rerun the oracle?)", class)

			continue
		}
		if want.Parent == nil && parent != "" || want.Parent != nil && *want.Parent != parent {
			t.Errorf("%s: parent %q, PHP's %v", class, parent, want.Parent)
		}
		for i := range known {
			if got, want := IsSubclass(class, i), slices.Contains(want.Interfaces, i); got != want {
				t.Errorf("%s implements %s: %v, PHP says %v", class, i, got, want)
			}
		}
	}
}

// exceptionClass is a string that names a PHP exception class.
var exceptionClass = regexp.MustCompile(`^(?:[A-Z][A-Za-z0-9]*\\)*[A-Z][A-Za-z0-9]*(?:Exception|Error)$`)

// Every exception class maestro's sources name is in the table, so
// ClassOf, InstanceOf and the plugins see one hierarchy.
func TestClasses_CoverTheSources(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	missing := map[string]string{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata") {
				return filepath.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			s = strings.TrimPrefix(s, `\`)
			if _, ok := parents[s]; !ok && exceptionClass.MatchString(s) {
				rel, _ := filepath.Rel(root, path)
				missing[s] = rel
			}

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for class, where := range missing {
		t.Errorf("%s (%s) is not in the class table", class, where)
	}
}

type exc struct {
	class string
	code  int
}

func (e *exc) Error() string    { return e.class }
func (e *exc) PHPClass() string { return e.class }
func (e *exc) PHPCode() int     { return e.code }

type reported struct {
	exc
	classes []string
}

func (r *reported) InstanceOf(class string) bool { return slices.Contains(r.classes, class) }

func TestClassOfAndInstanceOf(t *testing.T) {
	transport := &exc{`Composer\Downloader\MaxFileSizeExceededException`, 7}
	wrapped := fmt.Errorf("context: %w", transport)
	for _, c := range []struct {
		err   error
		class string
		code  int
		of    []string
		notOf []string
	}{
		{
			transport, `Composer\Downloader\MaxFileSizeExceededException`, 7,
			[]string{`Composer\Downloader\TransportException`, "RuntimeException", "Exception"},
			[]string{"Error", "LogicException"},
		},
		{
			wrapped, `Composer\Downloader\MaxFileSizeExceededException`, 7,
			[]string{`Composer\Downloader\TransportException`},
			nil,
		},
		{errors.New("maestro's own"), DefaultClass, 0, []string{"RuntimeException", "Exception"}, []string{"Error"}},
		{&exc{"ParseError", 0}, "ParseError", 0, []string{"CompileError", "Error"}, []string{"Exception"}},
		{
			&reported{exc{`Acme\Failure`, 0}, []string{`Acme\Failure`, `Acme\Base`, "RuntimeException", "Exception", "Throwable"}},
			`Acme\Failure`, 0,
			[]string{`Acme\Base`, "Throwable"},
			[]string{"Error"},
		},
	} {
		class, code := ClassOf(c.err)
		if class != c.class || code != c.code {
			t.Errorf("ClassOf(%v) = %s, %d; want %s, %d", c.err, class, code, c.class, c.code)
		}
		for _, of := range c.of {
			if !InstanceOf(c.err, of) {
				t.Errorf("%v is not instanceof %s", c.err, of)
			}
		}
		for _, of := range c.notOf {
			if InstanceOf(c.err, of) {
				t.Errorf("%v is instanceof %s", c.err, of)
			}
		}
	}
}
