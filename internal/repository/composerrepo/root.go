// Ports src/Composer/Repository/ComposerRepository.php: the root file
// (packages.json) and what it configures, v1 includes and provider
// listings, partial packages and package creation.

package composerrepo

import (
	"strings"

	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// noMaxAge is loadRootServerFile's null $rootMaxAge.
const noMaxAge = -1

// hasProvidersCheck ports hasProviders.
func (r *ComposerRepository) hasProvidersCheck() (bool, error) {
	if _, _, err := r.loadRootServerFile(noMaxAge); err != nil {
		return false, err
	}

	return r.hasProviders, nil
}

// providerNames ports getProviderNames.
func (r *ComposerRepository) providerNames() ([]string, error) {
	if _, _, err := r.loadRootServerFile(noMaxAge); err != nil {
		return nil, err
	}
	if err := r.ensureProviderListing(); err != nil {
		return nil, err
	}

	if r.lazyProvidersURL != "" {
		// Can not determine list of provided packages for lazy repositories
		return nil, nil
	}

	if r.providersURL != "" && r.providerListing != nil {
		return r.providerListing.Keys(), nil
	}

	return nil, nil
}

// ensureProviderListing loads the provider listings of the root file when
// that was not done yet.
func (r *ComposerRepository) ensureProviderListing() error {
	if r.providerListing != nil {
		return nil
	}
	data, consumed, err := r.loadRootServerFile(noMaxAge)
	if err != nil || consumed {
		return err
	}

	return r.loadProviderListings(data)
}

// packagesJSONURL ports getPackagesJsonUrl.
func (r *ComposerRepository) packagesJSONURL() string {
	if path := util.URLPath(php.Strtr(r.url, `\`, "/")); strings.Contains(path, ".json") {
		return r.url
	}

	return r.url + "/packages.json"
}

// loadRootServerFile ports loadRootServerFile; rootMaxAge noMaxAge is
// null. It returns the root data, or consumed true for PHP's true (the
// partial packages took the data over).
func (r *ComposerRepository) loadRootServerFile(rootMaxAge int64) (*php.Array, bool, error) {
	if r.rootLoaded {
		return r.rootData, r.rootConsumed, nil
	}

	var (
		data   *php.Array
		hasSet bool
	)
	cached, ok, err := r.cache.Read("packages.json")
	if err != nil {
		return nil, false, err
	}
	if ok && php.ToBool(cached) {
		cachedData := decodeArray(cached)
		if age, ok := r.cache.Age("packages.json"); rootMaxAge != noMaxAge && ok && age <= rootMaxAge {
			data, hasSet = cachedData, cachedData != nil
		} else if lastModified := get(cachedData, "last-modified"); lastModified != nil {
			response, fresh, err := r.fetchFileIfLastModified(r.packagesJSONURL(), "packages.json", php.ToString(lastModified))
			if err != nil {
				return nil, false, err
			}
			if fresh {
				response = cachedData
			}
			data, hasSet = response, response != nil
		}
	}

	if !hasSet {
		data, err = r.fetchFile(r.packagesJSONURL(), "packages.json", "", true)
		if err != nil {
			return nil, false, err
		}
	}
	if data == nil {
		data = php.NewArray()
	}

	if err := r.configureFromRoot(data); err != nil {
		return nil, false, err
	}

	r.rootData = data
	r.rootLoaded = true

	return data, false, nil
}

// configureFromRoot is the part of loadRootServerFile reading the root
// data's settings.
func (r *ComposerRepository) configureFromRoot(data *php.Array) error {
	canonical := func(key string) (string, bool, error) {
		v := get(data, key)
		if !php.ToBool(v) {
			return "", false, nil
		}
		url, err := r.canonicalizeURL(php.ToString(v))

		return url, true, err
	}
	setURL := func(dst *string, key string) error {
		url, ok, err := canonical(key)
		if ok && err == nil {
			*dst = url
		}

		return err
	}

	if php.ToBool(get(data, "notify-batch")) {
		if err := setURL(&r.notifyURL, "notify-batch"); err != nil {
			return err
		}
	} else if err := setURL(&r.notifyURL, "notify"); err != nil {
		return err
	}

	if err := setURL(&r.searchURL, "search"); err != nil {
		return err
	}

	if mirrors, _ := data.GetArray("mirrors"); mirrors != nil && mirrors.Len() > 0 {
		for _, raw := range mirrors.All() {
			m, _ := raw.(*php.Array)
			preferred := php.ToBool(get(m, "preferred"))
			for _, typ := range [2]string{"git", "hg"} {
				if url := get(m, typ+"-url"); php.ToBool(url) {
					r.addSourceMirror(typ, mirror{url: url, preferred: preferred})
				}
			}
			if url := get(m, "dist-url"); php.ToBool(url) {
				canonicalURL, err := r.canonicalizeURL(php.ToString(url))
				if err != nil {
					return err
				}
				if r.distMirrors == nil {
					r.distMirrors = php.NewArray()
				}
				r.distMirrors.Append(php.ArrayOf("url", canonicalURL, "preferred", preferred))
			}
		}
	}

	if url, ok, err := canonical("providers-lazy-url"); err != nil {
		return err
	} else if ok {
		r.lazyProvidersURL = url
		r.hasProviders = true

		r.hasPartialPackages = isNonEmptyArray(get(data, "packages"))
	}

	// metadata-url indicates V2 repo protocol so it takes over from all the V1 types
	// V2 only has lazyProviders and possibly partial packages, but no ability to process anything else,
	// V2 also supports async loading
	if url, ok, err := canonical("metadata-url"); err != nil {
		return err
	} else if ok {
		if err := r.configureV2(data, url); err != nil {
			return err
		}
	}

	if r.allowSslDowngrade {
		r.url = strings.ReplaceAll(r.url, "https://", "http://")
		r.baseURL = strings.ReplaceAll(r.baseURL, "https://", "http://")
	}

	if url, ok, err := canonical("providers-url"); err != nil {
		return err
	} else if ok {
		r.providersURL = url
		r.hasProviders = true
	}

	if err := setURL(&r.listURL, "list"); err != nil {
		return err
	}

	if php.ToBool(get(data, "providers")) || php.ToBool(get(data, "providers-includes")) {
		r.hasProviders = true
	}

	return setURL(&r.providersAPIURL, "providers-api")
}

// configureV2 is loadRootServerFile's handling of a metadata-url.
func (r *ComposerRepository) configureV2(data *php.Array, metadataURL string) error {
	r.lazyProvidersURL = metadataURL
	r.providersURL = ""
	r.hasProviders = false
	r.hasPartialPackages = isNonEmptyArray(get(data, "packages"))
	r.allowSslDowngrade = false

	// provides a list of package names that are available in this repo
	// this disables lazy-provider behavior in the sense that if a list is available we assume it is finite and won't search for other packages in that repo
	// while if no list is there lazyProvidersUrl is used when looking for any package name to see if the repo knows it
	if availablePackages := get(data, "available-packages"); php.ToBool(availablePackages) {
		list := asArray(availablePackages)
		r.availablePackages = []string{}
		r.availablePackageSet = map[string]struct{}{}
		for _, v := range list.All() {
			name := php.Strtolower(php.ToString(v))
			if _, ok := r.availablePackageSet[name]; !ok {
				r.availablePackageSet[name] = struct{}{}
				r.availablePackages = append(r.availablePackages, name)
			}
		}
		r.hasAvailablePackageList = true
	}

	// Provides a list of package name patterns (using * wildcards to match any substring, e.g. "vendor/*") that are available in this repo
	// Disables lazy-provider behavior as with available-packages, but may allow much more compact expression of packages covered by this repository.
	// Over-specifying covered packages is safe, but may result in increased traffic to your repository.
	if patterns := get(data, "available-package-patterns"); php.ToBool(patterns) {
		list := asArray(patterns)
		r.availablePackagePatterns = []*php.Regexp{}
		for _, v := range list.All() {
			re, err := php.Compile(pkg.PackageNameToRegexp(php.ToString(v), "{^%s$}i"))
			if err != nil {
				return err
			}
			r.availablePackagePatterns = append(r.availablePackagePatterns, re)
		}
		r.hasAvailablePackageList = true
	}

	// Remove legacy keys as most repos need to be compatible with Composer v1
	// as well but we are not interested in the old format anymore at this point
	data.Delete("providers-url")
	data.Delete("providers")
	data.Delete("providers-includes")

	if advisories, ok := data.GetArray("security-advisories"); ok {
		r.securityAdvisoryConfig = &securityAdvisoryConfig{metadata: php.ToBool(get(advisories, "metadata"))}
		if apiURL, ok := advisories.GetString("api-url"); ok {
			url, err := r.canonicalizeURL(apiURL)
			if err != nil {
				return err
			}
			r.securityAdvisoryConfig.apiURL = url
		}
		if r.securityAdvisoryConfig.apiURL == "" && !r.hasAvailablePackageList {
			return &util.UnexpectedValueError{Site: phperr.At("ComposerRepository.php", 1543), Message: "Invalid security advisory configuration on " + r.RepoName() + ": If the repository does not provide a security-advisories.api-url then available-packages or available-package-patterns are required to be provided for performance reason."}
		}
	}

	if filter, ok := data.GetArray("filter"); ok {
		r.filterConfig = filterlist.ComposerRepositoryFilterInformationFromData(filter, func(url string) string {
			// fromData only passes non-empty strings, which canonicalize
			canonical, _ := r.canonicalizeURL(url)

			return canonical
		})
	}

	return nil
}

func (r *ComposerRepository) addSourceMirror(typ string, m mirror) {
	if r.sourceMirrors == nil {
		r.sourceMirrors = map[string]*php.Array{}
	}
	list := r.sourceMirrors[typ]
	if list == nil {
		list = php.NewArray()
		r.sourceMirrors[typ] = list
	}
	list.Append(php.ArrayOf("url", m.url, "preferred", m.preferred))
}

// asArray returns v when it is an array, else an empty array: malformed
// metadata is read leniently instead of failing on PHP's warnings.
func asArray(v any) *php.Array {
	if a, ok := v.(*php.Array); ok {
		return a
	}

	return php.NewArray()
}

// isNonEmptyArray is !empty($v) && is_array($v).
func isNonEmptyArray(v any) bool {
	a, ok := v.(*php.Array)

	return ok && a.Len() > 0
}

var urlOriginRegex = php.MustCompile(`{^[^:]++://[^/]*+}`)

// canonicalizeURL ports canonicalizeUrl: an absolute path is resolved
// against the repository URL's scheme and host.
func (r *ComposerRepository) canonicalizeURL(url string) (string, error) {
	if url == "" {
		return "", &util.InvalidArgumentError{Site: phperr.At("ComposerRepository.php", 1583), Message: "Expected a string with a value and not an empty string"}
	}

	if strings.HasPrefix(url, "/") {
		if m, err := urlOriginRegex.Match(r.url); err != nil {
			return "", err
		} else if m != nil {
			return m.Get(0) + url, nil
		}

		return r.url, nil
	}

	return url, nil
}

// loadDataFromServer ports loadDataFromServer.
func (r *ComposerRepository) loadDataFromServer() ([]*php.Array, error) {
	data, consumed, err := r.loadRootServerFile(noMaxAge)
	if err != nil {
		return nil, err
	}
	if consumed {
		return nil, &util.LogicError{Site: phperr.At("ComposerRepository.php", 1604), Message: "loadRootServerFile should not return true during initialization"}
	}

	return r.loadIncludes(data)
}

// hasPartialPackagesCheck ports hasPartialPackages.
func (r *ComposerRepository) hasPartialPackagesCheck() (bool, error) {
	if r.hasPartialPackages && r.partialPackagesByName == nil {
		if err := r.initializePartialPackages(); err != nil {
			return false, err
		}
	}

	return r.hasPartialPackages, nil
}

// loadProviderListings ports loadProviderListings.
func (r *ComposerRepository) loadProviderListings(data *php.Array) error {
	if data == nil {
		return nil
	}
	if providers, ok := data.Get("providers"); ok && providers != nil {
		if r.providerListing == nil {
			r.providerListing = &repository.NameMap[string]{}
		}
		for name, v := range asArray(providers).All() {
			metadata, _ := v.(*php.Array)
			r.providerListing.Set(name.String(), php.ToString(get(metadata, "sha256")))
		}
	}

	includes, _ := data.Get("provider-includes")
	if r.providersURL != "" && includes != nil {
		for k, v := range asArray(includes).All() {
			include := k.String()
			metadata, _ := v.(*php.Array)
			sha256 := php.ToString(get(metadata, "sha256"))
			url := r.baseURL + "/" + strings.ReplaceAll(include, "%hash%", sha256)
			cacheKey := strings.ReplaceAll(strings.ReplaceAll(include, "%hash%", ""), "$", "")

			var includedData *php.Array
			cachedHash, ok, err := r.cache.Sha256(cacheKey)
			if err != nil {
				return err
			}
			if ok && cachedHash == sha256 {
				if includedData, err = r.readCachedJSON(cacheKey); err != nil {
					return err
				}
			} else if includedData, err = r.fetchFile(url, cacheKey, sha256, false); err != nil {
				return err
			}

			if err := r.loadProviderListings(includedData); err != nil {
				return err
			}
		}
	}

	return nil
}

// readCachedJSON is json_decode($this->cache->read($cacheKey), true) for
// arrays.
func (r *ComposerRepository) readCachedJSON(cacheKey string) (*php.Array, error) {
	contents, _, err := r.cache.Read(cacheKey)
	if err != nil {
		return nil, err
	}

	return decodeArray(contents), nil
}

// decodeArray is json_decode($json, true) when that gives an array, else
// nil.
func decodeArray(json string) *php.Array {
	v, err := php.JSONDecode(json, true)
	if err != nil {
		return nil
	}
	a, _ := v.(*php.Array)

	return a
}

// loadIncludes ports loadIncludes: the package versions of a v1 root
// file and its includes.
func (r *ComposerRepository) loadIncludes(data *php.Array) ([]*php.Array, error) {
	var packages []*php.Array
	if data == nil {
		data = php.NewArray()
	}

	// legacy repo handling
	if !data.Has("packages") && !data.Has("includes") {
		for _, raw := range data.All() {
			if versions, ok := asArray(raw).GetArray("versions"); ok {
				packages = appendVersions(packages, versions)
			}
		}

		return packages, nil
	}

	if all, _ := data.GetArray("packages"); all != nil {
		for k, raw := range all.All() {
			packageName := php.Strtolower(k.String())
			for _, rawMetadata := range asArray(raw).All() {
				metadata, _ := rawMetadata.(*php.Array)
				packages = append(packages, metadata)
				metadataName := php.ToString(get(metadata, "name"))
				if !r.displayedWarningAboutNonMatchingPackageIndex && packageName != php.Strtolower(metadataName) {
					r.displayedWarningAboutNonMatchingPackageIndex = true
					r.io.WriteError("<warning>Warning: the packages key '"+k.String()+"' doesn't match the name defined in the package metadata '"+metadataName+"' in repository "+r.baseURL+"</warning>", true, io.Normal)
				}
			}
		}
	}

	if includes, _ := data.GetArray("includes"); includes != nil {
		for k, raw := range includes.All() {
			include := k.String()
			metadata, _ := raw.(*php.Array)

			var includedData *php.Array
			sha1 := get(metadata, "sha1")
			cachedSha1, ok := "", false
			if sha1 != nil {
				var err error
				if cachedSha1, ok, err = r.cache.Sha1(include); err != nil {
					return nil, err
				}
			}
			var err error
			if sha1 != nil && ok && cachedSha1 == sha1 {
				includedData, err = r.readCachedJSON(include)
			} else {
				includedData, err = r.fetchFileRelative(include)
			}
			if err != nil {
				return nil, err
			}

			included, err := r.loadIncludes(includedData)
			if err != nil {
				return nil, err
			}
			packages = append(packages, included...)
		}
	}

	return packages, nil
}

// appendVersions appends the version arrays of a versions list.
func appendVersions(packages []*php.Array, versions *php.Array) []*php.Array {
	for _, v := range versions.All() {
		metadata, _ := v.(*php.Array)
		packages = append(packages, metadata)
	}

	return packages
}

// createPackages ports createPackages: the packages of the version
// arrays, with the repository's mirrors and transport options. The
// arrays get their notification-url set, so callers pass arrays nothing
// else uses (each decode of a metadata file is private to its caller).
func (r *ComposerRepository) createPackages(packages []*php.Array, source string) ([]pkg.PackageInterface, error) {
	if len(packages) == 0 {
		return nil, nil
	}

	var notifyURL any
	if r.notifyURL != "" {
		notifyURL = r.notifyURL
	}
	for i, data := range packages {
		if data == nil {
			data = php.NewArray()
			packages[i] = data
		}
		if v, _ := data.Get("notification-url"); v == nil {
			data.Set("notification-url", notifyURL)
		}
	}

	packageInstances, err := r.loader.LoadPackages(packages)
	if err != nil {
		if isPHPError(err) {
			return nil, err
		}
		from := ""
		if source != "" {
			from = " from " + source
		}

		return nil, newRuntimeError(phperr.At("ComposerRepository.php", 1726), "Could not load packages in "+r.RepoName()+from+": ["+exceptionClass(err)+"] "+err.Error(), err)
	}

	for _, p := range packageInstances {
		if mirrors, ok := r.sourceMirrors[p.SourceType().S]; ok && p.SourceType().Valid {
			p.SetSourceMirrors(mirrors)
		}
		p.SetDistMirrors(r.distMirrors)
		r.configurePackageTransportOptions(p)
	}

	return packageInstances, nil
}

// initializePartialPackages ports initializePartialPackages: it groups
// the packages inlined in a packages.json that has a providers-lazy-url
// or metadata-url by name. It is called once.
func (r *ComposerRepository) initializePartialPackages() error {
	rootData, consumed, err := r.loadRootServerFile(noMaxAge)
	if err != nil || consumed {
		return err
	}

	r.partialPackagesByName = &repository.NameMap[[]*php.Array]{}
	for k, raw := range asArray(get(rootData, "packages")).All() {
		for _, rawVersion := range asArray(raw).All() {
			version, _ := rawVersion.(*php.Array)
			versionName := php.ToString(get(version, "name"))
			versionPackageName := php.Strtolower(versionName)
			list, _ := r.partialPackagesByName.Get(versionPackageName)
			r.partialPackagesByName.Set(versionPackageName, append(list, version))
			if !r.displayedWarningAboutNonMatchingPackageIndex && versionPackageName != php.Strtolower(k.String()) {
				r.io.WriteError("<warning>Warning: the packages key '"+k.String()+"' doesn't match the name defined in the package metadata '"+versionName+"' in repository "+r.baseURL+"</warning>", true, io.Normal)
				r.displayedWarningAboutNonMatchingPackageIndex = true
			}
		}
	}

	// wipe rootData as it is fully consumed at this point and this saves some memory
	r.rootData = nil
	r.rootConsumed = true

	return nil
}

// lazyProvidersRepoContains ports lazyProvidersRepoContains: whether the
// name is in available-packages or matches available-package-patterns.
// Callers check hasAvailablePackageList first.
// The error is the PcreException Preg::isMatch throws.
func (r *ComposerRepository) lazyProvidersRepoContains(name string) (bool, error) {
	if _, ok := r.availablePackageSet[name]; ok {
		return true, nil
	}

	for _, providerRegex := range r.availablePackagePatterns {
		if m, err := providerRegex.IsMatch(name); err != nil || m {
			return m, err
		}
	}

	return false, nil
}
