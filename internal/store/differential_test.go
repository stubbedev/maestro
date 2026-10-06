//go:build unix

package store

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/archive/archivetest"
)

// methods are the import methods the differential tests run with; Auto
// also covers reflinks where the test directory supports them.
var methods = []Method{Auto, Hardlink, Copy}

// distCase is one archive the store differential tests install.
type distCase struct {
	real      func(t *testing.T, umask int) archivetest.Result
	dist      Dist
	name      string
	path      string
	mayRefuse bool
}

// runStoreDifferential installs every case through one shared store per
// umask and method (so objects are shared and healed across them) and
// compares each package directory with the real extractor's.
func runStoreDifferential(t *testing.T, what string, cases []distCase) {
	t.Helper()

	work := tempDir(t)
	root := filepath.Join(work, "store")

	var c archivetest.Tally

	for _, umask := range archivetest.Umasks {
		old := unix.Umask(umask)

		reals := make([]archivetest.Result, len(cases))
		for i, dc := range cases {
			reals[i] = dc.real(t, umask)
		}

		for _, m := range methods {
			s := openStore(t, root, m)

			for i, dc := range cases {
				name := fmt.Sprintf("%s/umask-%03o/%v", dc.name, umask, m)
				dst := filepath.Join(work, fmt.Sprintf("u%03o-%v", umask, m), strconv.Itoa(i))

				if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
					t.Fatal(err)
				}

				var got archivetest.Tree

				err := s.Install(dc.dist, dc.path, dst, ImportOptions{})
				if err == nil {
					got = snapshot(t, dst)
				}

				c.Compare(t, name, reals[i], got, err, dc.mayRefuse)
			}
		}

		unix.Umask(old)
	}

	s := openStore(t, root, Copy)
	if res, err := s.Verify(); err != nil || res.Corrupt != 0 || res.Missing != 0 || res.Restamped != 0 {
		t.Errorf("store after the differential run: %+v %v", res, err)
	}

	c.Log(t, what)
}

func zipCase(t *testing.T, dir, unzip string, tc archivetest.Case) distCase {
	t.Helper()

	path := writeFile(t, dir, tc.Name+".zip", tc.Data)

	return distCase{
		name: tc.Name,
		path: path,
		dist: Dist{Name: "corpus/" + tc.Name, Type: "zip", URL: "https://example.org/" + tc.Name + ".zip"},
		real: func(t *testing.T, umask int) archivetest.Result {
			t.Helper()
			return archivetest.Unzip(t, unzip, path, umask, "C.UTF-8")
		},
		mayRefuse: tc.MayRefuse,
	}
}

// TestDifferentialStoreZip: extraction, store and import together against
// `unzip -qq` and ArchiveDownloader's single-directory rule, entry by entry.
func TestDifferentialStoreZip(t *testing.T) {
	unzip := archivetest.NeedInfoZip(t)

	dir := tempDir(t)

	var cases []distCase
	for _, tc := range archivetest.ZipCorpus() {
		c := zipCase(t, dir, unzip, tc)
		if archivetest.UnstorableArchive(c.path, archive.Zip) {
			t.Logf("%s: skipped, this file system refuses names that are not UTF-8", tc.Name)
			continue
		}
		cases = append(cases, c)
	}

	runStoreDifferential(t, "zip corpus through the store", cases)
}

// TestDifferentialStoreTar: the tar corpus against PharData (tar.gz) and
// GNU tar (tar.xz).
func TestDifferentialStoreTar(t *testing.T) {
	tar := archivetest.NeedGNUTar(t)
	archivetest.Need(t, "php", "xz")

	dir := tempDir(t)

	var cases []distCase

	for _, tc := range archivetest.TarCorpus() {
		gz := writeFile(t, dir, tc.Name+".tar.gz", archivetest.Gzip(tc.Tar))
		xz := writeFile(t, dir, tc.Name+".tar.xz", archivetest.Xz(tc.Tar))

		cases = append(cases,
			distCase{
				name: tc.Name + ".tar.gz",
				path: gz,
				dist: Dist{Name: "corpus/" + tc.Name, Type: "tar", URL: "https://example.org/" + tc.Name + ".tar.gz"},
				real: func(t *testing.T, umask int) archivetest.Result {
					t.Helper()
					return archivetest.PharData(t, gz, umask)
				},
				mayRefuse: tc.MayRefusePhar,
			},
			distCase{
				name: tc.Name + ".tar.xz",
				path: xz,
				dist: Dist{Name: "corpus/" + tc.Name, Type: "xz", URL: "https://example.org/" + tc.Name + ".tar.xz"},
				real: func(t *testing.T, umask int) archivetest.Result {
					t.Helper()
					return archivetest.TarXz(t, tar, xz, umask)
				},
				mayRefuse: tc.MayRefuseGNU,
			},
		)
	}

	runStoreDifferential(t, "tar corpus through the store", cases)
}

// distsDir holds real package archives fetched by tools/fetchdists.
func distsDir() string {
	if dir := os.Getenv("MAESTRO_TEST_DISTS"); dir != "" {
		return dir
	}

	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}

	return filepath.Join(dir, "maestro-test-dists")
}

// TestDifferentialRealDists runs the differential test over real package
// archives, when tools/fetchdists has fetched them.
func TestDifferentialRealDists(t *testing.T) {
	dir := distsDir()

	names, err := os.ReadDir(dir)
	if err != nil || len(names) == 0 {
		t.Skipf("no real dists in %s (go run ./tools/fetchdists)", dir)
	}

	var cases []distCase

	for _, n := range names {
		path := filepath.Join(dir, n.Name())

		switch {
		case strings.HasSuffix(n.Name(), ".zip"):
			unzip, _ := archivetest.InfoZip()
			if unzip == "" || archivetest.UnstorableArchive(path, archive.Zip) {
				continue
			}

			cases = append(cases, distCase{
				name: n.Name(),
				path: path,
				dist: Dist{Name: "real/" + n.Name(), Type: "zip", URL: path},
				real: func(t *testing.T, umask int) archivetest.Result {
					t.Helper()
					return archivetest.Unzip(t, unzip, path, umask, "C.UTF-8")
				},
			})
		case strings.HasSuffix(n.Name(), ".tar.gz"):
			if _, err := exec.LookPath("php"); err != nil {
				continue
			}

			cases = append(cases, distCase{
				name: n.Name(),
				path: path,
				dist: Dist{Name: "real/" + n.Name(), Type: "tar", URL: path},
				real: func(t *testing.T, umask int) archivetest.Result {
					t.Helper()
					return archivetest.PharData(t, path, umask)
				},
			})
		}
	}

	runStoreDifferential(t, fmt.Sprintf("%d real dists through the store", len(cases)), cases)
}
