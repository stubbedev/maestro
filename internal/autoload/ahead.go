// Ports nothing: the class map scans' parsing done while packages enter
// the package store (deliberate deviation 3, speed).

package autoload

import (
	"path"
	"strings"

	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/store"
)

// aheadMode is what ParseAhead asked for: nothing, the class maps, or
// the class maps and the PSR-0 and PSR-4 directories.
type aheadMode int32

const (
	parseNothing aheadMode = iota
	parseClassmaps
	parseClassmapsAndPsr
)

// ParseAhead has the files of the packages the package store inserts
// from now on (Deriver) parsed as they are inserted, while other
// downloads are still running, and the results kept with their store
// releases, where the class map scans of the dump that follows find them
// (UseStore) instead of reading, hashing and parsing the installed files:
// those of the package's own classmap rules and, for scanPsrPackages (or
// a class map authoritative generator), of its PSR-0 and PSR-4
// directories. It changes no result: a scan uses a result kept for a
// file only while the file still holds the content parsed, and parses
// whatever it does not find, whichever files the dump turns out to scan.
func (g *Generator) ParseAhead(scanPsrPackages bool) {
	mode := parseClassmaps
	if scanPsrPackages || g.classMapAuthoritative {
		mode = parseClassmapsAndPsr
	}
	g.parseAhead.Store(int32(mode))
}

// Deriver returns what parses p's files as the package store inserts its
// archive (ParseAhead), nil when nothing is to be parsed.
func (g *Generator) Deriver(p pkg.PackageInterface) store.Deriver {
	mode := aheadMode(g.parseAhead.Load())
	if mode == parseNothing || g.store == nil {
		return nil
	}
	accept, ok := aheadFiles(p, mode == parseClassmapsAndPsr)
	if !ok {
		return nil
	}

	return releaseParse{classmap.NewReleaseParse(g.Parser, accept)}
}

// aheadFiles returns which of the files of p's tree its own autoload rules
// may have the class map scans parse: those in or under the paths of its
// classmap rules and, with psr, its PSR-0 and PSR-4 rules. Anything the
// paths do not tell exactly (a target-dir, which moves them, a glob, a
// path out of the package) takes every file. ok is false when no file
// is.
func aheadFiles(p pkg.PackageInterface, psr bool) (accept func(path string) bool, ok bool) {
	types := []autoloadType{typeClassmap}
	if psr {
		types = append(types, typePSR0, typePSR4)
	}
	every := func(path string) bool { return scanned(path) }
	if p.TargetDir().Valid {
		return every, true
	}
	var prefixes []string
	for _, typ := range types {
		v, _ := p.Autoload().Get(string(typ))
		rules, ok := v.(*php.Array)
		if !ok {
			continue
		}
		for _, paths := range rules.All() {
			for _, pv := range castArray(paths) {
				dir, ok := pv.(string)
				if !ok {
					continue
				}
				dir = path.Clean(strings.ReplaceAll(dir, `\`, "/"))
				if dir == "." || dir == ".." || strings.HasPrefix(dir, "../") || strings.Contains(dir, "*") {
					return every, true
				}
				if !strings.HasPrefix(dir, "/") {
					prefixes = append(prefixes, dir)
				}
			}
		}
	}
	if len(prefixes) == 0 {
		return nil, false
	}

	return func(file string) bool {
		if !scanned(file) {
			return false
		}
		for _, pre := range prefixes {
			if strings.HasPrefix(file, pre) && (len(file) == len(pre) || file[len(pre)] == '/') {
				return true
			}
		}

		return false
	}, true
}

// releaseParse is a classmap.ReleaseParse as a store.Deriver: its results
// are kept with the release as those the scans keep there (storeRelease).
type releaseParse struct{ *classmap.ReleaseParse }

// Inserted adds the results to those kept with release r.
func (rp releaseParse) Inserted(s *store.Store, r *store.Release) {
	kept, err := s.ReadDerived(r, releaseResults)
	if err != nil {
		return
	}
	if data, ok := rp.Merge(kept); ok {
		_ = s.WriteDerived(r, releaseResults, data)
	}
}
