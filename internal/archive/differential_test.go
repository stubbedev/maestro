package archive_test

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/archive/archivetest"
)

// extract runs maestro's extraction and returns the package tree as the
// given umask would leave it.
func extract(path string, format archive.Format, opts *archive.Options, umask int) (archivetest.Tree, error) {
	a, err := archive.Open(path, format, opts)
	if err != nil {
		return nil, err
	}

	defer func() { _ = a.Close() }()

	tree := archivetest.Tree{}
	files := 0

	for _, e := range a.Entries() {
		switch e.Kind {
		case archive.Dir:
			tree[e.Path] = archivetest.Node{Kind: 'd', Perm: e.Perm(os.FileMode(umask))}
		case archive.Symlink:
			tree[e.Path] = archivetest.Node{Kind: 'l', Data: e.Link}
		case archive.File:
			files++
		}
	}

	err = a.ReadFiles(func(i int, r io.Reader) error {
		e := &a.Entries()[i]

		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}

		if int64(len(data)) != e.Size {
			return fmt.Errorf("%s: read %d bytes, planned %d", e.Path, len(data), e.Size)
		}

		tree[e.Path] = archivetest.Node{Kind: 'f', Perm: e.Perm(os.FileMode(umask)), Data: string(data)}
		files--

		return nil
	})
	if err != nil {
		return nil, err
	}

	if files != 0 {
		return nil, fmt.Errorf("ReadFiles skipped %d files", files)
	}

	return tree, nil
}

// skipReference skips a comparison with the reference extractors in -short
// mode, and on Windows: maestro reproduces unzip, tar and PharData under a
// Unix umask, which Composer on Windows (ZipArchive or 7-Zip, on a
// filesystem without permission bits) does not run.
func skipReference(t *testing.T) {
	t.Helper()

	if testing.Short() {
		t.Skip("compares with the reference extractors; skipped in -short mode")
	}

	if runtime.GOOS == "windows" {
		t.Skip("compares with the Unix reference extractors, which Composer does not use on Windows")
	}
}

func write(t *testing.T, name string, data []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

// TestDifferentialZip extracts the generated zip corpus with real unzip and
// with maestro, under two umasks and two locales, and compares every entry.
// The reference is Info-ZIP UnZip 6.00 with UNICODE_SUPPORT
// (archivetest.InfoZip); a case whose names this file system cannot store
// is left out (archivetest.Unstorable).
func TestDifferentialZip(t *testing.T) {
	skipReference(t)
	unzip := archivetest.NeedInfoZip(t)

	locales := []struct {
		lcAll  string
		locale archive.Locale
	}{{"C.UTF-8", archive.LocaleUTF8}, {"C", archive.LocaleC}}
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		// LocaleC reproduces unzip on glibc, whose C locale cannot convert
		// non-ASCII characters (unzip escapes them as #Uxxxx), and on
		// macOS, whose C locale maps U+0080 to U+00FF to single Latin-1
		// bytes that APFS refuses.
		t.Log("C locale cases skipped: they reproduce glibc's and macOS's C locale")
		locales = locales[:1]
	}

	var c archivetest.Tally

	for _, tc := range archivetest.ZipCorpus() {
		path := write(t, "dist.zip", tc.Data)

		for _, umask := range archivetest.Umasks {
			for _, l := range locales {
				name := fmt.Sprintf("%s/umask-%03o/%s", tc.Name, umask, l.lcAll)
				got, err := extract(path, archive.Zip, &archive.Options{Locale: l.locale}, umask)
				if archivetest.Unstorable(got) {
					t.Logf("%s: skipped, this file system refuses names that are not UTF-8", name)
					continue
				}
				real := archivetest.Unzip(t, unzip, path, umask, l.lcAll)
				c.Compare(t, name, real, got, err, tc.MayRefuse)
			}
		}
	}

	c.Log(t, "zip corpus")
}

// TestDifferentialPharTar extracts the generated tar corpus, plain and
// gzip-compressed, with PHP's PharData and with maestro.
func TestDifferentialPharTar(t *testing.T) {
	skipReference(t)
	archivetest.Need(t, "php")

	var c archivetest.Tally

	for _, tc := range archivetest.TarCorpus() {
		for _, v := range []struct {
			ext  string
			data []byte
		}{{"tar", tc.Tar}, {"tar.gz", archivetest.Gzip(tc.Tar)}} {
			path := write(t, "dist."+v.ext, v.data)

			for _, umask := range archivetest.Umasks {
				name := fmt.Sprintf("%s/%s/umask-%03o", tc.Name, v.ext, umask)
				real := archivetest.PharData(t, path, umask)
				got, err := extract(path, archive.Tar, nil, umask)
				c.Compare(t, name, real, got, err, tc.MayRefusePhar)
			}
		}
	}

	c.Log(t, "tar corpus (PharData)")
}

// TestDifferentialPharTarBzip2 checks bzip2-compressed tars against
// PharData's extraction of the same tar uncompressed: PHP's bz2 extension
// is not needed to know what the decompressed tar holds.
func TestDifferentialPharTarBzip2(t *testing.T) {
	skipReference(t)
	archivetest.Need(t, "php", "bzip2")

	var c archivetest.Tally

	for _, tc := range archivetest.TarCorpus() {
		plain := write(t, "dist.tar", tc.Tar)

		out, err := exec.Command("bzip2", "-c", plain).Output()
		if err != nil {
			t.Fatal(err)
		}

		path := write(t, "dist.tar.bz2", out)

		for _, umask := range archivetest.Umasks {
			name := fmt.Sprintf("%s/tar.bz2/umask-%03o", tc.Name, umask)
			real := archivetest.PharData(t, plain, umask)
			got, err := extract(path, archive.Tar, nil, umask)
			c.Compare(t, name, real, got, err, tc.MayRefusePhar)
		}
	}

	c.Log(t, "tar.bz2 corpus (PharData)")
}

// TestDifferentialXz extracts the tar corpus, xz-compressed, with GNU tar
// and with maestro.
func TestDifferentialXz(t *testing.T) {
	skipReference(t)
	tar := archivetest.NeedGNUTar(t)
	archivetest.Need(t, "xz")

	var c archivetest.Tally

	for _, tc := range archivetest.TarCorpus() {
		path := write(t, "dist.tar.xz", archivetest.Xz(tc.Tar))

		for _, umask := range archivetest.Umasks {
			name := fmt.Sprintf("%s/xz/umask-%03o", tc.Name, umask)
			real := archivetest.TarXz(t, tar, path, umask)
			got, err := extract(path, archive.Xz, nil, umask)
			c.Compare(t, name, real, got, err, tc.MayRefuseGNU)
		}
	}

	c.Log(t, "tar.xz corpus (GNU tar)")
}

// TestDifferentialGzip decompresses gzip dists with gzip and with maestro.
func TestDifferentialGzip(t *testing.T) {
	skipReference(t)
	archivetest.Need(t, "gzip")

	member := archivetest.Gzip([]byte("hello\n"))
	cases := []archivetest.Case{
		{Name: "plain", URL: "https://example.org/dl/tool.phar.gz?x=1", Data: archivetest.Gzip([]byte("<?php echo 1;\n"))},
		{Name: "empty", URL: "https://example.org/empty.gz", Data: archivetest.Gzip(nil)},
		{Name: "multi-member", URL: "https://example.org/m.gz", Data: append(append([]byte{}, member...), member...)},
		{Name: "trailing-garbage", URL: "https://example.org/g.gz", Data: append(append([]byte{}, member...), "garbage"...)},
		{Name: "not-gzip", URL: "https://example.org/n.gz", Data: []byte("plain text")},
		{Name: "truncated", URL: "https://example.org/t.gz", Data: member[:len(member)-3]},
	}

	var c archivetest.Tally

	for _, tc := range cases {
		path := write(t, "dist.gz", tc.Data)

		for _, umask := range archivetest.Umasks {
			name := fmt.Sprintf("%s/umask-%03o", tc.Name, umask)
			target := ""

			if got, err := extract(path, archive.Gzip, &archive.Options{URL: tc.URL}, umask); err == nil {
				for p := range got {
					if p != "" {
						target = p
					}
				}

				real := archivetest.Gunzip(t, path, target, umask)
				c.Compare(t, name, real, got, nil, false)
			} else {
				real := archivetest.Gunzip(t, path, "out", umask)
				c.Compare(t, name, real, nil, err, tc.MayRefuse)
			}
		}
	}

	c.Log(t, "gzip")
}

// TestDifferentialZip64 checks that unzip extracts the zip64 and Deflate64
// cases of the corpus (TestDifferentialZip compares them), and compares
// archives made by real writers: Info-ZIP zip -fz (a zip64 end record and
// extra fields; written to a pipe, the archive unzip refuses) and 7-Zip's
// Deflate64 (dynamic Huffman blocks, distances past 32 KiB).
func TestDifferentialZip64(t *testing.T) {
	skipReference(t)
	unzip := archivetest.NeedInfoZip(t)

	cases := archivetest.Zip64Cases()

	for _, name := range []string{"zip-fz", "deflate64-7z"} {
		data, err := os.ReadFile(filepath.Join("testdata", name+".zip"))
		if err != nil {
			t.Fatal(err)
		}

		cases = append(cases, archivetest.Case{Name: name, Data: data})
	}

	pipe, err := os.ReadFile(filepath.Join("testdata", "zip-fz-pipe.zip"))
	if err != nil {
		t.Fatal(err)
	}

	cases = append(cases, archivetest.Case{Name: "zip-fz-pipe", Data: pipe, Fails: true})

	var c archivetest.Tally

	for _, tc := range cases {
		path := write(t, "dist.zip", tc.Data)

		for _, umask := range archivetest.Umasks {
			name := fmt.Sprintf("%s/umask-%03o", tc.Name, umask)
			real := archivetest.Unzip(t, unzip, path, umask, "C.UTF-8")

			if real.OK() == tc.Fails && !tc.MayRefuse {
				t.Errorf("%s: unzip exit %d, expected it to fail: %v (%s)", name, real.Exit, tc.Fails, strings.TrimSpace(real.Output))
			}

			got, err := extract(path, archive.Zip, &archive.Options{Locale: archive.LocaleUTF8}, umask)
			c.Compare(t, name, real, got, err, tc.MayRefuse)
		}
	}

	c.Log(t, "zip64 and Deflate64")
}

// TestDifferentialZipManyEntries compares archives of more than 65535
// entries: with a zip64 end record, and without one, where unzip counts
// the entries modulo 65536.
func TestDifferentialZipManyEntries(t *testing.T) {
	skipReference(t)
	unzip := archivetest.NeedInfoZip(t)

	entries := []archivetest.ZipEntry{archivetest.UnixDir("pkg/", 0o755)}
	for i := range 65537 {
		entries = append(entries, archivetest.ZipEntry{Name: fmt.Sprintf("pkg/d%d/f%d", i%100, i), Host: archivetest.HostUnix, HostVer: 30, Attr: 0o100644 << 16})
	}

	var c archivetest.Tally

	for _, tc := range []struct {
		name string
		opts archivetest.ZipOptions
	}{
		{"zip64", archivetest.ZipOptions{End64: true, Saturate: true}},
		{"plain-end", archivetest.ZipOptions{}},
	} {
		path := write(t, "dist.zip", archivetest.ZipWith(tc.opts, "", entries...))
		real := archivetest.Unzip(t, unzip, path, 0o022, "C.UTF-8")

		if !real.OK() {
			t.Errorf("%s: unzip failed: exit %d (%s)", tc.name, real.Exit, strings.TrimSpace(real.Output))
		}

		got, err := extract(path, archive.Zip, &archive.Options{Locale: archive.LocaleUTF8}, 0o022)
		c.Compare(t, tc.name, real, got, err, false)
	}

	c.Log(t, "65537 entries")
}
