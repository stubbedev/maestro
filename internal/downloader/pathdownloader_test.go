package downloader

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/testutil"
	"github.com/stubbedev/maestro/internal/util"
)

// PathDownloader has no test in Composer's suite; these cover its
// strategies and messages.

func pathPackage(url string) *pkg.CompletePackage {
	p := getPackage("a/b", "dev-main", "dev-main")
	p.SetDistType(pkg.Str("path"))
	p.SetDistURL(pkg.Str(url))
	p.SetDistReference(pkg.Str("abc"))

	return p
}

func newPathDownloader(t *testing.T) (*PathDownloader, *project) {
	t.Helper()

	pr := newProject(t, testutil.RealTempDir(t), projectOptions{})

	d, err := NewPathDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	return d, pr
}

func sourceTree(t *testing.T) string {
	t.Helper()

	// PathDownloader reports the source's realpath.
	src := testutil.RealTempDir(t) + "/src"
	for _, dir := range []string{"/lib", "/tests", "/.git", "/empty"} {
		if err := os.MkdirAll(src+dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	writeFile(t, src+"/composer.json", []byte(`{"name":"a/b"}`), 0o644)
	writeFile(t, src+"/lib/A.php", []byte("<?php"), 0o644)
	writeFile(t, src+"/tool", []byte("#!/bin/sh"), 0o755)
	writeFile(t, src+"/tests/ATest.php", []byte("<?php"), 0o644)
	writeFile(t, src+"/.git/HEAD", []byte("ref"), 0o644)
	writeFile(t, src+"/.gitattributes", []byte("/tests export-ignore\n# comment\n*.md -export-ignore\n"), 0o644)

	if err := os.Symlink("lib/A.php", src+"/link.php"); err != nil {
		t.Fatal(err)
	}

	return src
}

func TestPathDownloader_SymlinkRelative(t *testing.T) {
	d, pr := newPathDownloader(t)
	src := sourceTree(t)
	p := pathPackage(src)

	path, err := pr.install(d, p)
	if err != nil {
		t.Fatal(err)
	}

	// Windows gets an NTFS junction, always to the absolute realpath.
	verb := "Symlinking"
	if util.IsWindows() {
		verb = "Junctioning"
		if !util.IsJunction(path) {
			t.Fatalf("%s is no junction; output %q", path, downloadLines(pr.out))
		}
	} else {
		link, err := os.Readlink(path)
		if err != nil {
			t.Fatal(err)
		}

		if strings.HasPrefix(link, "/") || !strings.HasSuffix(link, "/") {
			t.Fatalf("link %q is not relative with a trailing slash", link)
		}
	}

	if !fileExists(path + "/lib/A.php") {
		t.Fatal("the symlink does not resolve to the source")
	}

	if want := "  - Installing a/b (dev-main abc): " + verb + " from " + src; !slices.Contains(downloadLines(pr.out), want) {
		t.Fatalf("output %q lacks %q", downloadLines(pr.out), want)
	}

	// removing does not touch the source
	if err := await(d.Remove(p, path)); err != nil {
		t.Fatal(err)
	}

	if fileExists(path) || !fileExists(src+"/lib/A.php") {
		t.Fatal("remove went wrong")
	}
}

func TestPathDownloader_SymlinkAbsolute(t *testing.T) {
	if util.IsWindows() {
		t.Skip("Windows gets a junction, which is always absolute (TestPathDownloader_SymlinkRelative)")
	}

	d, pr := newPathDownloader(t)
	src := sourceTree(t)
	p := pathPackage(src)
	p.SetTransportOptions(php.ArrayOf("relative", false))

	path, err := pr.install(d, p)
	if err != nil {
		t.Fatal(err)
	}

	if link, err := os.Readlink(path); err != nil || link != src+"/" {
		t.Fatalf("link %q, %v", link, err)
	}
}

func TestPathDownloader_Mirror(t *testing.T) {
	d, pr := newPathDownloader(t)
	src := sourceTree(t)
	p := pathPackage(src)
	p.SetTransportOptions(php.ArrayOf("symlink", false))

	path, err := pr.install(d, p)
	if err != nil {
		t.Fatal(err)
	}

	if fi, err := os.Lstat(path); err != nil || !fi.IsDir() {
		t.Fatalf("not a mirrored directory: %v", err)
	}

	for _, f := range []string{"/composer.json", "/lib/A.php", "/.gitattributes", "/empty"} {
		if !fileExists(path + f) {
			t.Errorf("%s was not mirrored", f)
		}
	}

	for _, f := range []string{"/tests", "/.git"} {
		if fileExists(path + f) {
			t.Errorf("%s was mirrored", f)
		}
	}

	// Windows keeps no execute bits.
	if fi, err := os.Stat(path + "/tool"); err != nil || !util.IsWindows() && fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("tool lost its executable bits: %v", err)
	}

	// ArchivableFilesFinder keeps a symlink only when its realpath() starts
	// with the slash-normalized source path, which a Windows realpath(),
	// with backslashes, never does: Composer leaves links out there.
	if util.IsWindows() {
		if _, err := os.Lstat(path + "/link.php"); err == nil {
			t.Error("link.php was mirrored on Windows")
		}
	} else if link, err := os.Readlink(path + "/link.php"); err != nil || link != "lib/A.php" {
		t.Errorf("link.php: %q, %v", link, err)
	}

	if want := "  - Installing a/b (dev-main abc): Mirroring from " + src; !slices.Contains(downloadLines(pr.out), want) {
		t.Fatalf("output %q lacks %q", downloadLines(pr.out), want)
	}

	appendix, err := d.installOperationAppendix(p, path)
	if err != nil || appendix != ": Mirroring from "+src {
		t.Fatalf("appendix %q, %v", appendix, err)
	}
}

func TestPathDownloader_SourceAlreadyPresent(t *testing.T) {
	d, pr := newPathDownloader(t)
	src := sourceTree(t)
	p := pathPackage(src)

	if err := await(d.Install(p, src)); err != nil {
		t.Fatal(err)
	}

	if err := await(d.Remove(p, src)); err != nil {
		t.Fatal(err)
	}

	if !fileExists(src + "/composer.json") {
		t.Fatal("the source was removed")
	}

	want := []string{
		"  - Installing a/b (dev-main abc): Source already present",
		"  - Removing a/b (dev-main abc), source is still present in " + src,
	}
	if got := downloadLines(pr.out); !slices.Equal(got, want) {
		t.Fatalf("output %q, want %q", got, want)
	}
}

func TestPathDownloader_DownloadErrors(t *testing.T) {
	d, _ := newPathDownloader(t)
	src := sourceTree(t)

	_, err := d.Download(pathPackage(src+"/missing"), t.TempDir()+"/x", nil)
	if err == nil || err.Error() != `Source path "`+src+`/missing" is not found for package a/b` {
		t.Fatalf("error %v", err)
	}

	_, err = d.Download(pathPackage(src), src+"/lib", nil)
	// Both paths as realpath() gives them (backslashes on Windows).
	if err == nil || err.Error() != `Package a/b cannot install to "`+util.Realpath(src+"/lib")+`" inside its source at "`+util.Realpath(src)+`"` {
		t.Fatalf("error %v", err)
	}

	noURL := pathPackage(src)
	noURL.SetDistURL(pkg.NullString{})

	_, err = d.Download(noURL, src, nil)
	if err == nil || err.Error() != "The package a/b has no dist url configured, cannot download." {
		t.Fatalf("error %v", err)
	}
}

func TestPathDownloader_MirrorPathReposEnv(t *testing.T) {
	t.Setenv("COMPOSER_MIRROR_PATH_REPOS", "1")

	current, allowed := computeAllowedStrategies(php.NewArray())
	if current != strategyMirror || allowed != allowSymlink|allowMirror {
		t.Fatalf("got %d, %d", current, allowed)
	}

	current, allowed = computeAllowedStrategies(php.ArrayOf("symlink", true))
	if current != strategySymlink || allowed != allowSymlink {
		t.Fatalf("got %d, %d", current, allowed)
	}
}
