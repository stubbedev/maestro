// Ports nothing: scanning ahead (deliberate deviation 3, speed).

package classmap

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// ScanRequest is one later ScanPaths call Prefetch prepares: its path and
// exclusion matcher (excludedDirs nil).
type ScanRequest struct {
	Path     string
	Excluded Matcher
}

// prefetched is what Prefetch found: the Finder's walk of each requested
// path, the parse result of each file, and the realpaths it resolved.
type prefetched struct {
	walks map[string]walkResult
	files map[string]parseResult
	dirs  realDirCache
	cwd   string // violationCwd's result, once known
	// realCwd is realCwd()'s result when realCwdOK.
	realCwd   string
	realCwdOK bool
}

type walkResult struct {
	files []foundFile
	dirs  []string // the directories listed
	err   error
}

type parseResult struct {
	classes []string
	err     error
}

// walk returns the prefetched walk of path, if any.
func (p *prefetched) walk(path string, excludedDirs []string) (walkResult, bool) {
	if p == nil || len(excludedDirs) > 0 {
		return walkResult{}, false
	}
	w, ok := p.walks[path]

	return w, ok
}

// parsed returns the prefetched parse result of a file, if any.
func (p *prefetched) parsed(filePath string) (parseResult, bool) {
	if p == nil {
		return parseResult{}, false
	}
	r, ok := p.files[filePath]

	return r, ok
}

// Prefetch walks the directories and parses the files of the given scans
// all at once, in parallel, so that the ScanPaths calls that follow (one
// per autoload rule, many of them small) find their files listed and
// parsed. It changes no result: the scans still run in order, with the
// same files, exclusions, duplicate checks and errors; they only skip the
// work done here (a file Prefetch did not parse is parsed by its scan).
// The files must not change between Prefetch and the scans.
func (g *Generator) Prefetch(requests []ScanRequest) {
	// a warm-up of the same files finishes first
	g.cache.Wait()
	g.prefetch(requests)
}

func (g *Generator) prefetch(requests []ScanRequest) {
	pre := &prefetched{walks: map[string]walkResult{}}
	g.pre = pre

	cwd, err := realCwd()
	if err != nil {
		return
	}
	pre.realCwd, pre.realCwdOK = cwd, true

	// the Finder walks, one request per worker
	type found struct {
		req    ScanRequest
		files  []foundFile
		dirs   []string
		items  []scanItem
		walked bool
	}
	results := make([]found, len(requests))
	parallel(len(requests), func(i int) {
		req := requests[i]
		results[i].req = req
		switch {
		case isFile(req.Path):
			results[i].files = []foundFile{{path: req.Path}}
		case isDir(req.Path) || strings.Contains(req.Path, "*"):
			dirs, err := finderIn(req.Path)
			if err != nil {
				return
			}
			// a failed walk is left to its scan, which throws it
			if files, listed, walkErr := finderFilesDirs(dirs, nil); walkErr == nil {
				results[i].files, results[i].dirs, results[i].walked = files, listed, true
			}
		}
		results[i].items = g.scanItems(results[i].files, cwd)
	})

	// every file once, with the matcher of the first scan listing it (a
	// file that scan excludes is left to the others' scans to parse)
	type job struct {
		it       scanItem
		excluded Matcher
	}
	total := 0
	for _, r := range results {
		total += len(r.items)
	}
	var (
		jobs = make([]job, 0, total)
		seen = make(map[string]bool, total)
	)
	for _, r := range results {
		if r.walked {
			pre.walks[r.req.Path] = walkResult{files: r.files, dirs: r.dirs}
		}
		for _, it := range r.items {
			if !seen[it.filePath] {
				seen[it.filePath] = true
				jobs = append(jobs, job{it, r.req.Excluded})
			}
		}
	}

	parsed := make([]parseResult, len(jobs))
	done := make([]bool, len(jobs))
	parallelWithBuffers(len(jobs), func(b *parseBuffers, i int) {
		if prefetchExcluded(&pre.dirs, &jobs[i].it, jobs[i].excluded) {
			return
		}
		parsed[i].classes, parsed[i].err = g.Parser.cachedFindClasses(b, jobs[i].it.filePath, g.cache)
		done[i] = true
	})
	pre.files = make(map[string]parseResult, len(jobs))
	for i, j := range jobs {
		if done[i] {
			pre.files[j.it.filePath] = parsed[i]
		}
	}
}

// prefetchExcluded reports whether a scan with the matcher would leave the
// item out (or fail before parsing it): prepare's checks.
func prefetchExcluded(dirs *realDirCache, it *scanItem, excluded Matcher) bool {
	if isStreamWrapperPath(it.filePath) {
		return true
	}
	real, ok := dirs.realpath(it.filePath, it.notLink)
	if !ok {
		return true
	}
	if excluded == nil {
		return false
	}
	for _, p := range [2]string{real, it.filePath} {
		matched, err := excluded.IsMatch(strings.ReplaceAll(p, `\`, "/"))
		if err != nil || matched {
			return true
		}
	}

	return false
}

// parallel runs fn(0..n-1) on GOMAXPROCS workers.
func parallel(n int, fn func(i int)) {
	parallelWithBuffers(n, func(_ *parseBuffers, i int) { fn(i) })
}

// parallelWithBuffers runs fn(0..n-1) on GOMAXPROCS workers, each with its
// parse buffers.
func parallelWithBuffers(n int, fn func(b *parseBuffers, i int)) {
	workers := min(runtime.GOMAXPROCS(0), n)
	var next atomic.Int64
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			b := getBuffers()
			defer bufferPool.Put(b)
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				fn(b, i)
			}
		})
	}
	wg.Wait()
}
