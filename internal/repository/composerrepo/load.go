// Ports src/Composer/Repository/ComposerRepository.php: whatProvides,
// loadAsyncPackages and isVersionAcceptable, the loading of package
// versions from provider files and v2 metadata.

package composerrepo

import (
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
				return nil, &pkg.TypeError{Message: "Illegal offset type"}
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

	downloads, err := r.startCachedAsyncDownloads(fileNames, realNames)
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

		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			results[i].packages, results[i].err = r.buildPackages(raw, get(response, "minified") == "composer/2.0", packagesSource, realNames[i], constraints[i], acceptableStabilities, stabilityFlags, alreadyLoaded)
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
func (r *ComposerRepository) buildPackages(raw any, minified bool, packagesSource, realName string, constraint semver.ConstraintInterface, acceptableStabilities, stabilityFlags *php.Array, alreadyLoaded repository.AlreadyLoaded) ([]pkg.PackageInterface, error) {
	versionList := asArray(raw)
	versions := make([]*php.Array, 0, versionList.Len())
	for _, v := range versionList.All() {
		data, ok := v.(*php.Array)
		if !ok {
			return nil, &pkg.TypeError{Message: "Cannot access offset of type string on " + php.TypeName(v)}
		}
		versions = append(versions, data)
	}

	if minified {
		versions = metadataminifier.Expand(versions)
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

// shallowClone copies an array without copying the arrays it holds.
func shallowClone(a *php.Array) *php.Array {
	if a == nil {
		return nil
	}
	c := php.NewArrayCap(a.Len())
	for k, v := range a.All() {
		c.SetKey(k, v)
	}

	return c
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
