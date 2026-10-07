package phperr_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// previousField matches the names maestro gives a PHP $previous field.
var previousField = regexp.MustCompile(`(?i)^prev(ious)?$`)

// previousMethods return an error's PHP previous exception.
var previousMethods = []string{"PHPPrevious"}

// TestNoUnwrapReturnsThePreviousException enforces docs/PORTING.md's rule
// that a $previous exception is reachable through phperr.Chained and
// never through Unwrap: PHP's catch and instanceof never look at the
// previous exception, so errors.As must not either. It fails for
// every Unwrap method in maestro's sources that reads its receiver's
// previous field (one named prev/previous, or the one PHPPrevious
// returns) or calls that method.
func TestNoUnwrapReturnsThePreviousException(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	leaks, err := unwrapLeaks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range leaks {
		t.Errorf("%s: Unwrap returns the PHP previous exception; expose it through phperr.Chained (PHPPrevious) only", leak)
	}
}

// unwrapLeaks returns the positions of the Unwrap methods under root's
// internal and cmd directories that read a previous exception.
func unwrapLeaks(root string) ([]string, error) {
	var leaks []string
	for _, top := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, top), func(dir string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			if name := d.Name(); name == "testdata" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			found, err := packageLeaks(root, dir)
			leaks = append(leaks, found...)

			return err
		})
		if err != nil {
			return nil, err
		}
	}

	return leaks, nil
}

// packageLeaks checks the non-test Go files of the package in dir.
func packageLeaks(root, dir string) ([]string, error) {
	fset := token.NewFileSet()
	entries, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	var files []*ast.File
	for _, path := range entries {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}

	prevFields := map[string]map[string]bool{} // type name -> field names
	addField := func(typ, field string) {
		if prevFields[typ] == nil {
			prevFields[typ] = map[string]bool{}
		}
		prevFields[typ][field] = true
	}
	var unwraps []*ast.FuncDecl
	for _, f := range files {
		for _, decl := range f.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					st, ok := ts.Type.(*ast.StructType)
					if !ok {
						continue
					}
					for _, field := range st.Fields.List {
						for _, name := range field.Names {
							if previousField.MatchString(name.Name) {
								addField(ts.Name.Name, name.Name)
							}
						}
					}
				}
			case *ast.FuncDecl:
				typ, recv := receiver(decl)
				if typ == "" || decl.Body == nil {
					continue
				}
				switch {
				case decl.Name.Name == "Unwrap":
					unwraps = append(unwraps, decl)
				case slices.Contains(previousMethods, decl.Name.Name):
					ast.Inspect(decl.Body, func(n ast.Node) bool {
						if field, ok := receiverSelector(n, recv); ok {
							addField(typ, field)
						}

						return true
					})
				}
			}
		}
	}

	var leaks []string
	for _, fn := range unwraps {
		typ, recv := receiver(fn)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sel, ok := receiverSelector(n, recv)
			if ok && (prevFields[typ][sel] || slices.Contains(previousMethods, sel)) {
				pos := fset.Position(n.Pos())
				rel, err := filepath.Rel(root, pos.Filename)
				if err != nil {
					rel = pos.Filename
				}
				leaks = append(leaks, filepath.ToSlash(rel)+":"+strconv.Itoa(pos.Line)+" ("+typ+"."+sel+")")
			}

			return true
		})
	}

	return leaks, nil
}

// receiver returns the receiver type and variable names of a method.
func receiver(fn *ast.FuncDecl) (typ, name string) {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return "", ""
	}
	field := fn.Recv.List[0]
	if len(field.Names) == 1 {
		name = field.Names[0].Name
	}
	expr := field.Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	switch x := expr.(type) {
	case *ast.Ident:
		typ = x.Name
	case *ast.IndexExpr: // generic receiver
		if id, ok := x.X.(*ast.Ident); ok {
			typ = id.Name
		}
	}

	return typ, name
}

// receiverSelector reports the selected name of n when n is recv.name.
func receiverSelector(n ast.Node, recv string) (string, bool) {
	sel, ok := n.(*ast.SelectorExpr)
	if !ok || recv == "" || recv == "_" {
		return "", false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok || id.Name != recv {
		return "", false
	}

	return sel.Sel.Name, true
}
