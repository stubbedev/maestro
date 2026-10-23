package composerrepo

import (
	"os"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/fsstate"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
)

const resolvedLastModified = "Tue, 01 Jan 2030 00:00:00 GMT"

// resolvedRepo is a repository of cfg whose root file names its v2
// metadata, which loads a/a as the loads do (and finds b/b missing, with
// notFound), and returns what Resolved returns then.
func resolvedRepo(t *testing.T, cfg *config.Config, notFound bool) []MetadataFile {
	t.Helper()
	lastModified := []string{"Last-Modified: " + resolvedLastModified}
	repo := newRepo(t, php.ArrayOf("url", "https://example.org"), cfg, mock(t,
		httpmock.Expectation{URL: "https://example.org/packages.json", Body: encode(t, php.ArrayOf("metadata-url", "/p2/%package%.json")), Headers: lastModified},
		httpmock.Expectation{URL: "https://example.org/p2/a/a.json", Body: encode(t, php.ArrayOf("packages", php.ArrayOf("a/a", php.NewArray()))), Headers: lastModified},
	))
	if _, _, err := repo.loadRootServerFile(600); err != nil {
		t.Fatal(err)
	}
	downloads := must(repo.startCachedAsyncDownloads([]string{"a/a"}, []string{""}, false))
	for _, d := range downloads {
		repo.waitFetches([]*asyncFetch{d.fetch})
		if _, _, err := repo.finishCachedDownload(d); err != nil {
			t.Fatal(err)
		}
	}

	if notFound {
		// as rejectFetch records a 404 (the mock cannot answer one)
		url := "https://example.org/p2/b/b.json"
		repo.fetched[url] = fetchedFile{cacheKey: "provider-b~b.json"}
		repo.packagesNotFoundCache[url] = struct{}{}
	}

	// the files were just written: too recent to be trusted
	if _, ok := repo.Resolved(time.Now(), 0); ok {
		t.Fatal("files written within the margin are resolved")
	}
	files, ok := repo.Resolved(time.Now(), fsstate.Margin(-time.Hour))
	if !ok {
		t.Fatal("not resolved")
	}

	return files
}

// Resolved lists the root file and every metadata file the loads read,
// with the identity of their cached copies or as not found.
func TestResolved(t *testing.T) {
	files := resolvedRepo(t, createConfig(t), true)
	if len(files) != 3 {
		t.Fatalf("resolved %+v", files)
	}
	root, a, b := files[0], files[1], files[2]
	if !root.Root || root.CacheKey != rootCacheKey || root.ID == (fsstate.ID{}) {
		t.Errorf("root %+v", root)
	}
	if a.URL != "https://example.org/p2/a/a.json" || a.NotFound || a.ID == (fsstate.ID{}) || a.CacheKey != "provider-a~a.json" {
		t.Errorf("a/a %+v", a)
	}
	if b.URL != "https://example.org/p2/b/b.json" || !b.NotFound {
		t.Errorf("b/b %+v", b)
	}
}

// Revalidate tells the files unchanged only when every request answers
// that they did not change and no cached copy changed.
func TestRevalidate(t *testing.T) {
	for _, tc := range []struct {
		name         string
		change       func(t *testing.T, cfg *config.Config)
		expectations []httpmock.Expectation
		want         bool
	}{
		{"unchanged", nil, []httpmock.Expectation{
			{URL: "https://example.org/p2/a/a.json", Status: 304},
		}, true},
		{"changed upstream", nil, []httpmock.Expectation{
			{URL: "https://example.org/p2/a/a.json", Body: `{"packages":{"a/a":[]},"x":1}`},
		}, false},
		{"cached copy changed", func(t *testing.T, cfg *config.Config) {
			t.Helper()
			path, _ := newRepo(t, php.ArrayOf("url", "https://example.org"), cfg, mock(t)).cache.Path("provider-a~a.json")
			if err := os.WriteFile(path, []byte(`{"packages":{"a/a":[]},"last-modified":"`+resolvedLastModified+`"}`), 0o644); err != nil {
				t.Fatal(err)
			}
		}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := createConfig(t)
			files := resolvedRepo(t, cfg, false)
			if tc.change != nil {
				tc.change(t, cfg)
			}
			repo := newRepo(t, php.ArrayOf("url", "https://example.org"), cfg, mock(t, tc.expectations...))
			if got := must(repo.Revalidate(files)); got != tc.want {
				t.Errorf("revalidated %v, want %v", got, tc.want)
			}
		})
	}
}

// A repository that read data no cache file keeps is not resolved.
func TestResolved_Uncached(t *testing.T) {
	repo := newRepo(t, php.ArrayOf("url", "https://example.org"), createConfig(t), mock(t))
	repo.rootLoaded, repo.lazyProvidersURL, repo.uncached = true, "https://example.org/p2/%package%.json", true
	if _, ok := repo.Resolved(time.Now(), fsstate.Margin(-time.Hour)); ok {
		t.Error("resolved after an API request")
	}
}
