package downloader

import (
	"os"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/archive/archivetest"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/store"
)

// listeningDispatcher is a fakeDispatcher that tells which events reach
// its listener (EventDispatcher.WillDispatchTo).
type listeningDispatcher struct {
	fakeDispatcher
	events []string
}

func (d *listeningDispatcher) WillDispatchTo(e eventdispatcher.Event) bool {
	return slices.Contains(d.events, e.Name())
}

// patchedZip is githubZip with its composer.json rewritten, as a
// POST_FILE_DOWNLOAD listener patching the dist would leave it.
func patchedZip() []byte {
	return archivetest.Zip("",
		archivetest.UnixDir("pkg-1a2b3c/", 0o755),
		archivetest.UnixFile("pkg-1a2b3c/composer.json", 0o644, `{"name":"a/b","patched":true}`),
		archivetest.UnixDir("pkg-1a2b3c/bin/", 0o755),
		archivetest.UnixFile("pkg-1a2b3c/bin/tool", 0o755, "#!/usr/bin/env php\n"),
	)
}

// patcher is a POST_FILE_DOWNLOAD listener replacing the downloaded file
// with patchedZip, recording what it saw of vendor/composer when called.
type patcher struct {
	t      *testing.T
	vendor string
	// seen are the entries of vendor/composer at each event.
	seen [][]string
}

func (pt *patcher) dispatcher() *listeningDispatcher {
	return &listeningDispatcher{
		events: []string{eventdispatcher.PostFileDownload},
		listener: func(e eventdispatcher.Event) error {
			ev, ok := e.(*eventdispatcher.PostFileDownloadEvent)
			if !ok {
				return nil
			}

			entries, _ := os.ReadDir(pt.vendor + "/composer")
			names := make([]string, len(entries))
			for i, e := range entries {
				names[i] = e.Name()
			}

			pt.seen = append(pt.seen, names)

			return os.WriteFile(ev.FileName().S, patchedZip(), 0o644)
		},
	}
}

func readComposerJSON(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path + "/composer.json")
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// On a cache hit whose release the store holds, POST_FILE_DOWNLOAD fires
// as Composer fires it: before anything of the package is extracted, with
// the cached archive at the file name, and what the listener leaves there
// is what gets installed. The shared store keeps the dist's own tree.
func TestArchiveDownloader_PostFileDownloadListenerOnStoreHit(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	opts := projectOptions{store: newStore(t), cacheDir: t.TempDir()}
	p := distPackage(srv.URL+"/a.zip", "zip")

	installIn(t, opts, p)

	pr := newProject(t, t.TempDir(), opts)
	pt := &patcher{t: t, vendor: pr.vendor}
	pr.deps.EventDispatcher = pt.dispatcher()

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	path := pr.vendor + "/" + p.PrettyName()
	if err := os.MkdirAll(path, 0o777); err != nil {
		t.Fatal(err)
	}

	promise, err := d.Download(p, path, nil)
	if err != nil {
		t.Fatal(err)
	}

	// dispatched synchronously, as on Composer's resolved promise
	if len(pt.seen) != 1 {
		t.Fatalf("POST_FILE_DOWNLOAD fired %d times before the download was waited for, want 1", len(pt.seen))
	}

	// nothing but the temporary file holding the archive
	if len(pt.seen[0]) != 1 {
		t.Fatalf("vendor/composer held %q at POST_FILE_DOWNLOAD, want only the archive", pt.seen[0])
	}

	if _, err := promise.Await(); err != nil {
		t.Fatal(err)
	}

	if err := await(d.Install(p, path)); err != nil {
		t.Fatal(err)
	}

	if err := await(d.Cleanup(operation.TypeInstall, p, path, nil)); err != nil {
		t.Fatal(err)
	}

	if got := readComposerJSON(t, path); got != `{"name":"a/b","patched":true}` {
		t.Fatalf("installed composer.json %q, want the listener's", got)
	}

	checkNoLeftovers(t, pr.vendor)

	// without the listener, the dist's own tree
	plain, _ := installIn(t, opts, p)
	checkTree(t, plain)

	if n := srv.requests("/a.zip"); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}

// A downloaded archive a POST_FILE_DOWNLOAD listener changes is what gets
// installed, and never enters the shared store as the dist's tree.
func TestArchiveDownloader_PostFileDownloadListenerOnDownload(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	opts := projectOptions{store: newStore(t), cacheDir: t.TempDir()}
	p := distPackage(srv.URL+"/a.zip", "zip")

	pr := newProject(t, t.TempDir(), opts)
	pt := &patcher{t: t, vendor: pr.vendor}
	pr.deps.EventDispatcher = pt.dispatcher()

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	installed, err := pr.install(d, p)
	if err != nil {
		t.Fatal(err)
	}

	if got := readComposerJSON(t, installed); got != `{"name":"a/b","patched":true}` {
		t.Fatalf("installed composer.json %q, want the listener's", got)
	}

	checkNoLeftovers(t, pr.vendor)

	if _, err := opts.store.Lookup(storeDist(p)); err == nil {
		t.Fatal("the listener's archive entered the shared store")
	}

	// the files cache holds the download, unchanged as Composer keeps it
	plain, _ := installIn(t, opts, p)
	checkTree(t, plain)

	if n := srv.requests("/a.zip"); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}

// A listener that leaves the file as it was keeps the store fast path.
func TestArchiveDownloader_PostFileDownloadReaderKeepsStore(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})

	hardlinks, err := store.Open(t.TempDir(), &store.Options{Method: store.Hardlink})
	if err != nil {
		t.Fatal(err)
	}

	opts := projectOptions{store: hardlinks, cacheDir: t.TempDir()}
	p := distPackage(srv.URL+"/a.zip", "zip")

	first, _ := installIn(t, opts, p)

	pr := newProject(t, t.TempDir(), opts)
	pr.deps.EventDispatcher = &listeningDispatcher{events: []string{eventdispatcher.PostFileDownload}, listener: func(eventdispatcher.Event) error { return nil }}

	d, err := NewZipDownloader(pr.deps)
	if err != nil {
		t.Fatal(err)
	}

	installed, err := pr.install(d, p)
	if err != nil {
		t.Fatal(err)
	}

	checkTree(t, installed)
	checkNoLeftovers(t, pr.vendor)

	if plain, _ := installIn(t, opts, p); !sameFile(t, first+"/composer.json", plain+"/composer.json") {
		t.Skip("the store does not hardlink here (reflinks or another device)")
	}

	if !sameFile(t, first+"/composer.json", installed+"/composer.json") {
		t.Fatal("the package was not imported from the store")
	}
}

// Nothing of a package is materialized ahead when a POST_FILE_DOWNLOAD
// listener waits: Composer extracts after the event.
func TestDownloadManager_PrefetchSkipsMaterialWithPostListener(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	opts := projectOptions{store: newStore(t), cacheDir: t.TempDir()}
	p := distPackage(srv.URL+"/a.zip", "zip")

	installIn(t, opts, p)

	for _, tc := range []struct {
		events []string
		ahead  bool
	}{
		{events: nil, ahead: true},
		{events: []string{eventdispatcher.PostFileDownload}, ahead: false},
	} {
		pr := newProject(t, t.TempDir(), opts)
		pr.deps.EventDispatcher = &listeningDispatcher{events: tc.events}

		zip, err := NewZipDownloader(pr.deps)
		if err != nil {
			t.Fatal(err)
		}

		m := NewDownloadManager(nullIO(), false, nil)
		m.SetDownloader("zip", zip)

		var q pkg.PackageInterface = distPackage(srv.URL+"/a.zip", "zip")
		m.Prefetch(q, nil)

		f := FileDownloaderOf(zip)
		if got := f.specs[q] != nil; got != tc.ahead {
			t.Errorf("listeners %q: materialized ahead %v, want %v", tc.events, got, tc.ahead)
		}

		m.DiscardPrefetched()
	}
}
