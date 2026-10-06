package composerrepo

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/metadataminifier"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util/http"
)

// TestExpandEach checks expandEach against MetadataMinifier::expand on
// random minified lists (unset keys, keys set again, emptied versions) and
// on the p2 fixtures: the same versions, keys in the same order, the same
// values, and keep() giving the entry itself where expand does.
func TestExpandEach(t *testing.T) {
	check := func(t *testing.T, items []*php.Array) {
		t.Helper()

		want := metadataminifier.Expand(items)
		values := make([]any, len(items))
		for i, a := range items {
			values[i] = a
		}
		var got []*php.Array
		i := 0
		ok, err := expandEach(values, func(data *php.Array, keep func() *php.Array) error {
			if !php.StrictEquals(data, want[i]) || !slices.Equal(data.Keys(), want[i].Keys()) {
				t.Fatalf("version %d: %s, want %s", i, encode(t, data), encode(t, want[i]))
			}
			kept := keep()
			if (kept == items[i]) != (want[i] == items[i]) {
				t.Fatalf("version %d: kept the entry itself: %v", i, kept == items[i])
			}
			got = append(got, kept)
			i++

			return nil
		})
		if !ok || err != nil || len(got) != len(want) {
			t.Fatalf("ok %v, err %v, %d versions, want %d", ok, err, len(got), len(want))
		}
		for i := range got {
			if !php.StrictEquals(got[i], want[i]) || !slices.Equal(got[i].Keys(), want[i].Keys()) {
				t.Fatalf("kept version %d: %s, want %s", i, encode(t, got[i]), encode(t, want[i]))
			}
		}
	}

	rng := rand.New(rand.NewPCG(5, 6))
	for round := range 300 {
		var items []*php.Array
		for range rng.IntN(12) {
			a := php.NewArray()
			for range rng.IntN(6) {
				key := string(rune('a' + rng.IntN(8)))
				switch rng.IntN(4) {
				case 0:
					a.Set(key, "__unset")
				case 1:
					a.Set(key, php.ListOf(rng.IntN(3)))
				default:
					a.Set(key, strconv.Itoa(rng.IntN(3)))
				}
			}
			items = append(items, a)
		}
		t.Run(strconv.Itoa(round), func(t *testing.T) { check(t, items) })
	}

	for _, name := range p2Names {
		fixture := strings.ReplaceAll(name, "/", "_")
		data := decodeArray(readGzip(t, p2Fixtures+"/"+fixture+".json.gz"))
		var items []*php.Array
		for _, v := range asArray(packageVersions(data, name)).Values() {
			items = append(items, v.(*php.Array))
		}
		t.Run(name, func(t *testing.T) { check(t, items) })
	}

	if ok, _ := expandEach([]any{php.ArrayOf(0, "x")}, nil); ok {
		t.Error("int keys: expandEach applies")
	}
	if ok, _ := expandEach([]any{"x"}, nil); ok {
		t.Error("a string entry: expandEach applies")
	}
}

// TestSlimFile checks that the slim form of a metadata file answers what
// the advisory and filter loads ask of it as the file does.
func TestSlimFile(t *testing.T) {
	advisories := php.ListOf(php.ArrayOf("advisoryId", "PKSA-1", "affectedVersions", "<1.0"))
	data := php.ArrayOf(
		"minified", "composer/2.0",
		"packages", php.ArrayOf("a/a", php.ListOf(php.ArrayOf("name", "a/a")), "b/b", nil, "c/c", false),
		"security-advisories", advisories,
		"last-modified", "Mon, 01 Sep 2025 10:00:00 GMT",
	)
	slim := slimFile(data)

	for _, name := range []string{"a/a", "b/b", "c/c", "d/d"} {
		if got, want := packageVersions(slim, name) == nil, packageVersions(data, name) == nil; got != want {
			t.Errorf("isset packages[%s]: %v, want %v", name, !got, !want)
		}
	}
	for _, key := range []string{"minified", "security-advisories", "last-modified", "filter"} {
		if !php.StrictEquals(get(slim, key), get(data, key)) {
			t.Errorf("%s: %v, want %v", key, get(slim, key), get(data, key))
		}
	}
	if get(slim, "security-advisories") == advisories {
		t.Error("the advisories are shared with the file")
	}

	var d decodedFiles
	d.rememberSlim("provider-a~a.json", "json", data)
	if got := d.slimOf("provider-a~a.json", "json"); !php.StrictEquals(got, slim) {
		t.Errorf("slimOf: %s", encode(t, got))
	}
	if got := d.slimOf("provider-a~a.json", "other json"); got != nil {
		t.Errorf("slimOf another file: %s", encode(t, got))
	}
}

// TestDecodedFiles_Offer checks that a decoded file is handed over once,
// for its JSON, with its packages, and not after its speculation stopped.
func TestDecodedFiles_Offer(t *testing.T) {
	var d decodedFiles
	gen := d.startSpeculation()
	data := php.NewArray()
	pre := &prebuilt{name: "a/a"}
	d.offer(gen, "k", "json", data, pre)

	if got := d.take("k", "other"); got != nil {
		t.Error("taken for another JSON")
	}
	if got := d.take("k", "json"); got != data {
		t.Error("not taken")
	}
	if got := d.take("k", "json"); got != nil {
		t.Error("taken twice")
	}
	if got := d.prebuiltFor(data); got != pre {
		t.Error("packages not handed over")
	}
	if got := d.prebuiltFor(data); got != nil {
		t.Error("packages handed over twice")
	}

	d.stopSpeculation(gen)
	d.offer(gen, "k", "json", data, nil)
	if got := d.take("k", "json"); got != nil {
		t.Error("taken from a stopped speculation")
	}
}

// TestPrebuilt_Take checks that a package built ahead is only taken by a
// load that builds it alike: the same name and notify URL, and once.
func TestPrebuilt_Take(t *testing.T) {
	p := pkg.NewCompletePackage("a/a", "1.0.0.0", "1.0.0")
	pre := &prebuilt{name: "a/a", notifyURL: "https://example.org/downloads/", packages: map[int]pkg.PackageInterface{3: p}}

	if pre.take("b/b", "https://example.org/downloads/", 3) != nil || pre.take("a/a", "", 3) != nil || pre.take("a/a", "https://example.org/downloads/", 2) != nil {
		t.Error("taken by another load")
	}
	if pre.take("a/a", "https://example.org/downloads/", 3) != p {
		t.Error("not taken")
	}
	if pre.take("a/a", "https://example.org/downloads/", 3) != nil {
		t.Error("taken twice")
	}
	if (*prebuilt)(nil).take("a/a", "", 0) != nil {
		t.Error("taken from nothing")
	}
}

// TestComposerRepository_SpeculateLoads loads the p2 fixtures through the
// real HttpDownloader with a speculation running ahead, cold and warm, and
// compares every package (dump, notification URL, mirrors) with the loads
// made without it; no file is requested twice.
func TestComposerRepository_SpeculateLoads(t *testing.T) {
	server := newP2Server(t)
	acceptable := php.ArrayOf("stable", 0, "dev", 20)
	constraints := &repository.ConstraintMap{}
	for _, name := range p2Names {
		constraints.Set(name, must(pkg.NewVersionParser().ParseConstraints(">=2")))
	}

	dumpAll := func(t *testing.T, result repository.LoadResult) []string {
		t.Helper()

		var out []string
		for _, p := range result.Packages {
			data, err := dumper.ArrayDumper{}.Dump(p)
			if err != nil {
				t.Fatal(err)
			}
			mirrors := "null"
			if m := p.DistMirrors(); m != nil {
				mirrors = encode(t, m)
			}
			out = append(out, encode(t, data)+" "+p.NotificationURL().S+" "+mirrors+" "+encode(t, p.TransportOptions()))
		}

		return out
	}

	var want [][]string
	for _, speculate := range []bool{false, true} {
		cfg := createConfig(t, "secure-http", false)
		var results [][]string
		for _, warm := range []bool{false, true} {
			// with TLS set up (the CA checked), as a Loop's: requests are
			// prefetched
			downloader, err := http.NewHttpDownloader(io.NewNullIO(), cfg.ForHTTP(), nil, false, http.NewStaticRuntime("8.4.0", "2.10.3"))
			if err != nil {
				t.Fatal(err)
			}
			downloader.EnableAsync()
			repo := newRepo(t, php.ArrayOf("url", server.URL), cfg, downloader)
			stop := func() {}
			var s *speculation
			if speculate {
				onSpeculation = func(started *speculation) { s = started }
				stop = repo.SpeculateLoads(constraints.Clone(), func(string) bool { return false }, acceptable, php.NewArray())
				onSpeculation = nil
				if warm {
					// with the root file cached, the speculation runs
					// at once: all of it is ahead of the loads
					s.busy.Wait()
					if n := len(repo.decoded.ahead); n < len(p2Names) {
						t.Errorf("%d files decoded ahead", n)
					}
				}
			}
			result := must(repo.LoadPackages(constraints.Clone(), acceptable, php.NewArray(), nil))
			if s != nil {
				s.busy.Wait()
			}
			if s != nil && warm {
				for _, name := range p2Names {
					if _, ok := repo.decoded.ahead["provider-"+strings.ReplaceAll(name, "/", "~")+".json"]; ok {
						t.Errorf("%s: the file decoded ahead was not taken", name)
					}
				}
			}
			stop()

			requests := server.takeRequests()
			if speculate && len(requests) <= 1+2*len(p2Names) {
				t.Errorf("warm %v: the speculation requested nothing ahead: %v", warm, requests)
			}
			for i := 1; i < len(requests); i++ {
				if requests[i] == requests[i-1] {
					t.Errorf("speculate %v, warm %v: %s requested twice", speculate, warm, requests[i])
				}
			}
			if len(result.Packages) < 100 {
				t.Fatalf("%d packages", len(result.Packages))
			}
			results = append(results, dumpAll(t, result))
		}

		if speculate {
			for i := range want {
				if !slices.Equal(results[i], want[i]) {
					t.Errorf("warm %v: the packages differ from the loads without speculation", i == 1)
				}
			}
		} else {
			want = results
		}
	}
}
