package autoload

import (
	"strconv"
	"testing"

	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/php"
)

// TestClassmapChunks checks that autoload_classmap.php and the static
// class map, built in chunks of classes, are what building them whole
// gives.
func TestClassmapChunks(t *testing.T) {
	for _, base := range []string{"/proj", "/pro'j", "/pro\nj"} {
		for _, n := range []int{0, 1, 3, classmapChunk - 1, classmapChunk, classmapChunk + 1, 3*classmapChunk + 7} {
			t.Run(strconv.Quote(base)+"/"+strconv.Itoa(n), func(t *testing.T) {
				vendor := base + "/vendor"
				d := &dump{
					basePath: base, vendorPath: vendor, realTarget: vendor + "/composer",
					vendorDir: vendor, baseDir: base,
					vendorPathCode: "__DIR__ . '/..'", appBaseDirCode: "dirname($vendorDir)",
				}
				m := &classmap.ClassMap{}
				for i := range n {
					class := `Vendor\Pkg\C` + strconv.Itoa(i)
					path := vendor + "/pkg/src/C" + strconv.Itoa(i) + ".php"
					switch i % 7 {
					case 1:
						path = base + "/src/C" + strconv.Itoa(i) + ".php"
					case 2:
						path = "/elsewhere/C" + strconv.Itoa(i) + ".php"
					case 3:
						path = vendor + "/pkg/a.phar/C" + strconv.Itoa(i) + ".php"
					case 4:
						path = vendor + `/pkg/it's\C` + strconv.Itoa(i) + ".php"
					case 5:
						class = strconv.Itoa(i) // an int key in a PHP array
					case 6:
						path = "src/rel" + strconv.Itoa(i) + ".php"
					}
					m.AddClass(class, path)
				}
				if err := d.classmap(m); err != nil {
					t.Fatal(err)
				}

				b := d.appendHeader("autoload_classmap.php")
				for class, path := range m.Map() {
					ref, err := d.pathRef(path)
					if err != nil {
						t.Fatal(err)
					}
					b = append(b, "    "...)
					b = php.AppendVarExport(b, class)
					b = append(b, " => "...)
					b = ref.appendCode(b)
					b = append(b, ",\n"...)
				}
				if want := string(append(b, ");\n"...)); d.classmapFile != want {
					t.Errorf("autoload_classmap.php:\n%s\nwant:\n%s", d.classmapFile, want)
				}

				replacements, err := d.staticReplacements()
				if err != nil {
					t.Fatal(err)
				}
				want := ""
				if n > 0 && base != "/pro\nj" {
					want = staticProperty(d.staticClassMapValue(), replacements)
				}
				if d.staticClassMap != want {
					t.Errorf("static class map:\n%s\nwant:\n%s", d.staticClassMap, want)
				}
			})
		}
	}
}
