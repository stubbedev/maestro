package classmap

// Ports tests/ClassMapGeneratorTest.php and tests/PhpFileParserTest.php.

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testsDir is the realpath of the tests directory (__DIR__ in PHP).
func testsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("testdata/tests")
	if err != nil {
		t.Fatal(err)
	}
	if dir, err = filepath.EvalSymlinks(dir); err != nil {
		t.Fatal(err)
	}

	return dir
}

func ambiguousOf(t *testing.T, cm *ClassMap, filter Matcher) []AmbiguousClass {
	t.Helper()
	a, err := cm.AmbiguousClasses(filter)
	if err != nil {
		t.Fatal(err)
	}

	return a
}

func mapOf(cm *ClassMap) map[string]string {
	return maps.Collect(cm.Map())
}

// assertEqualsNormalized compares class maps regardless of order, with
// backslashes in paths turned into slashes.
func assertEqualsNormalized(t *testing.T, expected, actual map[string]string) {
	t.Helper()
	norm := func(m map[string]string) map[string]string {
		out := make(map[string]string, len(m))
		for k, v := range m {
			out[k] = strings.ReplaceAll(v, `\`, "/")
		}

		return out
	}
	if e, a := norm(expected), norm(actual); !maps.Equal(e, a) {
		t.Errorf("class map differs\n got %v\nwant %v", a, e)
	}
}

func expectException(t *testing.T, err error, class, contains string) {
	t.Helper()
	var e *Exception
	if !errors.As(err, &e) {
		t.Fatalf("expected %s, got %v", class, err)
	}
	if e.Class != class || !strings.Contains(e.Message, contains) {
		t.Fatalf("expected %s containing %q, got %s: %s", class, contains, e.Class, e.Message)
	}
}

func TestClassMapGenerator_CreateMap(t *testing.T) {
	dir := testsDir(t)
	f := dir + "/Fixtures"
	classmap := map[string]string{
		`Foo\Bar\A`:                          f + "/classmap/sameNsMultipleClasses.php",
		`Foo\Bar\B`:                          f + "/classmap/sameNsMultipleClasses.php",
		`Alpha\A`:                            f + "/classmap/multipleNs.php",
		`Alpha\B`:                            f + "/classmap/multipleNs.php",
		`A`:                                  f + "/classmap/multipleNs.php",
		`Be\ta\A`:                            f + "/classmap/multipleNs.php",
		`Be\ta\B`:                            f + "/classmap/multipleNs.php",
		`ClassMap\SomeInterface`:             f + "/classmap/SomeInterface.php",
		`ClassMap\SomeParent`:                f + "/classmap/SomeParent.php",
		`ClassMap\SomeClass`:                 f + "/classmap/SomeClass.php",
		`ClassMap\LongString`:                f + "/classmap/LongString.php",
		`Foo\LargeClass`:                     f + "/classmap/LargeClass.php",
		`Foo\LargeGap`:                       f + "/classmap/LargeGap.php",
		`Foo\MissingSpace`:                   f + "/classmap/MissingSpace.php",
		`Foo\StripNoise`:                     f + "/classmap/StripNoise.php",
		`Foo\First`:                          f + "/classmap/StripNoise.php",
		`Foo\Second`:                         f + "/classmap/StripNoise.php",
		`Foo\Third`:                          f + "/classmap/StripNoise.php",
		`Foo\SlashedA`:                       f + "/classmap/BackslashLineEndingString.php",
		`Foo\SlashedB`:                       f + "/classmap/BackslashLineEndingString.php",
		"Unicode\\↑\\↑":                      f + "/classmap/Unicode.php",
		`ShortOpenTag`:                       f + "/classmap/ShortOpenTag.php",
		`Smarty_Internal_Compile_Block`:      f + "/classmap/InvalidUnicode.php",
		`Smarty_Internal_Compile_Blockclose`: f + "/classmap/InvalidUnicode.php",
		`ShortOpenTagDocblock`:               f + "/classmap/ShortOpenTagDocblock.php",
	}
	tests := []struct {
		directory string
		expected  map[string]string
	}{
		{f + "/Namespaced", map[string]string{
			`Namespaced\Bar`: f + "/Namespaced/Bar.inc",
			`Namespaced\Foo`: f + "/Namespaced/Foo.php",
			`Namespaced\Baz`: f + "/Namespaced/Baz.php",
		}},
		{f + "/beta/NamespaceCollision", map[string]string{
			`NamespaceCollision\A\B\Bar`: f + "/beta/NamespaceCollision/A/B/Bar.php",
			`NamespaceCollision\A\B\Foo`: f + "/beta/NamespaceCollision/A/B/Foo.php",
		}},
		{f + "/Pearlike", map[string]string{
			`Pearlike_Foo`: f + "/Pearlike/Foo.php",
			`Pearlike_Bar`: f + "/Pearlike/Bar.php",
			`Pearlike_Baz`: f + "/Pearlike/Baz.php",
		}},
		{f + "/classmap", classmap},
		{f + "/template", map[string]string{}},
		{f + "/php5.4", map[string]string{
			`TFoo`:        f + "/php5.4/traits.php",
			`CFoo`:        f + "/php5.4/traits.php",
			`Foo\TBar`:    f + "/php5.4/traits.php",
			`Foo\IBar`:    f + "/php5.4/traits.php",
			`Foo\TFooBar`: f + "/php5.4/traits.php",
			`Foo\CBar`:    f + "/php5.4/traits.php",
		}},
		{f + "/php7.0", map[string]string{
			`Dummy\Test\AnonClassHolder`: f + "/php7.0/anonclass.php",
		}},
		// PHP_VERSION_ID >= 80100
		{f + "/php8.1", map[string]string{
			`RolesBasicEnum`:                       f + "/php8.1/enum_basic.php",
			`RolesBackedEnum`:                      f + "/php8.1/enum_backed.php",
			`RolesClassLikeEnum`:                   f + "/php8.1/enum_class_semantics.php",
			`Foo\Bar\RolesClassLikeNamespacedEnum`: f + "/php8.1/enum_namespaced.php",
		}},
		// The hhvm3.3 case only runs on HHVM.
	}
	for _, tt := range tests {
		t.Run(filepath.Base(tt.directory), func(t *testing.T) {
			cm, err := CreateMap(tt.directory)
			if err != nil {
				t.Fatal(err)
			}
			assertEqualsNormalized(t, tt.expected, mapOf(cm))
		})
	}
}

// finderFilesIn is (new Finder())->files()->in($dir) as a list of files.
func finderFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	found, err := finderFiles([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	files := make([]string, len(found))
	for i, f := range found {
		files[i] = f.path
	}

	return files
}

func TestClassMapGenerator_CreateMapFinderSupport(t *testing.T) {
	f := testsDir(t) + "/Fixtures"
	g := NewGenerator(nil)
	if err := g.ScanFiles(finderFilesIn(t, f+"/beta/NamespaceCollision"), nil); err != nil {
		t.Fatal(err)
	}
	assertEqualsNormalized(t, map[string]string{
		`NamespaceCollision\A\B\Bar`: f + "/beta/NamespaceCollision/A/B/Bar.php",
		`NamespaceCollision\A\B\Foo`: f + "/beta/NamespaceCollision/A/B/Foo.php",
	}, mapOf(g.ClassMap()))
}

// TestClassMapGenerator_StreamWrapperSupport: PHP stream wrappers cannot be
// registered from Go, so files behind them cannot be read. What remains is
// recognizing such paths, which are neither made absolute nor realpath'd.
func TestClassMapGenerator_StreamWrapperSupport(t *testing.T) {
	for path, want := range map[string]bool{
		"test://BackslashLineEndingString.php": false, // not registered
		"phar://x.phar/Foo.php":                true,
		"file:///tmp/Foo.php":                  true,
		"compress.zlib://x.php":                true,
		"phar:/x.php":                          false,
		"/phar://x.php":                        false,
	} {
		if got := isStreamWrapperPath(path); got != want {
			t.Errorf("isStreamWrapperPath(%s) = %v", path, got)
		}
	}
}

func uniqueTmpDirectory(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

func TestClassMapGenerator_AmbiguousReference(t *testing.T) {
	tempDir := uniqueTmpDirectory(t)
	writeFile(t, tempDir+"/A.php", "<?php\nclass A {}")
	writeFile(t, tempDir+"/other/A.php", "<?php\nclass A {}")
	possibleAmbiguousPaths := []string{tempDir + "/A.php", tempDir + "/other/A.php"}

	g := NewGenerator([]string{"php", "inc", "hh"})
	if err := g.ScanFiles(finderFilesIn(t, tempDir), nil); err != nil {
		t.Fatal(err)
	}
	ambiguous := ambiguousOf(t, g.ClassMap(), DefaultDuplicatesFilter)
	if len(ambiguous) != 1 || ambiguous[0].Class != "A" || len(ambiguous[0].Paths) != 1 {
		t.Fatalf("ambiguous classes: %v", ambiguous)
	}
	if path := ambiguous[0].Paths[0]; !slices.Contains(possibleAmbiguousPaths, path) {
		t.Errorf("%s not found in expected paths %v", path, possibleAmbiguousPaths)
	}
}

// If one file has a class or interface defined more than once, an
// ambiguous reference warning should not be produced.
func TestClassMapGenerator_UnambiguousReference(t *testing.T) {
	tempDir := uniqueTmpDirectory(t)
	writeFile(t, tempDir+"/src/A.php", "<?php\nclass A {}")
	writeFile(t, tempDir+"/src/B.php", `<?php
                if (true) {
                    interface B {}
                } else {
                    interface B extends Iterator {}
                }
            `)
	for _, keyword := range []string{"test", "fixture", "example"} {
		writeFile(t, tempDir+"/ambiguous/"+keyword+"/A.php", "<?php\nclass A {}")
	}

	// if we scan src first, then test ambiguous refs will be ignored correctly
	g := NewGenerator([]string{"php", "inc", "hh"})
	for _, dir := range []string{"/src", "/ambiguous"} {
		if err := g.ScanPaths(tempDir+dir, nil, Classmap, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	cm := g.ClassMap()
	if n := len(ambiguousOf(t, cm, DefaultDuplicatesFilter)); n != 0 {
		t.Errorf("filtered ambiguous classes: %d", n)
	}
	// but when retrieving without filtering, the ambiguous classes are there
	if all := ambiguousOf(t, cm, nil); len(all) != 1 || len(all[0].Paths) != 3 {
		t.Errorf("ambiguous classes: %v", all)
	}

	// if we scan tests first however, then we always get ambiguous refs as
	// the test one is overriding src
	g = NewGenerator([]string{"php", "inc", "hh"})
	for _, dir := range []string{"/ambiguous", "/src"} {
		if err := g.ScanPaths(tempDir+dir, nil, Classmap, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	cm = g.ClassMap()
	// when retrieving with filtering, only the one from src is seen as ambiguous
	filtered := ambiguousOf(t, cm, DefaultDuplicatesFilter)
	if len(filtered) != 1 || filtered[0].Class != "A" || !slices.Equal(filtered[0].Paths, []string{tempDir + "/src/A.php"}) {
		t.Errorf("filtered ambiguous classes: %v", filtered)
	}
	// when retrieving without filtering, all the ambiguous classes are there
	if all := ambiguousOf(t, cm, nil); len(all) != 1 || len(all[0].Paths) != 3 {
		t.Errorf("ambiguous classes: %v", all)
	}
}

func TestClassMapGenerator_CreateMapThrowsWhenDirectoryDoesNotExist(t *testing.T) {
	_, err := CreateMap(testsDir(t) + "/no-file.no-foler")
	expectException(t, err, classRuntime, "Could not scan for classes inside")
}

func TestClassMapGenerator_CreateMapDoesNotHitRegexBacktraceLimit(t *testing.T) {
	f := testsDir(t) + "/Fixtures/pcrebacktracelimit"
	expected := map[string]string{
		`Foo\StripNoise`:            f + "/StripNoise.php",
		`Foo\VeryLongHeredoc`:       f + "/VeryLongHeredoc.php",
		`Foo\ClassAfterLongHereDoc`: f + "/VeryLongHeredoc.php",
		`Foo\VeryLongPHP73Heredoc`:  f + "/VeryLongPHP73Heredoc.php",
		`Foo\VeryLongPHP73Nowdoc`:   f + "/VeryLongPHP73Nowdoc.php",
		`Foo\ClassAfterLongNowDoc`:  f + "/VeryLongPHP73Nowdoc.php",
		`Foo\VeryLongNowdoc`:        f + "/VeryLongNowdoc.php",
	}
	cm, err := CreateMap(f)
	if err != nil {
		t.Fatal(err)
	}
	assertEqualsNormalized(t, expected, mapOf(cm))
}

// The PSR tests run from testdata, so that paths are shortened to
// ./tests/Fixtures/... as when PHPUnit runs from the package root.
func scanFromTestdata(t *testing.T, dir string, typ AutoloadType, namespace string) *ClassMap {
	t.Helper()
	tests := testsDir(t)
	t.Chdir(filepath.Dir(tests))
	g := NewGenerator([]string{"php", "inc", "hh"})
	if err := g.ScanPaths(tests+dir, nil, typ, namespace, nil); err != nil {
		t.Fatal(err)
	}

	return g.ClassMap()
}

func sortedViolations(cm *ClassMap) []string {
	v := cm.PsrViolations()
	slices.Sort(v)

	return v
}

func TestClassMapGenerator_GetPSR4Violations(t *testing.T) {
	cm := scanFromTestdata(t, "/Fixtures/psrViolations", PSR4, `ExpectedNamespace\`)
	want := []string{
		`Class ClassWithoutNameSpace located in ./tests/Fixtures/psrViolations/ClassWithoutNameSpace.php does not comply with psr-4 autoloading standard (rule: ExpectedNamespace\ => ./tests/Fixtures/psrViolations). Skipping.`,
		`Class ExpectedNamespace\UnexpectedSubNamespace\ClassWithIncorrectSubNamespace located in ./tests/Fixtures/psrViolations/ClassWithIncorrectSubNamespace.php does not comply with psr-4 autoloading standard (rule: ExpectedNamespace\ => ./tests/Fixtures/psrViolations). Skipping.`,
		`Class UnexpectedNamespace\ClassWithNameSpaceOutsideConfiguredScope located in ./tests/Fixtures/psrViolations/ClassWithNameSpaceOutsideConfiguredScope.php does not comply with psr-4 autoloading standard (rule: ExpectedNamespace\ => ./tests/Fixtures/psrViolations). Skipping.`,
	}
	if got := sortedViolations(cm); !slices.Equal(got, want) {
		t.Errorf("violations\n got %q\nwant %q", got, want)
	}
}

func TestClassMapGenerator_GetRawPSR4Violations(t *testing.T) {
	tests := testsDir(t)
	cm := scanFromTestdata(t, "/Fixtures/psrViolations", PSR4, `ExpectedNamespace\`)
	raw := maps.Collect(cm.RawPsrViolations())
	for _, c := range []struct{ file, class string }{
		{"ClassWithoutNameSpace", "ClassWithoutNameSpace"},
		{"ClassWithIncorrectSubNamespace", `ExpectedNamespace\UnexpectedSubNamespace\ClassWithIncorrectSubNamespace`},
		{"ClassWithNameSpaceOutsideConfiguredScope", `UnexpectedNamespace\ClassWithNameSpaceOutsideConfiguredScope`},
	} {
		path := tests + "/Fixtures/psrViolations/" + c.file + ".php"
		v, ok := raw[path]
		if !ok || len(v) != 1 {
			t.Fatalf("%s: %v", path, v)
		}
		want := "Class " + c.class + " located in ./tests/Fixtures/psrViolations/" + c.file +
			`.php does not comply with psr-4 autoloading standard (rule: ExpectedNamespace\ => ./tests/Fixtures/psrViolations). Skipping.`
		if v[0].Warning != want || v[0].ClassName != c.class {
			t.Errorf("%s: got %+v", path, v[0])
		}
	}
}

func TestClassMapGenerator_CreateMapWithDirectoryExcluded(t *testing.T) {
	f := testsDir(t) + "/Fixtures"
	g := NewGenerator([]string{"php", "inc", "hh"})
	if err := g.ScanPaths(f+"/beta", nil, Classmap, "", []string{"NamespaceCollision"}); err != nil {
		t.Fatal(err)
	}
	assertEqualsNormalized(t, map[string]string{
		`PrefixCollision_A_B_Bar`: f + "/beta/PrefixCollision/A/B/Bar.php",
		`PrefixCollision_A_B_Foo`: f + "/beta/PrefixCollision/A/B/Foo.php",
	}, mapOf(g.ClassMap()))
}

func TestClassMapGenerator_Psr0OptimizedClassmapRespectsNamespacePrefix(t *testing.T) {
	cm := scanFromTestdata(t, "/Fixtures/psr0NamespacePrefix", PSR0, "Acme_")
	for _, class := range []string{"Acme_Utils_Helper", "Acme_Logger"} {
		if !cm.HasClass(class) {
			t.Errorf("%s missing", class)
		}
	}
	for _, class := range []string{`Other\Controller\Page`, `Other\Widget`, "Other_Service"} {
		if cm.HasClass(class) {
			t.Errorf("%s present", class)
		}
	}
	want := []string{
		`Class Other\Controller\Page located in ./tests/Fixtures/psr0NamespacePrefix/Other/Controller/Page.php does not comply with psr-0 autoloading standard (rule: Acme_ => ./tests/Fixtures/psr0NamespacePrefix). Skipping.`,
		`Class Other\Widget located in ./tests/Fixtures/psr0NamespacePrefix/Other/Widget.php does not comply with psr-0 autoloading standard (rule: Acme_ => ./tests/Fixtures/psr0NamespacePrefix). Skipping.`,
		`Class Other_Service located in ./tests/Fixtures/psr0NamespacePrefix/Other/Service.php does not comply with psr-0 autoloading standard (rule: Acme_ => ./tests/Fixtures/psr0NamespacePrefix). Skipping.`,
	}
	if got := sortedViolations(cm); !slices.Equal(got, want) {
		t.Errorf("violations\n got %q\nwant %q", got, want)
	}
}

func TestPhpFileParser_FindClassesThrowsWhenFileDoesNotExist(t *testing.T) {
	_, err := FindClasses(testsDir(t) + "/no-file")
	expectException(t, err, classRuntime, "does not exist")
}
