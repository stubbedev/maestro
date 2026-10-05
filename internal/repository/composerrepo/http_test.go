package composerrepo

import (
	stdhttp "net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util/http"
)

var p2Names = []string{"doctrine/orm", "guzzlehttp/guzzle", "laravel/framework", "monolog/monolog", "phpunit/phpunit", "symfony/symfony"}

const p2LastModified = "Mon, 01 Sep 2025 10:00:00 GMT"

// p2Server serves the p2 fixtures as a v2 repository, with Last-Modified
// and If-Modified-Since, and records the requests.
type p2Server struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
}

func newP2Server(t testing.TB) *p2Server {
	t.Helper()

	files := map[string]string{}
	for _, name := range p2Names {
		fixture := strings.ReplaceAll(name, "/", "_")
		files["/p2/"+name+".json"] = readGzip(t, p2Fixtures+"/"+fixture+".json.gz")
		files["/p2/"+name+"~dev.json"] = readGzip(t, p2Fixtures+"/"+fixture+"~dev.json.gz")
	}
	files["/packages.json"] = `{"packages":[],"metadata-url":"/p2/%package%.json","notify-batch":"/downloads/"}`

	s := &p2Server{}
	s.Server = httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, req *stdhttp.Request) {
		s.mu.Lock()
		s.requests = append(s.requests, req.URL.Path+" "+req.Header.Get("If-Modified-Since"))
		s.mu.Unlock()

		body, ok := files[req.URL.Path]
		if !ok {
			stdhttp.NotFound(w, req)

			return
		}
		if req.Header.Get("If-Modified-Since") == p2LastModified {
			w.WriteHeader(stdhttp.StatusNotModified)

			return
		}
		w.Header().Set("Last-Modified", p2LastModified)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(s.Close)

	return s
}

func (s *p2Server) takeRequests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := s.requests
	s.requests = nil
	slices.Sort(out)

	return out
}

func realDownloader(t testing.TB, cfg *config.Config) *http.HttpDownloader {
	t.Helper()

	d, err := http.NewHttpDownloader(io.NewNullIO(), cfg.ForHTTP(), nil, true, http.NewStaticRuntime("8.4.0", "2.10.3"))
	if err != nil {
		t.Fatal(err)
	}

	return d
}

func allP2Names() *repository.ConstraintMap {
	m := &repository.ConstraintMap{}
	for _, name := range p2Names {
		m.Set(name, nil)
	}

	return m
}

// TestComposerRepository_RealHttpDownloader loads the p2 fixtures through
// the real HttpDownloader (parallel requests, decoding on other
// goroutines) from a local server, cold and then with a warm cache
// (If-Modified-Since, 304), and compares with the fake server's results.
func TestComposerRepository_RealHttpDownloader(t *testing.T) {
	server := newP2Server(t)
	cfg := createConfig(t, "secure-http", false)
	acceptable := php.ArrayOf("stable", 0, "dev", 20)

	load := func() repository.LoadResult {
		repo := newRepo(t, php.ArrayOf("url", server.URL), cfg, realDownloader(t, cfg))

		return must(repo.LoadPackages(allP2Names(), acceptable, php.NewArray(), nil))
	}

	cold := load()
	var wantRequests []string
	wantRequests = append(wantRequests, "/packages.json ")
	for _, name := range p2Names {
		wantRequests = append(wantRequests, "/p2/"+name+".json ", "/p2/"+name+"~dev.json ")
	}
	slices.Sort(wantRequests)
	if got := server.takeRequests(); !slices.Equal(got, wantRequests) {
		t.Errorf("cold requests:\n got %v\nwant %v", got, wantRequests)
	}

	warm := load()
	for i := range wantRequests {
		wantRequests[i] += p2LastModified
	}
	if got := server.takeRequests(); !slices.Equal(got, wantRequests) {
		t.Errorf("warm requests:\n got %v\nwant %v", got, wantRequests)
	}

	if !slices.Equal(cold.NamesFound, p2Names) || !slices.Equal(warm.NamesFound, p2Names) {
		t.Errorf("names found: %v / %v", cold.NamesFound, warm.NamesFound)
	}
	if len(cold.Packages) != len(warm.Packages) || len(cold.Packages) < 1000 {
		t.Fatalf("%d / %d packages", len(cold.Packages), len(warm.Packages))
	}
	for i := range cold.Packages {
		if cold.Packages[i].UniqueName() != warm.Packages[i].UniqueName() {
			t.Fatalf("#%d: %s / %s", i, cold.Packages[i].UniqueName(), warm.Packages[i].UniqueName())
		}
	}
}

// BenchmarkLoadPackages measures loadPackages of the six p2 fixtures
// (with their ~dev files) through the real HttpDownloader from a local
// server, with a warm cache (304 responses), as `composer update` does on
// a second run.
func BenchmarkLoadPackages(b *testing.B) {
	server := newP2Server(b)
	home := b.TempDir()
	cfg := config.New(false, "")
	if err := cfg.Merge(php.ArrayOf("config", php.ArrayOf("home", home, "cache-dir", home+"/cache", "secure-http", false)), "test"); err != nil {
		b.Fatal(err)
	}
	acceptable := php.ArrayOf("stable", 0, "dev", 20)

	for b.Loop() {
		repo, err := New(php.ArrayOf("url", server.URL), io.NewNullIO(), cfg, realDownloader(b, cfg), nil)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := repo.LoadPackages(allP2Names(), acceptable, php.NewArray(), nil); err != nil {
			b.Fatal(err)
		}
	}
}
