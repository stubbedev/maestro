package archiver

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fspath"
)

// Ports tests/Composer/Test/Package/Archiver/ArchivableFilesFinderTest.php.

var archivableFilesFinderTree = []string{
	".foo",
	"A/prefixA.foo",
	"A/prefixB.foo",
	"A/prefixC.foo",
	"A/prefixD.foo",
	"A/prefixE.foo",
	"A/prefixF.foo",
	"B/sub/prefixA.foo",
	"B/sub/prefixB.foo",
	"B/sub/prefixC.foo",
	"B/sub/prefixD.foo",
	"B/sub/prefixE.foo",
	"B/sub/prefixF.foo",
	"C/prefixA.foo",
	"C/prefixB.foo",
	"C/prefixC.foo",
	"C/prefixD.foo",
	"C/prefixE.foo",
	"C/prefixF.foo",
	"D/prefixA",
	"D/prefixB",
	"D/prefixC",
	"D/prefixD",
	"D/prefixE",
	"D/prefixF",
	"E/subtestA.foo",
	"F/subtestA.foo",
	"G/subtestA.foo",
	"H/subtestA.foo",
	"I/J/subtestA.foo",
	"K/dirJ/subtestA.foo",
	"toplevelA.foo",
	"toplevelB.foo",
	"prefixA.foo",
	"prefixB.foo",
	"prefixC.foo",
	"prefixD.foo",
	"prefixE.foo",
	"prefixF.foo",
	"parameters.yml",
	"parameters.yml.dist",
	"!important!.txt",
	"!important_too!.txt",
	"#weirdfile",
}

// archivableFilesFinderSetUp is the test case's setUp: the tree of empty
// files in a unique temporary directory.
func archivableFilesFinderSetUp(t *testing.T) string {
	t.Helper()

	sources := uniqueTmpDirectory(t)

	for _, relativePath := range archivableFilesFinderTree {
		path := sources + "/" + relativePath
		if err := util.EnsureDirectoryExists(php.Dirname(path)); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return sources
}

// uniqueTmpDirectory is TestCase::getUniqueTmpDirectory: a new directory,
// by its real path.
func uniqueTmpDirectory(t *testing.T) string {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return fspath.NormalizePath(dir)
}

// archivableFiles is the test case's getArchivableFiles: the finder's
// files by their real paths relative to sources, sorted.
func archivableFiles(t *testing.T, sources string, excludes []string, ignoreFilters bool) []string {
	t.Helper()

	finder, err := NewArchivableFilesFinder(sources, excludes, ignoreFilters)
	if err != nil {
		t.Fatal(err)
	}

	var files []string

	for _, f := range finder.Files() {
		if !f.IsDir {
			files = append(files, strings.TrimPrefix(fspath.NormalizePath(f.RealPath), sources))
		}
	}

	slices.Sort(files)

	return files
}

func assertArchivableFiles(t *testing.T, want, got []string) {
	t.Helper()

	if !slices.Equal(want, got) {
		t.Errorf("archivable files\n got %q\nwant %q", got, want)
	}
}

func TestArchivableFilesFinder_ManualExcludes(t *testing.T) {
	sources := archivableFilesFinderSetUp(t)

	excludes := []string{
		"prefixB.foo",
		"!/prefixB.foo",
		"/prefixA.foo",
		"prefixC.*",
		"!*/*/*/prefixC.foo",
		".*",
	}

	assertArchivableFiles(t, []string{
		"/!important!.txt",
		"/!important_too!.txt",
		"/#weirdfile",
		"/A/prefixA.foo",
		"/A/prefixD.foo",
		"/A/prefixE.foo",
		"/A/prefixF.foo",
		"/B/sub/prefixA.foo",
		"/B/sub/prefixC.foo",
		"/B/sub/prefixD.foo",
		"/B/sub/prefixE.foo",
		"/B/sub/prefixF.foo",
		"/C/prefixA.foo",
		"/C/prefixD.foo",
		"/C/prefixE.foo",
		"/C/prefixF.foo",
		"/D/prefixA",
		"/D/prefixB",
		"/D/prefixC",
		"/D/prefixD",
		"/D/prefixE",
		"/D/prefixF",
		"/E/subtestA.foo",
		"/F/subtestA.foo",
		"/G/subtestA.foo",
		"/H/subtestA.foo",
		"/I/J/subtestA.foo",
		"/K/dirJ/subtestA.foo",
		"/parameters.yml",
		"/parameters.yml.dist",
		"/prefixB.foo",
		"/prefixD.foo",
		"/prefixE.foo",
		"/prefixF.foo",
		"/toplevelA.foo",
		"/toplevelB.foo",
	}, archivableFiles(t, sources, excludes, false))
}

func TestArchivableFilesFinder_GitExcludes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not executable")
	}

	sources := archivableFilesFinderSetUp(t)

	if err := os.WriteFile(sources+"/.gitattributes", []byte(strings.Join([]string{
		"",
		"# gitattributes rules with comments and blank lines",
		"prefixB.foo export-ignore",
		"/prefixA.foo export-ignore",
		"prefixC.* export-ignore",
		"",
		"prefixE.foo export-ignore",
		"# and more",
		"# comments",
		"",
		"/prefixE.foo -export-ignore",
		"/prefixD.foo export-ignore",
		"prefixF.* export-ignore",
		"/*/*/prefixF.foo -export-ignore",
		"",
		"refixD.foo export-ignore",
		"/C export-ignore",
		"D/prefixA export-ignore",
		"E export-ignore",
		"F/ export-ignore",
		"G/* export-ignore",
		"H/** export-ignore",
		"J/ export-ignore",
		"parameters.yml export-ignore",
		`\!important!.txt export-ignore`,
		`\#* export-ignore`,
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	archived := archivedFiles(t, sources,
		`git init && `+
			`git config user.email "you@example.com" && `+
			`git config user.name "Your Name" && `+
			`git config commit.gpgsign false && `+
			`git add .git* && `+
			`git commit -m "ignore rules" && `+
			`git add . && `+
			`git commit -m "init" && `+
			`git archive --format=zip --prefix=archive/ -o archive.zip HEAD`)

	assertArchivableFiles(t, archived, archivableFiles(t, sources, nil, false))
}

func TestArchivableFilesFinder_SkipExcludes(t *testing.T) {
	sources := archivableFilesFinderSetUp(t)

	assertArchivableFiles(t, []string{
		"/!important!.txt",
		"/!important_too!.txt",
		"/#weirdfile",
		"/.foo",
		"/A/prefixA.foo",
		"/A/prefixB.foo",
		"/A/prefixC.foo",
		"/A/prefixD.foo",
		"/A/prefixE.foo",
		"/A/prefixF.foo",
		"/B/sub/prefixA.foo",
		"/B/sub/prefixB.foo",
		"/B/sub/prefixC.foo",
		"/B/sub/prefixD.foo",
		"/B/sub/prefixE.foo",
		"/B/sub/prefixF.foo",
		"/C/prefixA.foo",
		"/C/prefixB.foo",
		"/C/prefixC.foo",
		"/C/prefixD.foo",
		"/C/prefixE.foo",
		"/C/prefixF.foo",
		"/D/prefixA",
		"/D/prefixB",
		"/D/prefixC",
		"/D/prefixD",
		"/D/prefixE",
		"/D/prefixF",
		"/E/subtestA.foo",
		"/F/subtestA.foo",
		"/G/subtestA.foo",
		"/H/subtestA.foo",
		"/I/J/subtestA.foo",
		"/K/dirJ/subtestA.foo",
		"/parameters.yml",
		"/parameters.yml.dist",
		"/prefixA.foo",
		"/prefixB.foo",
		"/prefixC.foo",
		"/prefixD.foo",
		"/prefixE.foo",
		"/prefixF.foo",
		"/toplevelA.foo",
		"/toplevelB.foo",
	}, archivableFiles(t, sources, []string{"prefixB.foo"}, true))
}

// archivedFiles is the test case's getArchivedFiles: it runs command in
// sources and lists the files of the archive.zip it makes, relative to
// its archive/ prefix, then deletes the archive.
func archivedFiles(t *testing.T, sources, command string) []string {
	t.Helper()

	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = sources
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "HOME="+t.TempDir())

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", command, err, out)
	}

	r, err := zip.OpenReader(sources + "/archive.zip")
	if err != nil {
		t.Fatal(err)
	}

	var files []string

	for _, f := range r.File {
		if !strings.HasSuffix(f.Name, "/") {
			files = append(files, strings.TrimPrefix(f.Name, "archive"))
		}
	}

	_ = r.Close()

	// phar lists each directory's entries sorted
	slices.Sort(files)

	if err := os.Remove(sources + "/archive.zip"); err != nil {
		t.Fatal(err)
	}

	return files
}

// Finder's exceptions: DirectoryNotFoundException for a missing directory, and
// for an unreadable subdirectory the AccessDeniedException Symfony's
// RecursiveDirectoryIterator::getChildren() throws (line 127) around the SPL
// UnexpectedValueException of its constructor (line 48), as PHP 8.4 reports
// them for Finder::create()->in($dir).
func TestArchivableFilesFinderExceptions(t *testing.T) {
	// The finder works on the real path (macOS's temp dir is under the
	// /var -> /private/var symlink), and so do PHP's messages.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	_, err = findFiles(dir+"/missing", nil)
	if class, _ := phperr.ClassOf(err); class != util.ClassDirectoryNotFound {
		t.Errorf("missing source: %s %v, want %s", class, err, util.ClassDirectoryNotFound)
	}

	locked := filepath.Join(dir, "sub", "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("the directory is readable anyway (running as root)")
	}

	_, err = NewArchivableFilesFinder(dir, nil, false)
	if class, _ := phperr.ClassOf(err); class != util.ClassAccessDenied {
		t.Fatalf("unreadable subdirectory: %s %v, want %s", class, err, util.ClassAccessDenied)
	}
	want := "RecursiveDirectoryIterator::__construct(" + locked + "): Failed to open directory: Permission denied"
	if err.Error() != want {
		t.Errorf("message %q, want %q", err.Error(), want)
	}
	prev := phperr.PreviousOf(err)
	if class, _ := phperr.ClassOf(prev); class != "UnexpectedValueException" {
		t.Errorf("previous %s %v, want UnexpectedValueException", class, prev)
	}
}
