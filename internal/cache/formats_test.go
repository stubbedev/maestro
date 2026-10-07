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
	// caches are the owned caches (own.go) holding entries of the format.
	caches []string
	// roots are what computes the entries the caches keep, writes them and
	// reads them back (see fingerprint).
	roots []string
	// keyed are declarations and packages the roots depend on whose own
	// version or contents the entries are keyed on as they are used,
	// which the fingerprint leaves out.
	keyed []string
}

// ownFormats are the versions of what maestro's own caches keep.
var ownFormats = []ownFormat{
	{
		format: "internal/classmap.parseFormat",
		caches: []string{classMapParsePath, storePath},
		roots: []string{
			"internal/classmap.Parser.classesIn", "internal/classmap.Parser.key",
			"internal/classmap.diskCache.load", "internal/classmap.ParseCache.Save",
			"internal/classmap.encodeResults", "internal/classmap.decodeResults",
			"internal/classmap.ReleaseParse",
		},
	},
	{
		format: "internal/classmap.recordFormat",
		caches: []string{classMapRecordsPath},
		roots: []string{
			"internal/classmap.NewRecord", "internal/classmap.Record",
			"internal/classmap.Generator",
			// the exclusion patterns' matcher (internal/autoload's)
			"internal/php.Regexp.IsMatch",
		},
	},
	{
		format: "internal/json.schemaFormat",
		caches: []string{schemaMemoPath},
		roots: []string{
			"internal/json.ValidateJSONSchema", "internal/json.schemaMemo", "internal/json.schemaKey",
		},
	},
	{
		format: "internal/json.decodedFormat",
		caches: []string{decodedFilesPath},
		roots: []string{
			"internal/json.File.parse", "internal/cache.Decoded",
			"internal/php.AppendBinary", "internal/php.DecodeBinary",
		},
	},
	{
		format: "internal/repository/composerrepo.decodedFormat",
		caches: []string{decodedMetadataPath},
		roots: []string{
			"internal/cache.Decoded", "internal/repository/composerrepo.ComposerRepository.decodeCached",
			"internal/repository/composerrepo.appendP2", "internal/repository/composerrepo.decodeP2",
			"internal/repository/composerrepo.decodeFile", "internal/repository/composerrepo.p2File",
			"internal/repository/composerrepo.p2Slot", "internal/repository/composerrepo.p2Index",
			// what the slots' indexes keep of the versions, found by
			// the speculation's loads
			"internal/repository/composerrepo.indexDraft", "internal/repository/composerrepo.indexBuilder",
			"internal/repository/composerrepo.scanVersions", "internal/repository/composerrepo.fitsSkeleton",
			"internal/repository/composerrepo.speculation",
		},
	},
	{
		format: "internal/platform.probeCacheFormat",
		caches: []string{platformProbesPath},
		roots: []string{
			"internal/platform.probeCacheKey", "internal/platform.storeProbeCache",
			"internal/platform.loadProbeCache", "internal/platform.loadProbeCacheEntry",
		},
		// the script is part of every entry's key
		keyed: []string{"internal/platform.probeScript"},
	},
	{
		format: "internal/util/vcs.versionFormat",
		caches: []string{gitVersionPath},
		roots: []string{
			"internal/util/vcs.GetVersion", "internal/util/vcs.versionCachePath",
			"internal/util/vcs.loadVersion", "internal/util/vcs.storeVersion",
		},
	},
	{
		format: "internal/archive.rulesFormat",
		caches: []string{storePath},
		roots:  []string{"internal/archive.Open", "internal/archive.Archive", "internal/archive.Rules"},
	},
	{
		format: "internal/store.indexFormat",
		caches: []string{storePath},
		roots: []string{
			"internal/store.encodeIndex", "internal/store.decodeIndex",
			"internal/store.inserter", "internal/store.Store.InsertDir",
		},
		// a release's index is keyed on archive.Rules (archive.rulesFormat)
		keyed: []string{"internal/archive"},
	},
}

// contentAddressed are the owned caches whose entries are named by the
// hash of what they hold, so that no code can make one stale.
var contentAddressed = []string{caBundlesPath}

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
		name, version, err := l.formatOf(f.format)
		if err != nil {
			t.Fatal(err)
		}
		fp, keys, err := l.fingerprint(f.roots, append([]string{f.format}, f.keyed...))
		if err != nil {
			t.Fatalf("%s: %v", f.format, err)
		}
		t.Logf("%s %d covers %d declarations", name, version, len(keys))
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

// formatOf is the Name and Version of the Format variable spec names.
func (l *srcLoader) formatOf(spec string) (string, int, error) {
	obj, err := l.lookup(spec)
	if err != nil {
		return "", 0, err
	}
	if t, ok := obj.Type().(*types.Named); !ok || t.Obj().Name() != "Format" || t.Obj().Pkg().Path() != l.modPath+"/internal/util/fsstate" {
		return "", 0, fmt.Errorf("%s is not a fsstate.Format", spec)
	}
	p := l.pkgs[obj.Pkg().Path()]
	vs, ok := p.decls[obj].(*ast.ValueSpec)
	if !ok || len(vs.Values) != len(vs.Names) {
		return "", 0, fmt.Errorf("%s: not a variable with a value", spec)
	}
	var lit *ast.CompositeLit
	for i, n := range vs.Names {
		if p.info.Defs[n] == obj {
			lit, _ = vs.Values[i].(*ast.CompositeLit)
		}
	}
	if lit == nil {
		return "", 0, fmt.Errorf("%s is not a composite literal", spec)
	}
	name, version := "", 0
	for _, e := range lit.Elts {
		kv, ok := e.(*ast.KeyValueExpr)
		if !ok {
			return "", 0, fmt.Errorf("%s: fields without names", spec)
		}
		val := p.info.Types[kv.Value].Value
		if val == nil {
			return "", 0, fmt.Errorf("%s: %s is not a constant", spec, kv.Key)
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
		return "", 0, fmt.Errorf("%s: name %q, version %d", spec, name, version)
	}

	return name, version, nil
}

// Each Format names another cache, and every owned cache holds the
// entries of a Format or is content-addressed.
func TestOwnFormatsCover(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("fingerprints are taken on Linux")
	}
	l, err := sharedLoader()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	covered := map[string]bool{}
	for _, c := range contentAddressed {
		covered[c] = true
	}
	for _, f := range ownFormats {
		name, _, err := l.formatOf(f.format)
		if err != nil {
			t.Fatal(err)
		}
		if other, dup := seen[name]; dup {
			t.Errorf("%s and %s are both named %q", other, f.format, name)
		}
		seen[name] = f.format
		for _, c := range f.caches {
			covered[c] = true
		}
	}
	for _, o := range owned {
		if !covered[o.Path] {
			t.Errorf("no Format covers %s", o.Path)
		}
	}
}
