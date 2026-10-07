package archiver

import (
	"archive/zip"
	"io"
	"maps"
	"os"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// Ports tests/Composer/Test/Package/Archiver/PharArchiverTest.php and
// ZipArchiverTest.php (ArchiverTestCase's setupPackage only supplies the
// test directory as the source URL).

// writeDummyFiles is the test cases' setupDummyRepo/writeFile.
func writeDummyFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()

	for path, content := range files {
		if err := util.EnsureDirectoryExists(php.Dirname(dir + "/" + path)); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(dir+"/"+path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

var pharDummyRepo = map[string]string{
	"file.txt":         "content",
	"foo/bar/baz":      "content",
	"foo/bar/ignoreme": "content",
	"x/baz":            "content",
	"x/includeme":      "content",
}

func TestPharArchiver_TarArchive(t *testing.T) {
	testDir := uniqueTmpDirectory(t)
	writeDummyFiles(t, testDir, pharDummyRepo)

	target := uniqueTmpDirectory(t) + "/composer_archiver_test.tar"

	if _, err := NewPharArchiver().Archive(testDir, target, "tar", []string{"foo/bar", "baz", "!/foo/bar/baz"}, false); err != nil {
		t.Fatal(err)
	}

	if !php.FileExists(target) {
		t.Fatalf("%s does not exist", target)
	}
}

func TestPharArchiver_ZipArchive(t *testing.T) {
	testDir := uniqueTmpDirectory(t)
	writeDummyFiles(t, testDir, pharDummyRepo)

	target := uniqueTmpDirectory(t) + "/composer_archiver_test.zip"

	if _, err := NewPharArchiver().Archive(testDir, target, "zip", nil, false); err != nil {
		t.Fatal(err)
	}

	if !php.FileExists(target) {
		t.Fatalf("%s does not exist", target)
	}
}

func TestZipArchiver_SimpleFiles(t *testing.T) {
	cwd, err := util.GetCwd(false)
	if err != nil {
		t.Fatal(err)
	}

	order := []string{"file.txt", "foo/bar/baz", "x/baz", "x/includeme"}
	files := map[string]string{
		"file.txt":    "content",
		"foo/bar/baz": "content",
		"x/baz":       "content",
		"x/includeme": "content",
	}
	// A drive's colon cannot be in a file name on Windows.
	if !util.IsWindows() {
		order = append(order, "zfoo"+cwd+"/file.txt")
		files["zfoo"+cwd+"/file.txt"] = "content"
	}

	assertZipArchive(t, order, files)
}

func TestZipArchiver_GitignoreExcludeNegation(t *testing.T) {
	for _, include := range []string{"!/docs", "!/docs/"} {
		assertZipArchive(t, []string{".gitignore", "docs/README.md"}, map[string]string{
			".gitignore":     "/*\n.*\n!.git*\n" + include,
			"docs/README.md": "# The doc",
		})
	}
}

func TestZipArchiver_FolderWithBackslashes(t *testing.T) {
	if util.IsWindows() {
		t.Skip("Folder names cannot contain backslashes on Windows.")
	}

	assertZipArchive(t, []string{`folder\with\backslashes/README.md`}, map[string]string{
		`folder\with\backslashes/README.md`: "# doc",
	})
}

// assertZipArchive archives files with the ZipArchiver and checks that the
// zip holds exactly them, in order.
func assertZipArchive(t *testing.T, order []string, files map[string]string) {
	t.Helper()

	testDir := uniqueTmpDirectory(t)
	writeDummyFiles(t, testDir, files)

	target := t.TempDir() + "/composer_archiver_test.zip"

	if _, err := NewZipArchiver().Archive(testDir, target, "zip", nil, false); err != nil {
		t.Fatal(err)
	}

	r, err := zip.OpenReader(target)
	if err != nil {
		t.Fatalf("Failed asserting that Zip file can be opened: %v", err)
	}

	defer r.Close()

	var names []string

	contents := map[string]string{}

	for _, f := range r.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}

		data, err := io.ReadAll(rc)
		_ = rc.Close()

		if err != nil {
			t.Fatal(err)
		}

		names = append(names, f.Name)
		contents[f.Name] = string(data)
	}

	if !slices.Equal(names, order) || !maps.Equal(contents, files) {
		t.Errorf("Failed asserting that Zip created with the ZipArchiver contains all files from the repository.\n got %q %q\nwant %q %q", names, contents, order, files)
	}
}

func TestStatPerms(t *testing.T) {
	for _, c := range []struct {
		path    string
		mode    os.FileMode
		windows bool
		want    os.FileMode
	}{
		{`C:\p\vendor\bin\phpunit.bat`, 0o666, true, 0o777},
		{`C:\p\tool.EXE`, 0o444, true, 0o555},
		{`C:\p\a.cmd`, 0o666, true, 0o777},
		{`C:\p\a.com`, 0o666, true, 0o777},
		{`C:\p\vendor\bin\phpunit`, 0o666, true, 0o666},
		{`C:\p\bat`, 0o666, true, 0o666},
		{`C:\p\x.bats`, 0o666, true, 0o666},
		{`C:\p\dir.bat`, os.ModeDir | 0o777, true, 0o777},
		{"/p/vendor/bin/phpunit.bat", 0o644, false, 0o644},
	} {
		if got := statPerms(c.path, c.mode, c.windows); got != c.want {
			t.Errorf("statPerms(%q, %v, %v) = %v, want %v", c.path, c.mode, c.windows, got, c.want)
		}
	}
}
