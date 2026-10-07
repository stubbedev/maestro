package autoload

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
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

// anonymize replaces e's working directory in s, as written natively and
// with forward slashes (the form Composer prints paths in on Windows).
func (e *env) anonymize(s string) string {
	s = strings.ReplaceAll(s, e.workingDir, "<dir>")

	return strings.ReplaceAll(s, filepath.ToSlash(e.workingDir), "<dir>")
}

// generatedFiles are the files a dump of e wrote, by name.
func (e *env) generatedFiles() map[string]string {
	e.t.Helper()
	files := map[string]string{}
	for _, path := range append([]string{e.vendorDir + "/autoload.php"}, func() []string {
		var paths []string
		for _, name := range dumpFiles {
			paths = append(paths, e.vendorDir+"/composer/"+name)
		}

		return paths
	}()...) {
		if content, err := os.ReadFile(path); err == nil {
			files[strings.TrimPrefix(path, e.vendorDir)] = e.anonymize(string(content))
		}
	}

	return files
}

// A dump that takes the speculation writes and prints what one without
// it does, the files it found unchanged included.
func TestGenerator_DumpAheadIsTheDump(t *testing.T) {
	want, p := speculationEnv(t)
	want.dump(p, true, "_1")
	wantOut := want.anonymize(want.io.Output())
	wantFiles := want.generatedFiles()

	e, p := speculationEnv(t)
	e.dump(p, true, "_1") // the files a no-op install finds
	e.io = newBufferIO(t)
	e.generator = NewGenerator(e.dispatcher, e.io)
	e.generator.Speculate(e.config, e.repo, p, e.im, true)
	<-e.generator.speculation.done
	if e.generator.speculation.ahead == nil || len(e.generator.speculation.current) == 0 {
		t.Fatal("the speculation built no class map files")
	}
	// changed since the speculation read it: written again
	e.write(e.vendorDir+"/composer/autoload_psr4.php", "<?php return array( );\n")
	e.dump(p, true, "_1")

	if got := e.anonymize(e.io.Output()); got != wantOut {
		t.Errorf("output %q, want %q", got, wantOut)
	}
	if got := e.generatedFiles(); !maps.Equal(got, wantFiles) {
		for name, content := range wantFiles {
			if got[name] != content {
				t.Errorf("%s:\n%s\nwant:\n%s", name, got[name], content)
			}
		}
	}
}

// A dump with strictAmbiguous does not take the speculation, which
// analysed its scan without.
func TestGenerator_DumpStrictAmbiguousScans(t *testing.T) {
	e, p := speculationEnv(t)
	e.generator.Speculate(e.config, e.repo, p, e.im, true)
	e.speculated()
	if _, err := e.generator.Dump(e.config, e.repo, p, e.im, "composer", true, "_1", nil, true); err != nil {
		t.Fatal(err)
	}

	if !e.classmapHas("Late") {
		t.Error("the strict dump took the speculation: Late is missing from the class map")
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

// The dump takes the dev mode the speculation read only from the
// installed.json it read: one written since is read again.
func TestGenerator_DumpRereadsChangedInstalledDevMode(t *testing.T) {
	e, p := speculationEnv(t)
	p.SetDevAutoload(arr("classmap", list("dev/")))
	e.mkdir(e.workingDir + "/dev")
	e.write(e.workingDir+"/dev/d.php", `<?php class DevOnly {}`)
	e.mkdir(e.vendorDir + "/composer")
	e.write(e.vendorDir+"/composer/installed.json", `{"packages": [], "dev": true, "dev-package-names": []}`)

	e.generator.Speculate(e.config, e.repo, p, e.im, false)
	e.speculated()
	if s := e.generator.speculation; s.installedJSON == nil || !s.devMode {
		t.Fatalf("the speculation did not keep installed.json's dev mode: %v", s.devMode)
	}
	e.write(e.vendorDir+"/composer/installed.json", `{"packages": [], "dev": false, "dev-package-names": []}`)
	e.dump(p, false, "_1")

	if e.classmapHas("DevOnly") {
		t.Error("the dump used the speculation's dev mode: DevOnly is in the class map")
	}
}

// A warm-up runs before the dev mode is set, and parses the root's
// autoload-dev rules too.
func TestGenerator_WarmReadsRootDevRules(t *testing.T) {
	e, p := speculationEnv(t)
	p.SetDevAutoload(arr("classmap", list("dev/")))
	packageMap, err := e.generator.BuildPackageMap(e.im, p, e.repo.CanonicalPackages())
	if err != nil {
		t.Fatal(err)
	}
	autoloads, err := e.generator.parseAutoloads(packageMap, p, NoDevFilter, true)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(autoloads.Classmap, func(dir string) bool { return dir == "dev/" }) {
		t.Errorf("the root's autoload-dev classmap is missing: %v", autoloads.Classmap)
	}
	if e.generator.devMode {
		t.Error("parseAutoloads set the dev mode")
	}
}
