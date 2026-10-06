// Ports nothing: metadata requests started ahead of time (deliberate
// deviation 3, speed).

package composerrepo

import (
	"strings"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
)

// prefetcher is the part of HttpDownloader that starts requests ahead.
type prefetcher interface {
	Prefetch(url string, options *php.Array)
}

// listenerChecker tells whether an event would reach a listener
// (eventdispatcher.EventDispatcher.WillDispatchTo).
type listenerChecker interface {
	WillDispatchTo(event eventdispatcher.Event) bool
}

// PrefetchPackages starts, without output, the conditional requests that
// loading names will make: this repository's root file when it is not
// loaded yet, and each name's v2 metadata file (and its ~dev file when dev
// versions are acceptable, as loadAsyncPackages decides), each with the
// If-Modified-Since its cached copy gives. The requests the pool builder
// makes later, wave after wave, are then answered at once; they print and
// behave as without this. A guess that turns out unneeded costs a request.
//
// Nothing is prefetched when a PRE_FILE_DOWNLOAD listener could change the
// requests, or for a repository without v2 metadata.
func (r *ComposerRepository) PrefetchPackages(names []string, acceptableStabilities, stabilityFlags *php.Array) {
	p, ok := r.httpDownloader.(prefetcher)
	if !ok || r.cache == nil {
		return
	}
	if r.eventDispatcher != nil {
		l, ok := r.eventDispatcher.(listenerChecker)
		if !ok || l.WillDispatchTo(eventdispatcher.NewPreFileDownloadEvent(eventdispatcher.PreFileDownload, nil, "", "metadata", &MetadataContext{Repository: r})) {
			return
		}
	}

	metadataURL := r.lazyProvidersURL
	if !r.rootLoaded {
		// the root file's request, and the metadata-url its cached copy
		// names (the request may still change it: then nothing matches)
		cached, ok := r.cache.Peek("packages.json")
		if !ok {
			return
		}
		p.Prefetch(r.packagesJSONURL(), r.conditionalOptions(cached))

		data, _ := php.JSONDecode(cached, true)
		root, _ := data.(*php.Array)
		raw, ok := get(root, "metadata-url").(string)
		if !ok || raw == "" {
			return
		}
		var err error
		if metadataURL, err = r.canonicalizeURL(raw); err != nil {
			return
		}
	}
	if metadataURL == "" {
		return
	}

	for _, name := range names {
		name = php.Strtolower(name)
		if pkg.IsPlatformPackage(name) || name == "__root__" {
			continue
		}
		files := []string{name}
		if acceptableStabilities == nil || stabilityFlags == nil || version.IsPackageAcceptable(acceptableStabilities, stabilityFlags, []string{name}, "dev") {
			files = append(files, name+"~dev")
		}
		if acceptableStabilities != nil && acceptableStabilities.Has("dev") && acceptableStabilities.Len() == 1 && stabilityFlags != nil && stabilityFlags.Len() == 0 {
			files = files[1:]
		}
		for _, file := range files {
			url := strings.ReplaceAll(metadataURL, "%package%", file)
			if _, ok := r.packagesNotFoundCache[url]; ok {
				continue
			}
			if _, ok := r.freshMetadataUrls[url]; ok {
				continue
			}
			cached, _ := r.cache.Peek("provider-" + php.Strtr(file, "/", "~") + ".json")
			p.Prefetch(url, r.conditionalOptions(cached))
		}
	}
}

// conditionalOptions are the transport options of a request for a file
// whose cached copy is cached ("" for none): the repository's, with the
// If-Modified-Since of the copy's last-modified, as asyncFetchFile and
// fetchFileIfLastModified send them.
func (r *ComposerRepository) conditionalOptions(cached string) *php.Array {
	if lastModified := lastModifiedOf(cached); lastModified != "" {
		return withIfModifiedSince(r.options, lastModified)
	}

	return r.options
}

// lastModifiedOf finds the "last-modified" value of a cached metadata file
// without decoding it all: withLastModified stores it as a top-level
// string. A wrong guess only makes a prefetched request miss.
func lastModifiedOf(cached string) string {
	const key = `"last-modified":"`
	i := strings.LastIndex(cached, key)
	if i < 0 {
		return ""
	}
	rest := cached[i+len(key):]
	j := strings.IndexByte(rest, '"')
	if j < 0 || strings.ContainsRune(rest[:j], '\\') {
		return ""
	}

	return rest[:j]
}
