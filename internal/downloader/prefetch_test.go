package downloader

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// prefetchHTTP records PrefetchCopy calls.
type prefetchHTTP struct {
	fakeHTTP
	prefetched []string
}

func (f *prefetchHTTP) PrefetchCopy(url string, _ *php.Array) {
	f.prefetched = append(f.prefetched, url)
}

// holdsCache is a files cache holding the keys in held.
type holdsCache struct {
	fakeCache
	held []string
}

func (c *holdsCache) Holds(key string) bool { return slices.Contains(c.held, key) }

func TestDownloadManager_Prefetch(t *testing.T) {
	zipPackage := func(name string) *pkg.CompletePackage {
		p := getPackage(name, "1.0.0.0", "1.0.0")
		p.SetDistType(pkg.Str("zip"))
		p.SetDistURL(pkg.Str("https://example.org/" + name + ".zip"))
		p.SetSourceType(pkg.Str("git"))
		p.SetSourceURL(pkg.Str("https://example.org/" + name + ".git"))

		return p
	}

	cached := zipPackage("a/cached")
	meta := zipPackage("a/meta")
	meta.SetType("metapackage")

	for _, tc := range []struct {
		name         string
		preferSource bool
		events       EventDispatcher
		want         []string
	}{
		{name: "dist", want: []string{"https://example.org/a/new.zip"}},
		{name: "prefer source", preferSource: true},
		// a listener might change the request: only a dispatcher that tells
		// it has none allows the prefetch
		{name: "unknown listeners", events: &fakeDispatcher{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &prefetchHTTP{}
			c := &holdsCache{t: t, held: []string{cacheKey(cached, "https://example.org/a/cached.zip")}}

			zip, err := NewZipDownloader(Deps{IO: nullIO(), Config: getConfig(t), HTTPDownloader: h, Cache: c, EventDispatcher: tc.events})
			if err != nil {
				t.Fatal(err)
			}

			m := NewDownloadManager(nullIO(), tc.preferSource, nil)
			m.SetDownloader("zip", zip)

			for _, p := range []pkg.PackageInterface{zipPackage("a/new"), cached, meta} {
				m.Prefetch(p, nil)
			}

			if !slices.Equal(h.prefetched, tc.want) {
				t.Errorf("prefetched %q, want %q", h.prefetched, tc.want)
			}
			if len(h.calls) != 0 {
				t.Errorf("downloads %q", h.calls)
			}
		})
	}
}

// A dist the files cache holds and the store has is materialized while
// the lock is verified; the download takes that tree, and what no
// download took goes, with the directories made for it.
func TestDownloadManager_PrefetchMaterializes(t *testing.T) {
	srv := newDistServer(t, map[string][]byte{"/a.zip": githubZip()})
	shared := newStore(t)
	files := t.TempDir()

	first := newProject(t, t.TempDir(), projectOptions{store: shared, cacheDir: files})

	d, err := NewZipDownloader(first.deps)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := first.install(d, distPackage(srv.URL+"/a.zip", "zip")); err != nil {
		t.Fatal(err)
	}

	prefetched := func(t *testing.T) (*project, *FileDownloader, *DownloadManager, pkg.PackageInterface) {
		t.Helper()

		pr := newProject(t, t.TempDir(), projectOptions{store: shared, cacheDir: files, verbosity: console.VerbosityVeryVerbose})

		zip, err := NewZipDownloader(pr.deps)
		if err != nil {
			t.Fatal(err)
		}

		m := NewDownloadManager(nullIO(), false, nil)
		m.SetDownloader("zip", zip)

		p := distPackage(srv.URL+"/a.zip", "zip")
		m.Prefetch(p, nil)

		f := FileDownloaderOf(zip)

		sp := f.specs[p]
		if sp == nil {
			t.Fatal("nothing materialized ahead")
		}

		<-sp.done
		if sp.err != nil {
			t.Fatal(sp.err)
		}

		return pr, f, m, p
	}

	t.Run("taken", func(t *testing.T) {
		pr, f, m, p := prefetched(t)

		path, err := pr.install(f, p)
		if err != nil {
			t.Fatal(err)
		}

		if sp, ok := f.specs[p]; !ok || sp != nil {
			t.Fatalf("tree not taken: %v", sp)
		}

		m.DiscardPrefetched()
		checkTree(t, path)
		checkNoLeftovers(t, pr.vendor)

		want := []string{
			"  - Loading a/b (1.0.0) from cache",
			"  - Installing a/b (1.0.0): Extracting archive",
		}
		if got := downloadLines(pr.out); !slices.Equal(got, want) {
			t.Fatalf("output %q, want %q", got, want)
		}
	})

	t.Run("discarded", func(t *testing.T) {
		pr, _, m, _ := prefetched(t)

		m.DiscardPrefetched()

		// vendor/composer and vendor were made for it
		if _, err := os.Lstat(pr.vendor); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("vendor dir left: %v", err)
		}
	})

	if n := srv.requests("/a.zip"); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}
