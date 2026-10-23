// Ports nothing: the metadata files a resolution read, told unchanged in a
// later run (deliberate deviation 3, speed; internal/composer's resolution
// cache).

package composerrepo

import (
	"slices"
	"time"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// rootCacheKey is the cache file of the repository's root file.
const rootCacheKey = "packages.json"

// fetchedFile is a file asyncFetchFile was asked for: its cache key and
// the Last-Modified date its request sent ("" for none).
type fetchedFile struct {
	cacheKey, lastModified string
}

// MetadataFile is a file of the repository a resolution read: the root
// file, or a metadata file by URL with its cache key and the
// Last-Modified date its request sent; with the identity of its cached
// copy, unless the repository answered that it does not exist.
type MetadataFile struct {
	URL, CacheKey, LastModified string
	Root, NotFound              bool
	ID                          fsstate.ID
}

// Resolved returns the files the repository read so far, the root file
// first and the others by URL, and whether they are all it read: what it
// answered is in its cache (enabled and written), or was that the file
// does not exist; it read nothing else (no API request), and was reached
// (no degraded mode). It is false too when a cached copy is not old enough
// to be trusted, seen at now (margin): a copy changed within the
// timestamps' granularity may not show it.
func (r *ComposerRepository) Resolved(now time.Time, margin fsstate.Margin) ([]MetadataFile, bool) {
	if !r.rootLoaded || r.lazyProvidersURL == "" || r.hasProviders || r.uncached || r.degradedMode || !r.cache.IsEnabled() || r.cache.IsReadOnly() {
		return nil, false
	}

	identity := func(cacheKey string) (fsstate.ID, bool) {
		path, ok := r.cache.Path(cacheKey)
		if !ok {
			return fsstate.ID{}, false
		}
		id, ok := fsstate.Stat(path)

		return id, ok && id.IsRegular() && id.Trusted(now, margin)
	}

	rootID, ok := identity(rootCacheKey)
	if !ok {
		return nil, false
	}
	files := make([]MetadataFile, 0, len(r.fetched)+1)
	files = append(files, MetadataFile{URL: r.packagesJSONURL(), CacheKey: rootCacheKey, Root: true, ID: rootID})

	urls := make([]string, 0, len(r.fetched))
	for url := range r.fetched {
		urls = append(urls, url)
	}
	slices.Sort(urls)
	for _, url := range urls {
		f := r.fetched[url]
		file := MetadataFile{URL: url, CacheKey: f.cacheKey, LastModified: f.lastModified}
		if _, notFound := r.packagesNotFoundCache[url]; notFound {
			file.NotFound = true
		} else if _, fresh := r.freshMetadataUrls[url]; !fresh {
			return nil, false
		} else if file.ID, ok = identity(f.cacheKey); !ok {
			return nil, false
		}
		files = append(files, file)
	}

	return files, true
}

// Revalidate tells whether the repository still holds files, which
// Resolved returned in an earlier run, as they were: it loads its root
// file and requests the other files as the loads would, conditionally,
// all at once, and each is unchanged (its cached copy has the same
// identity before and after, and the request answered that it did not
// change) or still does not exist. What the requests answered is the
// repository's afterwards, as the loads' answers would be: a later load
// of a file takes its cached copy. On false, the requests made so far are
// answered and nothing else was done; an error is the root file's.
func (r *ComposerRepository) Revalidate(files []MetadataFile) (bool, error) {
	if !r.cache.IsEnabled() || r.cache.IsReadOnly() {
		return false, nil
	}
	// the requests leave at once, as the speculation's do, rather than a
	// few at a time
	if p := r.prefetcher(); p != nil && r.options != nil {
		for _, f := range files {
			if f.Root {
				continue
			}
			options := r.options
			if f.LastModified != "" {
				options = withIfModifiedSince(options, f.LastModified)
			}
			p.Prefetch(f.URL, options)
		}
	}
	if _, _, err := r.loadRootServerFile(600); err != nil {
		return false, err
	}

	unchanged := func(f MetadataFile) bool {
		path, ok := r.cache.Path(f.CacheKey)
		if !ok {
			return false
		}
		id, ok := fsstate.Stat(path)

		return ok && id == f.ID
	}

	fetches := make([]*asyncFetch, 0, len(files))
	requested := make([]MetadataFile, 0, len(files))
	defer func() {
		r.waitFetches(fetches)
		for _, f := range fetches {
			_, _ = r.finishFetch(f)
		}
	}()
	for _, f := range files {
		if f.Root {
			if !unchanged(f) || f.URL != r.packagesJSONURL() {
				return false, nil
			}

			continue
		}
		if r.lazyProvidersURL == "" || (!f.NotFound && !unchanged(f)) {
			return false, nil
		}
		fetch, err := r.asyncFetchFile(f.URL, f.CacheKey, f.LastModified)
		if err != nil {
			return false, nil
		}
		fetches = append(fetches, fetch)
		requested = append(requested, f)
	}

	r.waitFetches(fetches)
	same := true
	for i, fetch := range fetches {
		f := requested[i]
		res, err := r.finishFetch(fetch)
		_, notFound := r.packagesNotFoundCache[f.URL]
		switch {
		case err != nil:
			same = false
		case f.NotFound:
			same = same && notFound
		default:
			same = same && !notFound && res.fresh && unchanged(f)
		}
	}
	fetches = nil

	return same && !r.degradedMode && !r.uncached, nil
}
