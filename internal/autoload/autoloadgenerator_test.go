// Ports tests/Composer/Test/Autoload/AutoloadGeneratorTest.php.

package autoload

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

func TestAutoloadGenerator_RootPackageAutoloading(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr("Main", "src/", "Lala", list("src/", "lib/")),
		"psr-4", arr(`Acme\Fruit\`, "src-fruit/", `Acme\Cake\`, list("src-cake/", "lib-cake/")),
		"classmap", list("composersrc/"),
	))
	e.packages()

	e.mkdir(e.workingDir + "/composer")
	e.mkdir(e.workingDir + "/src/Lala/Test")
	e.mkdir(e.workingDir + "/lib")
	e.write(e.workingDir+"/src/Lala/ClassMapMain.php", `<?php namespace Lala; class ClassMapMain {}`)
	e.write(e.workingDir+"/src/Lala/Test/ClassMapMainTest.php", `<?php namespace Lala\Test; class ClassMapMainTest {}`)
	e.mkdir(e.workingDir + "/src-fruit")
	e.mkdir(e.workingDir + "/src-cake")
	e.mkdir(e.workingDir + "/lib-cake")
	e.write(e.workingDir+"/src-cake/ClassMapBar.php", `<?php namespace Acme\Cake; class ClassMapBar {}`)
	e.mkdir(e.workingDir + "/composersrc")
	e.write(e.workingDir+"/composersrc/foo.php", `<?php class ClassMapFoo {}`)

	e.dump(p, true, "_1")

	e.assertAutoloadFiles("main", e.vendorDir+"/composer", "namespaces")
	e.assertAutoloadFiles("psr4", e.vendorDir+"/composer", "psr4")
	e.assertAutoloadFiles("classmap", e.vendorDir+"/composer", "classmap")
}

func TestAutoloadGenerator_RootPackageDevAutoloading(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr("psr-0", arr("Main", "src/")))
	p.SetDevAutoload(arr(
		"files", list("devfiles/foo.php"),
		"psr-0", arr("Main", "tests/"),
	))
	e.packages()

	e.mkdir(e.workingDir + "/composer")
	e.mkdir(e.workingDir + "/src/Main")
	e.write(e.workingDir+"/src/Main/ClassMain.php", `<?php namespace Main; class ClassMain {}`)
	e.mkdir(e.workingDir + "/devfiles")
	e.write(e.workingDir+"/devfiles/foo.php", `<?php function foo() { echo "foo"; }`)

	// generate autoload files with the dev mode set to true
	e.generator.SetDevMode(true)
	e.dump(p, true, "_1")

	// check standard autoload
	e.assertAutoloadFiles("main5", e.vendorDir+"/composer", "namespaces")
	e.assertAutoloadFiles("classmap7", e.vendorDir+"/composer", "classmap")

	// make sure dev autoload is correctly dumped
	e.assertAutoloadFiles("files2", e.vendorDir+"/composer", "files")
}

func TestAutoloadGenerator_RootPackageDevAutoloadingDisabledByDefault(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr("psr-0", arr("Main", "src/")))
	p.SetDevAutoload(arr("files", list("devfiles/foo.php")))
	e.packages()

	e.mkdir(e.workingDir + "/composer")
	e.mkdir(e.workingDir + "/src/Main")
	e.write(e.workingDir+"/src/Main/ClassMain.php", `<?php namespace Main; class ClassMain {}`)
	e.mkdir(e.workingDir + "/devfiles")
	e.write(e.workingDir+"/devfiles/foo.php", `<?php function foo() { echo "foo"; }`)

	e.dump(p, true, "_1")

	e.assertAutoloadFiles("main4", e.vendorDir+"/composer", "namespaces")
	e.assertAutoloadFiles("classmap7", e.vendorDir+"/composer", "classmap")

	// make sure dev autoload is disabled when dev mode is set to false
	assertNotExists(t, e.vendorDir+"/composer/autoload_files.php")
}

func TestAutoloadGenerator_VendorDirSameAsWorkingDir(t *testing.T) {
	e := setUp(t)
	e.vendorDir = e.workingDir

	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr("Main", "src/", "Lala", "src/"),
		"psr-4", arr(`Acme\Fruit\`, "src-fruit/", `Acme\Cake\`, list("src-cake/", "lib-cake/")),
		"classmap", list("composersrc/"),
	))
	e.packages()

	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.vendorDir + "/src/Main")
	e.write(e.vendorDir+"/src/Main/Foo.php", `<?php namespace Main; class Foo {}`)
	e.mkdir(e.vendorDir + "/composersrc")
	e.write(e.vendorDir+"/composersrc/foo.php", `<?php class ClassMapFoo {}`)

	e.dump(p, true, "_2")
	e.assertAutoloadFiles("main3", e.vendorDir+"/composer", "namespaces")
	e.assertAutoloadFiles("psr4_3", e.vendorDir+"/composer", "psr4")
	e.assertAutoloadFiles("classmap3", e.vendorDir+"/composer", "classmap")
}

func TestAutoloadGenerator_RootPackageAutoloadingAlternativeVendorDir(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr("Main", "src/", "Lala", "src/"),
		"psr-4", arr(`Acme\Fruit\`, "src-fruit/", `Acme\Cake\`, list("src-cake/", "lib-cake/")),
		"classmap", list("composersrc/"),
	))
	e.packages()

	e.vendorDir += "/subdir"

	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.workingDir + "/src")
	e.mkdir(e.workingDir + "/composersrc")
	e.write(e.workingDir+"/composersrc/foo.php", `<?php class ClassMapFoo {}`)
	e.dump(p, false, "_3")
	e.assertAutoloadFiles("main2", e.vendorDir+"/composer", "namespaces")
	e.assertAutoloadFiles("psr4_2", e.vendorDir+"/composer", "psr4")
	e.assertAutoloadFiles("classmap2", e.vendorDir+"/composer", "classmap")
}

func TestAutoloadGenerator_RootPackageAutoloadingWithTargetDir(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr(`Main\Foo`, "", `Main\Bar`, ""),
		"classmap", list("Main/Foo/src", "lib"),
		"files", list("foo.php", "Main/Foo/bar.php"),
	))
	p.SetTargetDir(pkg.Str("Main/Foo/"))
	e.packages()

	e.mkdir(e.vendorDir + "/a")
	e.mkdir(e.workingDir + "/src")
	e.mkdir(e.workingDir + "/lib")
	e.write(e.workingDir+"/src/rootfoo.php", `<?php class ClassMapFoo {}`)
	e.write(e.workingDir+"/lib/rootbar.php", `<?php class ClassMapBar {}`)
	e.write(e.workingDir+"/foo.php", `<?php class FilesFoo {}`)
	e.write(e.workingDir+"/bar.php", `<?php class FilesBar {}`)

	e.dump(p, false, "TargetDir")
	e.assertFileContentEquals("autoload_target_dir.php", e.vendorDir+"/autoload.php")
	e.assertFileContentEquals("autoload_real_target_dir.php", e.vendorDir+"/composer/autoload_real.php")
	e.assertFileContentEquals("autoload_static_target_dir.php", e.vendorDir+"/composer/autoload_static.php")
	e.assertFileContentEquals("autoload_files_target_dir.php", e.vendorDir+"/composer/autoload_files.php")
	e.assertAutoloadFiles("classmap6", e.vendorDir+"/composer", "classmap")
}

func TestAutoloadGenerator_DuplicateFilesWarning(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr("files", list("foo.php", "bar.php", "./foo.php", "././foo.php")))
	e.packages()

	e.mkdir(e.vendorDir + "/a")
	e.mkdir(e.workingDir + "/src")
	e.mkdir(e.workingDir + "/lib")
	e.write(e.workingDir+"/foo.php", `<?php class FilesFoo {}`)
	e.write(e.workingDir+"/bar.php", `<?php class FilesBar {}`)

	e.dump(p, false, "FilesWarning")
	e.assertFileContentEquals("autoload_files_duplicates.php", e.vendorDir+"/composer/autoload_files.php")
	want := `<warning>The following "files" autoload rules are included multiple times, this may cause issues and should be resolved:</warning>` + "\n" +
		`<warning> - $baseDir . '/foo.php'</warning>` + "\n"
	if got := e.io.Output(); got != want {
		t.Errorf("output %q, want %q", got, want)
	}
}

func TestAutoloadGenerator_VendorsAutoloading(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a"), link("a", "b/b")))

	a := newPackage("a/a")
	b := newPackage("b/b")
	c := pkg.NewAliasPackage(b, "1.2", "1.2")
	a.SetAutoload(arr("psr-0", arr("A", "src/", `A\B`, "lib/")))
	b.SetAutoload(arr("psr-0", arr(`B\Sub\Name`, "src/")))
	e.packages(a, b, c)

	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.vendorDir + "/a/a/src")
	e.mkdir(e.vendorDir + "/a/a/lib")
	e.mkdir(e.vendorDir + "/b/b/src")

	e.dump(p, false, "_5")
	e.assertAutoloadFiles("vendors", e.vendorDir+"/composer", "namespaces")
	assertExists(t, e.vendorDir+"/composer/autoload_classmap.php")
}

func TestAutoloadGenerator_VendorsAutoloadingWithMetapackages(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a")))

	a := newPackage("a/a")
	b := newPackage("b/b")
	c := pkg.NewAliasPackage(b, "1.2", "1.2")
	a.SetAutoload(arr("psr-0", arr("A", "src/", `A\B`, "lib/")))
	b.SetAutoload(arr("psr-0", arr(`B\Sub\Name`, "src/")))
	a.SetType("metapackage")
	a.SetRequires(links(link("a/a", "b/b")))
	e.packages(a, b, c)

	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.vendorDir + "/b/b/src")
	// creating a/a files to make sure they would be found by autoloader
	// even tho they are technically not needed as the package is a
	// metapackage, but if it fails to be excluded it would find these
	e.mkdir(e.vendorDir + "/a/a/src")
	e.mkdir(e.vendorDir + "/a/a/lib")

	e.dump(p, false, "_5")
	e.assertAutoloadFiles("vendors_meta", e.vendorDir+"/composer", "namespaces")
	assertExists(t, e.vendorDir+"/composer/autoload_classmap.php")
}

func TestAutoloadGenerator_NonDevAutoloadExclusionWithRecursion(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a")))

	a := newPackage("a/a")
	b := newPackage("b/b")
	a.SetAutoload(arr("psr-0", arr("A", "src/", `A\B`, "lib/")))
	a.SetRequires(links(link("a/a", "b/b")))
	b.SetAutoload(arr("psr-0", arr(`B\Sub\Name`, "src/")))
	b.SetRequires(links(link("b/b", "a/a")))
	e.packages(a, b)

	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.vendorDir + "/a/a/src")
	e.mkdir(e.vendorDir + "/a/a/lib")
	e.mkdir(e.vendorDir + "/b/b/src")

	e.dump(p, false, "_5")
	e.assertAutoloadFiles("vendors", e.vendorDir+"/composer", "namespaces")
	assertExists(t, e.vendorDir+"/composer/autoload_classmap.php")
}

func TestAutoloadGenerator_NonDevAutoloadShouldIncludeReplacedPackages(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a")))

	a := newPackage("a/a")
	b := newPackage("b/b")
	a.SetRequires(links(link("a/a", "b/c")))
	b.SetAutoload(arr("psr-4", arr(`B\`, "src/")))
	eq, err := semver.NewConstraint("==", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	b.SetReplaces(links(pkg.NewLink("b/b", "b/c", eq, pkg.TypeReplace, pkg.NullString{})))
	e.packages(a, b)

	e.mkdir(e.vendorDir + "/b/b/src/C")
	e.write(e.vendorDir+"/b/b/src/C/C.php", `<?php namespace B\C; class C {}`)

	e.dump(p, true, "_5")

	assertEntries(t, include(t, e.vendorDir+"/composer/autoload_classmap.php"), [][2]string{
		{`B\C\C`, e.vendorDir + "/b/b/src/C/C.php"},
		{`Composer\InstalledVersions`, e.vendorDir + "/composer/InstalledVersions.php"},
	})
}

func TestAutoloadGenerator_NonDevAutoloadExclusionWithRecursionReplace(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a")))

	a := newPackage("a/a")
	b := newPackage("b/b")
	a.SetAutoload(arr("psr-0", arr("A", "src/", `A\B`, "lib/")))
	a.SetRequires(links(link("a/a", "c/c")))
	b.SetAutoload(arr("psr-0", arr(`B\Sub\Name`, "src/")))
	b.SetReplaces(links(link("b/b", "c/c")))
	e.packages(a, b)

	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.vendorDir + "/a/a/src")
	e.mkdir(e.vendorDir + "/a/a/lib")
	e.mkdir(e.vendorDir + "/b/b/src")

	e.dump(p, false, "_5")
	e.assertAutoloadFiles("vendors", e.vendorDir+"/composer", "namespaces")
	assertExists(t, e.vendorDir+"/composer/autoload_classmap.php")
}

func TestAutoloadGenerator_NonDevAutoloadReplacesNestedRequirements(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a")))

	a, b, c, d, f := newPackage("a/a"), newPackage("b/b"), newPackage("c/c"), newPackage("d/d"), newPackage("e/e")
	a.SetAutoload(arr("classmap", list("src/A.php")))
	a.SetRequires(links(link("a/a", "b/b")))
	b.SetAutoload(arr("classmap", list("src/B.php")))
	b.SetRequires(links(link("b/b", "e/e")))
	c.SetAutoload(arr("classmap", list("src/C.php")))
	c.SetReplaces(links(link("c/c", "b/b")))
	c.SetRequires(links(link("c/c", "d/d")))
	d.SetAutoload(arr("classmap", list("src/D.php")))
	f.SetAutoload(arr("classmap", list("src/E.php")))
	e.packages(a, b, c, d, f)

	for _, n := range []string{"a/a", "b/b", "c/c", "d/d", "e/e"} {
		e.mkdir(e.vendorDir + "/" + n + "/src")
	}
	e.write(e.vendorDir+"/a/a/src/A.php", `<?php class A {}`)
	e.write(e.vendorDir+"/b/b/src/B.php", `<?php class B {}`)
	e.write(e.vendorDir+"/c/c/src/C.php", `<?php class C {}`)
	e.write(e.vendorDir+"/d/d/src/D.php", `<?php class D {}`)
	e.write(e.vendorDir+"/e/e/src/E.php", `<?php class E {}`)

	e.dump(p, false, "_5")

	e.assertAutoloadFiles("classmap9", e.vendorDir+"/composer", "classmap")
}

func TestAutoloadGenerator_PharAutoload(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a")))
	p.SetAutoload(arr(
		"psr-0", arr("Foo", "foo.phar", "Bar", "dir/bar.phar/src"),
		"psr-4", arr(`Baz\`, "baz.phar", `Qux\`, "dir/qux.phar/src"),
	))

	vendorPackage := newPackage("a/a")
	vendorPackage.SetAutoload(arr(
		"psr-0", arr("Lorem", "lorem.phar", "Ipsum", "dir/ipsum.phar/src"),
		"psr-4", arr(`Dolor\`, "dolor.phar", `Sit\`, "dir/sit.phar/src"),
	))
	e.packages(vendorPackage)

	e.dump(p, true, "Phar")

	e.assertAutoloadFiles("phar", e.vendorDir+"/composer", "namespaces")
	e.assertAutoloadFiles("phar_psr4", e.vendorDir+"/composer", "psr4")
	e.assertAutoloadFiles("phar_static", e.vendorDir+"/composer", "static")
}

func TestAutoloadGenerator_PSRToClassMapIgnoresNonExistingDir(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr("Prefix", "foo/bar/non/existing/"),
		"psr-4", arr(`Prefix\`, "foo/bar/non/existing2/"),
	))
	e.packages()

	e.dump(p, true, "_8")
	assertEntries(t, include(t, e.vendorDir+"/composer/autoload_classmap.php"), [][2]string{
		{`Composer\InstalledVersions`, e.vendorDir + "/composer/InstalledVersions.php"},
	})
}

func TestAutoloadGenerator_PSRToClassMapIgnoresNonPSRClasses(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr("psr0_", "psr0/"),
		"psr-4", arr(`psr4\`, "psr4/"),
	))
	e.packages()

	e.mkdir(e.workingDir + "/psr0/psr0")
	e.mkdir(e.workingDir + "/psr4")
	e.write(e.workingDir+"/psr0/psr0/match.php", `<?php class psr0_match {}`)
	e.write(e.workingDir+"/psr0/psr0/badfile.php", `<?php class psr0_badclass {}`)
	e.write(e.workingDir+"/psr4/match.php", `<?php namespace psr4; class match {}`)
	e.write(e.workingDir+"/psr4/badfile.php", `<?php namespace psr4; class badclass {}`)

	e.dump(p, true, "_1")

	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_classmap.php", `<?php

// autoload_classmap.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
    'Composer\\InstalledVersions' => $vendorDir . '/composer/InstalledVersions.php',
    'psr0_match' => $baseDir . '/psr0/psr0/match.php',
    'psr4\\match' => $baseDir . '/psr4/match.php',
);
`)
}

func TestAutoloadGenerator_VendorsClassMapAutoloading(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a"), link("a", "b/b")))

	a, b := newPackage("a/a"), newPackage("b/b")
	a.SetAutoload(arr("classmap", list("src/")))
	b.SetAutoload(arr("classmap", list("src/", "lib/")))
	e.packages(a, b)

	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.vendorDir + "/a/a/src")
	e.mkdir(e.vendorDir + "/b/b/src")
	e.mkdir(e.vendorDir + "/b/b/lib")
	e.write(e.vendorDir+"/a/a/src/a.php", `<?php class ClassMapFoo {}`)
	e.write(e.vendorDir+"/b/b/src/b.php", `<?php class ClassMapBar {}`)
	e.write(e.vendorDir+"/b/b/lib/c.php", `<?php class ClassMapBaz {}`)

	e.dump(p, false, "_6")
	assertEntries(t, include(t, e.vendorDir+"/composer/autoload_classmap.php"), [][2]string{
		{"ClassMapBar", e.vendorDir + "/b/b/src/b.php"},
		{"ClassMapBaz", e.vendorDir + "/b/b/lib/c.php"},
		{"ClassMapFoo", e.vendorDir + "/a/a/src/a.php"},
		{`Composer\InstalledVersions`, e.vendorDir + "/composer/InstalledVersions.php"},
	})
	e.assertAutoloadFiles("classmap4", e.vendorDir+"/composer", "classmap")
}

func TestAutoloadGenerator_VendorsClassMapAutoloadingWithTargetDir(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a"), link("a", "b/b")))

	a, b := newPackage("a/a"), newPackage("b/b")
	a.SetAutoload(arr("classmap", list("target/src/", "lib/")))
	a.SetTargetDir(pkg.Str("target"))
	b.SetAutoload(arr("classmap", list("src/")))
	e.packages(a, b)

	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.vendorDir + "/a/a/target/src")
	e.mkdir(e.vendorDir + "/a/a/target/lib")
	e.mkdir(e.vendorDir + "/b/b/src")
	e.write(e.vendorDir+"/a/a/target/src/a.php", `<?php class ClassMapFoo {}`)
	e.write(e.vendorDir+"/a/a/target/lib/b.php", `<?php class ClassMapBar {}`)
	e.write(e.vendorDir+"/b/b/src/c.php", `<?php class ClassMapBaz {}`)

	e.dump(p, false, "_6")
	assertEntries(t, include(t, e.vendorDir+"/composer/autoload_classmap.php"), [][2]string{
		{"ClassMapBar", e.vendorDir + "/a/a/target/lib/b.php"},
		{"ClassMapBaz", e.vendorDir + "/b/b/src/c.php"},
		{"ClassMapFoo", e.vendorDir + "/a/a/target/src/a.php"},
		{`Composer\InstalledVersions`, e.vendorDir + "/composer/InstalledVersions.php"},
	})
}

func TestAutoloadGenerator_ClassMapAutoloadingEmptyDirAndExactFile(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a"), link("a", "b/b"), link("a", "c/c")))

	a, b, c := newPackage("a/a"), newPackage("b/b"), newPackage("c/c")
	a.SetAutoload(arr("classmap", list("")))
	b.SetAutoload(arr("classmap", list("test.php")))
	c.SetAutoload(arr("classmap", list("./")))
	e.packages(a, b, c)

	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.vendorDir + "/a/a/src")
	e.mkdir(e.vendorDir + "/b/b")
	e.mkdir(e.vendorDir + "/c/c/foo")
	e.write(e.vendorDir+"/a/a/src/a.php", `<?php class ClassMapFoo {}`)
	e.write(e.vendorDir+"/b/b/test.php", `<?php class ClassMapBar {}`)
	e.write(e.vendorDir+"/c/c/foo/test.php", `<?php class ClassMapBaz {}`)

	e.dump(p, false, "_7")
	assertEntries(t, include(t, e.vendorDir+"/composer/autoload_classmap.php"), [][2]string{
		{"ClassMapBar", e.vendorDir + "/b/b/test.php"},
		{"ClassMapBaz", e.vendorDir + "/c/c/foo/test.php"},
		{"ClassMapFoo", e.vendorDir + "/a/a/src/a.php"},
		{`Composer\InstalledVersions`, e.vendorDir + "/composer/InstalledVersions.php"},
	})
	e.assertAutoloadFiles("classmap5", e.vendorDir+"/composer", "classmap")
	real := e.vendorDir + "/composer/autoload_real.php"
	if e.fileContains(real, "$loader->setClassMapAuthoritative(true);") || e.fileContains(real, "$loader->setApcuPrefix(") {
		t.Error("autoload_real.php sets authoritative or apcu")
	}
}

func authoritativeApcuPackages(e *env) *pkg.RootPackage {
	p := newRoot("root/a")
	p.SetRequires(links(link("a", "a/a"), link("a", "b/b"), link("a", "c/c")))

	a, b, c := newPackage("a/a"), newPackage("b/b"), newPackage("c/c")
	a.SetAutoload(arr("psr-4", arr("", "src/")))
	b.SetAutoload(arr("psr-4", arr("", "./")))
	c.SetAutoload(arr("psr-4", arr("", "foo/")))
	e.packages(a, b, c)

	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.vendorDir + "/a/a/src")
	e.mkdir(e.vendorDir + "/b/b")
	e.mkdir(e.vendorDir + "/c/c/foo")
	e.write(e.vendorDir+"/a/a/src/ClassMapFoo.php", `<?php class ClassMapFoo {}`)
	e.write(e.vendorDir+"/b/b/ClassMapBar.php", `<?php class ClassMapBar {}`)
	e.write(e.vendorDir+"/c/c/foo/ClassMapBaz.php", `<?php class ClassMapBaz {}`)

	return p
}

func TestAutoloadGenerator_ClassMapAutoloadingAuthoritativeAndApcu(t *testing.T) {
	e := setUp(t)
	p := authoritativeApcuPackages(e)

	e.generator.SetClassMapAuthoritative(true)
	e.generator.SetApcu(true, nil)
	e.dump(p, false, "_7")

	assertEntries(t, include(t, e.vendorDir+"/composer/autoload_classmap.php"), [][2]string{
		{"ClassMapBar", e.vendorDir + "/b/b/ClassMapBar.php"},
		{"ClassMapBaz", e.vendorDir + "/c/c/foo/ClassMapBaz.php"},
		{"ClassMapFoo", e.vendorDir + "/a/a/src/ClassMapFoo.php"},
		{`Composer\InstalledVersions`, e.vendorDir + "/composer/InstalledVersions.php"},
	})
	e.assertAutoloadFiles("classmap8", e.vendorDir+"/composer", "classmap")

	real := e.vendorDir + "/composer/autoload_real.php"
	if !e.fileContains(real, "$loader->setClassMapAuthoritative(true);") || !e.fileContains(real, "$loader->setApcuPrefix(") {
		t.Error("autoload_real.php does not set authoritative and apcu")
	}
}

func TestAutoloadGenerator_ClassMapAutoloadingAuthoritativeAndApcuPrefix(t *testing.T) {
	e := setUp(t)
	p := authoritativeApcuPackages(e)

	prefix := `custom'Prefix`
	e.generator.SetClassMapAuthoritative(true)
	e.generator.SetApcu(true, &prefix)
	e.dump(p, false, "_7")

	assertEntries(t, include(t, e.vendorDir+"/composer/autoload_classmap.php"), [][2]string{
		{"ClassMapBar", e.vendorDir + "/b/b/ClassMapBar.php"},
		{"ClassMapBaz", e.vendorDir + "/c/c/foo/ClassMapBaz.php"},
		{"ClassMapFoo", e.vendorDir + "/a/a/src/ClassMapFoo.php"},
		{`Composer\InstalledVersions`, e.vendorDir + "/composer/InstalledVersions.php"},
	})
	e.assertAutoloadFiles("classmap8", e.vendorDir+"/composer", "classmap")

	real := e.vendorDir + "/composer/autoload_real.php"
	if !e.fileContains(real, "$loader->setClassMapAuthoritative(true);") || !e.fileContains(real, `$loader->setApcuPrefix('custom\'Prefix');`) {
		t.Error("autoload_real.php does not set authoritative and the apcu prefix")
	}
}

func filesAutoloadPackages(e *env) (*pkg.RootPackage, []pkg.PackageInterface) {
	p := newRoot("root/a")
	p.SetAutoload(arr("files", list("root.php")))
	p.SetRequires(links(link("a", "a/a"), link("a", "b/b"), link("a", "c/c")))

	a, b, c := newPackage("a/a"), newPackage("b/b"), newPackage("c/c")
	a.SetAutoload(arr("files", list("test.php")))
	b.SetAutoload(arr("files", list("test2.php")))
	c.SetAutoload(arr("files", list("test3.php", "foo/bar/test4.php")))
	c.SetTargetDir(pkg.Str("foo/bar"))

	e.mkdir(e.vendorDir + "/a/a")
	e.mkdir(e.vendorDir + "/b/b")
	e.mkdir(e.vendorDir + "/c/c/foo/bar")
	e.write(e.vendorDir+"/a/a/test.php", `<?php function testFilesAutoloadGeneration1() {}`)
	e.write(e.vendorDir+"/b/b/test2.php", `<?php function testFilesAutoloadGeneration2() {}`)
	e.write(e.vendorDir+"/c/c/foo/bar/test3.php", `<?php function testFilesAutoloadGeneration3() {}`)
	e.write(e.vendorDir+"/c/c/foo/bar/test4.php", `<?php function testFilesAutoloadGeneration4() {}`)
	e.write(e.workingDir+"/root.php", `<?php function testFilesAutoloadGenerationRoot() {}`)

	return p, []pkg.PackageInterface{a, b, c}
}

func TestAutoloadGenerator_FilesAutoloadGeneration(t *testing.T) {
	e := setUp(t)
	p, packages := filesAutoloadPackages(e)
	e.packages(packages...)

	e.dump(p, false, "FilesAutoload")
	e.assertFileContentEquals("autoload_functions.php", e.vendorDir+"/autoload.php")
	e.assertFileContentEquals("autoload_real_functions.php", e.vendorDir+"/composer/autoload_real.php")
	e.assertFileContentEquals("autoload_static_functions.php", e.vendorDir+"/composer/autoload_static.php")
	e.assertFileContentEquals("autoload_files_functions.php", e.vendorDir+"/composer/autoload_files.php")

	out := runPHP(t, `$loader = require '`+e.vendorDir+`/autoload.php'; $loader->unregister();
foreach (['testFilesAutoloadGeneration1', 'testFilesAutoloadGeneration2', 'testFilesAutoloadGeneration3', 'testFilesAutoloadGeneration4', 'testFilesAutoloadGenerationRoot'] as $f) { echo function_exists($f) ? 1 : 0; }`)
	if out != "11111" {
		t.Errorf("functions loaded: %q", out)
	}
}

func TestAutoloadGenerator_FilesAutoloadGenerationRemoveExtraEntitiesFromAutoloadFiles(t *testing.T) {
	e := setUp(t)
	autoloadPackage, autoloadPackages := filesAutoloadPackages(e)
	autoloadPackage.SetIncludePaths(list("/lib", "/src"))
	autoloadPackages[0].(*pkg.Package).SetIncludePaths(list("lib1", "src1"))
	autoloadPackages[1].(*pkg.Package).SetIncludePaths(list("lib2"))
	autoloadPackages[2].(*pkg.Package).SetIncludePaths(list("lib3"))

	notAutoloadPackage := newRoot("root/a")
	notAutoloadPackage.SetRequires(autoloadPackage.Requires())
	notAutoloadPackages := []pkg.PackageInterface{newPackage("a/a"), newPackage("b/b"), newPackage("c/c")}

	e.packages(autoloadPackages...)
	e.packages(notAutoloadPackages...)
	e.packages(notAutoloadPackages...)

	e.dump(autoloadPackage, false, "FilesAutoload")
	e.assertFileContentEquals("autoload_functions.php", e.vendorDir+"/autoload.php")
	e.assertFileContentEquals("autoload_real_functions_with_include_paths.php", e.vendorDir+"/composer/autoload_real.php")
	e.assertFileContentEquals("autoload_static_functions_with_include_paths.php", e.vendorDir+"/composer/autoload_static.php")
	e.assertFileContentEquals("autoload_files_functions.php", e.vendorDir+"/composer/autoload_files.php")
	e.assertFileContentEquals("include_paths_functions.php", e.vendorDir+"/composer/include_paths.php")

	e.dump(autoloadPackage, false, "FilesAutoload")
	e.assertFileContentEquals("autoload_functions.php", e.vendorDir+"/autoload.php")
	e.assertFileContentEquals("autoload_real_functions_with_include_paths.php", e.vendorDir+"/composer/autoload_real.php")
	e.assertFileContentEquals("autoload_files_functions_with_removed_extra.php", e.vendorDir+"/composer/autoload_files.php")
	e.assertFileContentEquals("include_paths_functions_with_removed_extra.php", e.vendorDir+"/composer/include_paths.php")

	e.dump(notAutoloadPackage, false, "FilesAutoload")
	e.assertFileContentEquals("autoload_functions.php", e.vendorDir+"/autoload.php")
	e.assertFileContentEquals("autoload_real_functions_with_removed_include_paths_and_autolad_files.php", e.vendorDir+"/composer/autoload_real.php")
	e.assertFileContentEquals("autoload_static_functions_with_removed_include_paths_and_autolad_files.php", e.vendorDir+"/composer/autoload_static.php")
	assertNotExists(t, e.vendorDir+"/composer/autoload_files.php")
	assertNotExists(t, e.vendorDir+"/composer/include_paths.php")
}

func TestAutoloadGenerator_FilesAutoloadOrderByDependencies(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr("files", list("root2.php")))
	p.SetRequires(links(link("a", "z/foo"), link("a", "b/bar"), link("a", "d/d"), link("a", "e/e")))

	z, b, d, c, f := newPackage("z/foo"), newPackage("b/bar"), newPackage("d/d"), newPackage("c/lorem"), newPackage("e/e")

	// expected order:
	// c requires nothing
	// d requires c
	// b requires c & d
	// e requires c
	// z requires c
	// (b, e, z ordered alphabetically)

	z.SetAutoload(arr("files", list("testA.php")))
	z.SetRequires(links(link("z/foo", "c/lorem")))
	b.SetAutoload(arr("files", list("testB.php")))
	b.SetRequires(links(link("b/bar", "c/lorem"), link("b/bar", "d/d")))
	c.SetAutoload(arr("files", list("testC.php")))
	d.SetAutoload(arr("files", list("testD.php")))
	d.SetRequires(links(link("d/d", "c/lorem")))
	f.SetAutoload(arr("files", list("testE.php")))
	f.SetRequires(links(link("e/e", "c/lorem")))
	e.packages(z, b, d, c, f)

	for _, n := range []string{"z/foo", "b/bar", "c/lorem", "d/d", "e/e"} {
		e.mkdir(e.vendorDir + "/" + n)
	}
	e.write(e.vendorDir+"/z/foo/testA.php", `<?php function testFilesAutoloadOrderByDependency1() {}`)
	e.write(e.vendorDir+"/b/bar/testB.php", `<?php function testFilesAutoloadOrderByDependency2() {}`)
	e.write(e.vendorDir+"/c/lorem/testC.php", `<?php function testFilesAutoloadOrderByDependency3() {}`)
	e.write(e.vendorDir+"/d/d/testD.php", `<?php function testFilesAutoloadOrderByDependency4() {}`)
	e.write(e.vendorDir+"/e/e/testE.php", `<?php function testFilesAutoloadOrderByDependency5() {}`)
	e.write(e.workingDir+"/root2.php", `<?php function testFilesAutoloadOrderByDependencyRoot() {}`)

	e.dump(p, false, "FilesAutoloadOrder")
	e.assertFileContentEquals("autoload_functions_by_dependency.php", e.vendorDir+"/autoload.php")
	e.assertFileContentEquals("autoload_real_files_by_dependency.php", e.vendorDir+"/composer/autoload_real.php")
	e.assertFileContentEquals("autoload_static_files_by_dependency.php", e.vendorDir+"/composer/autoload_static.php")

	out := runPHP(t, `$loader = require '`+e.vendorDir+`/autoload.php'; $loader->unregister();
foreach ([1, 2, 3, 4, 5, 'Root'] as $f) { echo function_exists('testFilesAutoloadOrderByDependency'.$f) ? 1 : 0; }`)
	if out != "111111" {
		t.Errorf("functions loaded: %q", out)
	}
}

// Test that PSR-0 and PSR-4 mappings are processed in the correct order
// for autoloading and for classmap generation:
// - The main package has priority over other packages.
// - Longer namespaces have priority over shorter namespaces.
func TestAutoloadGenerator_OverrideVendorsAutoloading(t *testing.T) {
	e := setUp(t)
	root := newRoot("root/z")
	root.SetAutoload(arr(
		"psr-0", arr(`A\B`, e.workingDir+"/lib"),
		"classmap", list(e.workingDir+"/src"),
	))
	root.SetRequires(links(link("z", "a/a"), link("z", "b/b")))

	a, b := newPackage("a/a"), newPackage("b/b")
	a.SetAutoload(arr(
		"psr-0", arr("A", "src/", `A\B`, "lib/"),
		"classmap", list("classmap"),
	))
	b.SetAutoload(arr("psr-0", arr(`B\Sub\Name`, "src/")))
	e.packages(a, b)

	e.mkdir(e.workingDir + "/lib/A/B")
	e.mkdir(e.workingDir + "/src/")
	e.mkdir(e.vendorDir + "/composer")
	e.mkdir(e.vendorDir + "/a/a/classmap")
	e.mkdir(e.vendorDir + "/a/a/src")
	e.mkdir(e.vendorDir + "/a/a/lib/A/B")
	e.mkdir(e.vendorDir + "/b/b/src")

	// Define the classes A\B\C and Foo\Bar in the main package.
	e.write(e.workingDir+"/lib/A/B/C.php", `<?php namespace A\B; class C {}`)
	e.write(e.workingDir+"/src/classes.php", `<?php namespace Foo; class Bar {}`)

	// Define the same two classes in the package a/a.
	e.write(e.vendorDir+"/a/a/lib/A/B/C.php", `<?php namespace A\B; class C {}`)
	e.write(e.vendorDir+"/a/a/classmap/classes.php", `<?php namespace Foo; class Bar {}`)

	e.dump(root, true, "_9")
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_namespaces.php", `<?php

// autoload_namespaces.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
    'B\\Sub\\Name' => array($vendorDir . '/b/b/src'),
    'A\\B' => array($baseDir . '/lib', $vendorDir . '/a/a/lib'),
    'A' => array($vendorDir . '/a/a/src'),
);
`)
	// autoload_psr4.php is expected to be empty in this example.
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_psr4.php", `<?php

// autoload_psr4.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
);
`)
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_classmap.php", `<?php

// autoload_classmap.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
    'A\\B\\C' => $baseDir . '/lib/A/B/C.php',
    'Composer\\InstalledVersions' => $vendorDir . '/composer/InstalledVersions.php',
    'Foo\\Bar' => $baseDir . '/src/classes.php',
);
`)
}

func TestAutoloadGenerator_IncludePathFileGeneration(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	a, b, c := newPackage("a/a"), newPackage("b/b"), newPackage("c")
	a.SetIncludePaths(list("lib/"))
	b.SetIncludePaths(list("library"))
	c.SetIncludePaths(list("library"))
	e.packages(a, b, c)

	e.mkdir(e.vendorDir + "/composer")

	e.dump(p, false, "_10")

	e.assertFileContentEquals("include_paths.php", e.vendorDir+"/composer/include_paths.php")
	assertEntries(t, include(t, e.vendorDir+"/composer/include_paths.php"), [][2]string{
		{"", e.vendorDir + "/a/a/lib"},
		{"", e.vendorDir + "/b/b/library"},
		{"", e.vendorDir + "/c/library"},
	})
}

func TestAutoloadGenerator_IncludePathsArePrependedInAutoloadFile(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	a := newPackage("a/a")
	a.SetIncludePaths(list("lib/"))
	e.packages(a)

	e.mkdir(e.vendorDir + "/composer")

	e.dump(p, false, "_11")

	out := runPHP(t, `$old = get_include_path(); $loader = require '`+e.vendorDir+`/autoload.php'; $loader->unregister();
echo get_include_path() === '`+e.vendorDir+`/a/a/lib'.PATH_SEPARATOR.$old ? 'ok' : get_include_path();`)
	if out != "ok" {
		t.Errorf("include path: %q", out)
	}
}

func TestAutoloadGenerator_IncludePathsInRootPackage(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetIncludePaths(list("/lib", "/src"))
	a := newPackage("a/a")
	a.SetIncludePaths(list("lib/"))
	e.packages(a)

	e.mkdir(e.vendorDir + "/composer")

	e.dump(p, false, "_12")

	out := runPHP(t, `$old = get_include_path(); $loader = require '`+e.vendorDir+`/autoload.php'; $loader->unregister();
echo get_include_path() === '`+e.workingDir+`/lib'.PATH_SEPARATOR.'`+e.workingDir+`/src'.PATH_SEPARATOR.'`+e.vendorDir+`/a/a/lib'.PATH_SEPARATOR.$old ? 'ok' : get_include_path();`)
	if out != "ok" {
		t.Errorf("include path: %q", out)
	}
}

func TestAutoloadGenerator_IncludePathFileWithoutPathsIsSkipped(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	e.packages(newPackage("a/a"))

	e.mkdir(e.vendorDir + "/composer")

	e.dump(p, false, "_12")

	assertNotExists(t, e.vendorDir+"/composer/include_paths.php")
}

func TestAutoloadGenerator_PreAndPostEventsAreDispatchedDuringAutoloadDump(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr("psr-0", arr("Prefix", "foo/bar/non/existing/")))
	e.packages()

	e.generator.SetRunScripts(true)
	e.dump(p, true, "_8")

	want := []dispatchedScript{{PreAutoloadDump, false}, {PostAutoloadDump, false}}
	if len(e.dispatcher.calls) != len(want) || e.dispatcher.calls[0] != want[0] || e.dispatcher.calls[1] != want[1] {
		t.Errorf("dispatched %v, want %v", e.dispatcher.calls, want)
	}
}

func TestAutoloadGenerator_UseGlobalIncludePath(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr("psr-0", arr(`Main\Foo`, "", `Main\Bar`, "")))
	p.SetTargetDir(pkg.Str("Main/Foo/"))
	e.packages()

	e.config["use-include-path"] = true

	e.mkdir(e.vendorDir + "/a")

	e.dump(p, false, "IncludePath")
	e.assertFileContentEquals("autoload_real_include_path.php", e.vendorDir+"/composer/autoload_real.php")
	e.assertFileContentEquals("autoload_static_include_path.php", e.vendorDir+"/composer/autoload_static.php")
}

func TestAutoloadGenerator_VendorDirExcludedFromWorkingDir(t *testing.T) {
	e := setUp(t)
	workingDir := e.vendorDir + "/working-dir"
	vendorDir := workingDir + "/../vendor"

	e.mkdir(workingDir)
	t.Chdir(workingDir)

	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr("Foo", "src"),
		"psr-4", arr(`Acme\Foo\`, "src-psr4"),
		"classmap", list("classmap"),
		"files", list("test.php"),
	))
	p.SetRequires(links(link("a", "b/b")))

	vendorPackage := newPackage("b/b")
	vendorPackage.SetAutoload(arr(
		"psr-0", arr("Bar", "lib"),
		"psr-4", arr(`Acme\Bar\`, "lib-psr4"),
		"classmap", list("classmaps"),
		"files", list("bootstrap.php"),
	))
	e.packages(vendorPackage)

	e.mkdir(workingDir + "/src/Foo")
	e.mkdir(workingDir + "/classmap")
	e.mkdir(vendorDir + "/composer")
	e.mkdir(vendorDir + "/b/b/lib/Bar")
	e.mkdir(vendorDir + "/b/b/classmaps")
	e.write(workingDir+"/src/Foo/Bar.php", `<?php namespace Foo; class Bar {}`)
	e.write(workingDir+"/classmap/classes.php", `<?php namespace Foo; class Foo {}`)
	e.write(workingDir+"/test.php", `<?php class Foo {}`)
	e.write(vendorDir+"/b/b/lib/Bar/Foo.php", `<?php namespace Bar; class Foo {}`)
	e.write(vendorDir+"/b/b/classmaps/classes.php", `<?php namespace Bar; class Bar {}`)
	e.write(vendorDir+"/b/b/bootstrap.php", `<?php class Bar {}`)

	e.vendorDir = vendorDir
	e.dump(p, true, "_13")

	e.assertStringEqualsFile(vendorDir+"/composer/autoload_namespaces.php", `<?php

// autoload_namespaces.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir).'/working-dir';

return array(
    'Foo' => array($baseDir . '/src'),
    'Bar' => array($vendorDir . '/b/b/lib'),
);
`)
	e.assertStringEqualsFile(vendorDir+"/composer/autoload_psr4.php", `<?php

// autoload_psr4.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir).'/working-dir';

return array(
    'Acme\\Foo\\' => array($baseDir . '/src-psr4'),
    'Acme\\Bar\\' => array($vendorDir . '/b/b/lib-psr4'),
);
`)
	e.assertStringEqualsFile(vendorDir+"/composer/autoload_classmap.php", `<?php

// autoload_classmap.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir).'/working-dir';

return array(
    'Bar\\Bar' => $vendorDir . '/b/b/classmaps/classes.php',
    'Bar\\Foo' => $vendorDir . '/b/b/lib/Bar/Foo.php',
    'Composer\\InstalledVersions' => $vendorDir . '/composer/InstalledVersions.php',
    'Foo\\Bar' => $baseDir . '/src/Foo/Bar.php',
    'Foo\\Foo' => $baseDir . '/classmap/classes.php',
);
`)
	files := vendorDir + "/composer/autoload_files.php"
	if !e.fileContains(files, "$vendorDir . '/b/b/bootstrap.php',\n") || !e.fileContains(files, "$baseDir . '/test.php',\n") {
		t.Error("autoload_files.php lacks bootstrap.php or test.php")
	}
}

func TestAutoloadGenerator_UpLevelRelativePaths(t *testing.T) {
	e := setUp(t)
	workingDir := e.workingDir + "/working-dir"
	e.mkdir(workingDir)
	t.Chdir(workingDir)

	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr("Foo", "../path/../src"),
		"psr-4", arr(`Acme\Foo\`, "../path/../src-psr4"),
		"classmap", list("../classmap", "../classmap2/subdir", "classmap3", "classmap4"),
		"files", list("../test.php"),
		"exclude-from-classmap", list("./../classmap/excluded", "../classmap2", "classmap3/classes.php", "classmap4/*/classes.php"),
	))
	e.packages()

	e.mkdir(e.workingDir + "/src/Foo")
	e.mkdir(e.workingDir + "/classmap/excluded")
	e.mkdir(e.workingDir + "/classmap2/subdir")
	e.mkdir(e.workingDir + "/working-dir/classmap3")
	e.mkdir(e.workingDir + "/working-dir/classmap4/foo/")
	e.write(e.workingDir+"/src/Foo/Bar.php", `<?php namespace Foo; class Bar {}`)
	e.write(e.workingDir+"/classmap/classes.php", `<?php namespace Foo; class Foo {}`)
	e.write(e.workingDir+"/classmap/excluded/classes.php", `<?php namespace Foo; class Boo {}`)
	e.write(e.workingDir+"/classmap2/subdir/classes.php", `<?php namespace Foo; class Boo2 {}`)
	e.write(e.workingDir+"/working-dir/classmap3/classes.php", `<?php namespace Foo; class Boo3 {}`)
	e.write(e.workingDir+"/working-dir/classmap4/foo/classes.php", `<?php namespace Foo; class Boo4 {}`)
	e.write(e.workingDir+"/test.php", `<?php class Foo {}`)

	e.dump(p, true, "_14")

	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_namespaces.php", `<?php

// autoload_namespaces.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir).'/working-dir';

return array(
    'Foo' => array($baseDir . '/../src'),
);
`)
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_psr4.php", `<?php

// autoload_psr4.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir).'/working-dir';

return array(
    'Acme\\Foo\\' => array($baseDir . '/../src-psr4'),
);
`)
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_classmap.php", `<?php

// autoload_classmap.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir).'/working-dir';

return array(
    'Composer\\InstalledVersions' => $vendorDir . '/composer/InstalledVersions.php',
    'Foo\\Bar' => $baseDir . '/../src/Foo/Bar.php',
    'Foo\\Foo' => $baseDir . '/../classmap/classes.php',
);
`)
	if !e.fileContains(e.vendorDir+"/composer/autoload_files.php", "$baseDir . '/../test.php',\n") {
		t.Error("autoload_files.php lacks ../test.php")
	}
}

func TestAutoloadGenerator_AutoloadRulesInPackageThatDoesNotExistOnDisk(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetRequires(links(pkg.NewLink("root/a", "dep/a", semver.NewMatchAllConstraint(), "requires", pkg.NullString{})))
	dep := pkg.NewCompletePackage("dep/a", "1.0", "1.0")
	e.packages(dep)

	dep.SetAutoload(arr("psr-0", arr("Foo", "./src")))
	e.dump(p, true, "_19")
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_namespaces.php", `<?php

// autoload_namespaces.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
    'Foo' => array($vendorDir . '/dep/a/src'),
);
`)

	dep.SetAutoload(arr("psr-4", arr(`Acme\Foo\`, "./src-psr4")))
	e.dump(p, true, "_19")
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_psr4.php", `<?php

// autoload_psr4.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
    'Acme\\Foo\\' => array($vendorDir . '/dep/a/src-psr4'),
);
`)

	dep.SetAutoload(arr("classmap", list("classmap")))
	_, err := e.generator.Dump(e.config, e.repo, p, e.im, "composer", true, "_19", nil, false)
	want := `Could not scan for classes inside "` + e.vendorDir + `/dep/a/classmap" which does not appear to be a file nor a folder`
	if err == nil || err.Error() != want || !isRuntimeException(err) {
		t.Errorf("error %v, want RuntimeException %q", err, want)
	}

	dep.SetAutoload(arr("files", list("./test.php")))
	e.dump(p, true, "_19")
	if !e.fileContains(e.vendorDir+"/composer/autoload_files.php", "$vendorDir . '/dep/a/test.php',\n") {
		t.Error("autoload_files.php lacks dep/a/test.php")
	}

	p.SetAutoload(arr("exclude-from-classmap", list("../excludedroot", "root/excl")))
	dep.SetAutoload(arr("exclude-from-classmap", list("../../excluded", "foo/bar")))
	packageMap, err := e.generator.BuildPackageMap(e.im, p, []pkg.PackageInterface{dep})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := e.generator.ParseAutoloads(packageMap, p, NoDevFilter)
	if err != nil {
		t.Fatal(err)
	}
	want1 := php.PregQuote(filepath.Dir(e.workingDir), "") + "/excludedroot($|/)"
	want2 := php.PregQuote(e.workingDir, "") + "/root/excl($|/)"
	if len(parsed.ExcludeFromClassmap) != 2 || parsed.ExcludeFromClassmap[0] != want1 || parsed.ExcludeFromClassmap[1] != want2 {
		t.Errorf("exclude-from-classmap %q, want [%q %q]", parsed.ExcludeFromClassmap, want1, want2)
	}
}

func TestAutoloadGenerator_EmptyPaths(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr("Foo", ""),
		"psr-4", arr(`Acme\Foo\`, ""),
		"classmap", list(""),
	))
	e.packages()

	e.mkdir(e.workingDir + "/Foo")
	e.write(e.workingDir+"/Foo/Bar.php", `<?php namespace Foo; class Bar {}`)
	e.write(e.workingDir+"/class.php", `<?php namespace Classmap; class Foo {}`)

	e.dump(p, true, "_15")

	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_namespaces.php", `<?php

// autoload_namespaces.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
    'Foo' => array($baseDir . '/'),
);
`)
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_psr4.php", `<?php

// autoload_psr4.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
    'Acme\\Foo\\' => array($baseDir . '/'),
);
`)
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_classmap.php", `<?php

// autoload_classmap.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
    'Classmap\\Foo' => $baseDir . '/class.php',
    'Composer\\InstalledVersions' => $vendorDir . '/composer/InstalledVersions.php',
    'Foo\\Bar' => $baseDir . '/Foo/Bar.php',
);
`)
}

func TestAutoloadGenerator_VendorSubstringPath(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr("Foo", "composer-test-autoload-src/src"),
		"psr-4", arr(`Acme\Foo\`, "composer-test-autoload-src/src-psr4"),
	))
	e.packages()

	e.mkdir(e.vendorDir + "/a")

	e.dump(p, false, "VendorSubstring")
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_namespaces.php", `<?php

// autoload_namespaces.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
    'Foo' => array($baseDir . '/composer-test-autoload-src/src'),
);
`)
	e.assertStringEqualsFile(e.vendorDir+"/composer/autoload_psr4.php", `<?php

// autoload_psr4.php @generated by Composer

$vendorDir = dirname(__DIR__);
$baseDir = dirname($vendorDir);

return array(
    'Acme\\Foo\\' => array($baseDir . '/composer-test-autoload-src/src-psr4'),
);
`)
}

func TestAutoloadGenerator_ExcludeFromClassmap(t *testing.T) {
	e := setUp(t)
	p := newRoot("root/a")
	p.SetAutoload(arr(
		"psr-0", arr("Main", "src/", "Lala", list("src/", "lib/")),
		"psr-4", arr(`Acme\Fruit\`, "src-fruit/", `Acme\Cake\`, list("src-cake/", "lib-cake/")),
		"classmap", list("composersrc/"),
		"exclude-from-classmap", list(
			"/composersrc/foo/bar/",
			"/composersrc/excludedTests/",
			"/composersrc/ClassToExclude.php",
			"/composersrc/*/excluded/excsubpath",
			"**/excsubpath",
			"composers", // should _not_ cause exclusion of /composersrc/**, as it is equivalent to /composers/**
			"/src-ca/",  // should _not_ cause exclusion of /src-cake/**, as it is equivalent to /src-ca/**
		),
	))
	e.packages()

	e.mkdir(e.workingDir + "/composer")
	e.mkdir(e.workingDir + "/src/Lala/Test")
	e.mkdir(e.workingDir + "/lib")
	e.write(e.workingDir+"/src/Lala/ClassMapMain.php", `<?php namespace Lala; class ClassMapMain {}`)
	e.write(e.workingDir+"/src/Lala/Test/ClassMapMainTest.php", `<?php namespace Lala\Test; class ClassMapMainTest {}`)
	e.mkdir(e.workingDir + "/src-fruit")
	e.mkdir(e.workingDir + "/src-cake")
	e.mkdir(e.workingDir + "/lib-cake")
	e.write(e.workingDir+"/src-cake/ClassMapBar.php", `<?php namespace Acme\Cake; class ClassMapBar {}`)
	e.mkdir(e.workingDir + "/composersrc")
	e.mkdir(e.workingDir + "/composersrc/tests")
	e.write(e.workingDir+"/composersrc/foo.php", `<?php class ClassMapFoo {}`)

	// these classes should not be found in the classmap
	e.mkdir(e.workingDir + "/composersrc/excludedTests")
	e.write(e.workingDir+"/composersrc/excludedTests/bar.php", `<?php class ClassExcludeMapFoo {}`)
	e.write(e.workingDir+"/composersrc/ClassToExclude.php", `<?php class ClassClassToExclude {}`)
	e.mkdir(e.workingDir + "/composersrc/long/excluded/excsubpath")
	e.write(e.workingDir+"/composersrc/long/excluded/excsubpath/foo.php", `<?php class ClassExcludeMapFoo2 {}`)
	e.write(e.workingDir+"/composersrc/long/excluded/excsubpath/bar.php", `<?php class ClassExcludeMapBar {}`)

	// symlink directory in project directory in classmap
	e.mkdir(e.workingDir + "/forks/bar/src/exclude")
	e.mkdir(e.workingDir + "/composersrc/foo")
	e.write(e.workingDir+"/forks/bar/src/exclude/FooExclClass.php", `<?php class FooExclClass {};`)
	if err := os.Symlink(e.workingDir+"/forks/bar/", e.workingDir+"/composersrc/foo/bar"); err != nil {
		t.Fatal(err)
	}

	e.dump(p, true, "_1")

	e.assertAutoloadFiles("classmap", e.vendorDir+"/composer", "classmap")
}

// platformCheckFilter is PlatformRequirementFilterFactory::fromBoolOrList.
type ignoreAllFilter struct{}

func (ignoreAllFilter) IsIgnored(string) bool           { return true }
func (ignoreAllFilter) IgnoresAllPlatformRequirements() {}

// ignoreListFilter is IgnoreListPlatformRequirementFilter::isIgnored.
type ignoreListFilter struct{ re *php.Regexp }

func newIgnoreListFilter(reqs []string) ignoreListFilter {
	return ignoreListFilter{php.MustCompile(pkg.PackageNamesToRegexp(reqs, "{^(?:%s)$}iD"))}
}

func (f ignoreListFilter) IsIgnored(req string) bool {
	if !pkg.IsPlatformPackage(req) {
		return false
	}
	ok, _ := f.re.IsMatch(req)

	return ok
}

func TestAutoloadGenerator_GeneratesPlatformCheck(t *testing.T) {
	vp := pkg.NewVersionParser()
	l := func(key, target, constraint string) [2]any {
		c, err := vp.ParseConstraints(constraint)
		if err != nil {
			t.Fatal(err)
		}

		return [2]any{key, pkg.NewLink("a", target, c, "", pkg.NullString{})}
	}
	build := func(entries ...[2]any) pkg.Links {
		var b pkg.LinksBuilder
		for _, e := range entries {
			b.Set(e[0].(string), e[1].(*pkg.Link))
		}

		return b.Build()
	}

	tests := []struct {
		name               string
		requires           pkg.Links
		fixture            string
		provides, replaces pkg.Links
		ignore             any
	}{
		{"Typical project requirements", build(l("php", "php", "^7.2"), l("ext-xml", "ext-xml", "*"), l("ext-json", "ext-json", "*")), "typical", pkg.Links{}, pkg.Links{}, false},
		{"No PHP lower bound", build(l("php", "php", "< 8")), "", pkg.Links{}, pkg.Links{}, false},
		{"No PHP upper bound", build(l("php", "php", ">= 7.2")), "no_php_upper_bound", pkg.Links{}, pkg.Links{}, false},
		{"Specific PHP release version", build(l("php", "php", "^7.2.8")), "specific_php_release", pkg.Links{}, pkg.Links{}, false},
		{"Specific 64-bit PHP version", build(l("php-64bit", "php-64bit", "^7.2.8")), "specific_php_64bit_required", pkg.Links{}, pkg.Links{}, false},
		{"64-bit PHP required", build(l("php-64bit", "php-64bit", "*")), "php_64bit_required", pkg.Links{}, pkg.Links{}, false},
		{"No PHP required", build(l("ext-xml", "ext-xml", "*"), l("ext-json", "ext-json", "*")), "no_php_required", pkg.Links{}, pkg.Links{}, false},
		{"Ignoring all platform requirements skips check completely", build(l("php", "php", "^7.2"), l("ext-xml", "ext-xml", "*"), l("ext-json", "ext-json", "*")), "", pkg.Links{}, pkg.Links{}, true},
		{"Ignored platform requirements are not checked for", build(l("php", "php", "^7.2.8"), l("ext-xml", "ext-xml", "*"), l("ext-json", "ext-json", "*"), l("ext-pdo", "ext-pdo", "*")), "no_php_required", pkg.Links{}, pkg.Links{}, []string{"php", "ext-pdo"}},
		{"Via wildcard ignored platform requirements are not checked for", build(l("php", "php", "^7.2.8"), l("ext-xml", "ext-xml", "*"), l("ext-json", "ext-json", "*"), l("ext-fileinfo", "ext-fileinfo", "*"), l("ext-filesystem", "ext-filesystem", "*"), l("ext-filter", "ext-filter", "*")), "no_php_required", pkg.Links{}, pkg.Links{}, []string{"php", "ext-fil*"}},
		{"No extensions required", build(l("php", "php", "^7.2")), "no_extensions_required", pkg.Links{}, pkg.Links{}, false},
		{
			"Replaced/provided extensions are not checked for + checking case insensitivity",
			build(l("ext-xml", "ext-xml", "^7.2"), l("ext-pdo", "ext-Pdo", "^7.2"), l("ext-bcmath", "ext-bcMath", "^7.2")),
			"replaced_provided_exts",
			// constraint does not satisfy all the ^7.2 requirement so we
			// do not accept it as being replaced; valid replace of bcmath
			// so no need to check for it
			build(l("ext-pdo", "ext-PDO", "7.1.*"), l("ext-bcmath", "ext-BCMath", "^7.1")),
			// valid provide of ext-xml so no need to check for it
			build(l("ext-xml", "ext-XML", "*")),
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setUp(t)
			p := newRoot("root/a")
			p.SetRequires(tt.requires)
			if tt.provides.Len() > 0 {
				p.SetProvides(tt.provides)
			}
			if tt.replaces.Len() > 0 {
				p.SetReplaces(tt.replaces)
			}
			e.packages()

			switch ignore := tt.ignore.(type) {
			case bool:
				if ignore {
					e.generator.SetPlatformRequirementFilter(ignoreAllFilter{})
				}
			case []string:
				e.generator.SetPlatformRequirementFilter(newIgnoreListFilter(ignore))
			}
			e.dump(p, true, "_1")

			real := e.vendorDir + "/composer/autoload_real.php"
			if tt.fixture == "" {
				assertNotExists(t, e.vendorDir+"/composer/platform_check.php")
				if e.fileContains(real, "require __DIR__ . '/platform_check.php';") {
					t.Error("autoload_real.php requires platform_check.php")
				}
			} else {
				e.assertFileContentEquals("platform/"+tt.fixture+".php", e.vendorDir+"/composer/platform_check.php")
				if !e.fileContains(real, "require __DIR__ . '/platform_check.php';") {
					t.Error("autoload_real.php does not require platform_check.php")
				}
			}
		})
	}
}

func TestAutoloadGenerator_AbsoluteSymlinkWithPsr4DoesNotGenerateWarnings(t *testing.T) {
	e := setUp(t)
	p := pkg.NewRootPackage("test/package", "1.0", "1.0")

	// Create a directory structure with PSR-4 autoloading
	e.mkdir(e.workingDir + "/tools-real/vendor")
	e.mkdir(e.workingDir + "/tools-real/vendor/phpunit/phpunit/src/Framework/Exception")
	e.write(e.workingDir+"/tools-real/vendor/phpunit/phpunit/src/Framework/Exception/Exception.php", `<?php namespace PHPUnit\Framework; class Exception extends \Exception {}`)

	// Create an absolute symlink
	if err := os.Symlink(e.workingDir+"/tools-real", e.workingDir+"/tools"); err != nil {
		t.Fatal(err)
	}

	p.SetAutoload(arr(
		"psr-4", arr(`MyTools\`, "tools/"),
		"exclude-from-classmap", list("**/vendor/"),
	))
	e.packages()

	// Capture IO output to check for warnings
	bio := newBufferIO(t)
	e.generator = NewGenerator(e.dispatcher, bio)

	e.dump(p, true, "_9")

	// Should not contain PSR-4 violation warnings
	if strings.Contains(bio.Output(), "does not comply with psr-4 autoloading standard") {
		t.Errorf("output has PSR-4 violations: %s", bio.Output())
	}
}

func TestAutoloadGenerator_AbsoluteSymlinkWithClassmapExcludeFromClassmap(t *testing.T) {
	e := setUp(t)
	p := pkg.NewRootPackage("test/package", "1.0", "1.0")

	// Create a directory structure with files
	e.mkdir(e.workingDir + "/tools-real/vendor/phpunit/phpunit/src/Framework")
	e.write(e.workingDir+"/tools-real/vendor/phpunit/phpunit/src/Framework/Exception.php", `<?php namespace PHPUnit\Framework; class Exception extends \Exception {}`)
	e.write(e.workingDir+"/tools-real/MyClass.php", `<?php class MyClass {}`)

	// Create an absolute symlink
	if err := os.Symlink(e.workingDir+"/tools-real", e.workingDir+"/tools"); err != nil {
		t.Fatal(err)
	}

	p.SetAutoload(arr(
		"classmap", list("tools/"),
		"exclude-from-classmap", list("**/vendor/"),
	))
	e.packages()

	classMap, err := e.generator.Dump(e.config, e.repo, p, e.im, "composer", false, "_9", nil, false)
	if err != nil {
		t.Fatal(err)
	}

	// Check that MyClass is included but vendor files are excluded
	if !classMap.HasClass("MyClass") {
		t.Error("MyClass is not in the class map")
	}
	if classMap.HasClass(`PHPUnit\Framework\Exception`) {
		t.Error(`PHPUnit\Framework\Exception is in the class map`)
	}
}
