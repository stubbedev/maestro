package classmap

import (
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkFindClasses parses the fixtures and synthetic files, reusing
// buffers as a scan worker does.
func BenchmarkFindClasses(b *testing.B) {
	var paths []string
	for _, pattern := range []string{"testdata/synthetic/*.php", "testdata/tests/Fixtures/*/*.php"} {
		p, _ := filepath.Glob(pattern)
		paths = append(paths, p...)
	}
	size := int64(0)
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil {
			size += info.Size()
		}
	}
	b.SetBytes(size)
	b.ReportAllocs()
	var buf parseBuffers
	for b.Loop() {
		for _, p := range paths {
			_, _ = DefaultParser.findClasses(&buf, p)
		}
	}
}

// BenchmarkStripWhitespace measures the scanner alone on a large file.
func BenchmarkStripWhitespace(b *testing.B) {
	src, err := os.ReadFile("testdata/tests/Fixtures/classmap/LargeClass.php")
	if err != nil {
		b.Fatal(err)
	}
	buf := append(src, make([]byte, stripPadding)...)
	var out []byte
	var l lexer
	b.SetBytes(int64(len(src)))
	b.ReportAllocs()
	for b.Loop() {
		out = l.strip(out[:0], buf, len(src), true, defaultPHPVersion)
	}
}

// BenchmarkScanComposer builds the class map of Composer's sources and
// dependencies (.ref/composer src and vendor), as an optimized dump does.
func BenchmarkScanComposer(b *testing.B) {
	root, err := filepath.Abs("../../.ref/composer")
	if err == nil {
		root, err = filepath.EvalSymlinks(root)
	}
	if err != nil {
		b.Skip(".ref/composer is not available")
	}
	b.ReportAllocs()
	for b.Loop() {
		g := NewGenerator([]string{"php", "inc", "hh"})
		g.AvoidDuplicateScans(nil)
		for _, dir := range []string{"src", "vendor"} {
			if err := g.ScanPaths(root+"/"+dir, nil, Classmap, "", nil); err != nil {
				b.Fatal(err)
			}
		}
	}
}
