package archive_test

import (
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/archive/archivetest"
)

var formats = []archive.Format{archive.Zip, archive.Tar, archive.Xz, archive.Gzip}

// FuzzOpen feeds arbitrary bytes to every extractor: it must never panic,
// and whatever it accepts must be a well-formed tree whose content is
// exactly what it planned.
func FuzzOpen(f *testing.F) {
	for _, c := range archivetest.ZipCorpus() {
		f.Add(byte(0), c.Data)
	}

	for _, c := range archivetest.TarCorpus()[:20] {
		f.Add(byte(1), c.Tar)
		f.Add(byte(1), archivetest.Gzip(c.Tar))
		f.Add(byte(2), archivetest.Xz(c.Tar))
	}

	f.Add(byte(3), archivetest.Gzip([]byte("content")))

	dir := f.TempDir()

	f.Fuzz(func(t *testing.T, which byte, data []byte) {
		format := formats[int(which)%len(formats)]
		path := filepath.Join(dir, "fuzz")

		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}

		limits := archive.Limits{MaxEntries: 10000, MaxFileSize: 64 << 20, MaxTotalSize: 128 << 20}

		a, err := archive.Open(path, format, &archive.Options{URL: "https://x/y.gz", Limits: limits, Locale: archive.LocaleUTF8})
		if err != nil {
			checkError(t, err)
			return
		}

		defer func() { _ = a.Close() }()

		checkTree(t, a.Entries())

		read := map[int]bool{}

		err = a.ReadFiles(func(i int, r io.Reader) error {
			e := &a.Entries()[i]
			if e.Kind != archive.File || read[i] {
				t.Fatalf("ReadFiles handed %q (kind %v) twice or as a file", e.Path, e.Kind)
			}

			read[i] = true

			n, err := io.Copy(io.Discard, r)
			if err == nil && n != e.Size {
				t.Fatalf("%q: %d bytes read, %d planned", e.Path, n, e.Size)
			}

			return err
		})
		if err != nil {
			checkError(t, err)
			return
		}

		for i, e := range a.Entries() {
			if e.Kind == archive.File && !read[i] {
				t.Fatalf("%q never read", e.Path)
			}
		}
	})
}

// checkError insists on the package's own error type.
func checkError(t *testing.T, err error) {
	t.Helper()

	if _, ok := errors.AsType[*archive.Error](err); !ok {
		t.Fatalf("error of type %T: %v", err, err)
	}
}

// checkTree checks the invariants every planned tree satisfies.
func checkTree(t *testing.T, entries []archive.Entry) {
	t.Helper()

	if len(entries) == 0 || entries[0].Path != "" || entries[0].Kind != archive.Dir {
		t.Fatal("the tree does not start with the package directory")
	}

	kinds := map[string]archive.Kind{"": archive.Dir}

	for i, e := range entries[1:] {
		if e.Path == "" || strings.IndexByte(e.Path, 0) >= 0 || strings.HasPrefix(e.Path, "/") {
			t.Fatalf("bad path %q", e.Path)
		}

		for c := range strings.SplitSeq(e.Path, "/") {
			if c == "" || c == "." || c == ".." {
				t.Fatalf("bad path %q", e.Path)
			}
		}

		parent := ""
		if j := strings.LastIndexByte(e.Path, '/'); j >= 0 {
			parent = e.Path[:j]
		}

		if kinds[parent] != archive.Dir {
			t.Fatalf("%q is not below a directory listed before it", e.Path)
		}

		if _, dup := kinds[e.Path]; dup {
			t.Fatalf("%q listed twice", e.Path)
		}

		if i > 0 && archive.ComparePaths(entries[i].Path, e.Path) >= 0 {
			t.Fatalf("%q out of order", e.Path)
		}

		if e.Mode&^0o777 != 0 || (e.Kind == archive.Symlink && (e.Link == "" || strings.IndexByte(e.Link, 0) >= 0)) {
			t.Fatalf("%q: bad mode or link", e.Path)
		}

		kinds[e.Path] = e.Kind
	}
}

// TestHostileArchives: the classic attacks are refused or neutralised.
func TestHostileArchives(t *testing.T) {
	big := strings.Repeat("\x00", 100<<20)

	for _, tc := range []struct {
		name   string
		format archive.Format
		data   []byte
		kind   error
	}{
		{"zip slip", archive.Zip, archivetest.Zip("", archivetest.UnixFile("/etc/cron.d/x", 0o644, "x")), archive.ErrIrreproducible},
		{"overlapping members", archive.Zip, archivetest.Zip("", archivetest.UnixFile("a", 0o644, "aaaa"), archivetest.ZipEntry{Name: "b", Host: archivetest.HostUnix, Attr: 0o100644 << 16, Data: "aaaa", Deflate: true, Link: 1}), archive.ErrBomb},
		{"symlink then write through it", archive.Zip, archivetest.Zip("", archivetest.UnixLink("p/l", "/etc"), archivetest.UnixFile("p/l/passwd", 0o644, "x")), archive.ErrIrreproducible},
		{"compression bomb", archive.Zip, archivetest.Zip("", archivetest.UnixFile("bomb", 0o644, big)), archive.ErrLimit},
		{"tar symlink then write through it", archive.Xz, archivetest.Xz(archivetest.Tar(0, false, archivetest.TSym("p/l", "/etc"), archivetest.TFile("p/l/passwd", 0o644, "x"))), archive.ErrIrreproducible},
		{"tar parent climb", archive.Xz, archivetest.Xz(archivetest.Tar(0, false, archivetest.TFile("../../x", 0o644, "x"))), archive.ErrIrreproducible},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "a")
			if err := os.WriteFile(path, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}

			limits := archive.Limits{MaxTotalSize: 64 << 20, MaxFileSize: 64 << 20}

			_, err := archive.Open(path, tc.format, &archive.Options{Limits: limits, Locale: archive.LocaleUTF8})
			if !errors.Is(err, tc.kind) {
				t.Fatalf("expected %v, got %v", tc.kind, err)
			}
		})
	}

	// PharData normalises ".." away instead: the tree stays inside.
	path := filepath.Join(t.TempDir(), "a.tar")
	if err := os.WriteFile(path, archivetest.Tar(0, false, archivetest.TFile("../../../x", 0o644, "x")), 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := archive.Open(path, archive.Tar, nil)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = a.Close() }()

	if e := a.Entries(); len(e) != 2 || e[1].Path != "x" {
		t.Fatalf("unexpected tree %+v", e)
	}
}

// TestCorruptContent: content that does not match what the headers
// promise fails while reading, never silently.
func TestCorruptContent(t *testing.T) {
	data := archivetest.Zip("", archivetest.ZipEntry{Name: "f", Data: "hello world", Host: archivetest.HostUnix, Attr: 0o100644 << 16})
	data[30+1] ^= 0xff // stored: flip a content byte, the CRC no longer matches

	path := filepath.Join(t.TempDir(), "a.zip")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	a, err := archive.Open(path, archive.Zip, nil)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = a.Close() }()

	h := sha256.New()

	err = a.ReadFiles(func(_ int, r io.Reader) error {
		_, err := io.Copy(h, r)
		return err
	})
	if !errors.Is(err, archive.ErrIrreproducible) || !strings.Contains(err.Error(), "bad CRC") {
		t.Fatalf("expected a CRC error, got %v", err)
	}
}
