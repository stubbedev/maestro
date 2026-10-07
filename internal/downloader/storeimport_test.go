package downloader

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
)

// installIn installs p in a fresh project sharing the store and files
// cache, returning the package path and the project's output.
func installIn(t *testing.T, opts projectOptions, p pkg.PackageInterface) (string, []string) {
	t.Helper()

	pr := newProject(t, t.TempDir(), opts)

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	path, err := pr.install(d, p)
	if err != nil {
		t.Fatal(err)
	}

	checkTree(t, path)
	checkNoLeftovers(t, pr.vendor)

	return path, downloadLines(pr.out)
}

func sameFile(t *testing.T, a, b string) bool {
	t.Helper()

	fa, err := os.Lstat(a)
	if err != nil {
		t.Fatal(err)
	}

	fb, err := os.Lstat(b)
	if err != nil {
		t.Fatal(err)
	}

	return os.SameFile(fa, fb)
}

// Composer plugins never get files hard-linked to the store, cold (from the
// downloaded archive) or warm (from the store): plugins such as
// phpstan/extension-installer rewrite their own files in place, which must
// not reach the store or another project. Libraries are hard-linked where
// the filesystem has no reflinks (the default import method).
func TestArchiveDownloader_PluginsAreNeverHardlinked(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	opts := projectOptions{store: newStore(t), cacheDir: t.TempDir()}

	library := distPackage(srv.URL+"/a.zip", "zip")

	plugin := distPackage(srv.URL+"/a.zip", "zip")
	plugin.SetType("composer-plugin")

	installer := distPackage(srv.URL+"/a.zip", "zip")
	installer.SetType("composer-installer")

	lib1, _ := installIn(t, opts, library)
	lib2, _ := installIn(t, opts, library)

	if !sameFile(t, lib1+"/composer.json", lib2+"/composer.json") {
		t.Skip("the store does not hardlink here (reflinks or another device)")
	}

	for _, p := range []*pkg.CompletePackage{plugin, installer} {
		// warm: the release is in the store already
		path1, _ := installIn(t, opts, p)
		path2, _ := installIn(t, opts, p)

		for _, name := range []string{"/composer.json", "/bin/tool"} {
			for _, other := range []string{lib1, path2} {
				if sameFile(t, path1+name, other+name) {
					t.Errorf("%s: %s shares its inode with %s", p.Type(), path1+name, other+name)
				}
			}
		}

		// The plugin rewrites its own file in place.
		if err := os.WriteFile(path1+"/composer.json", []byte(`{"generated":true}`), 0o644); err != nil {
			t.Fatal(err)
		}

		for _, other := range []string{lib1, lib2, path2} {
			checkTree(t, other)
		}
	}

	// cold: a fresh store, the plugin first
	cold := projectOptions{store: newStore(t), cacheDir: t.TempDir()}
	path1, _ := installIn(t, cold, plugin)
	lib3, _ := installIn(t, cold, library)

	if sameFile(t, path1+"/composer.json", lib3+"/composer.json") {
		t.Error("a plugin extracted from its archive shares its inode with the store")
	}
}

// A vendor file edited in place through its hardlink (pnpm's accepted
// risk: projects linked to it see the edit) is noticed before the store
// imports the object again: the next project gets the release's content,
// healed from the cached archive without a request, with Composer's cache
// hit output.
func TestArchiveDownloader_EditedHardlinkHealedFromFilesCache(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	shared := newStore(t)
	opts := projectOptions{store: shared, cacheDir: t.TempDir()}
	p := distPackage(srv.URL+"/a.zip", "zip")

	first, _ := installIn(t, opts, p)

	f, err := os.OpenFile(first+"/composer.json", os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.WriteString(" // patched"); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	opts.verbosity = console.VerbosityVeryVerbose
	second, got := installIn(t, opts, p)

	want := []string{
		"  - Loading a/b (1.0.0) from cache",
		"  - Installing a/b (1.0.0): Extracting archive",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("output %q, want %q", got, want)
	}

	if n := srv.requests("/a.zip"); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}

	if data, _ := os.ReadFile(first + "/composer.json"); !strings.HasSuffix(string(data), "patched") {
		t.Error("the edited project lost its edit")
	}

	if sameFile(t, first+"/composer.json", second+"/composer.json") {
		t.Error("the new project was linked to the edited inode")
	}

	if res, err := shared.Verify(); err != nil || res.Missing != 0 || res.Corrupt != 0 || res.Restamped != 0 {
		t.Fatalf("store not healed: %+v, %v", res, err)
	}
}

// installWarm installs p in a second project after a first one put it in
// the shared store and files cache, configuring the second project's deps
// with setup.
func installWarm(t *testing.T, p pkg.PackageInterface, ctor func(Deps) (*ArchiveDownloader, error), setup func(*Deps)) (*project, string, error) {
	t.Helper()

	opts := projectOptions{store: newStore(t), cacheDir: t.TempDir()}

	first := newProject(t, t.TempDir(), opts)
	d, err := ctor(first.deps)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := first.install(d, p); err != nil {
		t.Fatal(err)
	}

	pr := newProject(t, t.TempDir(), opts)
	setup(&pr.deps)

	if d, err = ctor(pr.deps); err != nil {
		t.Fatal(err)
	}

	path, err := pr.install(d, p)

	return pr, path, err
}

// Served from the store, the package still comes with the temporary file
// POST_FILE_DOWNLOAD names, holding the archive as Composer's copy from the
// files cache would, for listeners that read it.
func TestArchiveDownloader_StorePostFileDownloadFileExists(t *testing.T) {
	archive := githubZip()
	srv := newDistServer(t, map[string][]byte{"/a.zip": archive})

	var seen []string

	pr, path, err := installWarm(t, distPackage(srv.URL+"/a.zip", "zip"), NewZipDownloader, func(deps *Deps) {
		deps.EventDispatcher = &fakeDispatcher{listener: func(e eventdispatcher.Event) error {
			if ev, ok := e.(*eventdispatcher.PostFileDownloadEvent); ok {
				data, err := os.ReadFile(ev.FileName().S)
				if err != nil {
					return err
				}

				if !bytes.Equal(data, archive) {
					t.Errorf("POST_FILE_DOWNLOAD file holds %d bytes, want the %d of the archive", len(data), len(archive))
				}

				seen = append(seen, ev.FileName().S)
			}

			return nil
		}}
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(seen) != 1 {
		t.Fatalf("POST_FILE_DOWNLOAD fired %d times, want 1", len(seen))
	}

	checkTree(t, path)
	checkNoLeftovers(t, pr.vendor)

	if n := srv.requests("/a.zip"); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}

// A tar.gz the store holds from a run whose PHP had zlib still fails as
// new \PharData() does when the PHP Composer runs on lacks it.
func TestTarDownloader_StoreHitNeedsCompressionExtension(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.tar.gz": archivetest.Gzip(tarball())})

	_, _, err := installWarm(t, distPackage(srv.URL+"/a.tar.gz", "tar"), NewTarDownloader, func(deps *Deps) {
		deps.ExtensionLoaded = func(name string) bool { return name != "zlib" }
	})
	mustContain(t, err, "unable to decompress gzipped phar archive")

	if !phperr.InstanceOf(err, "UnexpectedValueException") {
		t.Errorf("%T, want an UnexpectedValueException", err)
	}
}
