// Ports tests/Composer/Test/Autoload/ClassLoaderTest.php, which runs the
// embedded ClassLoader.php (with php, when available), and checks the Go
// side of the class loader against it.

package autoload

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// classLoaderPHP is the path of the embedded ClassLoader.php.
var classLoaderPHP = strings.TrimSuffix(testdataDir, "/testdata") + "/res/ClassLoader.php"

func TestClassLoader_LoadClass(t *testing.T) {
	for _, class := range []string{`Namespaced\Foo`, `Pearlike_Foo`, `ShinyVendor\ShinyPackage\SubNamespace\Foo`} {
		t.Run(class, func(t *testing.T) {
			fixtures := testdataDir + "/Fixtures"
			out := runPHP(t, `require '`+classLoaderPHP+`';
$loader = new Composer\Autoload\ClassLoader();
$loader->add('Namespaced\\', '`+fixtures+`');
$loader->add('Pearlike_', '`+fixtures+`');
$loader->addPsr4('ShinyVendor\\ShinyPackage\\', '`+fixtures+`');
$loader->loadClass('`+strings.ReplaceAll(class, `\`, `\\`)+`');
echo class_exists('`+strings.ReplaceAll(class, `\`, `\\`)+`', false) ? 'ok' : 'missing';`)
			if out != "ok" {
				t.Errorf("->loadClass() loads '%s': %s", class, out)
			}
		})
	}
}

func TestClassLoader_GetPrefixesWithNoPSR0Configuration(t *testing.T) {
	if NewClassLoader("").PrefixesPsr0.Len() != 0 {
		t.Error("a new loader has PSR-0 prefixes")
	}
	if out := runPHP(t, `require '`+classLoaderPHP+`'; $loader = new Composer\Autoload\ClassLoader(); echo empty($loader->getPrefixes()) ? 'ok' : 'not empty';`); out != "ok" {
		t.Error(out)
	}
}

func TestClassLoader_Serializability(t *testing.T) {
	out := runPHP(t, `require '`+classLoaderPHP+`';
$loader = new Composer\Autoload\ClassLoader();
$loader->add('Pearlike_', '/Fixtures');
$loader->add('', '/FALLBACK');
$loader->addPsr4('ShinyVendor\\ShinyPackage\\', '/Fixtures');
$loader->addPsr4('', '/FALLBACKPSR4');
$loader->addClassMap(['A' => '', 'B' => 'path']);
$loader->setApcuPrefix('prefix');
$loader->setClassMapAuthoritative(true);
$loader->setUseIncludePath(true);
$loader2 = unserialize(serialize($loader));
echo $loader2 instanceof Composer\Autoload\ClassLoader
    && $loader->getApcuPrefix() === $loader2->getApcuPrefix()
    && $loader->getClassMap() === $loader2->getClassMap()
    && $loader->getFallbackDirs() === $loader2->getFallbackDirs()
    && $loader->getFallbackDirsPsr4() === $loader2->getFallbackDirsPsr4()
    && $loader->getPrefixes() === $loader2->getPrefixes()
    && $loader->getPrefixesPsr4() === $loader2->getPrefixesPsr4()
    && $loader->getUseIncludePath() === $loader2->getUseIncludePath() ? 'ok' : 'differs';`)
	if out != "ok" {
		t.Error(out)
	}
}

// TestClassLoader_MatchesPHP runs random sequences of registrations on the
// Go ClassLoader and on the embedded ClassLoader.php and compares the
// resulting properties, or the exception.
func TestClassLoader_MatchesPHP(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	prefixes := []string{"", "0", "A", `A\`, `A\B\`, "Foo_", `Foo\Bar`, "1", `1\`, "Ab"}
	paths := []string{"/a", "/b", "src", "/c/d"}

	var script strings.Builder
	script.WriteString("require '" + classLoaderPHP + "';\n")
	// Composer's error handler turns warnings into exceptions.
	script.WriteString("set_error_handler(function ($l, $m) { throw new ErrorException($m); });\n")
	var want strings.Builder
	for i := range 60 {
		l := NewClassLoader("")
		script.WriteString("$l = new Composer\\Autoload\\ClassLoader(); try {\n")
		var failed error
		for range 1 + r.IntN(8) {
			prefix := prefixes[r.IntN(len(prefixes))]
			list := php.NewArray()
			var code []string
			for range 1 + r.IntN(2) {
				p := paths[r.IntN(len(paths))]
				list.Append(p)
				code = append(code, php.VarExport(p))
			}
			// the prefix comes from an array key, as in createLoader and
			// getStaticFile
			arg := "key(array(" + php.VarExport(prefix) + " => 0)), array(" + strings.Join(code, ", ") + ")"
			prepend := r.IntN(2) == 0
			// PHP stops at the first exception, so does Go.
			run := func(op func() error) {
				if failed == nil {
					failed = op()
				}
			}
			switch op := r.IntN(5); op {
			case 0:
				run(func() error { return l.Add(php.StrKey(prefix), list, prepend) })
				script.WriteString("$l->add(" + arg + ", " + strconv.FormatBool(prepend) + ");\n")
			case 1:
				run(func() error { return l.AddPsr4(php.StrKey(prefix), list, prepend) })
				script.WriteString("$l->addPsr4(" + arg + ", " + strconv.FormatBool(prepend) + ");\n")
			case 2:
				run(func() error { return l.Set(php.StrKey(prefix), list) })
				script.WriteString("$l->set(" + arg + ");\n")
			case 3:
				run(func() error { return l.SetPsr4(php.StrKey(prefix), list) })
				script.WriteString("$l->setPsr4(" + arg + ");\n")
			default:
				run(func() error { l.AddClassMap(php.ArrayOf(`C\`+prefix, list.Values()[0])); return nil })
				script.WriteString("$l->addClassMap(array(" + php.VarExport(`C\`+prefix) + " => " + code[0] + "));\n")
			}
		}
		script.WriteString("} catch (\\Throwable $e) { echo $e->getMessage(), \"\\n\"; }\n")
		script.WriteString("foreach ((array) $l as $k => $v) { if (is_array($v) && !str_ends_with($k, 'missingClasses')) { echo substr($k, strlen(\"\\0Composer\\\\Autoload\\\\ClassLoader\\0\")), '=', var_export($v, true), \"\\n\"; } }\necho \"--" + strconv.Itoa(i) + "\\n\";\n")

		if failed != nil {
			want.WriteString(failed.Error() + "\n")
		}
		for _, p := range l.properties() {
			want.WriteString(p.name + "=" + php.VarExport(p.value) + "\n")
		}
		want.WriteString("--" + strconv.Itoa(i) + "\n")
	}

	got := runPHP(t, script.String())
	gotBlocks, wantBlocks := strings.Split(got, "\n--"), strings.Split(want.String(), "\n--")
	for i := range wantBlocks {
		if i >= len(gotBlocks) {
			t.Fatalf("php output ends at block %d", i)
		}
		if gotBlocks[i] != wantBlocks[i] {
			t.Errorf("block %d:\n got %q\nwant %q", i, gotBlocks[i], wantBlocks[i])
		}
	}
}

func TestIndentExport(t *testing.T) {
	inputs := []string{
		"",
		"array (\n)",
		"array (\n  'a' => \n  array (\n    0 => 'x',\n  ),\n)",
		"  lead\n  \n   \ntrail   ",
		"x  \n",
		"\n\n  a  \n",
		" \t x \n\t\n",
	}
	r := rand.New(rand.NewPCG(3, 4))
	for range 500 {
		var b strings.Builder
		for range r.IntN(30) {
			b.WriteByte(" \n\ta'"[r.IntN(5)])
		}
		inputs = append(inputs, b.String())
	}
	for _, in := range inputs {
		want, _, err := php.PregReplace("/^ */m", "    $0$0", in, -1)
		if err != nil {
			t.Fatal(err)
		}
		want, _, err = php.PregReplace("/ +$/m", "", php.Ltrim(want), -1)
		if err != nil {
			t.Fatal(err)
		}
		if got := indentExport(in); got != want {
			t.Errorf("indentExport(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGenerator_CreateLoader(t *testing.T) {
	e := setUp(t)
	e.mkdir(e.workingDir + "/cm")
	e.write(e.workingDir+"/cm/a.php", `<?php class CmA {}`)
	e.write(e.workingDir+"/cm/b.php", `<?php class CmB {}`)

	autoloads := &Autoloads{
		PSR0:                php.ArrayOf("Foo", php.ListOf("/x"), "", php.ListOf("/fallback")),
		PSR4:                php.ArrayOf(`Bar\`, php.ListOf("/y", "/z")),
		Classmap:            []string{"cm", "missing"},
		Files:               php.NewArray(),
		ExcludeFromClassmap: []string{php.PregQuote(filepath.ToSlash(e.workingDir), "") + "/cm/b.php($|/)"},
	}
	l, err := e.generator.CreateLoader(autoloads, e.vendorDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := php.VarExport(l.PrefixesPsr0), "array (\n  'F' => \n  array (\n    'Foo' => \n    array (\n      0 => '/x',\n    ),\n  ),\n)"; got != want {
		t.Errorf("prefixesPsr0 %s, want %s", got, want)
	}
	if got, want := php.VarExport(l.FallbackDirsPsr0), "array (\n  0 => '/fallback',\n)"; got != want {
		t.Errorf("fallbackDirsPsr0 %s, want %s", got, want)
	}
	if got, want := php.VarExport(l.PrefixLengthsPsr4), "array (\n  'B' => \n  array (\n    'Bar\\\\' => 4,\n  ),\n)"; got != want {
		t.Errorf("prefixLengthsPsr4 %s, want %s", got, want)
	}
	if got, want := php.VarExport(l.ClassMap), "array (\n  'CmA' => '"+filepath.ToSlash(e.workingDir)+"/cm/a.php',\n)"; got != want {
		t.Errorf("classMap %s, want %s", got, want)
	}
	if want := `<warning>Could not scan for classes inside "missing" which does not appear to be a file nor a folder</warning>` + "\n"; e.io.Output() != want {
		t.Errorf("output %q, want %q", e.io.Output(), want)
	}
	if _, err := os.Stat(e.vendorDir + "/composer"); err == nil {
		t.Error("CreateLoader wrote files")
	}
}
