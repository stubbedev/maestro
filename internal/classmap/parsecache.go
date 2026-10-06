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
	// known maps the identities of files whose contents maestro knows
	// without reading them (KnowContent) to their SHA-256.
	known    sync.Map // fileKey -> [32]byte
	knownAny atomic.Bool
}

// KnowContent records that the regular file at path holds contents whose
// SHA-256 is sum: for files maestro itself just created from contents it
// knows (the package store's imports, by their object hashes). The file
// is identified now (one stat), so the record survives renames and holds
// for this run while the file keeps that identity; a parse looks the
// contents' result up by the hash instead of reading the file.
func (c *ParseCache) KnowContent(path string, sum [32]byte) {
	if c == nil {
		return
	}
	if key, ok := statKey(path); ok {
		c.known.Store(key, sum)
		c.knownAny.Store(true)
	}
}

// NewParseCache returns an empty cache.
func NewParseCache() *ParseCache { return &ParseCache{} }

// lookupByIdentity finds the file's result by its identity (one stat):
// in memory, or through the content hash an earlier run recorded for it,
// when that file was not changed since.
func (c *ParseCache) lookupByIdentity(p Parser, path string) ([]string, bool) {
	inMemory, disk := c.inMemory(), c != nil && c.disk != nil
	if !inMemory && !disk {
		return nil, false
	}
	key, ok := statKey(path)
	if !ok {
		return nil, false
	}
	if inMemory {
		if classes, ok := c.lookupKey(p, key); ok {
			return classes, true
		}
	}
	if !disk {
		return nil, false
	}
	sum, ok := c.disk.statSum(key)
	if !ok && c.knownAny.Load() {
		if v, known := c.known.Load(key); known {
			sum, ok = v.([32]byte)
			if ok {
				c.disk.putStat(key, sum)
			}
		}
	}
	if !ok {
		return nil, false
	}
	classes, ok := c.disk.get(contentKey{sum: sum, parser: p.key()})
	if ok {
		c.store(p, key, classes)
	}

	return classes, ok
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
	if c == nil {
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
	if classes, ok := cache.lookupByIdentity(p, path); ok {
		return classes, nil
	}
	disk := cache != nil && cache.disk != nil
	n, key, keyed, err := b.readFileKey(path)
	if err != nil {
		return nil, readError(path, err)
	}
	var content contentKey
	if disk {
		content = contentKeyOf(p, b.src[:n])
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
	classes, err := p.classesIn(b, n, path)
	if err == nil {
		if keyed {
			cache.store(p, key, classes)
		}
		if disk {
			cache.disk.put(content, classes)
		}
	}

	return classes, err
}
