// Ports nothing: a cache of parse results (deliberate deviation 3, speed).

package classmap

import (
	"sync"
	"sync/atomic"
)

// fileKey identifies a file's content as the file system describes it:
// device, inode, size, and modification and change times (nanoseconds).
// Writing to the file changes its times; replacing it changes the inode.
type fileKey struct {
	dev, ino            uint64
	size                int64
	mtimeSec, mtimeNsec int64
	ctimeSec, ctimeNsec int64
}

type cacheKey struct {
	file   fileKey
	parser parserKey
}

// parserKey is what a parse result depends on besides the file's contents:
// the Parser settings, with the PHP version reduced to the scanner it
// selects.
type parserKey struct {
	shortOpenTag bool
	php          phpVersion
}

func (p Parser) key() parserKey { return parserKey{p.ShortOpenTag, p.phpVersion()} }

// ParseCache keeps the classes found in files, by file identity, so that a
// file parsed ahead of time (Warm, while maestro waits on the network or
// on processes) is not parsed again by the scan that needs it, as long as
// it did not change. Only successful parses are kept: errors name paths
// and are left to the scans. It is safe for concurrent use.
type ParseCache struct {
	m       sync.Map // cacheKey -> []string
	entries atomic.Int64
	warming sync.WaitGroup
	// warmed is set once Warm ran: before that the identities are only
	// those of files the scans themselves read, never looked up again.
	warmed atomic.Bool
	// disk is the persistent part (UseFile), nil without one.
	disk *diskCache
	// releases are the package trees maestro installed from its store
	// (AddReleases); stampedAny is set once they have files.
	releases   releaseSet
	stampedAny atomic.Bool
	// releasesPending runs AddReleasesAsync.
	releasesPending sync.WaitGroup
	// reads counts the files the cache did not know and had read.
	reads atomic.Int64
	// trust is how old a file must be for the cache file's identity of
	// it to be trusted.
	trust trustMargin
}

// NewParseCache returns an empty cache.
func NewParseCache() *ParseCache { return &ParseCache{} }

// lookupByIdentity finds the file's result by its identity (one stat):
// in memory, by the content of the release file its stamp shows it to
// be, or through the content hash an earlier run recorded for it, when
// that file was not changed since. A release file whose result is not
// kept yet comes back as stamped, for the parse to keep it.
func (c *ParseCache) lookupByIdentity(p Parser, path string) (classes []string, ok bool, stamped *stampCandidate) {
	if c != nil {
		c.releasesPending.Wait()
	}
	inMemory, disk, releases := c.inMemory(), c != nil && c.disk != nil, c != nil && c.stampedAny.Load()
	if !inMemory && !disk && !releases {
		return nil, false, nil
	}
	key, keyed := statKey(path)
	if !keyed {
		if releases {
			if size, mtime, ok := statStamp(path); ok {
				if cand, ok := c.releases.stamped(path, size, mtime); ok {
					classes, ok := c.releases.result(cand, contentKey{sum: cand.sum, parser: p.key()})

					return classes, ok, cand
				}
			}
		}

		return nil, false, nil
	}
	if inMemory {
		if classes, ok := c.lookupKey(p, key); ok {
			return classes, true, nil
		}
	}
	if releases && key.mtimeNsec == 0 {
		if cand, ok := c.releases.stamped(path, key.size, key.mtimeSec); ok {
			classes, ok := c.releases.result(cand, contentKey{sum: cand.sum, parser: p.key()})
			if ok {
				c.store(p, key, classes)
			}

			return classes, ok, cand
		}
	}
	if !disk {
		return nil, false, nil
	}
	sum, ok := c.disk.statSum(key, c.trust)
	if !ok {
		return nil, false, nil
	}
	classes, ok = c.disk.get(contentKey{sum: sum, parser: p.key()})
	if ok {
		c.store(p, key, classes)
	}

	return classes, ok, nil
}

// inMemory reports whether lookups by file identity can find anything.
func (c *ParseCache) inMemory() bool {
	return c != nil && c.warmed.Load() && c.entries.Load() > 0
}

// lookupKey is lookup for a file whose identity is known.
func (c *ParseCache) lookupKey(p Parser, key fileKey) ([]string, bool) {
	v, ok := c.m.Load(cacheKey{key, p.key()})
	if !ok {
		return nil, false
	}
	classes, _ := v.([]string)

	return classes, true
}

func (c *ParseCache) store(p Parser, key fileKey, classes []string) {
	// only a warm-up's results are ever looked up by identity in memory
	if c == nil || !c.warmed.Load() {
		return
	}
	if _, loaded := c.m.LoadOrStore(cacheKey{key, p.key()}, classes); !loaded {
		c.entries.Add(1)
	}
}

// Wait waits for the Warm calls in progress.
func (c *ParseCache) Wait() {
	if c != nil {
		c.warming.Wait()
	}
}

// Warm parses the files the given scans would parse into the cache, in the
// background; Wait waits for it. The scans themselves still decide
// everything, and parse whatever the cache does not hold as it is now.
func (c *ParseCache) Warm(p Parser, extensions []string, requests []ScanRequest) {
	if extensions == nil {
		extensions = DefaultExtensions
	}
	g := &Generator{Parser: p, extensions: extensions, cache: c}
	c.warmed.Store(true)
	c.warming.Go(func() { g.prefetch(requests) })
}

// cachedFindClasses is findClasses() through the cache.
func (p Parser) cachedFindClasses(b *parseBuffers, path string, cache *ParseCache) ([]string, error) {
	classes, ok, stamped := cache.lookupByIdentity(p, path)
	if ok {
		return classes, nil
	}
	disk := cache != nil && cache.disk != nil
	if cache != nil {
		cache.reads.Add(1)
	}
	n, key, keyed, err := b.readFileKey(path)
	if err != nil {
		return nil, readError(path, err)
	}
	var content contentKey
	if disk || stamped != nil {
		content = contentKeyOf(p, b.src[:n])
	}
	// a release file read as its release has it: its result is kept with
	// the release, not in the cache file
	release := stamped != nil && content.sum == stamped.sum
	if disk && !release {
		if keyed {
			cache.disk.putStat(key, content.sum)
		}
		if classes, ok := cache.disk.get(content); ok {
			if keyed {
				cache.store(p, key, classes)
			}

			return classes, nil
		}
	}
	classes, err = p.classesIn(b, n, path)
	if err == nil {
		if keyed {
			cache.store(p, key, classes)
		}
		switch {
		case release:
			cache.releases.put(stamped, content, classes)
		case disk:
			cache.disk.put(content, classes)
		}
	}

	return classes, err
}
