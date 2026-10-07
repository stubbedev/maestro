package composerrepo

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Loads that read their files back from the decoded cache build skeleton
// packages, with and without a speculation ahead of them; once complete
// they are the packages loads without the cache build, the repository's
// mirrors and transport options included.
func TestComposerRepository_SkeletonLoads(t *testing.T) {
	server := newP2Server(t)
	acceptable, flags := php.ArrayOf("stable", 0, "dev", 20), php.NewArray()
	constraints := &repository.ConstraintMap{}
	for _, name := range p2Names {
		constraints.Set(name, must(pkg.NewVersionParser().ParseConstraints(">=2")))
	}

	load := func(t *testing.T, cfg *config.Config, speculate bool) (dump []string, skeletons int) {
		t.Helper()
		downloader, err := http.NewHttpDownloader(io.NewNullIO(), cfg.ForHTTP(), nil, false, http.NewStaticRuntime("8.4.0", "2.10.3"))
		if err != nil {
			t.Fatal(err)
		}
		downloader.EnableAsync()
		repo := newRepo(t, php.ArrayOf("url", server.URL), cfg, downloader)
		stop := func() {}
		var s *speculation
		if speculate {
			repo.observe.speculation = func(started *speculation) { s = started }
			stop = repo.SpeculateLoads(constraints.Clone(), func(string) bool { return false }, acceptable, flags)
		}
		result := must(repo.LoadPackages(constraints.Clone(), acceptable, flags, nil))
		if s != nil {
			s.busy.Wait()
		}
		stop()
		server.takeRequests()

		for _, p := range result.Packages {
			if pkg.IsSkeleton(p) {
				skeletons++
			}
		}
		for _, p := range result.Packages {
			data, err := dumper.ArrayDumper{}.Dump(p)
			if err != nil {
				t.Fatal(err)
			}
			mirrors := "null"
			if m := p.DistMirrors(); m != nil {
				mirrors = encode(t, m)
			}
			dump = append(dump, encode(t, data)+" "+p.NotificationURL().S+" "+mirrors+" "+encode(t, p.TransportOptions()))
		}

		return dump, skeletons
	}

	want, _ := load(t, createConfig(t, "secure-http", false), false)
	if len(want) < 100 {
		t.Fatalf("%d packages", len(want))
	}

	withDecodedCache(t)
	for _, speculate := range []bool{false, true} {
		cfg := createConfig(t, "secure-http", false)
		// cold, then warm: the files are cached and their slots written,
		// then read back
		for pass := range 3 {
			got, skeletons := load(t, cfg, speculate)
			if !slices.Equal(got, want) {
				t.Errorf("speculate %v, pass %d: the packages differ from the loads without the decoded cache", speculate, pass)
			}
			if pass == 2 && skeletons == 0 {
				t.Errorf("speculate %v: no skeleton packages", speculate)
			}
		}
	}
}
