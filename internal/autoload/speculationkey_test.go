package autoload

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stubbedev/maestro/internal/classmap"
)

// A package whose autoload rules change in place between the speculation
// and the dump (a plugin setting them, say) is scanned as it is then.
func TestGenerator_DumpScansAutoloadChangedInPlace(t *testing.T) {
	e, p := speculationEnv(t)
	e.generator.Speculate(e.config, e.repo, p, e.im, true)
	e.speculated()
	e.mkdir(e.workingDir + "/more")
	e.write(e.workingDir+"/more/m.php", `<?php class More {}`)
	p.SetAutoload(arr("classmap", list("lib/", "more/"), "psr-4", arr(`App\`, "src/")))
	e.dump(p, true, "_1")

	if !e.classmapHas("Late") || !e.classmapHas("More") {
		t.Error("the dump took a speculation of the autoload rules before they changed")
	}
}

// perturb changes v, a key field, to another value of its type.
func perturb(t *testing.T, name string, v reflect.Value) {
	t.Helper()
	v = reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem() // unexported fields too
	switch v.Kind() {
	case reflect.String:
		v.SetString(v.String() + "/other")
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int:
		v.SetInt(v.Int() + 1)
	case reflect.Struct:
		for i := range v.NumField() {
			perturb(t, name+"."+v.Type().Field(i).Name, v.Field(i))
		}
	default:
		t.Fatalf("perturb cannot change %s, a %s: add a case", name, v.Type())
	}
}

// Every field of dumpPaths keeps a dump from taking class map files built
// for another value of it: a field added to it is compared without
// anything else to update.
func TestSpeculation_TakeClassmapComparesEveryPath(t *testing.T) {
	paths := dumpPaths{"/p", "/p/vendor", "/p/vendor/composer", "/p/vendor/composer", "__DIR__ . '/..'", "__DIR__ . '/composer'", "dirname($vendorDir)", "/p/vendor", "/p"}
	s := &scanResult{ahead: &dump{dumpPaths: paths, classes: []string{"A"}}}
	if d := (&dump{dumpPaths: paths}); !s.takeClassmap(d) || len(d.classes) != 1 {
		t.Fatal("a dump with the same paths did not take the class map files")
	}
	typ := reflect.TypeFor[dumpPaths]()
	for i := range typ.NumField() {
		d := &dump{dumpPaths: paths}
		perturb(t, typ.Field(i).Name, reflect.ValueOf(&d.dumpPaths).Elem().Field(i))
		if s.takeClassmap(d) {
			t.Errorf("a dump with another %s took the class map files", typ.Field(i).Name)
		}
	}
}

// Every field of scanInputs keeps a dump from taking a scan speculated
// for another value of it.
func TestSpeculation_ScanKeyComparesEveryInput(t *testing.T) {
	key := scanKey{scanInputs{parser: classmap.DefaultParser, scanPsrPackages: true, basePath: "/p", vendorPath: "/p/vendor"}, &Autoloads{Classmap: []string{"lib/"}}}
	if !key.same(scanKey{key.scanInputs, &Autoloads{Classmap: []string{"lib/"}}}) {
		t.Fatal("the same scan's keys differ")
	}
	if key.same(scanKey{key.scanInputs, &Autoloads{Classmap: []string{"lib/", "more/"}}}) {
		t.Error("the keys of scans of other rules are the same")
	}
	typ := reflect.TypeFor[scanInputs]()
	for i := range typ.NumField() {
		other := key
		perturb(t, typ.Field(i).Name, reflect.ValueOf(&other.scanInputs).Elem().Field(i))
		if key.same(other) {
			t.Errorf("the key of a scan with another %s is the same", typ.Field(i).Name)
		}
	}
}
