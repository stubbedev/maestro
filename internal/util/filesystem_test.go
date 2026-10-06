package util

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Ports tests/Composer/Test/Util/FilesystemTest.php.

func TestFilesystem_FindShortestPathCode(t *testing.T) {
	cases := []struct {
		a, b           string
		directory      bool
		expected       string
		static         bool
		preferRelative bool
	}{
		{"/foo/bar", "/foo/bar", false, "__FILE__", false, false},
		{"/foo/bar", "/foo/baz", false, "__DIR__.'/baz'", false, false},
		{"/foo/bin/run", "/foo/vendor/acme/bin/run", false, "dirname(__DIR__).'/vendor/acme/bin/run'", false, false},
		{"/foo/bin/run", "/bar/bin/run", false, "'/bar/bin/run'", false, false},
		{"c:/bin/run", "c:/vendor/acme/bin/run", false, "dirname(__DIR__).'/vendor/acme/bin/run'", false, false},
		{`c:\bin\run`, "c:/vendor/acme/bin/run", false, "dirname(__DIR__).'/vendor/acme/bin/run'", false, false},
		{"c:/bin/run", "D:/vendor/acme/bin/run", false, "'D:/vendor/acme/bin/run'", false, false},
		{`c:\bin\run`, "d:/vendor/acme/bin/run", false, "'D:/vendor/acme/bin/run'", false, false},
		{"/foo/bar", "/foo/bar", true, "__DIR__", false, false},
		{"/foo/bar/", "/foo/bar", true, "__DIR__", false, false},
		{"/foo", "/baz", true, "dirname(__DIR__).'/baz'", false, false},
		{"/foo/bar", "/foo/baz", true, "dirname(__DIR__).'/baz'", false, false},
		{"/foo/bin/run", "/foo/vendor/acme/bin/run", true, "dirname(dirname(__DIR__)).'/vendor/acme/bin/run'", false, false},
		{"/foo/bin/run", "/bar/bin/run", true, "'/bar/bin/run'", false, false},
		{"/app/vendor/foo/bar", "/lib", true, "dirname(dirname(dirname(dirname(__DIR__)))).'/lib'", false, true},
		{"/bin/run", "/bin/run", true, "__DIR__", false, false},
		{"c:/bin/run", `C:\bin/run`, true, "__DIR__", false, false},
		{"c:/bin/run", "c:/vendor/acme/bin/run", true, "dirname(dirname(__DIR__)).'/vendor/acme/bin/run'", false, false},
		{`c:\bin\run`, "c:/vendor/acme/bin/run", true, "dirname(dirname(__DIR__)).'/vendor/acme/bin/run'", false, false},
		{"c:/bin/run", "d:/vendor/acme/bin/run", true, "'D:/vendor/acme/bin/run'", false, false},
		{`c:\bin\run`, "d:/vendor/acme/bin/run", true, "'D:/vendor/acme/bin/run'", false, false},
		{"C:/Temp/test", `C:\Temp`, true, "dirname(__DIR__)", false, false},
		{"C:/Temp", `C:\Temp\test`, true, "__DIR__ . '/test'", false, false},
		{"/tmp/test", "/tmp", true, "dirname(__DIR__)", false, false},
		{"/tmp", "/tmp/test", true, "__DIR__ . '/test'", false, false},
		{"C:/Temp", `c:\Temp\test`, true, "__DIR__ . '/test'", false, false},
		{"/tmp/test/./", "/tmp/test/", true, "__DIR__", false, false},
		{"/tmp/test/../vendor", "/tmp/test", true, "dirname(__DIR__).'/test'", false, false},
		{"/tmp/test/.././vendor", "/tmp/test", true, "dirname(__DIR__).'/test'", false, false},
		{"C:/Temp", `c:\Temp\..\..\test`, true, "dirname(__DIR__).'/test'", false, false},
		{"C:/Temp/../..", `d:\Temp\..\..\test`, true, "'D:/test'", false, false},
		{"/foo/bar", "/foo/bar_vendor", true, "dirname(__DIR__).'/bar_vendor'", false, false},
		{"/foo/bar_vendor", "/foo/bar", true, "dirname(__DIR__).'/bar'", false, false},
		{"/foo/bar_vendor", "/foo/bar/src", true, "dirname(__DIR__).'/bar/src'", false, false},
		{"/foo/bar_vendor/src2", "/foo/bar/src/lib", true, "dirname(dirname(__DIR__)).'/bar/src/lib'", false, false},

		// static use case
		{"/tmp/test/../vendor", "/tmp/test", true, "__DIR__ . '/..'.'/test'", true, false},
		{"/tmp/test/.././vendor", "/tmp/test", true, "__DIR__ . '/..'.'/test'", true, false},
		{"C:/Temp", `c:\Temp\..\..\test`, true, "__DIR__ . '/..'.'/test'", true, false},
		{"C:/Temp/../..", `d:\Temp\..\..\test`, true, "'D:/test'", true, false},
		{"/foo/bar", "/foo/bar_vendor", true, "__DIR__ . '/..'.'/bar_vendor'", true, false},
		{"/foo/bar_vendor", "/foo/bar", true, "__DIR__ . '/..'.'/bar'", true, false},
		{"/foo/bar_vendor", "/foo/bar/src", true, "__DIR__ . '/..'.'/bar/src'", true, false},
		{"/foo/bar_vendor/src2", "/foo/bar/src/lib", true, "__DIR__ . '/../..'.'/bar/src/lib'", true, false},
	}

	for _, c := range cases {
		got, err := FindShortestPathCode(c.a, c.b, c.directory, c.static, c.preferRelative)
		if err != nil || got != c.expected {
			t.Errorf("FindShortestPathCode(%q, %q, %v, %v, %v) = %q, %v; want %q", c.a, c.b, c.directory, c.static, c.preferRelative, got, err, c.expected)
		}
	}
}

func TestFilesystem_FindShortestPath(t *testing.T) {
	cases := []struct {
		a, b, expected            string
		directory, preferRelative bool
	}{
		{"/foo/bar", "/foo/bar", "./bar", false, false},
		{"/foo/bar", "/foo/baz", "./baz", false, false},
		{"/foo/bar/", "/foo/baz", "./baz", false, false},
		{"/foo/bar", "/foo/bar", "./", true, false},
		{"/foo/bar", "/foo/baz", "../baz", true, false},
		{"/foo/bar/", "/foo/baz", "../baz", true, false},
		{"C:/foo/bar/", "c:/foo/baz", "../baz", true, false},
		{"/foo/bin/run", "/foo/vendor/acme/bin/run", "../vendor/acme/bin/run", false, false},
		{"/foo/bin/run", "/bar/bin/run", "/bar/bin/run", false, false},
		{"/foo/bin/run", "/bar/bin/run", "/bar/bin/run", true, false},
		{"c:/foo/bin/run", "d:/bar/bin/run", "D:/bar/bin/run", true, false},
		{"c:/bin/run", "c:/vendor/acme/bin/run", "../vendor/acme/bin/run", false, false},
		{`c:\bin\run`, "c:/vendor/acme/bin/run", "../vendor/acme/bin/run", false, false},
		{"c:/bin/run", "d:/vendor/acme/bin/run", "D:/vendor/acme/bin/run", false, false},
		{`c:\bin\run`, "d:/vendor/acme/bin/run", "D:/vendor/acme/bin/run", false, false},
		{"C:/Temp/test", `C:\Temp`, "./", false, false},
		{"/tmp/test", "/tmp", "./", false, false},
		{"C:/Temp/test/sub", `C:\Temp`, "../", false, false},
		{"/tmp/test/sub", "/tmp", "../", false, false},
		{"/tmp/test/sub", "/tmp", "../../", true, false},
		{"c:/tmp/test/sub", "c:/tmp", "../../", true, false},
		{"/tmp", "/tmp/test", "test", false, false},
		{"C:/Temp", `C:\Temp\test`, "test", false, false},
		{"C:/Temp", `c:\Temp\test`, "test", false, false},
		{"/tmp/test/./", "/tmp/test", "./", true, false},
		{"/tmp/test/../vendor", "/tmp/test", "../test", true, false},
		{"/tmp/test/.././vendor", "/tmp/test", "../test", true, false},
		{"C:/Temp", `c:\Temp\..\..\test`, "../test", true, false},
		{"C:/Temp/../..", `c:\Temp\..\..\test`, "./test", true, false},
		{"C:/Temp/../..", `D:\Temp\..\..\test`, "D:/test", true, false},
		{"/app/vendor/foo/bar", "/lib", "../../../../lib", true, true},
		{"/tmp", "/tmp/../../test", "../test", true, false},
		{"/tmp", "/test", "../test", true, false},
		{"/foo/bar", "/foo/bar_vendor", "../bar_vendor", true, false},
		{"/foo/bar_vendor", "/foo/bar", "../bar", true, false},
		{"/foo/bar_vendor", "/foo/bar/src", "../bar/src", true, false},
		{"/foo/bar_vendor/src2", "/foo/bar/src/lib", "../../bar/src/lib", true, false},
		{"C:/", "C:/foo/bar/", "foo/bar", true, false},
	}

	for _, c := range cases {
		got, err := FindShortestPath(c.a, c.b, c.directory, c.preferRelative)
		if err != nil || got != c.expected {
			t.Errorf("FindShortestPath(%q, %q, %v, %v) = %q, %v; want %q", c.a, c.b, c.directory, c.preferRelative, got, err, c.expected)
		}
	}
}

func TestFilesystem_FindShortestPathRelative(t *testing.T) {
	_, err := FindShortestPath("foo", "/bar", false, false)

	var invalid *InvalidArgumentError
	if !errors.As(err, &invalid) || err.Error() != "$from (foo) and $to (/bar) must be absolute paths." {
		t.Errorf("got %v", err)
	}

	if _, err := FindShortestPathCode("/foo", "bar", false, false, false); !errors.As(err, &invalid) {
		t.Errorf("got %v", err)
	}
}

func TestFilesystem_RemoveDirectoryPhp(t *testing.T) {
	workingDir := t.TempDir()

	mustMkdir(t, workingDir+"/level1/level2")
	mustWrite(t, workingDir+"/level1/level2/hello.txt", "hello world")

	if ok, err := RemoveDirectoryPhp(workingDir); !ok || err != nil {
		t.Fatalf("RemoveDirectoryPhp = %v, %v", ok, err)
	}

	assertNotExists(t, workingDir+"/level1/level2/hello.txt")
	assertNotExists(t, workingDir)
}

func TestFilesystem_FileSize(t *testing.T) {
	testFile := t.TempDir() + "/composer_test_file"
	mustWrite(t, testFile, "Hello")

	if size, err := Size(testFile); err != nil || size < 5 {
		t.Errorf("Size = %d, %v", size, err)
	}
}

func TestFilesystem_DirectorySize(t *testing.T) {
	workingDir := t.TempDir()
	mustWrite(t, workingDir+"/file1.txt", "Hello")
	mustWrite(t, workingDir+"/file2.txt", "World")

	if size, err := Size(workingDir); err != nil || size < 10 {
		t.Errorf("Size = %d, %v", size, err)
	}

	if _, err := Size(workingDir + "/missing"); err == nil || err.Error() != workingDir+"/missing does not exist." {
		t.Errorf("Size(missing) error = %v", err)
	}
}

func TestFilesystem_NormalizePath(t *testing.T) {
	cases := []struct{ expected, actual string }{
		{"../foo", "../foo"},
		{"C:/foo/bar", "c:/foo//bar"},
		{"C:/foo/bar", "C:/foo/./bar"},
		{"C:/foo/bar", "C://foo//bar"},
		{"C:/foo/bar", "C:///foo//bar"},
		{"C:/bar", "C:/foo/../bar"},
		{"/bar", "/foo/../bar/"},
		{"phar://C:/Foo", "phar://c:/Foo/Bar/.."},
		{"phar://C:/Foo", "phar://c:///Foo/Bar/.."},
		{"phar://C:/", "phar://c:/Foo/Bar/../../../.."},
		{"/", "/Foo/Bar/../../../.."},
		{"/", "/"},
		{"/", "//"},
		{"/", "///"},
		{"/Foo", "///Foo"},
		{"C:/", `c:\`},
		{"../src", "Foo/Bar/../../../src"},
		{"C:../b", `c:.\..\a\..\b`},
		{"phar://C:../Foo", "phar://c:../Foo"},
		{"//foo/bar", `\\foo\bar`},
	}

	for _, c := range cases {
		if got := NormalizePath(c.actual); got != c.expected {
			t.Errorf("NormalizePath(%q) = %q, want %q", c.actual, got, c.expected)
		}
	}
}

func TestFilesystem_UnlinkSymlinkedDirectory(t *testing.T) {
	basepath := t.TempDir()
	symlinked := basepath + "/linked"

	mustMkdir(t, basepath+"/real")
	mustWrite(t, basepath+"/real/FILE", "")

	if err := os.Symlink(basepath+"/real", symlinked); err != nil {
		t.Skip("Symbolic links for directories not supported on this platform")
	}

	if !isDir(symlinked) {
		t.Fatal("Precondition assertion failed (is_dir is false on symbolic link to directory).")
	}

	if err := Unlink(symlinked); err != nil {
		t.Fatal(err)
	}

	assertNotExists(t, symlinked)
}

func TestFilesystem_RemoveSymlinkedDirectoryWithTrailingSlash(t *testing.T) {
	workingDir := t.TempDir()

	mustMkdir(t, workingDir+"/real")
	mustWrite(t, workingDir+"/real/FILE", "")

	symlinked := workingDir + "/linked"
	symlinkedTrailingSlash := symlinked + "/"

	if err := os.Symlink(workingDir+"/real", symlinked); err != nil {
		t.Skip("Symbolic links for directories not supported on this platform")
	}

	if !isDir(symlinked) {
		t.Fatal("Precondition assertion failed (is_dir is false on symbolic link to directory).")
	}

	if !isDir(symlinkedTrailingSlash) {
		t.Fatal("Precondition assertion failed (is_dir false w trailing slash).")
	}

	ok, err := NewFilesystem(nil).RemoveDirectory(symlinkedTrailingSlash)
	if !ok || err != nil {
		t.Fatalf("RemoveDirectory = %v, %v", ok, err)
	}

	assertNotExists(t, symlinkedTrailingSlash)
	assertNotExists(t, symlinked)

	// The target survives.
	if !fileExists(workingDir + "/real/FILE") {
		t.Error("the symlink target was removed")
	}
}

func TestFilesystem_Junctions(t *testing.T) {
	workingDir := t.TempDir()
	mustMkdir(t, workingDir+"/real/nesting/testing")

	fs := NewFilesystem(nil)

	// Non-Windows systems do not support this and will return false on all
	// tests, and an exception on creation.
	if runtime.GOOS != "windows" {
		if IsJunction(workingDir) {
			t.Error("IsJunction is true")
		}

		if ok, err := RemoveJunction(workingDir); ok || err != nil {
			t.Errorf("RemoveJunction = %v, %v", ok, err)
		}

		err := fs.Junction(workingDir+"/real/../real/nesting", workingDir+"/junction")

		var logic *LogicError
		if !errors.As(err, &logic) || !strings.Contains(err.Error(), "not available on non-Windows platform") {
			t.Errorf("Junction error = %v", err)
		}

		return
	}

	target := workingDir + "/real/../real/nesting"
	junction := workingDir + "/junction"

	// Create and detect junction.
	if err := fs.Junction(target, junction); err != nil {
		t.Fatal(err)
	}

	for path, want := range map[string]bool{
		junction:                   true,
		target:                     false,
		target + "/../../junction": true,
		junction + "/../real":      false,
		junction + "/../junction":  true,
	} {
		if got := IsJunction(path); got != want {
			t.Errorf("IsJunction(%q) = %v, want %v", path, got, want)
		}
	}

	// Remove junction.
	if !isDir(junction) {
		t.Fatal(junction + " is not a directory")
	}

	if ok, err := RemoveJunction(junction); !ok || err != nil {
		t.Fatalf("RemoveJunction = %v, %v", ok, err)
	}

	if isDir(junction) {
		t.Error(junction + " is still a directory")
	}
}

func TestFilesystem_OverrideJunctions(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Only runs on windows")
	}

	workingDir := t.TempDir()
	mustMkdir(t, workingDir+"/real/nesting/testing")

	fs := NewFilesystem(nil)
	oldTarget := workingDir + "/real/nesting/testing"
	target := workingDir + "/real/../real/nesting"
	junction := workingDir + "/junction"

	// Override non-broken junction.
	for _, to := range []string{oldTarget, target} {
		if err := fs.Junction(to, junction); err != nil {
			t.Fatal(err)
		}
	}

	if !IsJunction(junction) || !IsJunction(target+"/../../junction") {
		t.Fatal("junction not detected")
	}

	if ok, err := RemoveJunction(junction); !ok || err != nil {
		t.Fatalf("RemoveJunction = %v, %v", ok, err)
	}

	// Override broken junction.
	if err := fs.Junction(oldTarget, junction); err != nil {
		t.Fatal(err)
	}

	if _, err := fs.RemoveDirectory(oldTarget); err != nil {
		t.Fatal(err)
	}

	if err := fs.Junction(target, junction); err != nil {
		t.Fatal(err)
	}

	if !IsJunction(junction) || !IsJunction(target+"/../../junction") {
		t.Fatal("junction not detected")
	}
}

func setUpCopyTree(t *testing.T) (workingDir, testFile string) {
	t.Helper()

	workingDir = t.TempDir()
	testFile = t.TempDir() + "/composer_test_file"

	mustMkdir(t, workingDir+"/foo/bar")
	mustMkdir(t, workingDir+"/foo/baz")
	mustWrite(t, workingDir+"/foo/foo.file", "foo")
	mustWrite(t, workingDir+"/foo/bar/foobar.file", "foobar")
	mustWrite(t, workingDir+"/foo/baz/foobaz.file", "foobaz")
	mustWrite(t, testFile, "testfile")

	return workingDir, testFile
}

func TestFilesystem_Copy(t *testing.T) {
	workingDir, testFile := setUpCopyTree(t)

	if ok, err := Copy(workingDir+"/foo", workingDir+"/foop"); !ok || err != nil {
		t.Fatalf("Copying directory failed: %v, %v", ok, err)
	}

	for _, dir := range []string{"/foop", "/foop/bar", "/foop/baz"} {
		if !isDir(workingDir + dir) {
			t.Error("Not a directory: " + workingDir + dir)
		}
	}

	for _, file := range []string{"/foop/foo.file", "/foop/bar/foobar.file", "/foop/baz/foobaz.file"} {
		if !fileExists(workingDir + file) {
			t.Error("Not a file: " + workingDir + file)
		}
	}

	if ok, err := Copy(testFile, workingDir+"/testfile.file"); !ok || err != nil {
		t.Fatalf("Copy = %v, %v", ok, err)
	}

	if !fileExists(workingDir + "/testfile.file") {
		t.Error("Not a file: " + workingDir + "/testfile.file")
	}

	// PHP's copy() fails silently when source and target are one file.
	if ok, err := Copy(testFile, testFile); ok || err != nil {
		t.Errorf("Copy onto itself = %v, %v", ok, err)
	}

	err := func() error { _, err := Copy(workingDir+"/missing", workingDir+"/x"); return err }()
	if err == nil || err.Error() != "copy("+workingDir+"/missing): Failed to open stream: No such file or directory" {
		t.Errorf("Copy(missing) error = %v", err)
	}
}

func TestFilesystem_CopyThenRemove(t *testing.T) {
	workingDir, testFile := setUpCopyTree(t)

	if err := CopyThenRemove(testFile, workingDir+"/testfile.file"); err != nil {
		t.Fatal(err)
	}

	assertNotExists(t, testFile)

	if err := CopyThenRemove(workingDir+"/foo", workingDir+"/foop"); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/foo/baz/foobaz.file", "/foo/bar/foobar.file", "/foo/foo.file", "/foo/baz", "/foo/bar", "/foo"} {
		assertNotExists(t, workingDir+path)
	}

	if content, err := os.ReadFile(workingDir + "/foop/bar/foobar.file"); err != nil || string(content) != "foobar" {
		t.Errorf("copied content = %q, %v", content, err)
	}
}

// The tests below cover behaviour FilesystemTest does not.

func TestFilesystem_RemoveDirectory(t *testing.T) {
	workingDir := t.TempDir()
	mustMkdir(t, workingDir+"/a/b/c")
	mustWrite(t, workingDir+"/a/b/c/file", "x")

	if err := os.Symlink(workingDir+"/a/b", workingDir+"/a/link"); err != nil {
		t.Skip(err)
	}

	fs := NewFilesystem(nil)

	if ok, err := fs.RemoveDirectory(workingDir + "/a"); !ok || err != nil {
		t.Fatalf("RemoveDirectory = %v, %v", ok, err)
	}

	assertNotExists(t, workingDir+"/a")

	// A missing directory counts as removed.
	if ok, err := fs.RemoveDirectory(workingDir + "/a"); !ok || err != nil {
		t.Errorf("RemoveDirectory(missing) = %v, %v", ok, err)
	}

	for _, root := range []string{"/", "//", `\`, "C:/", "c:\\"} {
		if !isDir(root) {
			continue
		}

		_, err := fs.RemoveDirectory(root)

		var runtimeErr *RuntimeError
		if !errors.As(err, &runtimeErr) || err.Error() != "Aborting an attempted deletion of "+root+", this was probably not intended, if it is a real use case please report it." {
			t.Errorf("RemoveDirectory(%q) error = %v", root, err)
		}
	}
}

func TestFilesystem_RemoveDirectoryPhpSymlinks(t *testing.T) {
	workingDir := t.TempDir()
	mustMkdir(t, workingDir+"/tree/sub")
	mustMkdir(t, workingDir+"/target")
	mustWrite(t, workingDir+"/target/keep", "x")

	if err := os.Symlink(workingDir+"/target/keep", workingDir+"/tree/filelink"); err != nil {
		t.Skip(err)
	}

	if err := os.Symlink(workingDir+"/missing", workingDir+"/tree/sub/broken"); err != nil {
		t.Fatal(err)
	}

	if ok, err := RemoveDirectoryPhp(workingDir + "/tree"); !ok || err != nil {
		t.Fatalf("RemoveDirectoryPhp = %v, %v", ok, err)
	}

	assertNotExists(t, workingDir+"/tree")

	if !fileExists(workingDir + "/target/keep") {
		t.Error("a symlink target was removed")
	}

	// RecursiveIteratorIterator calls isDir() on a symlink to a directory,
	// so it is rmdir'ed, which fails on Unix; on Windows rmdir() is how a
	// directory symlink is removed, and the target stays.
	mustMkdir(t, workingDir+"/tree2")

	if err := os.Symlink(workingDir+"/target", workingDir+"/tree2/dirlink"); err != nil {
		t.Fatal(err)
	}

	_, err := RemoveDirectoryPhp(workingDir + "/tree2")
	if runtime.GOOS == "windows" {
		if err != nil || !fileExists(workingDir+"/target/keep") {
			t.Errorf("RemoveDirectoryPhp(dir symlink) error = %v", err)
		}

		return
	}

	if err == nil || !strings.HasPrefix(err.Error(), "Could not delete "+workingDir+"/tree2/dirlink: rmdir("+workingDir+"/tree2/dirlink): ") {
		t.Errorf("RemoveDirectoryPhp(dir symlink) error = %v", err)
	}
}

func TestFilesystem_EnsureDirectoryExists(t *testing.T) {
	workingDir := t.TempDir()

	if err := EnsureDirectoryExists(workingDir + "/a/b/c"); err != nil || !isDir(workingDir+"/a/b/c") {
		t.Fatalf("EnsureDirectoryExists = %v", err)
	}

	if err := EnsureDirectoryExists(workingDir + "/a/b/c"); err != nil {
		t.Errorf("EnsureDirectoryExists(existing) = %v", err)
	}

	mustWrite(t, workingDir+"/file", "")

	if err := EnsureDirectoryExists(workingDir + "/file"); err == nil || err.Error() != workingDir+"/file exists and is not a directory." {
		t.Errorf("EnsureDirectoryExists(file) = %v", err)
	}

	// PHP's recursive mkdir() stops at the existing file and creates
	// file/sub below it, which is ENOTDIR on Unix and ERROR_PATH_NOT_FOUND
	// (ENOENT) on Windows.
	want := "Not a directory"
	if runtime.GOOS == "windows" {
		want = "No such file or directory"
	}

	err := EnsureDirectoryExists(workingDir + "/file/sub")
	if err == nil || err.Error() != workingDir+"/file/sub does not exist and could not be created: mkdir(): "+want {
		t.Errorf("EnsureDirectoryExists(file/sub) = %v", err)
	}

	// A broken symlink is replaced.
	if err := os.Symlink(workingDir+"/missing", workingDir+"/broken"); err != nil {
		t.Skip(err)
	}

	if err := EnsureDirectoryExists(workingDir + "/broken"); err != nil || isLink(workingDir+"/broken") || !isDir(workingDir+"/broken") {
		t.Errorf("EnsureDirectoryExists(broken symlink) = %v", err)
	}

	// path/to/broken-symlink/../foo, see composer/composer#11864.
	if err := os.Symlink(workingDir+"/missing", workingDir+"/broken2"); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDirectoryExists(workingDir + "/broken2/../foo"); err != nil || !isDir(workingDir+"/foo") {
		t.Errorf("EnsureDirectoryExists(broken2/../foo) = %v", err)
	}
}

func TestFilesystem_UnlinkErrors(t *testing.T) {
	path := t.TempDir() + "/missing"

	err := Unlink(path)

	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) || err.Error() != "Could not delete "+path+": unlink("+path+"): No such file or directory" {
		t.Errorf("Unlink error = %v", err)
	}

	if err := Rmdir(path); err == nil || err.Error() != "Could not delete "+path+": rmdir("+path+"): No such file or directory" {
		t.Errorf("Rmdir error = %v", err)
	}
}

func TestFilesystem_EmptyDirectory(t *testing.T) {
	workingDir := t.TempDir()
	mustMkdir(t, workingDir+"/dir/.git/objects")
	mustWrite(t, workingDir+"/dir/.hidden", "")
	mustWrite(t, workingDir+"/dir/file", "")

	fs := NewFilesystem(nil)

	if empty, err := IsDirEmpty(workingDir + "/dir/"); empty || err != nil {
		t.Errorf("IsDirEmpty = %v, %v", empty, err)
	}

	if err := fs.EmptyDirectory(workingDir+"/dir/", true); err != nil {
		t.Fatal(err)
	}

	if empty, err := IsDirEmpty(workingDir + "/dir"); !empty || err != nil {
		t.Errorf("IsDirEmpty after EmptyDirectory = %v, %v", empty, err)
	}

	if err := fs.EmptyDirectory(workingDir+"/new", true); err != nil || !isDir(workingDir+"/new") {
		t.Errorf("EmptyDirectory(new) = %v", err)
	}

	if err := fs.EmptyDirectory(workingDir+"/other", false); err != nil || fileExists(workingDir+"/other") {
		t.Errorf("EmptyDirectory(other, false) = %v", err)
	}

	_, err := IsDirEmpty(workingDir + "/missing/")

	var invalid *InvalidArgumentError
	if !errors.As(err, &invalid) || err.Error() != `The "`+workingDir+`/missing" directory does not exist.` {
		t.Errorf("IsDirEmpty(missing) error = %v", err)
	}
}

func TestFilesystem_Rename(t *testing.T) {
	workingDir, testFile := setUpCopyTree(t)
	fs := NewFilesystem(nil)

	if err := fs.Rename(workingDir+"/foo", workingDir+"/moved"); err != nil {
		t.Fatal(err)
	}

	assertNotExists(t, workingDir+"/foo")

	if !fileExists(workingDir + "/moved/bar/foobar.file") {
		t.Error("rename lost a file")
	}

	if err := fs.Rename(testFile, workingDir+"/moved.file"); err != nil || !fileExists(workingDir+"/moved.file") {
		t.Errorf("Rename(file) = %v", err)
	}
}

func TestFilesystem_RelativeSymlink(t *testing.T) {
	workingDir := t.TempDir()
	mustMkdir(t, workingDir+"/vendor/bin")
	mustMkdir(t, workingDir+"/vendor/acme/pkg/bin")
	mustWrite(t, workingDir+"/vendor/acme/pkg/bin/tool", "x")

	ok, err := RelativeSymlink(workingDir+"/vendor/acme/pkg/bin/tool", workingDir+"/vendor/bin/tool")
	if !ok || err != nil {
		t.Fatalf("RelativeSymlink = %v, %v", ok, err)
	}

	// PHP's symlink() on Windows normalizes the target to backslashes.
	if link, err := os.Readlink(workingDir + "/vendor/bin/tool"); err != nil || link != filepath.FromSlash("../acme/pkg/bin/tool") {
		t.Errorf("link = %q, %v", link, err)
	}

	_, err = RelativeSymlink(workingDir+"/vendor/acme/pkg/bin/tool", workingDir+"/missing/tool")
	if err == nil || err.Error() != "chdir(): No such file or directory (errno 2)" {
		t.Errorf("RelativeSymlink(missing dir) error = %v", err)
	}
}

func TestFilesystem_IsSymlinkedDirectory(t *testing.T) {
	workingDir := t.TempDir()
	mustMkdir(t, workingDir+"/real")

	if err := os.Symlink(workingDir+"/real", workingDir+"/link"); err != nil {
		t.Skip(err)
	}

	for path, want := range map[string]bool{
		workingDir + "/real":    false,
		workingDir + "/link":    true,
		workingDir + "/link/":   true,
		workingDir + "/link//":  true,
		workingDir + "/missing": false,
	} {
		if got := IsSymlinkedDirectory(path); got != want {
			t.Errorf("IsSymlinkedDirectory(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestFilesystem_FilePutContentsIfModified(t *testing.T) {
	path := t.TempDir() + "/file"

	for _, c := range []struct {
		content string
		want    int
	}{{"abc", 3}, {"abc", 0}, {"abcd", 4}, {"", 0}} {
		if c.content == "" {
			mustWrite(t, path, "")
		}

		if got, err := FilePutContentsIfModified(path, []byte(c.content)); got != c.want || err != nil {
			t.Errorf("FilePutContentsIfModified(%q) = %d, %v; want %d", c.content, got, err, c.want)
		}
	}

	dir := t.TempDir()
	if _, err := FilePutContentsIfModified(dir, []byte("x")); err == nil || err.Error() != "file_put_contents("+dir+"): Failed to open stream: Is a directory" {
		t.Errorf("FilePutContentsIfModified(dir) error = %v", err)
	}
}

func TestFilesystem_SafeCopy(t *testing.T) {
	dir := t.TempDir()
	source, target := dir+"/source", dir+"/target"

	mustWrite(t, source, strings.Repeat("x", 10000))

	mtime := time.Date(2020, 1, 2, 3, 4, 5, 600_000_000, time.UTC)
	if err := os.Chtimes(source, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	if err := SafeCopy(source, target); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	// touch() takes whole seconds.
	if !fi.ModTime().Equal(mtime.Truncate(time.Second)) {
		t.Errorf("mtime = %v, want %v", fi.ModTime(), mtime.Truncate(time.Second))
	}

	// Equal contents are left alone.
	later := mtime.Add(time.Hour)
	if err := os.Chtimes(target, later, later); err != nil {
		t.Fatal(err)
	}

	if err := SafeCopy(source, target); err != nil {
		t.Fatal(err)
	}

	if fi, _ := os.Stat(target); !fi.ModTime().Equal(later) {
		t.Error("SafeCopy rewrote an identical file")
	}

	mustWrite(t, target, strings.Repeat("x", 9999)+"y")

	if err := SafeCopy(source, target); err != nil {
		t.Fatal(err)
	}

	if content, _ := os.ReadFile(target); string(content) != strings.Repeat("x", 10000) {
		t.Error("SafeCopy did not copy differing contents")
	}
}

func TestFilesystem_IsReadable(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, dir+"/file", "")

	for path, want := range map[string]bool{dir: true, dir + "/file": true, dir + "/missing": false} {
		if got := IsReadable(path); got != want {
			t.Errorf("IsReadable(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestFilesystem_PlatformVariants(t *testing.T) {
	for _, c := range []struct {
		path          string
		local, local2 bool
	}{
		{`\\server\share`, false, false},
		{"//server/share", false, true},
		{"file:////server", false, true},
		{"/foo", true, true},
		{"c:/foo", true, true},
	} {
		if got := isLocalPath(c.path, true); got != c.local {
			t.Errorf("isLocalPath(%q, windows) = %v, want %v", c.path, got, c.local)
		}

		if got := isLocalPath(c.path, false); got != c.local2 {
			t.Errorf("isLocalPath(%q, posix) = %v, want %v", c.path, got, c.local2)
		}
	}

	if got := getPlatformPath("file:///c:/foo", true); got != "c:/foo" {
		t.Errorf("getPlatformPath(windows) = %q", got)
	}

	if got := getPlatformPath("file:///c:/foo", false); got != "/c:/foo" {
		t.Errorf("getPlatformPath(posix) = %q", got)
	}

	// Windows dirname keeps the drive and uses backslashes.
	for _, c := range []struct{ in, want string }{
		{`c:\foo\bar`, `c:\foo`}, {`c:\foo`, `c:\`}, {"c:", "c:"}, {"c:foo", "c:."}, {`\\`, `\`}, {"/a/b", "/a"},
	} {
		if got := phpDirname(c.in, true); got != c.want {
			t.Errorf("phpDirname(%q, windows) = %q, want %q", c.in, got, c.want)
		}
	}

	got, err := findShortestPathCode("C:/vendor/composer", "C:/src", true, false, false, true)
	if err != nil || got != "dirname(dirname(__DIR__)).'/src'" {
		t.Errorf("findShortestPathCode(windows) = %q, %v", got, err)
	}
}

func TestFilesystem_NormalizePathAllocations(t *testing.T) {
	if allocs := testing.AllocsPerRun(100, func() { NormalizePath("/foo/bar/vendor/composer") }); allocs != 0 {
		t.Errorf("NormalizePath of a normalized path allocates %v times", allocs)
	}

	if allocs := testing.AllocsPerRun(100, func() { NormalizePath("/foo/bar/../vendor/./composer/") }); allocs != 1 {
		t.Errorf("NormalizePath allocates %v times, want 1", allocs)
	}
}

func BenchmarkNormalizePath(b *testing.B) {
	for b.Loop() {
		NormalizePath(`C:\projects\app\vendor\acme\pkg\..\..\composer\./autoload_real.php`)
	}
}

func BenchmarkFindShortestPathCode(b *testing.B) {
	for b.Loop() {
		_, _ = FindShortestPathCode("/app/vendor/composer", "/app/src/Foo/Bar.php", true, true, false)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()

	if err := os.MkdirAll(dir, 0o777); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

func assertNotExists(t *testing.T, path string) {
	t.Helper()

	if _, err := os.Lstat(filepath.FromSlash(path)); err == nil {
		t.Errorf("%s still exists", path)
	}
}
