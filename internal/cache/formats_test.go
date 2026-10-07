package cache

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/switches"
)

// ownFormat is a fsstate.Format of maestro's own caches and the code it
// covers.
type ownFormat struct {
	// format is the Format variable, "<package>.<name>".
	format string
	// roots are what computes the entries the caches keep, writes them and
	// reads them back (see fingerprint).
	roots []string
}

// ownFormats are the versions of what maestro's own caches keep.
var ownFormats = []ownFormat{
	{
		format: "internal/classmap.parseFormat",
		roots: []string{
			"internal/classmap.Parser.classesIn", "internal/classmap.Parser.key",
			"internal/classmap.diskCache.load", "internal/classmap.ParseCache.Save",
			"internal/classmap.encodeResults", "internal/classmap.decodeResults",
			"internal/classmap.ReleaseParse",
		},
	},
	{
		format: "internal/classmap.recordFormat",
		roots: []string{
			"internal/classmap.NewRecord", "internal/classmap.Record",
			"internal/classmap.Generator",
			// the exclusion patterns' matcher (internal/autoload's)
			"internal/php.Regexp.IsMatch",
		},
	},
	{
		format: "internal/json.schemaFormat",
		roots: []string{
			"internal/json.ValidateJSONSchema", "internal/json.schemaMemo", "internal/json.schemaKey",
		},
	},
}

// formatsFile records, for each version of each Format, the fingerprint of
// the code it covers. It only grows: a version, once recorded, keeps its
// fingerprint.
const formatsFile = "testdata/formats.txt"

// TestOwnFormats: the code each Format covers is the code its version was
// recorded with, so that an entry a maestro kept is only used by one
// computing, writing and reading it the same way, whichever build wrote
// it. A change to that code needs a new version, recorded by running the
// test with MAESTRO_UPDATE_FORMATS=1.
func TestOwnFormats(t *testing.T) {
	if runtime.GOOS != "linux" {
		// the fingerprints are of the code built for Linux
		t.Skip("fingerprints are taken on Linux")
	}
	l, err := sharedLoader()
	if err != nil {
		t.Fatal(err)
	}
	recorded, err := readFormats()
	if err != nil {
		t.Fatal(err)
	}
	update := os.Getenv(switches.UpdateFormats) == "1"
	var added []string
	for _, f := range ownFormats {
		obj, name, version, err := l.formatOf(f.format)
		if err != nil {
			t.Fatal(err)
		}
		fp, _, err := l.fingerprint(f.roots, []types.Object{obj})
		if err != nil {
			t.Fatalf("%s: %v", f.format, err)
		}
		versions := recorded[name]
		last := 0
		for v := range versions {
			last = max(last, v)
		}
		switch got, ok := versions[version]; {
		case ok && got == fp:
		case ok:
			t.Errorf("the code %s covers changed since version %d was recorded: set its Version to %d and run this test with MAESTRO_UPDATE_FORMATS=1 to record it", f.format, version, last+1)
		case version <= last:
			t.Errorf("%s is version %d, %d is recorded already", f.format, version, last)
		case update:
			added = append(added, fmt.Sprintf("%s %d %s", name, version, fp))
		default:
			t.Errorf("%s version %d is not recorded: run this test with MAESTRO_UPDATE_FORMATS=1", f.format, version)
		}
	}
	if len(added) > 0 {
		f, err := os.OpenFile(formatsFile, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		for _, line := range added {
			if _, err := f.WriteString(line + "\n"); err != nil {
				t.Fatal(err)
			}
			t.Logf("recorded %s", line)
		}
	}
}

// readFormats reads formatsFile: name -> version -> fingerprint.
func readFormats() (map[string]map[int]string, error) {
	f, err := os.Open(formatsFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	recorded := map[string]map[int]string{}
	for s := bufio.NewScanner(f); s.Scan(); {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return nil, fmt.Errorf("%s: malformed line %q", formatsFile, line)
		}
		v, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("%s: malformed line %q", formatsFile, line)
		}
		if recorded[fields[0]] == nil {
			recorded[fields[0]] = map[int]string{}
		}
		if _, dup := recorded[fields[0]][v]; dup {
			return nil, fmt.Errorf("%s: %s version %d recorded twice", formatsFile, fields[0], v)
		}
		recorded[fields[0]][v] = fields[2]
	}

	return recorded, nil
}

// formatOf is the Format variable spec names, with its Name and Version.
func (l *srcLoader) formatOf(spec string) (types.Object, string, int, error) {
	obj, err := l.lookup(spec)
	if err != nil {
		return nil, "", 0, err
	}
	if t, ok := obj.Type().(*types.Named); !ok || t.Obj().Name() != "Format" || t.Obj().Pkg().Path() != l.modPath+"/internal/util/fsstate" {
		return nil, "", 0, fmt.Errorf("%s is not a fsstate.Format", spec)
	}
	p := l.pkgs[obj.Pkg().Path()]
	vs, ok := p.decls[obj].(*ast.ValueSpec)
	if !ok || len(vs.Values) != len(vs.Names) {
		return nil, "", 0, fmt.Errorf("%s: not a variable with a value", spec)
	}
	var lit *ast.CompositeLit
	for i, n := range vs.Names {
		if p.info.Defs[n] == obj {
			lit, _ = vs.Values[i].(*ast.CompositeLit)
		}
	}
	if lit == nil {
		return nil, "", 0, fmt.Errorf("%s is not a composite literal", spec)
	}
	name, version := "", 0
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			return nil, "", 0, fmt.Errorf("%s: fields without names", spec)
		}
		val := p.info.Types[kv.Value].Value
		if val == nil {
			return nil, "", 0, fmt.Errorf("%s: %s is not a constant", spec, kv.Key)
		}
		switch kv.Key.(*ast.Ident).Name {
		case "Name":
			name = constant.StringVal(val)
		case "Version":
			v, _ := constant.Int64Val(val)
			version = int(v)
		}
	}
	if name == "" || strings.ContainsAny(name, " \t\n") || version < 1 {
		return nil, "", 0, fmt.Errorf("%s: name %q, version %d", spec, name, version)
	}

	return obj, name, version, nil
}

// Each Format names another cache.
func TestOwnFormatsNamed(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fingerprints are taken on Linux")
	}
	l, err := sharedLoader()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, f := range ownFormats {
		_, name, _, err := l.formatOf(f.format)
		if err != nil {
			t.Fatal(err)
		}
		if other, dup := seen[name]; dup {
			t.Errorf("%s and %s are both named %q", other, f.format, name)
		}
		seen[name] = f.format
	}
}
