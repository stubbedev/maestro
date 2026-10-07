package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/build"
	"go/importer"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// A fingerprint is the SHA-256 of the declarations a set of roots depends
// on in this module: the roots, and every function, method, type, constant
// and variable of the module their declarations refer to, transitively,
// with the init functions of the packages whose variables they read and
// the files those variables embed. A declaration counts by its tokens, so
// comments and formatting do not change it; a constant counts by its
// value. Code outside the module (the standard library, dependencies) is
// taken as fixed.
//
// Calls through an interface are followed to every method of that name of
// the module's types the declarations refer to, as are the methods the
// standard library calls through its interfaces (dispatched, below).

// dispatched are the methods the standard library calls through its own
// interfaces (fmt.Stringer, error, sort.Interface, io.Reader, ...): a type
// the fingerprinted code refers to counts with its methods of these names.
var dispatched = []string{
	"String", "GoString", "Format", "Error", "Unwrap", "Is", "As",
	"Len", "Less", "Swap",
	"Read", "Write", "Close", "Seek", "ReadByte", "UnreadByte", "WriteByte", "ReadRune", "WriteString", "ReadFrom", "WriteTo", "ReadAt", "WriteAt",
	"MarshalJSON", "UnmarshalJSON", "MarshalText", "UnmarshalText", "MarshalBinary", "UnmarshalBinary", "AppendText", "AppendBinary",
	"Name", "Size", "Mode", "ModTime", "IsDir", "Sys", "Type", "Info",
}

// srcPkg is a module package, parsed and type-checked.
type srcPkg struct {
	dir   string
	files []*ast.File
	info  *types.Info
	types *types.Package
	// decls maps each package-level object and method to its declaration:
	// an *ast.FuncDecl, *ast.TypeSpec or *ast.ValueSpec.
	decls map[types.Object]ast.Node
	// embeds maps a variable's ValueSpec to its //go:embed patterns.
	embeds map[*ast.ValueSpec][]string
	inits  []*ast.FuncDecl
}

// srcLoader loads the module's packages from source.
type srcLoader struct {
	fset    *token.FileSet
	root    string // module root directory
	modPath string
	std     types.Importer
	pkgs    map[string]*srcPkg
	src     map[*token.File][]byte
}

func newSrcLoader(root, modPath string) *srcLoader {
	fset := token.NewFileSet()

	return &srcLoader{
		fset: fset, root: root, modPath: modPath,
		std:  importer.ForCompiler(fset, "source", nil),
		pkgs: map[string]*srcPkg{},
		src:  map[*token.File][]byte{},
	}
}

// sharedLoader is the loader of this module, shared by the tests.
var sharedLoader = sync.OnceValues(func() (*srcLoader, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil, err
	}
	first, _, _ := strings.Cut(string(data), "\n")
	modPath, ok := strings.CutPrefix(first, "module ")
	if !ok {
		return nil, fmt.Errorf("go.mod starts with %q", first)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	return newSrcLoader(abs, strings.TrimSpace(modPath)), nil
})

func (l *srcLoader) inModule(path string) bool {
	return path == l.modPath || strings.HasPrefix(path, l.modPath+"/")
}

// Import implements types.Importer.
func (l *srcLoader) Import(path string) (*types.Package, error) {
	if !l.inModule(path) {
		return l.std.Import(path)
	}
	p, err := l.load(path)
	if err != nil {
		return nil, err
	}

	return p.types, nil
}

// load parses and type-checks the module package path, as built for the
// running platform without tests.
func (l *srcLoader) load(path string) (*srcPkg, error) {
	if p, ok := l.pkgs[path]; ok {
		return p, nil
	}
	dir := filepath.Join(l.root, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(path, l.modPath), "/")))
	bp, err := build.Default.ImportDir(dir, 0)
	if err != nil {
		return nil, err
	}
	p := &srcPkg{dir: dir, decls: map[types.Object]ast.Node{}, embeds: map[*ast.ValueSpec][]string{}}
	for _, name := range bp.GoFiles {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(l.fset, filepath.Join(dir, name), data, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		p.files = append(p.files, f)
		l.src[l.fset.File(f.Pos())] = data
	}
	p.info = &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: l}
	p.types, err = conf.Check(path, l.fset, p.files, p.info)
	if err != nil {
		return nil, err
	}
	for _, f := range p.files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.Name == "init" {
					p.inits = append(p.inits, d)

					continue
				}
				p.decls[p.info.Defs[d.Name]] = d
			case *ast.GenDecl:
				for _, s := range d.Specs {
					switch s := s.(type) {
					case *ast.TypeSpec:
						p.decls[p.info.Defs[s.Name]] = s
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if obj := p.info.Defs[n]; obj != nil {
								p.decls[obj] = s
							}
						}
						if d.Tok == token.VAR {
							p.embeds[s] = embedPatterns(d, s)
						}
					}
				}
			}
		}
	}
	l.pkgs[path] = p

	return p, nil
}

// embedPatterns are the //go:embed patterns of the variable spec s of d.
func embedPatterns(d *ast.GenDecl, s *ast.ValueSpec) []string {
	var patterns []string
	for _, doc := range []*ast.CommentGroup{d.Doc, s.Doc} {
		if doc == nil {
			continue
		}
		for _, c := range doc.List {
			rest, ok := strings.CutPrefix(c.Text, "//go:embed ")
			if !ok {
				continue
			}
			for f := range strings.FieldsSeq(rest) {
				if u, err := strconv.Unquote(f); err == nil {
					f = u
				}
				patterns = append(patterns, f)
			}
		}
	}

	return patterns
}

// lookup is the object spec names: "<package path in the module>.<name>"
// or "<package path in the module>.<type>.<method>".
func (l *srcLoader) lookup(spec string) (types.Object, error) {
	slash := strings.LastIndex(spec, "/")
	pkgPath, rest, ok := strings.Cut(spec[slash+1:], ".")
	if !ok {
		return nil, fmt.Errorf("%s: no name", spec)
	}
	pkgPath = spec[:slash+1] + pkgPath
	p, err := l.load(l.modPath + "/" + pkgPath)
	if err != nil {
		return nil, err
	}
	name, method, _ := strings.Cut(rest, ".")
	obj := p.types.Scope().Lookup(name)
	if obj == nil {
		return nil, fmt.Errorf("%s: %s not found", spec, name)
	}
	if method == "" {
		return obj, nil
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		return nil, fmt.Errorf("%s: %s is not a named type", spec, name)
	}
	for m := range named.Methods() {
		if m.Name() == method {
			return m, nil
		}
	}

	return nil, fmt.Errorf("%s: no method %s", spec, method)
}

// closure is what a fingerprint covers.
type closure struct {
	l *srcLoader
	// skip are declarations left out, skipPkgs packages left out.
	skip     map[types.Object]bool
	skipPkgs map[string]bool
	seen     map[types.Object]bool
	inits    map[*srcPkg]bool
	queue    []types.Object
	named    []*types.Named // the module's named types included
	methods  map[string]bool
	// entries are the fingerprinted declarations, by key.
	entries map[string][]byte
}

func (c *closure) add(obj types.Object) {
	if obj == nil || obj.Pkg() == nil || !c.l.inModule(obj.Pkg().Path()) {
		return
	}
	switch o := obj.(type) {
	case *types.Func:
		obj = o.Origin()
	}
	if _, isFunc := obj.(*types.Func); !isFunc && obj.Parent() != obj.Pkg().Scope() {
		return // a field, or local to a declaration counted with it
	}
	if c.seen[obj] || c.skip[obj] || c.skipPkgs[obj.Pkg().Path()] {
		return
	}
	c.seen[obj] = true
	c.queue = append(c.queue, obj)
}

// addMethodName follows calls of interface methods named name.
func (c *closure) addMethodName(name string) {
	if c.methods[name] {
		return
	}
	c.methods[name] = true
	for _, n := range c.named {
		c.addMethodsNamed(n, name)
	}
}

func (c *closure) addMethodsNamed(n *types.Named, name string) {
	for m := range n.Methods() {
		if m.Name() == name {
			c.add(m)
		}
	}
}

// fingerprint is the fingerprint of the declarations roots depend on,
// leaving out the declarations and packages skip names (as lookup takes
// them, or a package path in the module), and the keys of those
// declarations.
func (l *srcLoader) fingerprint(roots, skip []string) (string, []string, error) {
	c := &closure{l: l, skip: map[types.Object]bool{}, skipPkgs: map[string]bool{}, seen: map[types.Object]bool{}, inits: map[*srcPkg]bool{}, methods: map[string]bool{}, entries: map[string][]byte{}}
	for _, s := range skip {
		if !strings.Contains(s[strings.LastIndex(s, "/")+1:], ".") {
			if _, err := l.load(l.modPath + "/" + s); err != nil {
				return "", nil, err
			}
			c.skipPkgs[l.modPath+"/"+s] = true

			continue
		}
		obj, err := l.lookup(s)
		if err != nil {
			return "", nil, err
		}
		c.skip[obj] = true
	}
	for _, name := range dispatched {
		c.methods[name] = true
	}
	for _, spec := range roots {
		obj, err := l.lookup(spec)
		if err != nil {
			return "", nil, err
		}
		c.add(obj)
		if named, ok := obj.Type().(*types.Named); ok {
			if _, isType := obj.(*types.TypeName); isType {
				for m := range named.Methods() {
					c.add(m)
				}
			}
		}
	}
	for len(c.queue) > 0 {
		obj := c.queue[0]
		c.queue = c.queue[1:]
		if err := c.visit(obj); err != nil {
			return "", nil, err
		}
	}
	keys := make([]string, 0, len(c.entries))
	for k := range c.entries {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	h := sha256.New()
	for _, k := range keys {
		fmt.Fprintf(h, "%s\n%d\n", k, len(c.entries[k]))
		h.Write(c.entries[k])
	}

	return hex.EncodeToString(h.Sum(nil)), keys, nil
}

// key names obj among the fingerprinted declarations.
func key(obj types.Object) string {
	if f, ok := obj.(*types.Func); ok {
		if recv := f.Signature().Recv(); recv != nil {
			t := recv.Type()
			if p, ok := t.(*types.Pointer); ok {
				t = p.Elem()
			}
			if n, ok := t.(*types.Named); ok {
				return obj.Pkg().Path() + "." + n.Obj().Name() + "." + obj.Name()
			}
		}
	}

	return obj.Pkg().Path() + "." + obj.Name()
}

// visit fingerprints obj's declaration and adds what it refers to.
func (c *closure) visit(obj types.Object) error {
	p, err := c.l.load(obj.Pkg().Path())
	if err != nil {
		return err
	}
	if f, ok := obj.(*types.Func); ok {
		if recv := f.Signature().Recv(); recv != nil {
			if _, isIface := recv.Type().Underlying().(*types.Interface); isIface {
				// an interface's method: its implementations count
				c.addMethodName(f.Name())

				return nil
			}
		}
	}
	decl, ok := p.decls[obj]
	if !ok {
		return fmt.Errorf("%s: no declaration", key(obj))
	}
	var b bytes.Buffer
	switch o := obj.(type) {
	case *types.Const:
		// by value, which iota and implicit repetition make of the spec
		fmt.Fprintf(&b, "const %s %s", o.Type(), o.Val().ExactString())
	case *types.TypeName:
		c.tokens(&b, decl)
		if n, ok := o.Type().(*types.Named); ok {
			c.named = append(c.named, n)
			for name := range c.methods {
				c.addMethodsNamed(n, name)
			}
		}
	case *types.Var:
		c.tokens(&b, decl)
		if err := c.embedded(&b, p, decl.(*ast.ValueSpec)); err != nil {
			return err
		}
		if !c.inits[p] {
			// init functions may set the package's variables
			c.inits[p] = true
			for _, fn := range p.inits {
				var ib bytes.Buffer
				c.tokens(&ib, fn)
				pos := c.l.fset.Position(fn.Pos())
				c.entries[p.types.Path()+".init "+filepath.Base(pos.Filename)+":"+strconv.Itoa(pos.Line)] = ib.Bytes()
				c.refs(p, fn)
			}
		}
	default:
		c.tokens(&b, decl)
	}
	c.entries[key(obj)] = b.Bytes()
	if _, isConst := obj.(*types.Const); !isConst {
		c.refs(p, decl)
	}

	return nil
}

// refs adds the module objects node refers to.
func (c *closure) refs(p *srcPkg, node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			if obj := p.info.Uses[n]; obj != nil {
				if _, isPkg := obj.(*types.PkgName); !isPkg {
					c.add(obj)
				}
			}
		case ast.Expr:
			if tv, ok := p.info.Types[n]; ok && tv.Type != nil {
				c.addNamed(tv.Type)
			}
		}

		return true
	})
}

// addNamed adds the module's named types t is made of, as an expression's
// type the code never names (one inferred from a call's result).
func (c *closure) addNamed(t types.Type) {
	switch t := t.(type) {
	case *types.Named:
		c.add(t.Origin().Obj())
		for a := range t.TypeArgs().Types() {
			c.addNamed(a)
		}
	case *types.Pointer:
		c.addNamed(t.Elem())
	case *types.Slice:
		c.addNamed(t.Elem())
	case *types.Array:
		c.addNamed(t.Elem())
	case *types.Map:
		c.addNamed(t.Key())
		c.addNamed(t.Elem())
	case *types.Chan:
		c.addNamed(t.Elem())
	}
}

// tokens writes the tokens of node's source, without comments.
func (c *closure) tokens(b *bytes.Buffer, node ast.Node) {
	tf := c.l.fset.File(node.Pos())
	part := c.l.src[tf][tf.Offset(node.Pos()):tf.Offset(node.End())]
	var s scanner.Scanner
	fs := token.NewFileSet()
	s.Init(fs.AddFile("", -1, len(part)), part, nil, 0)
	for {
		_, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON {
			lit = ""
		}
		b.WriteString(tok.String())
		if lit != "" {
			b.WriteByte(' ')
			b.WriteString(lit)
		}
		b.WriteByte('\n')
	}
}

// embedded writes the files the variable spec embeds.
func (c *closure) embedded(b *bytes.Buffer, p *srcPkg, spec *ast.ValueSpec) error {
	for _, pattern := range p.embeds[spec] {
		matches, err := filepath.Glob(filepath.Join(p.dir, filepath.FromSlash(pattern)))
		if err != nil || len(matches) == 0 {
			return fmt.Errorf("%s: go:embed %s matches nothing", p.dir, pattern)
		}
		for _, m := range matches {
			err := filepath.WalkDir(m, func(path string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(p.dir, path)
				fmt.Fprintf(b, "embed %s %d\n", filepath.ToSlash(rel), len(data))
				b.Write(data)

				return nil
			})
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// fingerprintModule is a module the fingerprint tests change one thing in
// at a time.
var fingerprintModule = map[string]string{
	"go.mod": "module example.com/m\n\ngo 1.24\n",
	"a/a.go": `package a

import (
	_ "embed"
	"fmt"

	"example.com/m/b"
)

//go:embed data.txt
var data string

var table []int

func init() { table = []int{1, 2} }

const (
	zero = iota
	one
)

type shape interface{ area() int }

type square struct{ n int }

func (s square) area() int { return s.n * s.n }

func (s square) unused() int { return 0 }

type label struct{}

func (label) String() string { return "label" }

// Root is what the fingerprints cover.
func Root(n int) string {
	var s shape = square{n}
	return fmt.Sprint(s.area(), one, table, data, b.Helper(n), label{})
}

func unrelated() int { return 1 }
`,
	"a/data.txt": "data\n",
	"b/b.go": `package b

// Helper helps.
func Helper(n int) int { return n + 1 }
`,
}

// fingerprintOf is the fingerprint of a.Root in fingerprintModule with
// edit applied to the file at path.
func fingerprintOf(t *testing.T, path string, edit func(string) string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range fingerprintModule {
		if name == path {
			content = edit(content)
		}
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	fp, _, err := newSrcLoader(dir, "example.com/m").fingerprint([]string{"a.Root"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	return fp
}

// A fingerprint changes with the code its roots depend on, and only with
// it.
func TestFingerprint(t *testing.T) {
	replace := func(old, new string) func(string) string {
		return func(s string) string {
			if !strings.Contains(s, old) {
				t.Fatalf("%q not found", old)
			}

			return strings.Replace(s, old, new, 1)
		}
	}
	base := fingerprintOf(t, "", nil)
	for _, c := range []struct {
		name, path string
		edit       func(string) string
		changes    bool
	}{
		{"a comment", "a/a.go", replace("// Root is what", "// Root is all"), false},
		{"formatting", "a/a.go", replace("func unrelated() int { return 1 }", "func unrelated() int {\n\treturn 1\n}"), false},
		{"an unrelated function", "a/a.go", replace("return 1 }", "return 2 }"), false},
		{"a method no interface call reaches", "a/a.go", replace("return 0 }", "return 1 }"), false},
		{"another package's comment", "b/b.go", replace("// Helper helps.", "// Helper adds one."), false},
		{"the root", "a/a.go", replace("s.area(), one", "s.area(), one, 1"), true},
		{"a method called through an interface", "a/a.go", replace("s.n * s.n", "s.n + s.n"), true},
		{"a method the standard library calls", "a/a.go", replace(`"label"`, `"tag"`), true},
		{"a function of another package", "b/b.go", replace("n + 1", "n + 2"), true},
		{"an init function", "a/a.go", replace("{1, 2}", "{1, 3}"), true},
		{"an iota constant's value", "a/a.go", replace("zero = iota", "zero = iota + 1"), true},
		{"an embedded file", "a/data.txt", replace("data", "date"), true},
	} {
		if got := fingerprintOf(t, c.path, c.edit); (got != base) != c.changes {
			t.Errorf("%s: changed %v, want %v", c.name, got != base, c.changes)
		}
	}
}
