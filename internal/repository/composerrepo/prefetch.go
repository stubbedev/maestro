// Ports nothing: metadata requests started ahead of time (deliberate
// deviation 3, speed).

package composerrepo

import (
	"strings"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
)

// prefetcher is the part of HttpDownloader that starts requests ahead.
type prefetcher interface {
	Prefetch(url string, options *php.Array)
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
	p := r.prefetcher()
	if p == nil {
		return
	}

	metadataURL := r.aheadMetadataURL(p)
	if metadataURL == "" {
		if !r.rootLoaded {
			// no cached root file to take the metadata URL from: the
			// requests start once the loads have loaded it
			r.pendingPrefetch = func() { r.PrefetchPackages(names, acceptableStabilities, stabilityFlags) }
		}

		return
	}

	for _, name := range names {
		name = php.Strtolower(name)
		if pkg.IsPlatformPackage(name) || name == "__root__" {
			continue
		}
		for _, file := range metadataFiles(name, acceptableStabilities, stabilityFlags) {
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

// metadataFiles are the v2 metadata files loadAsyncPackages loads for
// name: its own and its ~dev file when dev versions are acceptable, only
// the latter when only dev versions are.
func metadataFiles(name string, acceptableStabilities, stabilityFlags *php.Array) []string {
	files := []string{name}
	if acceptableStabilities == nil || stabilityFlags == nil || version.IsPackageAcceptable(acceptableStabilities, stabilityFlags, []string{name}, "dev") {
		files = append(files, name+"~dev")
	}
	if acceptableStabilities != nil && acceptableStabilities.Has("dev") && acceptableStabilities.Len() == 1 && stabilityFlags != nil && stabilityFlags.Len() == 0 {
		files = files[1:]
	}

	return files
}

// aheadMetadataURL is the v2 metadata URL ("" for none) requests made
// ahead use: the root file's when it is loaded, else the one its cached
// copy names, after prefetching the root file's request (that request may
// still change it: then nothing matches).
func (r *ComposerRepository) aheadMetadataURL(p prefetcher) string {
	metadataURL, _, _ := r.aheadRoot(p)

	return metadataURL
}

// aheadRoot is aheadMetadataURL, with the notify URL createPackages will
// use (notifyOK false when it cannot be told).
func (r *ComposerRepository) aheadRoot(p prefetcher) (metadataURL, notifyURL string, notifyOK bool) {
	if r.rootLoaded {
		return r.lazyProvidersURL, r.notifyURL, true
	}

	cached, ok := r.cache.Peek("packages.json")
	if !ok {
		return "", "", false
	}
	p.Prefetch(r.packagesJSONURL(), r.conditionalOptions(cached))

	data, _ := php.JSONDecode(cached, true)
	root, _ := data.(*php.Array)
	raw, ok := root.At("metadata-url").(string)
	if !ok || raw == "" {
		return "", "", false
	}
	metadataURL, err := r.canonicalizeURL(raw)
	if err != nil {
		return "", "", false
	}

	// as configureFromRoot sets it
	notifyOK = true
	for _, key := range []string{"notify-batch", "notify"} {
		if v := root.At(key); php.ToBool(v) {
			if notifyURL, err = r.canonicalizeURL(php.ToString(v)); err != nil {
				notifyOK = false
			}

			break
		}
	}

	return metadataURL, notifyURL, notifyOK
}

// prefetcher is the downloader to prefetch with, nil when this repository
// may not: no cache to read the If-Modified-Since from, or a
// PRE_FILE_DOWNLOAD listener that could change the requests.
func (r *ComposerRepository) prefetcher() prefetcher {
	p, ok := r.httpDownloader.(prefetcher)
	if !ok || r.cache == nil {
		return nil
	}
	if eventdispatcher.MayListen(r.eventDispatcher, eventdispatcher.NewPreFileDownloadEvent(eventdispatcher.PreFileDownload, nil, "", "metadata", &MetadataContext{Repository: r})) {
		return nil
	}

	return p
}

// PrefetchFilterSummary starts, without output, the conditional request
// for the filter list summary that loadFilterSummary will make, taking the
// summary-url from the root file (its cached copy when it is not loaded
// yet). As with PrefetchPackages, the later request takes the response and
// behaves as without this.
func (r *ComposerRepository) PrefetchFilterSummary() {
	p := r.prefetcher()
	if p == nil || r.userFilterDisabled {
		return
	}

	summaryURL := ""
	if r.rootLoaded {
		if r.filterConfig != nil {
			summaryURL = r.filterConfig.SummaryURL
		}
	} else if cached, ok := r.cache.Peek("packages.json"); ok {
		// FilterLists loads the root file with a 600 s max age: past it,
		// a conditional request for it comes first
		if age, ok := r.cache.Age("packages.json"); ok && age > 600 {
			p.Prefetch(r.packagesJSONURL(), r.conditionalOptions(cached))
		}
		data, _ := php.JSONDecode(cached, true)
		if filter, ok := asArrayOrNil(data).At("filter").(*php.Array); ok {
			summaryURL = filterlist.ComposerRepositoryFilterInformationFromData(filter, func(url string) string {
				canonical, _ := r.canonicalizeURL(url)

				return canonical
			}).SummaryURL
		}
	}
	if summaryURL == "" {
		return
	}

	cached, _ := r.cache.Peek("filter-summary.json")
	p.Prefetch(summaryURL, r.conditionalOptions(cached))
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
