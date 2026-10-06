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
	file         fileKey
	shortOpenTag bool
}

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
}

// NewParseCache returns an empty cache.
func NewParseCache() *ParseCache { return &ParseCache{} }

// lookup returns the cached classes of the file at path for the parser.
func (c *ParseCache) lookup(p Parser, path string) ([]string, bool) {
	if c == nil || !c.warmed.Load() || c.entries.Load() == 0 {
		return nil, false
	}
	key, ok := statKey(path)
	if !ok {
		return nil, false
	}
	v, ok := c.m.Load(cacheKey{key, p.ShortOpenTag})
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
	if _, loaded := c.m.LoadOrStore(cacheKey{key, p.ShortOpenTag}, classes); !loaded {
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
	if classes, ok := cache.lookup(p, path); ok {
		return classes, nil
	}
	n, key, keyed, err := b.readFileKey(path)
	if err != nil {
		return nil, readError(path, err)
	}
	var content contentKey
	disk := cache != nil && cache.disk != nil
	if disk {
		content = contentKeyOf(p, b.src[:n])
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
