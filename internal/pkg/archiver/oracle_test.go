package archiver

import (
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/testutil"
	"github.com/stubbedev/maestro/internal/util"
)

// The goldens come from tools/oracle/archiver/archiver.php.

type oracleEntry struct {
	Path    string
	Type    string
	Content string // base64 for files, the target for links
	Mode    uint32
	Mtime   int64
}

func (e *oracleEntry) UnmarshalJSON(data []byte) error {
	var raw []any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	e.Path, _ = raw[0].(string)
	e.Type, _ = raw[1].(string)
	e.Content, _ = raw[2].(string)
	e.Mode = uint32(raw[3].(float64))
	e.Mtime = int64(raw[4].(float64))

	return nil
}

type oracleResult struct {
	OK    *string           `json:"ok"`
	E     []string          `json:"e"`
	Files map[string]string `json:"files"`
	Bytes string            `json:"bytes"`
}

type oracleGolden struct {
	Cases []struct {
		Tree     []oracleEntry `json:"tree"`
		Excludes []string      `json:"excludes"`
		Ignore   bool          `json:"ignore"`
		Finder   struct {
			OK []json.RawMessage `json:"ok"`
			E  []string          `json:"e"`
		} `json:"finder"`
		Phar map[string]oracleResult `json:"phar"`
		Zip  oracleResult            `json:"zip"`
	} `json:"cases"`
	Targets []struct {
		Name   string       `json:"name"`
		Format string       `json:"format"`
		Result oracleResult `json:"result"`
	} `json:"targets"`
	Manager []struct {
		Zip             bool          `json:"zip"`
		ArchiveName     *string       `json:"archiveName"`
		Excludes        []string      `json:"excludes"`
		SourceReference *string       `json:"sourceReference"`
		PrettyVersion   string        `json:"prettyVersion"`
		FileName        *string       `json:"fileName"`
		Format          string        `json:"format"`
		TargetDir       string        `json:"targetDir"`
		Tree            []oracleEntry `json:"tree"`
		Result          oracleResult  `json:"result"`
	} `json:"manager"`
	Parts []struct {
		Name            string          `json:"name"`
		Version         string          `json:"version"`
		ArchiveName     *string         `json:"archiveName"`
		DistReference   *string         `json:"distReference"`
		DistType        *string         `json:"distType"`
		SourceReference *string         `json:"sourceReference"`
		Parts           json.RawMessage `json:"parts"`
		Filename        string          `json:"filename"`
	} `json:"parts"`
}

func loadOracle(t *testing.T) *oracleGolden {
	t.Helper()

	var g oracleGolden
	testutil.LoadJSONGolden(t, "testdata/oracle/archiver.json.gz", &g)

	return &g
}

// skipLocked reports whether a tree holds a file nobody may read, which
// root reads anyway.
func skipLocked(t *testing.T, tree []oracleEntry) bool {
	t.Helper()

	if os.Geteuid() != 0 {
		return false
	}

	for _, e := range tree {
		if e.Type == "file" && e.Mode&0o444 == 0 {
			return true
		}
	}

	return false
}

// makeOracleTree recreates the oracle's work directory layout: work/pkg
// holding the tree, beside work/outside and work/pkg2.
func makeOracleTree(t *testing.T, tree []oracleEntry) string {
	t.Helper()

	// The oracle's work directory had no symlink in its path; the
	// temporary directory may (/var -> /private/var on macOS), which
	// realpath() would resolve in some messages and not in others.
	work, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"pkg", "outside", "pkg2"} {
		if err := os.Mkdir(work+"/"+dir, 0o777); err != nil {
			t.Fatal(err)
		}
	}

	mustWrite(t, work+"/outside/file", "outside")
	mustWrite(t, work+"/pkg2/file", "pkg2")

	var dirs []oracleEntry

	for _, e := range tree {
		full := work + "/pkg/" + e.Path

		switch e.Type {
		case "file":
			content, err := base64.StdEncoding.DecodeString(e.Content)
			if err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(full, content, 0o600); err != nil {
				t.Fatal(err)
			}

			setModeAndTime(t, full, e)
		case "dir":
			if err := os.Mkdir(full, 0o700); err != nil {
				t.Fatal(err)
			}

			dirs = append(dirs, e)
		default:
			if err := os.Symlink(e.Content, full); err != nil {
				t.Fatal(err)
			}
		}
	}

	for _, dir := range slices.Backward(dirs) {
		setModeAndTime(t, work+"/pkg/"+dir.Path, dir)
	}

	// the tree's directories may not be writable
	t.Cleanup(func() {
		_ = filepath.WalkDir(work, func(path string, d os.DirEntry, _ error) error {
			if d != nil && d.IsDir() {
				_ = os.Chmod(path, 0o700)
			}

			return nil
		})
	})

	return work
}

func setModeAndTime(t *testing.T, path string, e oracleEntry) {
	t.Helper()

	mode := os.FileMode(e.Mode & 0o777)
	if e.Mode&0o4000 != 0 {
		mode |= os.ModeSetuid
	}

	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}

	mtime := time.Unix(e.Mtime, 0)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// phpClass names the PHP exception class of an error.
func phpClass(err error) string {
	switch err.(type) { //nolint:errorlint // exact classes
	case *util.RuntimeError:
		return "RuntimeException"
	case *util.ErrorException:
		return "ErrorException"
	case *util.InvalidArgumentError:
		return "InvalidArgumentException"
	case *util.UnexpectedValueError:
		return "UnexpectedValueException"
	case *BadMethodCallError:
		return "BadMethodCallException"
	case *PharError:
		return "PharException"
	case *pkg.TypeError:
		return "TypeError"
	}

	return "?" + err.Error()
}

// checkResult compares a call's outcome with the golden's.
func checkResult(t *testing.T, label, work string, want oracleResult, got string, err error) {
	t.Helper()

	switch {
	case err != nil && want.E == nil:
		t.Errorf("%s: error %v, want %q", label, err, *want.OK)
	case err != nil:
		if gotE := []string{phpClass(err), strings.ReplaceAll(err.Error(), work, "<W>")}; gotE[0] != want.E[0] || gotE[1] != want.E[1] {
			t.Errorf("%s: error\n got %q\nwant %q", label, gotE, want.E)
		}
	case want.E != nil:
		t.Errorf("%s: got %q, want error %q", label, got, want.E)
	case strings.ReplaceAll(got, work, "<W>") != *want.OK:
		t.Errorf("%s: got %q, want %q", label, got, *want.OK)
	}
}

// checkOutFiles compares the files written into work/out (and
// work/out/dir.d) with the golden's.
func checkOutFiles(t *testing.T, label, work string, want map[string]string, links map[string]bool) {
	t.Helper()

	got := map[string][]byte{}

	for _, dir := range []string{"", "dir.d/"} {
		entries, _ := os.ReadDir(work + "/out/" + dir)
		for _, e := range entries {
			if !e.IsDir() {
				data, err := os.ReadFile(work + "/out/" + dir + e.Name())
				if err != nil {
					t.Fatal(err)
				}

				got[dir+e.Name()] = data
			}
		}
	}

	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: unexpected file %s", label, name)
		}
	}

	for name, b64 := range want {
		data, ok := got[name]
		if !ok {
			t.Errorf("%s: missing file %s", label, name)

			continue
		}

		wantData, _ := base64.StdEncoding.DecodeString(b64)
		compareArchives(t, label+" "+name, wantData, data, links)
	}
}

// compareArchives compares two archives as written by the same PHP code:
// tar and phar zip archives byte for byte with their entry times zeroed,
// compressed archives by their headers and contents, libzip archives
// entry by entry (Go's deflate output differs from zlib's).
// links names the symbolic links of the tree: libzip gives their entries
// their targets' times, which the oracle created when it ran.
func compareArchives(t *testing.T, label string, want, got []byte, links map[string]bool) {
	t.Helper()

	switch {
	case bytes.HasPrefix(want, []byte{0x1f, 0x8b}):
		if !bytes.Equal(want[:10], got[:min(10, len(got))]) {
			t.Errorf("%s: gzip header %x, want %x", label, got[:min(10, len(got))], want[:10])
		}

		compareArchives(t, label, decompress(t, label, want), decompress(t, label, got), links)
	case bytes.HasPrefix(want, []byte{0x78}):
		if !bytes.Equal(want[:2], got[:min(2, len(got))]) {
			t.Errorf("%s: zlib header %x, want %x", label, got[:min(2, len(got))], want[:2])
		}

		compareArchives(t, label, decompress(t, label, want), decompress(t, label, got), links)
	case bytes.HasPrefix(want, []byte("PK\x03\x04")) && binary.LittleEndian.Uint16(want[4:]) == libzipNeeded:
		compareLibzip(t, label, want, got, links)
	case bytes.HasPrefix(want, []byte("PK")):
		if w, g := normalizePharZip(want), normalizePharZip(got); !bytes.Equal(w, g) {
			t.Errorf("%s: zip differs: %s", label, firstDiff(w, g))
		}
	default:
		if w, g := normalizeTar(want), normalizeTar(got); !bytes.Equal(w, g) {
			t.Errorf("%s: tar differs: %s", label, firstDiff(w, g))
		}
	}
}

func decompress(t *testing.T, label string, data []byte) []byte {
	t.Helper()

	var (
		r   io.Reader
		err error
	)

	switch {
	case bytes.HasPrefix(data, []byte{0x1f, 0x8b}):
		r, err = gzip.NewReader(bytes.NewReader(data))
	case bytes.HasPrefix(data, []byte("BZh")):
		r = bzip2.NewReader(bytes.NewReader(data))
	default:
		r, err = zlib.NewReader(bytes.NewReader(data))
	}

	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}

	return out
}

// normalizeTar zeroes the mtime of each header of a phar tar and
// recomputes its checksum.
func normalizeTar(data []byte) []byte {
	data = bytes.Clone(data)

	for off := 0; off+512 <= len(data); {
		h := data[off : off+512]
		if bytes.Equal(h, make([]byte, 512)) {
			break
		}

		size, _ := strconv.ParseInt(strings.TrimRight(string(h[124:135]), "\x00"), 8, 64)
		tarOctal(h[136:147], 0)
		copy(h[148:156], "        ")

		var sum int64
		for _, b := range h {
			sum += int64(b)
		}

		tarOctal(h[148:155], sum)

		off += 512 + int((size+511)/512*512)
	}

	return data
}

// normalizePharZip zeroes the DOS times of a zip's local and central
// headers.
func normalizePharZip(data []byte) []byte {
	data = bytes.Clone(data)
	le := binary.LittleEndian

	for off := 0; off+4 <= len(data); {
		switch {
		case bytes.HasPrefix(data[off:], []byte("PK\x03\x04")):
			copy(data[off+10:off+14], []byte{0, 0, 0, 0})
			off += 30 + int(le.Uint16(data[off+26:])) + int(le.Uint16(data[off+28:])) + int(le.Uint32(data[off+18:]))
		case bytes.HasPrefix(data[off:], []byte("PK\x01\x02")):
			copy(data[off+12:off+16], []byte{0, 0, 0, 0})
			off += 46 + int(le.Uint16(data[off+28:])) + int(le.Uint16(data[off+30:])) + int(le.Uint16(data[off+32:]))
		default:
			return data
		}
	}

	return data
}

// compareLibzip compares ZipArchive's archives entry by entry; directory
// entries have the time of archiving.
func compareLibzip(t *testing.T, label string, want, got []byte, links map[string]bool) {
	t.Helper()

	wr, err := zip.NewReader(bytes.NewReader(want), int64(len(want)))
	if err != nil {
		t.Fatal(err)
	}

	gr, err := zip.NewReader(bytes.NewReader(got), int64(len(got)))
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}

	if len(wr.File) != len(gr.File) {
		t.Errorf("%s: %d entries, want %d", label, len(gr.File), len(wr.File))

		return
	}

	for i, w := range wr.File {
		g := gr.File[i]
		wh, gh := w.FileHeader, g.FileHeader

		//nolint:staticcheck // the DOS fields are what libzip writes
		if !strings.HasSuffix(wh.Name, "/") && !links[wh.Name] && (wh.ModifiedDate != gh.ModifiedDate || wh.ModifiedTime != gh.ModifiedTime) {
			t.Errorf("%s: %s time %d %d, want %d %d", label, wh.Name, gh.ModifiedDate, gh.ModifiedTime, wh.ModifiedDate, wh.ModifiedTime) //nolint:staticcheck // as above
		}

		wh.ModifiedDate, wh.ModifiedTime, gh.ModifiedDate, gh.ModifiedTime = 0, 0, 0, 0 //nolint:staticcheck // as above
		wh.Modified, gh.Modified = time.Time{}, time.Time{}
		wh.CompressedSize64, gh.CompressedSize64 = 0, 0
		wh.CompressedSize, gh.CompressedSize = 0, 0 //nolint:staticcheck // compared fields

		if wh.Method != gh.Method {
			t.Logf("%s: %s method %d, want %d (deflate output differs)", label, wh.Name, gh.Method, wh.Method)

			wh.Method, gh.Method = 0, 0
			wh.Flags &^= libzipFlagMaxComp
			gh.Flags &^= libzipFlagMaxComp
		}

		if wjs, gjs := mustJSON(t, wh), mustJSON(t, gh); wjs != gjs {
			t.Errorf("%s: entry %d\n got %s\nwant %s", label, i, gjs, wjs)
		}

		if wc, gc := readZipFile(t, w), readZipFile(t, g); !bytes.Equal(wc, gc) {
			t.Errorf("%s: %s content differs", label, wh.Name)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()

	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

func readZipFile(t *testing.T, f *zip.File) []byte {
	t.Helper()

	r, err := f.Open()
	if err != nil {
		t.Fatal(err)
	}

	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

// archiveIntoOut runs an archiver like the oracle's archiveWith: a fresh
// copy of the tree, archived into work/out.
func archiveIntoOut(t *testing.T, tree []oracleEntry, run func(work string) (string, error)) (string, string, error) {
	t.Helper()

	work := makeOracleTree(t, tree)
	if err := os.MkdirAll(work+"/out/dir.d", 0o777); err != nil {
		t.Fatal(err)
	}

	got, err := run(work)

	return work, got, err
}

func TestOracle_Archivers(t *testing.T) {
	g := loadOracle(t)

	for i, c := range g.Cases {
		if skipLocked(t, c.Tree) {
			continue
		}

		label := "case " + strconv.Itoa(i)

		work := makeOracleTree(t, c.Tree)

		finder, err := NewArchivableFilesFinder(work+"/pkg", c.Excludes, c.Ignore)
		if err != nil {
			if c.Finder.E == nil || phpClass(err) != c.Finder.E[0] || strings.ReplaceAll(err.Error(), work, "<W>") != c.Finder.E[1] {
				t.Errorf("%s: finder error %v, want %q", label, err, c.Finder.E)
			}
		} else {
			got := []json.RawMessage{}
			for _, f := range finder.Files() {
				got = append(got, json.RawMessage(mustJSON(t, []any{f.RelativePathname, f.IsDir})))
			}

			if mustJSON(t, got) != mustJSON(t, c.Finder.OK) {
				t.Errorf("%s: finder\n got %s\nwant %s", label, mustJSON(t, got), mustJSON(t, c.Finder.OK))
			}
		}

		for _, format := range []string{"tar", "zip", "tar.gz"} {
			want := c.Phar[format]
			work, got, err := archiveIntoOut(t, c.Tree, func(work string) (string, error) {
				return NewPharArchiver().Archive(work+"/pkg", work+"/out/p."+format, format, c.Excludes, c.Ignore)
			})
			checkResult(t, label+" phar "+format, work, want, got, err)
			checkOutFiles(t, label+" phar "+format, work, want.Files, nil)
		}

		work, got, err := archiveIntoOut(t, c.Tree, func(work string) (string, error) {
			return NewZipArchiver().Archive(work+"/pkg", work+"/out/z.zip", "zip", c.Excludes, c.Ignore)
		})
		checkResult(t, label+" zip", work, c.Zip, got, err)
		checkOutFiles(t, label+" zip", work, c.Zip.Files, linkNames(c.Tree))
	}
}

func TestOracle_PharTargets(t *testing.T) {
	g := loadOracle(t)

	tree := []oracleEntry{{"a", "file", "eA==", 0o644, 1600000000}, {"d", "dir", "", 0o755, 1600000000}, {"d/b", "file", "eXk=", 0o600, 1600000000}, {"e", "dir", "", 0o755, 1600000000}}

	for _, c := range g.Targets {
		label := c.Name + " " + c.Format
		work, got, err := archiveIntoOut(t, tree, func(work string) (string, error) {
			return NewPharArchiver().Archive(work+"/pkg", work+"/out/"+c.Name, c.Format, nil, false)
		})

		if c.Format == "tar.bz2" {
			// PHP without bz2 cannot compress (deviation: this port can)
			if err != nil || got != work+"/out/p.tar.bz2" {
				t.Errorf("%s: %q, %v", label, got, err)
			}

			data, _ := os.ReadFile(got)
			want, _ := base64.StdEncoding.DecodeString(c.Result.Files["p.tar"])
			compareArchives(t, label, want, decompress(t, label, data), nil)

			continue
		}

		checkResult(t, label, work, c.Result, got, err)
		checkOutFiles(t, label, work, c.Result.Files, nil)
	}
}

func TestOracle_ArchiveManagerRootPackage(t *testing.T) {
	g := loadOracle(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = os.Chdir(cwd) }()

	for i, c := range g.Manager {
		label := "manager " + strconv.Itoa(i)

		m := NewArchiveManager(nil, nil)
		if c.Zip {
			m.AddArchiver(NewZipArchiver())
		}

		m.AddArchiver(NewPharArchiver())

		p := pkg.NewRootPackage("vendor/name", "1.0.0.0", c.PrettyVersion)
		if c.ArchiveName != nil {
			p.SetArchiveName(pkg.Str(*c.ArchiveName))
		}

		excludes := php.NewArray()
		for _, e := range c.Excludes {
			excludes.Append(e)
		}

		p.SetArchiveExcludes(excludes)

		if c.SourceReference != nil {
			p.SetSourceReference(pkg.Str(*c.SourceReference))
		}

		fileName := pkg.NullString{}
		if c.FileName != nil {
			fileName = pkg.Str(*c.FileName)
		}

		work := makeOracleTree(t, c.Tree)
		if err := os.Chdir(work + "/pkg"); err != nil {
			t.Fatal(err)
		}

		got, err := m.Archive(p, c.Format, c.TargetDir, fileName, false)
		checkResult(t, label, work, c.Result, got, err)

		if err == nil && c.Result.OK != nil {
			data, rerr := os.ReadFile(got)
			if rerr != nil {
				t.Fatal(rerr)
			}

			want, _ := base64.StdEncoding.DecodeString(c.Result.Bytes)
			compareArchives(t, label, want, data, nil)
		}
	}
}

func TestOracle_PackageFilenameParts(t *testing.T) {
	g := loadOracle(t)
	m := NewArchiveManager(nil, nil)

	nullable := func(s *string) pkg.NullString {
		if s == nil {
			return pkg.NullString{}
		}

		return pkg.Str(*s)
	}

	for _, c := range g.Parts {
		p := pkg.NewCompletePackage(c.Name, "1.0.0.0", c.Version)
		if c.ArchiveName != nil {
			p.SetArchiveName(pkg.Str(*c.ArchiveName))
		}

		p.SetDistReference(nullable(c.DistReference))
		p.SetDistType(nullable(c.DistType))
		p.SetSourceReference(nullable(c.SourceReference))

		var want []FilenamePart

		dec := json.NewDecoder(bytes.NewReader(c.Parts))
		_, _ = dec.Token()

		for dec.More() {
			k, _ := dec.Token()
			v, _ := dec.Token()
			want = append(want, FilenamePart{Key: k.(string), Value: v.(string)})
		}

		if got := m.PackageFilenameParts(p); mustJSON(t, got) != mustJSON(t, want) {
			t.Errorf("%+v: parts\n got %v\nwant %v", c, got, want)
		}

		if got := m.PackageFilename(p); got != c.Filename {
			t.Errorf("%+v: filename %q, want %q", c, got, c.Filename)
		}
	}
}

// firstDiff describes where two byte strings first differ.
func firstDiff(want, got []byte) string {
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}

	start := max(i-16, 0)

	return "at " + strconv.Itoa(i) + " (lengths " + strconv.Itoa(len(got)) + ", want " + strconv.Itoa(len(want)) + ")\n got " +
		strconv.Quote(string(got[start:min(i+48, len(got))])) + "\nwant " + strconv.Quote(string(want[start:min(i+48, len(want))]))
}

// linkNames returns the paths of a tree's symbolic links.
func linkNames(tree []oracleEntry) map[string]bool {
	links := map[string]bool{}

	for _, e := range tree {
		if e.Type == "link" {
			links[e.Path] = true
		}
	}

	return links
}
