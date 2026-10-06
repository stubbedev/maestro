package downloader

import (
	"slices"
	"testing"

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
