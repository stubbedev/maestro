// Ports nothing: the metadata a pool builder is about to load, fetched and
// decoded ahead of its waves (deliberate deviation 3, speed).

package composerrepo

import (
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
)

var _ repository.LoadSpeculator = (*ComposerRepository)(nil)

// onSpeculation, when set (by tests), is called with each speculation
// SpeculateLoads starts.
var onSpeculation func(*speculation)

// responsePrefetcher is the part of HttpDownloader that starts requests
// ahead and lets a speculative reader see their responses.
type responsePrefetcher interface {
	prefetcher
	PrefetchResponse(url string, options *php.Array) func() (status int, body string, ok bool)
}

// SpeculateLoads implements repository.LoadSpeculator for v2 metadata.
//
// The pool builder loads a name's versions that match the constraints it
// is required with, then the names those versions require, one round
// trip per level of the dependency graph. The speculation walks the same
// graph on other goroutines, as fast as the responses come: it requests
// each name's metadata files as loadAsyncPackages will (the requests are
// prefetched: the later loads take their responses), decodes them, and
// follows the requirements of the versions loadAsyncPackages would accept
// for the constraints seen so far. The decoded files are handed to the
// loads that read them (decodedFiles), and the constraints it parses go
// to the repository's parser cache, where the loader finds them.
//
// It only reads the repository's immutable state, the cache files (no
// output) and its own; what the loads do, print and return is unchanged.
func (r *ComposerRepository) SpeculateLoads(roots *repository.ConstraintMap, skip func(name string) bool, acceptableStabilities, stabilityFlags *php.Array) (stop func()) {
	p, ok := r.prefetcher().(responsePrefetcher)
	if !ok || r.options == nil {
		return func() {}
	}

	s := &speculation{
		r:           r,
		p:           p,
		roots:       roots,
		options:     cloneOptions(r.options),
		peek:        r.cache.Peeker(),
		stabilities: acceptableStabilities,
		flags:       stabilityFlags,
		skip:        skip,
		gen:         r.decoded.startSpeculation(),
		slots:       make(chan struct{}, runtime.GOMAXPROCS(0)),
		names:       map[string]*speculatedName{},
		followed:    map[string]struct{}{},
	}
	if onSpeculation != nil {
		onSpeculation(s)
	}
	if metadataURL, notifyURL, notifyOK := r.aheadRoot(p); metadataURL != "" {
		s.begin(metadataURL, notifyURL, notifyOK)
	} else if !r.rootLoaded {
		// no cached root file to take the metadata URL from: the
		// speculation begins once the loads have loaded it
		r.pendingSpeculation = s
	}

	return func() {
		s.stopped.Store(true)
		if r.pendingSpeculation == s {
			r.pendingSpeculation = nil
		}
		r.decoded.stopSpeculation(s.gen)
	}
}

// rootFileLoaded starts what waited for the root file to be loaded: the
// metadata requests PrefetchPackages could not start and the speculation
// SpeculateLoads could not begin, for want of a metadata URL.
func (r *ComposerRepository) rootFileLoaded() {
	if pending := r.pendingPrefetch; pending != nil {
		r.pendingPrefetch = nil
		if r.lazyProvidersURL != "" {
			pending()
		}
	}
	if s := r.pendingSpeculation; s != nil {
		r.pendingSpeculation = nil
		if r.lazyProvidersURL != "" {
			s.begin(r.lazyProvidersURL, r.notifyURL, true)
		}
	}
}

// begin starts the speculation's walk from its roots, with the metadata
// URL and the notify URL to build packages with (notifyOK false: none
// are built ahead).
func (s *speculation) begin(metadataURL, notifyURL string, notifyOK bool) {
	s.metadataURL, s.notifyURL, s.prebuildOK = metadataURL, notifyURL, notifyOK
	for name, constraint := range s.roots.All() {
		s.want(name, constraint, true)
	}
}

// speculation is a SpeculateLoads walk.
type speculation struct {
	r           *ComposerRepository
	p           responsePrefetcher
	roots       *repository.ConstraintMap
	metadataURL string
	notifyURL   string
	prebuildOK  bool
	options     *php.Array
	peek        func(file string) (string, bool)
	stabilities *php.Array
	flags       *php.Array
	skip        func(name string) bool
	gen         int
	stopped     atomic.Bool
	// slots bound the goroutines decoding and scanning at once; busy
	// counts the goroutines.
	slots chan struct{}
	busy  sync.WaitGroup

	mu    sync.Mutex
	names map[string]*speculatedName
	// followed are the requirements followed: target and constraint.
	followed map[string]struct{}
}

// speculatedName is the speculation's state for a package name: its
// versions once its files are loaded, the constraints it is required
// with.
type speculatedName struct {
	loaded      bool
	versions    []*speculatedVersion
	constraints []semver.ConstraintInterface
}

// speculatedVersion is what the speculation needs of a version: what
// isVersionAcceptable looks at (whether the version and its alias have
// an acceptable stability), and its requirements, followed once a
// constraint accepts it.
type speculatedVersion struct {
	index               int // in the file's version list
	normalized          string
	alias               string // "" for none
	stable, aliasStable bool
	requires            []speculatedLink
	followed            atomic.Bool
}

type speculatedLink struct {
	target, constraint string
}

// want notes that name is required with constraint (nil: any): its files
// are loaded the first time, its versions scanned for constraint once
// they are.
func (s *speculation) want(name string, constraint semver.ConstraintInterface, now bool) {
	name = php.Strtolower(name)
	if s.stopped.Load() || pkg.IsPlatformPackage(name) || name == "__root__" || s.skip(name) {
		return
	}

	s.mu.Lock()
	st, ok := s.names[name]
	if !ok {
		st = &speculatedName{constraints: []semver.ConstraintInterface{constraint}}
		s.names[name] = st
		s.mu.Unlock()

		if now {
			files := s.requestFiles(name)
			s.busy.Go(func() { s.load(name, st, files) })
		} else {
			s.busy.Go(func() { s.load(name, st, s.requestFiles(name)) })
		}

		return
	}
	st.constraints = append(st.constraints, constraint)
	if !st.loaded {
		s.mu.Unlock()

		return
	}
	versions := st.versions
	s.mu.Unlock()

	s.busy.Go(func() { s.scan(versions, constraint) })
}

// conditionalOptions is ComposerRepository.conditionalOptions with the
// speculation's copy of the options.
func (s *speculation) conditionalOptions(cached string) *php.Array {
	if lastModified := lastModifiedOf(cached); lastModified != "" {
		return withIfModifiedSince(s.options, lastModified)
	}

	return s.options
}

// speculatedFile is a metadata file of a name the speculation loads: its
// cached copy ("" for none), and the prefetched request for it (nil when
// the loads requested it already: a request made again would not be
// taken, so the cached copy stands for it).
type speculatedFile struct {
	cacheKey, cached string
	response         func() (int, string, bool)
	versions         []*speculatedVersion
	// json and data are the file decoded last, data until it is handed
	// over
	json string
	data *php.Array
}

// requestFiles reads name's cached files and starts their requests.
func (s *speculation) requestFiles(name string) []*speculatedFile {
	var files []*speculatedFile
	for _, fileName := range metadataFiles(name, s.stabilities, s.flags) {
		url := strings.ReplaceAll(s.metadataURL, "%package%", fileName)
		f := &speculatedFile{cacheKey: "provider-" + php.Strtr(fileName, "/", "~") + ".json"}
		f.cached, _ = s.peek(f.cacheKey)
		if !s.r.decoded.wasRequested(url) {
			f.response = s.p.PrefetchResponse(url, s.conditionalOptions(f.cached))
		}
		files = append(files, f)
	}

	return files
}

// load decodes name's files, then scans its versions for the
// constraints seen. When the files are cached, it goes on with the cached
// copies at once (in a warm cache most requests answer that they did not
// change), and scans again what a request answers with a new file.
func (s *speculation) load(name string, st *speculatedName, files []*speculatedFile) {
	allCached := true
	for _, f := range files {
		allCached = allCached && f.cached != ""
	}

	// decode reads f's versions from json; handOver builds its packages
	// and offers the decoded file to the loads, after the versions were
	// scanned (the next level's requests come first)
	decode := func(f *speculatedFile, json string) {
		f.json, f.data, f.versions = json, nil, nil
		if json == "" || s.stopped.Load() {
			return
		}
		s.slots <- struct{}{}
		defer func() { <-s.slots }()

		if f.data = decodeArray(json); f.data != nil {
			f.versions = s.versionsOf(name, f.data)
		}
	}
	handOver := func(f *speculatedFile) {
		if f.data == nil || s.stopped.Load() {
			return
		}
		s.slots <- struct{}{}
		defer func() { <-s.slots }()

		pre := s.prebuild(name, f.data, f.versions)
		s.r.decoded.rememberSlim(f.cacheKey, f.json, f.data)
		s.r.decoded.offer(s.gen, f.cacheKey, f.json, f.data, pre)
		f.data = nil
	}
	publish := func() {
		var versions []*speculatedVersion
		for _, f := range files {
			versions = append(versions, f.versions...)
		}

		s.mu.Lock()
		st.loaded = true
		st.versions = versions
		constraints := slices.Clone(st.constraints)
		s.mu.Unlock()

		for _, constraint := range constraints {
			s.scan(versions, constraint)
		}
	}

	if allCached {
		for _, f := range files {
			decode(f, f.cached)
		}
		publish()
		for _, f := range files {
			handOver(f)
		}
	}

	changed := !allCached
	for _, f := range files {
		json := f.cached
		if f.response != nil {
			switch status, body, ok := f.response(); {
			case ok && status == 200:
				json = body
			case ok && status == 404:
				json = ""
			}
		}
		if json != f.cached || !allCached {
			decode(f, json)
			changed = true
		}
	}
	if changed {
		publish()
		for _, f := range files {
			handOver(f)
		}
	}
}

// prebuilt are the packages a speculation built ahead from the versions of
// name in a metadata file (minified, as expandEach expands it), by their
// index in the version list, as createPackages builds them in a
// repository whose notify URL is notifyURL.
type prebuilt struct {
	name      string
	notifyURL string
	packages  map[int]pkg.PackageInterface
}

// take returns, once, the package built from the version at index i, for
// a load of name in a repository whose notify URL is notifyURL; nil when
// there is none that load would build alike.
func (p *prebuilt) take(name, notifyURL string, i int) pkg.PackageInterface {
	if p == nil || p.name != name || p.notifyURL != notifyURL {
		return nil
	}
	built := p.packages[i]
	delete(p.packages, i)

	return built
}

// prebuild builds the packages createPackages would build from name's
// versions in data that the scans accepted (versions): from a copy of each
// version (data is not changed) given the notification-url createPackages
// gives it, with the repository's loader, before the mirrors and transport
// options (configureLoaded) which the load applies. Versions without a
// version_normalized, which the load normalizes first, and versions the
// loader refuses are left to the load.
func (s *speculation) prebuild(name string, data *php.Array, versions []*speculatedVersion) *prebuilt {
	if !s.prebuildOK || get(data, "minified") != "composer/2.0" {
		return nil
	}
	accepted := map[int]bool{}
	for _, v := range versions {
		if v.followed.Load() {
			accepted[v.index] = true
		}
	}
	if len(accepted) == 0 {
		return nil
	}
	raw, ok := packageVersions(data, name).(*php.Array)
	if !ok {
		return nil
	}

	notifyURL := notificationURL(s.notifyURL)
	var (
		indexes []int
		copies  []*php.Array
	)
	i := -1
	ok, _ = expandEach(raw.Values(), func(v *php.Array, _ func() *php.Array) error {
		i++
		if normalized := get(v, "version_normalized"); !accepted[i] || normalized == nil || normalized == pkg.DefaultBranchAlias {
			return nil
		}

		c := v.ShallowClone()
		if n, _ := c.Get("notification-url"); n == nil {
			c.Set("notification-url", notifyURL)
		}
		indexes = append(indexes, i)
		copies = append(copies, c)

		return nil
	})
	if !ok || len(copies) == 0 {
		return nil
	}

	pre := &prebuilt{name: name, notifyURL: s.notifyURL, packages: make(map[int]pkg.PackageInterface, len(copies))}
	if packages, err := s.r.loader.LoadPackages(copies); err == nil {
		for j, p := range packages {
			pre.packages[indexes[j]] = p
		}

		return pre
	}
	// a version the loader refuses: the others one by one
	for j, c := range copies {
		if packages, err := s.r.loader.LoadPackages([]*php.Array{c}); err == nil && len(packages) == 1 {
			pre.packages[indexes[j]] = packages[0]
		}
	}

	return pre
}

// versionsOf reads name's versions from a decoded metadata file, as
// loadAsyncPackages does, without changing data.
func (s *speculation) versionsOf(name string, data *php.Array) []*speculatedVersion {
	raw, ok := packageVersions(data, name).(*php.Array)
	if !ok {
		return nil
	}
	items := raw.Values()

	var versions []*speculatedVersion
	// versions share their unchanged require arrays
	links := map[*php.Array][]speculatedLink{}
	index := -1
	add := func(v *php.Array) {
		index++
		normalized, _ := get(v, "version_normalized").(string)
		if normalized == "" || normalized == pkg.DefaultBranchAlias {
			var err error
			if normalized, err = s.r.versionParser.Normalize(php.ToString(get(v, "version"))); err != nil {
				return
			}
		}
		alias, ok, err := s.r.loader.GetBranchAlias(v)
		if err != nil || !ok {
			alias = ""
		}
		normalizedStable := s.stable(name, normalized)
		aliasStable := alias != "" && s.stable(name, alias)
		if !normalizedStable && !aliasStable {
			return
		}

		require, _ := get(v, "require").(*php.Array)
		requires, ok := links[require]
		if !ok && require != nil {
			requires = make([]speculatedLink, 0, require.Len())
			for target, constraint := range require.All() {
				if c, ok := constraint.(string); ok {
					requires = append(requires, speculatedLink{target: php.Strtolower(target.String()), constraint: c})
				}
			}
			links[require] = requires
		}
		versions = append(versions, &speculatedVersion{index: index, normalized: normalized, alias: alias, requires: requires, stable: normalizedStable, aliasStable: aliasStable})
	}

	if get(data, "minified") == "composer/2.0" {
		if ok, _ := expandEach(items, func(v *php.Array, _ func() *php.Array) error { add(v); return nil }); ok {
			return versions
		}
		var err error
		if items, err = expandVersions(items); err != nil {
			return nil
		}
	}
	index = -1
	for _, item := range items {
		if v, ok := item.(*php.Array); ok {
			add(v)
		} else {
			index++
		}
	}

	return versions
}

// packageVersions is $data['packages'][$name] ?? null.
func packageVersions(data *php.Array, name string) any {
	packages, _ := get(data, "packages").(*php.Array)

	return get(packages, name)
}

// stable tells whether name's version v has an acceptable stability.
func (s *speculation) stable(name, v string) bool {
	return s.stabilities == nil || s.flags == nil || version.IsPackageAcceptable(s.stabilities, s.flags, []string{name}, semver.ParseStability(v))
}

// scan follows the requirements of the versions loadAsyncPackages would
// accept for constraint (isVersionAcceptable).
func (s *speculation) scan(versions []*speculatedVersion, constraint semver.ConstraintInterface) {
	if s.stopped.Load() {
		return
	}

	matches := func(string) bool { return true }
	if constraint != nil {
		matches = semver.CompilingMatcher.Matcher(constraint, semver.OpEQ)
	}

	for _, v := range versions {
		if v.followed.Load() {
			continue
		}
		if accepted := (v.stable && matches(v.normalized)) || (v.aliasStable && matches(v.alias)); !accepted {
			continue
		}
		if v.followed.Swap(true) {
			continue
		}
		for _, link := range v.requires {
			key := link.target + "\x00" + link.constraint
			s.mu.Lock()
			_, seen := s.followed[key]
			s.followed[key] = struct{}{}
			s.mu.Unlock()
			if seen {
				continue
			}

			parsed, err := s.r.versionParser.ParseConstraints(link.constraint)
			if err != nil {
				continue
			}
			s.want(link.target, parsed, false)
		}
	}
}
