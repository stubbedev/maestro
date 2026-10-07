// Ports src/Composer/Repository/ComposerRepository.php: whatProvides,
// loadAsyncPackages and isVersionAcceptable, the loading of package
// versions from provider files and v2 metadata.

package composerrepo

import (
	"errors"
	"runtime"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/metadataminifier"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// nameMapValues returns the values of m in order.
func nameMapValues[V any](m *repository.NameMap[V]) []V {
	out := make([]V, 0, m.Len())
	for _, v := range m.All() {
		out = append(out, v)
	}

	return out
}

// whatProvides ports whatProvides: the packages named name from the
// provider file (or the partial packages of the root file), keyed by uid
// and uid-alias.
func (r *ComposerRepository) whatProvides(name string, acceptableStabilities, stabilityFlags *php.Array, alreadyLoaded repository.AlreadyLoaded) (*repository.NameMap[pkg.PackageInterface], error) {
	result := &repository.NameMap[pkg.PackageInterface]{}

	hasPartial, err := r.hasPartialPackagesCheck()
	if err != nil {
		return nil, err
	}

	var (
		packages              *php.Array
		packagesSource        string
		loadingPartialPackage bool
	)
	if !hasPartial || !r.partialPackagesByName.Has(name) {
		// skip platform packages, root package and composer-plugin-api
		if pkg.IsPlatformPackage(name) || name == "__root__" {
			return result, nil
		}

		if err := r.ensureProviderListing(); err != nil {
			return nil, err
		}

		var (
			hash, url, cacheKey  string
			useLastModifiedCheck bool
		)
		switch {
		case r.lazyProvidersURL != "" && !r.providerListing.Has(name):
			url = strings.ReplaceAll(r.lazyProvidersURL, "%package%", name)
			cacheKey = "provider-" + php.Strtr(name, "/", "$") + ".json"
			useLastModifiedCheck = true
		case r.providersURL != "":
			// package does not exist in this repo
			if !r.providerListing.Has(name) {
				return result, nil
			}

			hash, _ = r.providerListing.Get(name)
			url = strings.ReplaceAll(strings.ReplaceAll(r.providersURL, "%package%", name), "%hash%", hash)
			cacheKey = "provider-" + php.Strtr(name, "/", "$") + ".json"
		default:
			return result, nil
		}

		cachedSource := "cached file (" + cacheKey + " originating from " + util.SanitizeURL(url) + ")"
		if !useLastModifiedCheck && hash != "" {
			cachedHash, ok, err := r.cache.Sha256(cacheKey)
			if err != nil {
				return nil, err
			}
			if ok && cachedHash == hash {
				if packages, err = r.readCachedJSON(cacheKey); err != nil {
					return nil, err
				}
				packagesSource = cachedSource
			}
		} else if useLastModifiedCheck {
			contents, ok, err := r.cache.Read(cacheKey)
			if err != nil {
				return nil, err
			}
			if ok && php.ToBool(contents) {
				decoded := decodeArray(contents)
				// we already loaded some packages from this file, so assume it is fresh and avoid fetching it again
				if _, loaded := alreadyLoaded[name]; loaded {
					packages = decoded
					packagesSource = cachedSource
				} else if lastModified := get(decoded, "last-modified"); lastModified != nil {
					response, fresh, err := r.fetchFileIfLastModified(url, cacheKey, php.ToString(lastModified))
					if err != nil {
						return nil, err
					}
					if fresh {
						packages = decoded
						packagesSource = cachedSource
					} else {
						packages = response
						packagesSource = "downloaded file (" + util.SanitizeURL(url) + ")"
					}
				}
			}
		}

		if packages == nil || packages.Len() == 0 {
			packages, err = r.fetchFile(url, cacheKey, hash, useLastModifiedCheck)
			packagesSource = "downloaded file (" + util.SanitizeURL(url) + ")"
			if err != nil {
				// 404s are acceptable for lazy provider repos
				code := statusCode(err)
				if r.lazyProvidersURL == "" || (code != 404 && code != 499) {
					return nil, err
				}
				packages = php.ArrayOf("packages", php.NewArray())
				packagesSource = "not-found file (" + util.SanitizeURL(url) + ")"
				if code == 499 {
					r.io.Error("<warning>"+err.Error()+"</warning>", nil)
				}
			}
		}
	} else {
		// copies: the version data is completed below, and the root data
		// must stay as it is
		partial, _ := r.partialPackagesByName.Get(name)
		versions := php.NewArrayCap(len(partial))
		for _, v := range partial {
			versions.Append(shallowClone(v))
		}
		packages = php.ArrayOf("packages", php.ArrayOf("versions", versions))
		packagesSource = "root file (" + util.SanitizeURL(r.packagesJSONURL()) + ")"
		loadingPartialPackage = true
	}

	versionsToLoad := php.NewArray()
	for _, rawVersions := range asArray(get(packages, "packages")).All() {
		for _, rawVersion := range asArray(rawVersions).All() {
			data := asArray(rawVersion)
			normalizedName := php.Strtolower(php.ToString(get(data, "name")))

			// only load the actual named package, not other packages that might find themselves in the same file
			if normalizedName != name {
				continue
			}

			if !loadingPartialPackage && hasPartial && r.partialPackagesByName.Has(normalizedName) {
				continue
			}

			uid := get(data, "uid")
			if _, ok := uid.(*php.Array); ok {
				return nil, &php.EngineError{Class: php.ClassTypeError, Message: "Cannot access offset of type array in isset or empty"}
			}
			if v, _ := versionsToLoad.Get(uid); v != nil {
				continue
			}

			versionNormalized, err := r.normalizeVersionData(data)
			if err != nil {
				return nil, err
			}

			// avoid loading packages which have already been loaded
			if alreadyLoaded[name][versionNormalized] != nil {
				continue
			}

			acceptable, err := r.isVersionAcceptable(nil, normalizedName, data, versionNormalized, acceptableStabilities, stabilityFlags)
			if err != nil {
				return nil, err
			}
			if acceptable {
				versionsToLoad.Set(uid, data)
			}
		}
	}

	// load acceptable packages in the providers
	toLoad := make([]*php.Array, 0, versionsToLoad.Len())
	for _, v := range versionsToLoad.All() {
		data, _ := v.(*php.Array)
		toLoad = append(toLoad, data)
	}
	loadedPackages, err := r.createPackages(toLoad, packagesSource)
	if err != nil {
		return nil, err
	}
	uids := versionsToLoad.Keys()

	for index, p := range loadedPackages {
		if err := p.SetRepository(r); err != nil {
			return nil, err
		}
		uid := uids[index].String()

		if alias, ok := p.(pkg.Alias); ok {
			aliased := alias.AliasOf()
			if err := aliased.SetRepository(r); err != nil {
				return nil, err
			}

			result.Set(uid, aliased)
			result.Set(uid+"-alias", p)
		} else {
			result.Set(uid, p)
		}
	}

	return result, nil
}

// normalizeVersionData sets a version's version_normalized when it is
// missing, or is the default branch alias that Composer v1 compatible
// repositories put there, and returns it.
func (r *ComposerRepository) normalizeVersionData(data *php.Array) (string, error) {
	versionNormalized := get(data, "version_normalized")
	if versionNormalized != nil && versionNormalized != pkg.DefaultBranchAlias {
		return php.ToString(versionNormalized), nil
	}

	// handling of existing repos which need to remain composer v1 compatible, in case the version_normalized contained VersionParser::DEFAULT_BRANCH_ALIAS, we renormalize it
	normalized, err := r.versionParser.Normalize(php.ToString(get(data, "version")))
	if err != nil {
		return "", err
	}
	data.Set("version_normalized", normalized)

	return normalized, nil
}

var devSuffixRegex = php.MustCompile(`{~dev$}`)

// loadAsyncPackages ports loadAsyncPackages: the packages of the names
// (and their ~dev files, when dev versions are acceptable) from the v2
// metadata, fetched in parallel. acceptableStabilities and stabilityFlags
// are nil for null.
func (r *ComposerRepository) loadAsyncPackages(packageNames *repository.ConstraintMap, acceptableStabilities, stabilityFlags *php.Array, alreadyLoaded repository.AlreadyLoaded) (repository.LoadResult, error) {
	if _, _, err := r.loadRootServerFile(noMaxAge); err != nil {
		return repository.LoadResult{}, err
	}
	r.rootFileLoaded()

	if r.lazyProvidersURL == "" {
		return repository.LoadResult{}, &util.LogicError{Message: "loadAsyncPackages only supports v2 protocol composer repos with a metadata-url"}
	}

	// load ~dev versions of the packages as well if needed
	names := packageNames.Clone()
	for name, constraint := range packageNames.All() {
		if acceptableStabilities == nil || stabilityFlags == nil || version.IsPackageAcceptable(acceptableStabilities, stabilityFlags, []string{name}, "dev") {
			names.Set(name+"~dev", constraint)
		}
		// if only dev stability is requested, we skip loading the non dev file
		if acceptableStabilities != nil && acceptableStabilities.Has("dev") && acceptableStabilities.Len() == 1 && stabilityFlags.Len() == 0 {
			names.Delete(name)
		}
	}

	var (
		fileNames, realNames []string
		constraints          []semver.ConstraintInterface
	)
	for name, constraint := range names.All() {
		name = php.Strtolower(name)

		realName, _, err := devSuffixRegex.Replace(name, "", -1)
		if err != nil {
			return repository.LoadResult{}, err
		}
		// skip platform packages, root package and composer-plugin-api
		if pkg.IsPlatformPackage(realName) || realName == "__root__" {
			continue
		}

		fileNames = append(fileNames, name)
		realNames = append(realNames, realName)
		constraints = append(constraints, constraint)
	}

	downloads, err := r.startCachedAsyncDownloads(fileNames, realNames, false)
	if err != nil {
		return repository.LoadResult{}, err
	}
	fetches := make([]*asyncFetch, len(downloads))
	for i, d := range downloads {
		fetches[i] = d.fetch
	}
	r.waitFetches(fetches)

	// Composer's callbacks run as each file arrives; here the events,
	// warnings and cache writes run in request order on this goroutine,
	// and the packages of each file are built in parallel, the results
	// being collected in request order.
	type fileResult struct {
		found    bool
		packages []pkg.PackageInterface
		err      error
	}
	results := make([]fileResult, len(downloads))
	var wg sync.WaitGroup
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	for i, d := range downloads {
		response, packagesSource, err := r.finishCachedDownload(d)
		if err != nil {
			results[i].err = err

			continue
		}
		raw := get(get2(response, "packages"), realNames[i])
		if raw == nil {
			continue
		}
		results[i].found = true
		pre := r.decoded.prebuiltFor(response)

		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			results[i].packages, results[i].err = r.buildPackages(raw, get(response, "minified") == "composer/2.0", packagesSource, realNames[i], constraints[i], acceptableStabilities, stabilityFlags, alreadyLoaded, pre)
		})
	}
	wg.Wait()

	packages := newPackageSet()
	namesFound := &repository.NameMap[struct{}]{}
	var firstErr error
	for i, result := range results {
		if result.found {
			namesFound.Set(realNames[i], struct{}{})
		}
		err := result.err
		for _, p := range result.packages {
			if err != nil {
				break
			}
			if err = p.SetRepository(r); err != nil {
				break
			}
			packages.add(p)

			if alias, ok := p.(pkg.Alias); ok && !packages.has(alias.AliasOf()) {
				if err = alias.AliasOf().SetRepository(r); err == nil {
					packages.add(alias.AliasOf())
				}
			}
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if firstErr != nil {
		return repository.LoadResult{}, firstErr
	}

	return repository.LoadResult{NamesFound: namesFound.Keys(), Packages: packages.list}, nil
}

// buildPackages is the part of loadAsyncPackages' callback that builds
// the packages of a file's version list (raw, minified or not). It only
// reads the repository, so files are built in parallel.
func (r *ComposerRepository) buildPackages(raw any, minified bool, packagesSource, realName string, constraint semver.ConstraintInterface, acceptableStabilities, stabilityFlags *php.Array, alreadyLoaded repository.AlreadyLoaded, pre *prebuilt) ([]pkg.PackageInterface, error) {
	items := asArray(raw).Values()
	if minified {
		if versions := pre.versionsFor(realName, len(items), acceptableStabilities, stabilityFlags); versions != nil {
			if packages, ok, err := r.buildFromVersions(items, versions, packagesSource, realName, constraint, alreadyLoaded, pre); ok {
				if r.observe.versionsShared != nil {
					r.observe.versionsShared()
				}

				return packages, err
			}
		}

		// the versions loaded are copied out of the expansion, the others
		// only looked at; the packages a speculation built from them
		// already (pre) are taken instead, in their place
		var (
			versionsToLoad []*php.Array
			loaded         []pkg.PackageInterface // nil where versionsToLoad has the version
		)
		index := -1
		ok, err := expandEach(items, func(data *php.Array, keep func() *php.Array) error {
			index++
			var versionNormalized string
			normalized := true
			if v := get(data, "version_normalized"); v != nil && v != pkg.DefaultBranchAlias {
				versionNormalized = php.ToString(v)
			} else {
				// normalizeVersionData sets it in the version
				normalized = false
				data = keep()
				keep = func() *php.Array { return data }
				var err error
				if versionNormalized, err = r.normalizeVersionData(data); err != nil {
					return err
				}
			}

			// avoid loading packages which have already been loaded
			if alreadyLoaded[realName][versionNormalized] != nil {
				return nil
			}

			acceptable, err := r.isVersionAcceptable(constraint, realName, data, versionNormalized, acceptableStabilities, stabilityFlags)
			if err != nil {
				return err
			}
			if acceptable {
				var built pkg.PackageInterface
				if normalized {
					built = pre.take(realName, r.notifyURL, index)
				}
				if built == nil {
					versionsToLoad = append(versionsToLoad, keep())
				}
				loaded = append(loaded, built)
			}

			return nil
		})
		if err != nil {
			return nil, err
		}
		if ok {
			return r.createAround(versionsToLoad, loaded, packagesSource)
		}

		if items, err = expandVersions(items); err != nil {
			return nil, err
		}
	}
	versions := make([]*php.Array, 0, len(items))
	for _, v := range items {
		data, ok := v.(*php.Array)
		if !ok {
			// isset($version['version_normalized']) is false, then
			// $version['version'] is read for normalize()
			return nil, versionOffsetError(v)
		}
		versions = append(versions, data)
	}

	versionsToLoad := make([]*php.Array, 0, len(versions))
	for _, data := range versions {
		versionNormalized, err := r.normalizeVersionData(data)
		if err != nil {
			return nil, err
		}

		// avoid loading packages which have already been loaded
		if alreadyLoaded[realName][versionNormalized] != nil {
			continue
		}

		acceptable, err := r.isVersionAcceptable(constraint, realName, data, versionNormalized, acceptableStabilities, stabilityFlags)
		if err != nil {
			return nil, err
		}
		if acceptable {
			versionsToLoad = append(versionsToLoad, data)
		}
	}

	return r.createPackages(versionsToLoad, packagesSource)
}

// buildFromVersions is buildPackages for a minified list of which a
// speculation read every version exactly (versions, see versionsOf): the
// versions' stabilities, version_normalized and branch aliases decide
// which are loaded, as isVersionAcceptable decides, without expanding
// the list; it is expanded only up to the last version loaded that the
// speculation did not build already (pre). ok is false, and nothing
// created, if the list does not expand as versionsOf read it.
func (r *ComposerRepository) buildFromVersions(items []any, versions []*speculatedVersion, packagesSource, realName string, constraint semver.ConstraintInterface, alreadyLoaded repository.AlreadyLoaded, pre *prebuilt) ([]pkg.PackageInterface, bool, error) {
	matches := func(string) bool { return true }
	if constraint != nil {
		matches = semver.CompilingMatcher.Matcher(constraint, semver.OpEQ)
	}

	var (
		loaded []pkg.PackageInterface // nil where the version is to be loaded
		load   = map[int]bool{}       // the indexes of those
		last   = -1
	)
	for _, v := range versions {
		// avoid loading packages which have already been loaded
		if alreadyLoaded[realName][v.normalized] != nil {
			continue
		}
		if accepted := (v.stable && matches(v.normalized)) || (v.aliasStable && matches(v.alias)); !accepted {
			continue
		}
		built := pre.take(realName, r.notifyURL, v.index)
		if built == nil {
			load[v.index] = true
			last = v.index
		}
		loaded = append(loaded, built)
	}

	var versionsToLoad []*php.Array
	if last >= 0 {
		index := -1
		ok, err := expandEach(items, func(_ *php.Array, keep func() *php.Array) error {
			index++
			if index > last {
				return errStopExpanding
			}
			if load[index] {
				versionsToLoad = append(versionsToLoad, keep())
			}

			return nil
		})
		if !ok || (err != nil && !errors.Is(err, errStopExpanding)) || len(versionsToLoad) != len(load) {
			return nil, false, nil
		}
	}
	packages, err := r.createAround(versionsToLoad, loaded, packagesSource)

	return packages, true, err
}

// createAround creates the packages of versionsToLoad and returns them in
// the places loaded leaves for them (nil entries), between the packages
// it holds, which are configured as createPackages configures the
// packages it creates.
func (r *ComposerRepository) createAround(versionsToLoad []*php.Array, loaded []pkg.PackageInterface, packagesSource string) ([]pkg.PackageInterface, error) {
	created, err := r.createPackages(versionsToLoad, packagesSource)
	if err != nil || len(created) == len(loaded) {
		return created, err
	}
	for i, p := range loaded {
		if p == nil {
			loaded[i], created = created[0], created[1:]
		} else {
			r.configureLoaded(p)
		}
	}

	return loaded, nil
}

// errStopExpanding stops expandEach where the versions needed end.
var errStopExpanding = errors.New("stop expanding")

// shallowClone copies an array without copying the arrays it holds.
func shallowClone(a *php.Array) *php.Array {
	if a == nil {
		return nil
	}
	return a.ShallowClone()
}

// get2 is get() for a value that may not be an array.
func get2(v any, key string) *php.Array {
	a, _ := get(asArrayOrNil(v), key).(*php.Array)

	return a
}

func asArrayOrNil(v any) *php.Array {
	a, _ := v.(*php.Array)

	return a
}

// isVersionAcceptable ports isVersionAcceptable: whether the version, or
// its branch alias, has an acceptable stability and matches constraint
// (nil: any).
func (r *ComposerRepository) isVersionAcceptable(constraint semver.ConstraintInterface, name string, versionData *php.Array, versionNormalized string, acceptableStabilities, stabilityFlags *php.Array) (bool, error) {
	versions := []string{versionNormalized}

	alias, ok, err := r.loader.GetBranchAlias(versionData)
	if err != nil {
		return false, err
	}
	if ok && php.ToBool(alias) {
		versions = append(versions, alias)
	}

	for _, v := range versions {
		if acceptableStabilities != nil && stabilityFlags != nil && !version.IsPackageAcceptable(acceptableStabilities, stabilityFlags, []string{name}, semver.ParseStability(v)) {
			continue
		}

		if constraint != nil && !semver.CompilingMatcher.Match(constraint, semver.OpEQ, v) {
			continue
		}

		return true, nil
	}

	return false, nil
}

// versionOffsetError is reading $version['version'] (ComposerRepository.php
// line 1329) of a version entry that is not an array: a TypeError for a
// string, else PHP's "Trying to access array offset" warning, which
// Composer's ErrorHandler throws.
func versionOffsetError(v any) error {
	if _, ok := v.(string); ok {
		return &php.EngineError{Class: php.ClassTypeError, Message: "Cannot access offset of type string on string"}
	}

	return &util.ErrorException{Message: "Trying to access array offset on " + php.ZvalValueName(v)}
}

// expandEach calls fn with each version MetadataMinifier::expand($versions)
// gives, in order, without copying every version as expand does: data is
// the expanded version, which fn must not change and may only read until
// it returns, and keep returns it as expand returns it, an array of its
// own (the entry itself for a version expand does not copy). It stops at
// fn's first error. ok is false, and fn not called, for lists that are
// not all arrays with string keys only (expandVersions handles those).
func expandEach(items []any, fn func(data *php.Array, keep func() *php.Array) error) (ok bool, err error) {
	arrays := make([]*php.Array, len(items))
	for i, v := range items {
		a, ok := v.(*php.Array)
		if !ok {
			return false, nil
		}
		for k := range a.All() {
			if k.IsInt() {
				return false, nil
			}
		}
		arrays[i] = a
	}

	// working is the expanded version, kept up to date in place: with
	// string keys only, applying a version's changes to it gives the keys,
	// order and values of the copy expand makes.
	var working *php.Array
	for _, versionData := range arrays {
		if working == nil || working.Len() == 0 {
			// expand takes the entry itself; it is copied before fn may
			// have it changed
			working = shallowClone(versionData)
			if err := fn(working, func() *php.Array { return versionData }); err != nil {
				return true, err
			}

			continue
		}

		for k, v := range versionData.All() {
			if v == "__unset" {
				working.DeleteKey(k)
			} else {
				working.SetKey(k, v)
			}
		}
		if err := fn(working, func() *php.Array { return shallowClone(working) }); err != nil {
			return true, err
		}
	}

	return true, nil
}

// expandVersions is MetadataMinifier::expand($versions). Entries that are
// all arrays go through metadataminifier.Expand; otherwise PHP's loop is
// followed with its untyped values: a falsy expanded version is replaced
// by the next entry, an entry that is not an array is foreach()'s warning,
// and an expanded version that is a truthy scalar fails the write or
// unset() of each key (composer/metadata-minifier declares no
// strict_types).
func expandVersions(items []any) ([]any, error) {
	arrays := make([]*php.Array, 0, len(items))
	for _, v := range items {
		a, ok := v.(*php.Array)
		if !ok {
			break
		}
		arrays = append(arrays, a)
	}
	if len(arrays) == len(items) {
		expanded := metadataminifier.Expand(arrays)
		out := make([]any, len(expanded))
		for i, a := range expanded {
			out[i] = a
		}

		return out, nil
	}

	expanded := make([]any, 0, len(items))
	var expandedVersion any
	for _, versionData := range items {
		if !php.ToBool(expandedVersion) {
			expandedVersion = versionData
			expanded = append(expanded, expandedVersion)

			continue
		}

		data, ok := versionData.(*php.Array)
		if !ok {
			return nil, &util.ErrorException{Message: "foreach() argument must be of type array|object, " + php.ZvalValueName(versionData) + " given"}
		}
		// arrays are values: the entry appended before keeps its keys
		if a, ok := expandedVersion.(*php.Array); ok {
			expandedVersion = a.Clone()
		}
		for k, val := range data.All() {
			if val == "__unset" {
				switch c := expandedVersion.(type) {
				case *php.Array:
					c.DeleteKey(k)
				case string:
					return nil, &php.EngineError{Class: "Error", Message: "Cannot unset string offsets"}
				default:
					return nil, &php.EngineError{Class: "Error", Message: "Cannot unset offset in a non-array variable"}
				}

				continue
			}
			// expandedVersion is truthy: never null or false here
			a, _, _, e := php.WritableArray(expandedVersion)
			if e != nil {
				return nil, e
			}
			a.SetKey(k, val)
		}

		expanded = append(expanded, expandedVersion)
	}

	return expanded, nil
}
