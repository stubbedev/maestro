package autoload

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
)

// speculationEnv is a project whose root package has a classmap rule and
// a PSR-4 rule, with two definitions of one class.
func speculationEnv(t *testing.T) (*env, *pkg.RootPackage) {
	t.Helper()
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr("classmap", list("lib/"), "psr-4", arr(`App\`, "src/")))
	e.packages()
	e.mkdir(e.workingDir + "/lib")
	e.mkdir(e.workingDir + "/src")
	e.write(e.workingDir+"/lib/a.php", `<?php class Dup {} class A {}`)
	e.write(e.workingDir+"/lib/b.php", `<?php class Dup {}`)
	e.write(e.workingDir+"/src/C.php", `<?php namespace App; class C {}`)

	return e, p
}

// speculated waits for the speculation to end, then adds a class the
// speculation cannot have seen.
func (e *env) speculated() {
	e.t.Helper()
	if e.generator.speculation == nil {
		e.t.Fatal("Speculate started no scan")
	}
	<-e.generator.speculation.done
	e.write(e.workingDir+"/lib/late.php", `<?php class Late {}`)
}

func (e *env) classmapHas(class string) bool {
	e.t.Helper()
	for _, entry := range include(e.t, e.vendorDir+"/composer/autoload_classmap.php") {
		if entry[0] == class {
			return true
		}
	}

	return false
}

func TestGenerator_DumpTakesSpeculation(t *testing.T) {
	e, p := speculationEnv(t)
	e.generator.Speculate(e.config, e.repo, p, e.im, true)
	e.speculated()
	e.dump(p, true, "_1")

	if e.classmapHas("Late") {
		t.Error("the dump scanned again: Late is in the class map")
	}
	for _, class := range []string{"A", "Dup", `App\C`} {
		if !e.classmapHas(class) {
			t.Errorf("%s is missing from the class map", class)
		}
	}
	if out := e.io.Output(); !strings.Contains(out, `Warning: Ambiguous class resolution, "Dup"`) {
		t.Errorf("no ambiguous class warning; output:\n%s", out)
	}
	if e.generator.speculation != nil {
		t.Error("the speculation was kept after the dump")
	}
}

func TestGenerator_DumpScansAfterDiscardedSpeculation(t *testing.T) {
	e, p := speculationEnv(t)
	e.generator.Speculate(e.config, e.repo, p, e.im, true)
	e.speculated()
	e.generator.DiscardSpeculation()
	e.dump(p, true, "_1")

	if !e.classmapHas("Late") {
		t.Error("Late is missing from the class map")
	}
}

func TestGenerator_DumpScansWhenSpeculationScannedOtherwise(t *testing.T) {
	e, p := speculationEnv(t)
	// without the PSR directories
	e.generator.Speculate(e.config, e.repo, p, e.im, false)
	e.speculated()
	e.dump(p, true, "_1")

	if !e.classmapHas("Late") || !e.classmapHas(`App\C`) {
		t.Error("the dump took a speculation of other scans")
	}
}

func TestGenerator_SpeculationUsesInstalledDevMode(t *testing.T) {
	e, p := speculationEnv(t)
	p.SetDevAutoload(arr("classmap", list("dev/")))
	e.mkdir(e.workingDir + "/dev")
	e.write(e.workingDir+"/dev/d.php", `<?php class DevOnly {}`)
	e.mkdir(e.vendorDir + "/composer")
	e.write(e.vendorDir+"/composer/installed.json", `{"packages": [], "dev": true, "dev-package-names": []}`)

	e.generator.Speculate(e.config, e.repo, p, e.im, false)
	e.speculated()
	e.dump(p, false, "_1")

	if e.classmapHas("Late") {
		t.Error("the dump scanned again: Late is in the class map")
	}
	if !e.classmapHas("DevOnly") {
		t.Error("DevOnly is missing from the class map")
	}
}
