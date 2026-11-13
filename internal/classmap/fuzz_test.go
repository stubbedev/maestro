package classmap

import (
	"os"
	"path/filepath"
	"testing"
)

// fuzzVersions are the PHP versions the fuzz targets pick from: unknown,
// then each minor version's scanner.
var fuzzVersions = []int{0, 70205, 70300, 70400, 80000, 80100, 80200, 80300, 80400, 80500}

// fuzzSeeds adds the synthetic and fixture sources as seeds.
func fuzzSeeds(f *testing.F) {
	f.Helper()
	for _, pattern := range []string{"testdata/synthetic/*.php", "testdata/tests/Fixtures/*/*.php"} {
		paths, _ := filepath.Glob(pattern)
		for _, p := range paths {
			if src, err := os.ReadFile(p); err == nil && len(src) < 1<<16 {
				f.Add(src, true, uint8(0))
			}
		}
	}
	f.Add([]byte("<?php $x = <<<EOT\n  a\n EOT;"), false, uint8(1))
	f.Add([]byte("<? class A {} ?>"), false, uint8(1))
}

// FuzzFindClasses runs php_strip_whitespace(), PhpFileCleaner and the class
// extraction on arbitrary input: they must not panic, and must not depend
// on what follows the input in the buffer.
func FuzzFindClasses(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, src []byte, shortTags bool, version uint8) {
		p := Parser{ShortOpenTag: shortTags, PHPVersionID: fuzzVersions[int(version)%len(fuzzVersions)]}
		var b parseBuffers
		b.src = append(append(b.src, src...), make([]byte, stripPadding)...)
		classes, err := p.classesIn(&b, len(src), "fuzz.php")
		contents, raw := p.contents(&b, len(src), p.phpVersion())
		seen := string(contents)

		// The same input in a larger, dirty buffer.
		var b2 parseBuffers
		b2.src = append(append(b2.src, src...), make([]byte, stripPadding)...)
		b2.src = append(b2.src, "<?php class Dirty {} \"'"...)
		b2.strip = append(b2.strip, "garbage"...)
		classes2, err2 := p.classesIn(&b2, len(src), "fuzz.php")
		contents2, raw2 := p.contents(&b2, len(src), p.phpVersion())
		if string(contents2) != seen || raw != raw2 || (err == nil) != (err2 == nil) || len(classes) != len(classes2) {
			t.Fatalf("result depends on the buffer: %q vs %q", seen, contents2)
		}
		for i := range classes {
			if classes[i] != classes2[i] || classes[i] == "" {
				t.Fatalf("classes %q vs %q", classes, classes2)
			}
		}
	})
}

// FuzzPhpFileCleaner runs the cleaner alone on arbitrary input, with both
// strategies (one or more candidate keywords), on source and on
// php_strip_whitespace() output.
func FuzzPhpFileCleaner(f *testing.F) {
	fuzzSeeds(f)
	f.Fuzz(func(t *testing.T, src []byte, single bool, version uint8) {
		maxMatches := 2
		if single {
			maxMatches = 1
		}
		v := phpVersionOf(fuzzVersions[int(version>>1)%len(fuzzVersions)])
		clean := cleanPhpFile(nil, src, maxMatches, v, version%2 == 0)
		_ = extractClasses(clean, v >= php81)
	})
}
