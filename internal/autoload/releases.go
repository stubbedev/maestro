// Ports nothing: what the package store knows of installed package trees,
// for the class map scans (deliberate deviation 3, speed).

package autoload

import (
	"runtime"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/archive"
	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/store"
)

// UseStore lets the class map scans know the files of packages installed
// from maestro's package store by their stamps instead of reading them,
// and keep the classes found in them with their releases (store derived
// data), where any project installing the same release finds them.
func (g *Generator) UseStore(s *store.Store) { g.store = s }

// releaseResults names the derived data the class map results are kept
// as.
const releaseResults = "classmap"

// addReleases tells the parse cache, in the background, the store
// releases of packages (package map entries without the root package,
// which is never one).
func (g *Generator) addReleases(packages []PackageMapEntry) {
	if g.store == nil || len(packages) == 0 {
		return
	}
	g.parseCache.AddReleasesAsync(func() []classmap.Release { return g.findReleases(packages) })
}

// findReleases looks the releases of the packages up in the store.
func (g *Generator) findReleases(packages []PackageMapEntry) []classmap.Release {
	found := make([]classmap.Release, len(packages))
	var (
		next int
		mu   sync.Mutex
		wg   sync.WaitGroup
	)
	for range min(runtime.GOMAXPROCS(0), len(packages)) {
		wg.Go(func() {
			for {
				mu.Lock()
				i := next
				next++
				mu.Unlock()
				if i >= len(packages) {
					return
				}
				if r := g.storeRelease(packages[i].Package); r != nil {
					found[i] = r
				}
			}
		})
	}
	wg.Wait()

	releases := found[:0]
	for _, r := range found {
		if r != nil {
			releases = append(releases, r)
		}
	}

	return releases
}

// storeRelease is the store's release of p's dist, nil when it has none.
func (g *Generator) storeRelease(p pkg.PackageInterface) classmap.Release {
	d := store.Dist{
		Name:      p.Name(),
		Type:      p.DistType().S,
		URL:       p.DistURL().S,
		Reference: p.DistReference().S,
		Shasum:    p.DistSha1Checksum().S,
	}
	if _, ok := archive.ParseFormat(d.Type); !ok {
		return nil
	}
	r, err := g.store.Lookup(d)
	if err != nil {
		return nil
	}

	return &storeRelease{
		store:   g.store,
		release: r,
		name:    strings.Join([]string{d.Name, d.Type, d.URL, d.Reference, d.Shasum}, "\x00"),
	}
}

// storeRelease is a store release as the class map scans see it.
type storeRelease struct {
	store   *store.Store
	release *store.Release
	name    string
}

func (r *storeRelease) Name() string { return r.name }

// Files are the release's files with an extension the scans parse.
func (r *storeRelease) Files() []classmap.StampedFile {
	entries := r.release.Entries()
	files := make([]classmap.StampedFile, 0, len(entries))
	for i := range entries {
		e := &entries[i]
		if e.Kind != archive.File || !scanned(e.Path) {
			continue
		}
		files = append(files, classmap.StampedFile{Path: e.Path, Size: e.Size, ModTime: e.ModTime(), Sum: e.Hash})
	}

	return files
}

func (r *storeRelease) ReadResults() ([]byte, error) {
	return r.store.ReadDerived(r.release, releaseResults)
}

func (r *storeRelease) WriteResults(data []byte) error {
	return r.store.WriteDerived(r.release, releaseResults, data)
}

// scanned reports whether a file has one of the extensions the class map
// scans parse (classMapExtensions).
func scanned(path string) bool {
	for _, ext := range classMapExtensions {
		if len(path) > len(ext) && path[len(path)-len(ext)-1] == '.' && strings.HasSuffix(path, ext) {
			return true
		}
	}

	return false
}
