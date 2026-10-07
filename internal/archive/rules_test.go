package archive

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestParseFormat(t *testing.T) {
	for _, f := range []Format{Zip, Tar, Xz, Gzip} {
		if got, ok := ParseFormat(f.String()); !ok || got != f {
			t.Errorf("%v does not round-trip", f)
		}
	}

	for _, s := range []string{"rar", "7z", "phar", "", "ZIP"} {
		if _, ok := ParseFormat(s); ok {
			t.Errorf("%q accepted", s)
		}
	}
}

func TestRules(t *testing.T) {
	utf8 := Rules(Zip, &Options{Locale: LocaleUTF8})
	if utf8 == Rules(Zip, &Options{Locale: LocaleC}) {
		t.Error("the locale must change the zip rules")
	}

	if Rules(Tar, &Options{Locale: LocaleUTF8}) != Rules(Tar, &Options{Locale: LocaleC}) {
		t.Error("the locale must not change the tar rules")
	}

	t.Setenv("LC_ALL", "")
	t.Setenv("LC_CTYPE", "")
	t.Setenv("LANG", "de_DE.UTF-8")

	if Rules(Zip, nil) != utf8 {
		t.Error("the environment's locale is not used")
	}
}

func TestClassifyLocale(t *testing.T) {
	for name, want := range map[string]Locale{
		"": LocaleC, "C": LocaleC, "POSIX": LocaleC,
		"C.UTF-8": LocaleUTF8, "en_US.UTF-8": LocaleUTF8, "en_US.utf8": LocaleUTF8, "de_DE.UTF-8@euro": LocaleUTF8,
		"en_US": LocaleOther, "en_US.ISO-8859-1": LocaleOther, "ja_JP.eucJP": LocaleOther,
	} {
		if got := classifyLocale(name); got != want {
			t.Errorf("classifyLocale(%q) = %v, want %v", name, got, want)
		}
	}

	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "en_US.UTF-8")

	if localeFromEnv() != LocaleC {
		t.Error("LC_ALL must win over LANG")
	}
}

func TestComparePaths(t *testing.T) {
	paths := []string{"a/b", "a", "a.b", "a/b/c", "a-b", "b", "a/a", ""}
	slices.SortFunc(paths, ComparePaths)

	want := []string{"", "a", "a/a", "a/b", "a/b/c", "a-b", "a.b", "b"}
	if !slices.Equal(paths, want) {
		t.Errorf("got %q, want %q", paths, want)
	}
}

// TestCleanName checks the fast path against the full mapping: whenever
// cleanName accepts a name, mapname's slow path maps it to itself.
func TestCleanName(t *testing.T) {
	z := &zipPlanner{locale: LocaleUTF8}
	alphabet := []string{"a", "b", "/", ".", "..", ";", "1", "\x01", "\x7f", "\xff", "é", " "}
	rng := rand.New(rand.NewPCG(1, 2))

	for range 200000 {
		var b strings.Builder
		for range rng.IntN(8) + 1 {
			b.WriteString(alphabet[rng.IntN(len(alphabet))])
		}

		name := b.String()
		if !cleanName(name) || strings.HasPrefix(name, "/") {
			continue
		}

		path, isDir, err := z.mapnameSlow(name)
		if err != nil || path != strings.TrimSuffix(name, "/") || isDir != strings.HasSuffix(name, "/") {
			t.Fatalf("%q: clean, but maps to %q, %v, %v", name, path, isDir, err)
		}
	}
}

func TestMapname(t *testing.T) {
	z := &zipPlanner{locale: LocaleUTF8}

	for name, want := range map[string]string{
		"a/b":          "a/b",
		"a/b/":         "a/b",
		"./a//b":       "a/b",
		"../../a":      "a",
		"a/../b":       "a/b",
		"a/.":          "a/_",
		"a/..":         "a/__",
		"a;1":          "a",
		"a;12;3":       "a;12",
		"a;x":          "a;x",
		"a;1/b":        "a;1/b",
		"a\x01b":       "ab",
		"\x01/b":       "b",
		"./":           "",
		"a\xffb":       "ab",
		"caf\xc3\xa9/": "caf\xc3\xa9",
	} {
		got, _, err := z.mapname(name)
		if err != nil || got != want {
			t.Errorf("mapname(%q) = %q, %v; want %q", name, got, err, want)
		}
	}

	for _, name := range []string{";1", "a/\x01", "a/;2"} {
		if _, _, err := z.mapname(name); err == nil {
			t.Errorf("mapname(%q): expected a conversion failure", name)
		}
	}
}

func TestVirtualPath(t *testing.T) {
	for name, want := range map[string]string{
		"a/b": "a/b", "/a": "a", "a//b/": "a/b", "./a": "a", "a/../b": "b", "../../a": "a", "a/b/..": "a",
	} {
		if got, ok := virtualPath(name); !ok || got != want {
			t.Errorf("virtualPath(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}

	for _, name := range []string{"", "/", ".", "..", "a/..", "./"} {
		if _, ok := virtualPath(name); ok {
			t.Errorf("virtualPath(%q) accepted", name)
		}
	}
}

func TestGzipTarget(t *testing.T) {
	for url, want := range map[string]string{
		"https://example.org/dl/tool.phar.gz":       "tool.phar",
		"https://example.org/dl/tool.gz?x=1#frag":   "tool",
		"https://example.org/dl/tool":               "tool",
		"https://example.org/dl/tool/":              "tool",
		`https://example.org\dl\tool.gz`:            "tool",
		"/srv/files/archive.tar.gz":                 "archive.tar",
		"http://user:pass@host:8080/a/b.c.gz":       "b.c",
		"https://example.org/dl/%20space.gz":        "%20space",
		"svn+ssh://example.org/x.gz":                "x",
		"https://example.org/.gz":                   "",
		"https://example.org/":                      "",
		"https://example.org":                       "",
		"relative/path.gz":                          "",
		"//host/x.gz":                               "",
		"https://example.org/caf\xc3\xa9.gz":        "",
		"https://example.org/a b.gz":                "",
		"1http://example.org/x.gz":                  "",
		"https://example.org/x.gz?redirect=/y/z.gz": "x",
		"C:/Users/me/dists/single.php.gz":           "single.php",
		`C:\Users\me\dists\single.php.gz`:           "single.php",
		"c:/a:b/c.gz?x#y":                           "c",
		"C:/":                                       "",
		"C:relative.gz":                             "",
	} {
		got, ok := gzipTarget(url)
		if want == "" {
			if ok {
				t.Errorf("gzipTarget(%q) = %q, expected a refusal", url, got)
			}

			continue
		}

		if !ok || got != want {
			t.Errorf("gzipTarget(%q) = %q, %v; want %q", url, got, ok, want)
		}
	}
}

func TestTarNumbers(t *testing.T) {
	for in, want := range map[string]int64{"0000644\x00": 0o644, "  755 ": 0o755, "": 0, "9": 0, "7778": 0o777, "00000000017\x00": 0o17} {
		if got := tarNumber64([]byte(in)); got != want {
			t.Errorf("tarNumber64(%q) = %o, want %o", in, got, want)
		}
	}
}

func TestLimits(t *testing.T) {
	dir := t.TempDir()

	var buf bytes.Buffer

	w := zip.NewWriter(&buf)
	for i := range 20 {
		f, _ := w.Create("p/" + strings.Repeat("x", i+1))
		_, _ = f.Write([]byte(strings.Repeat("y", 1000)))
	}

	_ = w.Close()

	path := filepath.Join(dir, "a.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, l := range map[string]Limits{
		"entries":    {MaxEntries: 10},
		"file size":  {MaxFileSize: 999},
		"total size": {MaxTotalSize: 19999},
	} {
		if _, err := Open(path, Zip, &Options{Limits: l}); !errors.Is(err, ErrLimit) {
			t.Errorf("%s: expected ErrLimit, got %v", name, err)
		}
	}

	a, err := Open(path, Zip, &Options{Limits: Limits{MaxEntries: 21, MaxFileSize: 1000, MaxTotalSize: 20000}})
	if err != nil {
		t.Fatalf("at the limits: %v", err)
	}

	_ = a.Close()
}

func TestErrorFormat(t *testing.T) {
	err := error(&Error{Format: Zip, Err: ErrIrreproducible, Entry: "a/b", Reason: "why", ExitCode: 1})
	if err.Error() != `zip: "a/b": why` || !errors.Is(err, ErrIrreproducible) {
		t.Errorf("unexpected %q", err)
	}
}

// unicodePathZip is a zip of one file stored as "pkg/f.txt" whose Info-ZIP
// Unicode Path extra field names it "pkg/" + name.
func unicodePathZip(t *testing.T, name string) string {
	t.Helper()

	stored := "pkg/f.txt"
	unicode := "pkg/" + name

	extra := binary.LittleEndian.AppendUint16(nil, 0x7075)
	extra = binary.LittleEndian.AppendUint16(extra, uint16(5+len(unicode)))
	extra = append(extra, 1)
	extra = binary.LittleEndian.AppendUint32(extra, crc32.ChecksumIEEE([]byte(stored)))
	extra = append(extra, unicode...)

	var buf bytes.Buffer

	w := zip.NewWriter(&buf)

	// Raw, with the sizes known up front: no data descriptor.
	fh := &zip.FileHeader{Name: stored, Method: zip.Store, Extra: extra, CRC32: crc32.ChecksumIEEE([]byte("x")), CompressedSize64: 1, UncompressedSize64: 1}
	fh.SetMode(0o644)

	f, err := w.CreateRaw(fh)
	if err != nil {
		t.Fatal(err)
	}

	_, _ = f.Write([]byte("x"))
	_ = w.Close()

	path := filepath.Join(t.TempDir(), "a.zip")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

// TestZipCLocaleLatin1: unzip in the C locale escapes what wctomb() cannot
// convert. glibc converts nothing beyond ASCII; macOS converts U+0080 to
// U+00FF to Latin-1 bytes, which APFS refuses, so unzip fails there and
// Composer falls back to ZipArchive, which maestro does not reproduce.
func TestZipCLocaleLatin1(t *testing.T) {
	old := cLocaleLatin1
	t.Cleanup(func() { cLocaleLatin1 = old })

	for _, c := range []struct {
		latin1     bool
		name, want string
	}{
		{false, "plaín.txt", "pla#U00edn.txt"},
		{false, "ж.txt", "#U0436.txt"},
		{true, "plaín.txt", ""},
		{true, "ж.txt", "#U0436.txt"},
	} {
		cLocaleLatin1 = c.latin1

		a, err := Open(unicodePathZip(t, c.name), Zip, &Options{Locale: LocaleC})
		if c.want == "" {
			if !errors.Is(err, ErrIrreproducible) {
				t.Errorf("latin1 %v, %q: expected ErrIrreproducible, got %v", c.latin1, c.name, err)
			}

			continue
		}

		if err != nil {
			t.Fatalf("latin1 %v, %q: %v", c.latin1, c.name, err)
		}

		var paths []string
		for _, e := range a.Entries() {
			paths = append(paths, e.Path)
		}

		_ = a.Close()

		if !slices.Contains(paths, c.want) {
			t.Errorf("latin1 %v, %q: entries %q, want %q", c.latin1, c.name, paths, c.want)
		}
	}

	cLocaleLatin1 = true
	if Rules(Zip, &Options{Locale: LocaleC}) == Rules(Zip, &Options{Locale: LocaleUTF8}) {
		t.Error("macOS's C locale needs zip rules of its own")
	}
}
