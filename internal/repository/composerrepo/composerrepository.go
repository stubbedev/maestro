// Ports src/Composer/Repository/ComposerRepository.php: the constructor,
// the RepositoryInterface methods, search and providers.

package composerrepo

import (
	"strings"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Class is the PHP class ComposerRepository mirrors.
const Class = `Composer\Repository\ComposerRepository`

// HTTPDownloader is the part of Composer\Util\HttpDownloader the
// repository uses: synchronous and asynchronous GET requests.
// *http.HttpDownloader implements it, as does httpmock.Downloader.
//
// When the downloader also has Wait() (as *http.HttpDownloader does), the
// repository calls it to drive asynchronous requests, as Composer's Loop
// does; a downloader without it must return settled promises from Add.
// EnableAsync(), when present, is called once, as new Loop() does.
type HTTPDownloader interface {
	http.Getter
	Add(url string, options *php.Array) (*util.Promise[*http.Response], error)
}

// EventDispatcher is the part of Composer\EventDispatcher\EventDispatcher
// the repository uses to fire PRE_FILE_DOWNLOAD and POST_FILE_DOWNLOAD
// for metadata; *eventdispatcher.EventDispatcher implements it. All
// dispatches happen on the goroutine calling the repository.
type EventDispatcher interface {
	Dispatch(eventName string, event eventdispatcher.Event) (int, error)
}

// Config is the part of Composer\Config the repository reads;
// *config.Config implements it.
type Config interface {
	Get(key string, flags int) (any, error)
}

type mirror struct {
	url       any
	preferred bool
}

// securityAdvisoryConfig is the repository's security-advisories
// configuration: ['metadata' => bool, 'api-url' => ?string].
type securityAdvisoryConfig struct {
	metadata bool
	apiURL   string // "" for null
}

// ComposerRepository ports Composer\Repository\ComposerRepository: a
// repository served by a Composer repository server (packages.json),
// Packagist included.
//
// It is not safe for concurrent use; it runs the decoding of the
// metadata it downloads in parallel itself.
type ComposerRepository struct {
	repository.ArrayRepository

	repoConfig     *php.Array
	options        *php.Array
	url            string
	baseURL        string
	io             io.IO
	httpDownloader HTTPDownloader
	cache          *cache.Cache
	// URLs from packages.json, "" for null.
	notifyURL       string
	searchURL       string
	providersAPIURL string
	hasProviders    bool
	providersURL    string
	listURL         string
	// hasAvailablePackageList tells whether a comprehensive list of
	// packages this repository might provide is expressed in the
	// repository root.
	hasAvailablePackageList bool
	// availablePackages is nil for null; availablePackageSet holds the
	// same names.
	availablePackages        []string
	availablePackageSet      map[string]struct{}
	availablePackagePatterns []*php.Regexp // nil for null
	lazyProvidersURL         string
	// providerListing maps provider names to their sha256; nil for null.
	providerListing   *repository.NameMap[string]
	versionParser     *pkg.VersionParser
	loader            *loader.ArrayLoader
	allowSslDowngrade bool
	eventDispatcher   EventDispatcher
	sourceMirrors     map[string]*php.Array // type => list of ['url' => ..., 'preferred' => ...]
	distMirrors       *php.Array            // nil for null
	degradedMode      bool
	// rootData is loadRootServerFile's result: unset (rootLoaded false),
	// the packages.json data, or true once the partial packages consumed
	// it (rootConsumed).
	rootData     *php.Array
	rootLoaded   bool
	rootConsumed bool

	hasPartialPackages    bool
	partialPackagesByName *repository.NameMap[[]*php.Array] // nil for null

	displayedWarningAboutNonMatchingPackageIndex bool

	securityAdvisoryConfig *securityAdvisoryConfig
	filterConfig           *filterlist.ComposerRepositoryFilterInformation
	// userFilterDisabled is the per-repo filter config `false`, which
	// disables every advertised list; userFilterSkipped names the lists
	// set to false.
	userFilterDisabled bool
	userFilterSkipped  map[string]struct{}

	// freshMetadataUrls are the metadata URLs known to be fresh, which can
	// be loaded from the cache directly when loaded again.
	freshMetadataUrls map[string]struct{}
	// packagesNotFoundCache are the URLs which returned a 404 and must
	// not be fetched again.
	packagesNotFoundCache map[string]struct{}

	filterAPIClient    *filterlist.FilterListApiClient
	filterEntryBuilder *filterlist.FilterListEntryBuilder

	// initialized tells whether ArrayRepository::initialize ran, for
	// getProviders' `if ($this->packages)`.
	initialized bool
}

var (
	_ repository.RepositoryInterface    = (*ComposerRepository)(nil)
	_ repository.ConfigurableRepository = (*ComposerRepository)(nil)
	_ repository.AdvisoryProvider       = (*ComposerRepository)(nil)
	_ repository.FilterListProvider     = (*ComposerRepository)(nil)
	_ repository.Constructor            = Constructor
	_ HTTPDownloader                    = (*http.HttpDownloader)(nil)
	_ EventDispatcher                   = (*eventdispatcher.EventDispatcher)(nil)
)

// Constructor is the repository.Constructor of the "composer" type, for
// repository.Manager's ExternalTypes.
func Constructor(config *php.Array, deps repository.Deps) (repository.RepositoryInterface, error) {
	if deps.HTTPDownloader == nil {
		return nil, &util.LogicError{Message: "ComposerRepository needs an HttpDownloader"}
	}
	var dispatcher EventDispatcher
	if d, ok := deps.EventDispatcher.(EventDispatcher); ok && d != nil {
		dispatcher = d
	}

	return New(config, deps.IO, deps.Config, deps.HTTPDownloader, dispatcher)
}

var (
	schemeRegex    = php.MustCompile(`{^[\w.]+\??://}`)
	packagistRegex = php.MustCompile(`{^(?P<proto>https?)://packagist\.org/?$}i`)
	baseURLRegex   = php.MustCompile(`{(?:/[^/\\]+\.json)?(?:[?#].*)?$}`)
	cacheDirRegex  = php.MustCompile(`{[^a-z0-9.]}i`)
)

// New ports new ComposerRepository($repoConfig, $io, $config,
// $httpDownloader, $eventDispatcher); eventDispatcher may be nil.
func New(repoConfig *php.Array, ioi io.IO, config Config, httpDownloader HTTPDownloader, eventDispatcher EventDispatcher) (*ComposerRepository, error) {
	repoConfig = repoConfig.Clone()
	rawURL, ok := repoConfig.Get("url")
	if !ok {
		return nil, &util.ErrorException{Site: phperr.At("ComposerRepository.php", 164), Message: `Undefined array key "url"`}
	}
	url, ok := rawURL.(string)
	if !ok {
		return nil, &pkg.TypeError{Message: "Composer\\Pcre\\Preg::isMatch(): Argument #2 ($subject) must be of type string, " + php.TypeName(rawURL) + " given"}
	}
	hasScheme, err := schemeRegex.IsMatch(url)
	if err != nil {
		return nil, err
	}
	if !hasScheme {
		if localFilePath, ok := util.RealpathOK(url); ok {
			// it is a local path, add file scheme
			url = "file://" + localFilePath
		} else {
			// otherwise, assume http as the default protocol
			url = "http://" + url
		}
	}
	url = php.RtrimSet(url, "/")
	if url == "" {
		return nil, &util.InvalidArgumentError{Site: phperr.At("ComposerRepository.php", 175), Message: "The repository url must not be an empty string"}
	}

	if strings.HasPrefix(url, "https?") {
		url = "https" + url[6:]
	}
	repoConfig.Set("url", url)

	if urlBits, ok := util.ParseURL(php.Strtr(url, `\`, "/")); !ok || urlBits.Scheme == "" {
		return nil, &util.UnexpectedValueError{Site: phperr.At("ComposerRepository.php", 184), Message: "Invalid url given for Composer repository: " + util.SanitizeURL(url)}
	}

	if v, _ := repoConfig.Get("options"); v == nil {
		repoConfig.Set("options", php.NewArray())
	}
	r := &ComposerRepository{
		io:                    ioi,
		httpDownloader:        httpDownloader,
		eventDispatcher:       eventDispatcher,
		freshMetadataUrls:     map[string]struct{}{},
		packagesNotFoundCache: map[string]struct{}{},
	}
	if v, _ := repoConfig.Get("allow_ssl_downgrade"); v == true {
		r.allowSslDowngrade = true
	}

	options, _ := repoConfig.Get("options")
	r.options, ok = options.(*php.Array)
	if !ok {
		return nil, &pkg.TypeError{Message: "Composer\\Repository\\ComposerRepository::__construct(): $repoConfig['options'] must be of type array, " + php.TypeName(options) + " given"}
	}
	r.url = url

	// force url for packagist.org to repo.packagist.org
	if m, err := packagistRegex.Match(r.url); err != nil {
		return nil, err
	} else if m != nil {
		r.url = m.Get(1) + "://repo.packagist.org"
	}

	baseURL, _, err := baseURLRegex.Replace(r.url, "", -1)
	if err != nil {
		return nil, err
	}
	r.baseURL = php.RtrimSet(baseURL, "/")

	cacheRepoDir, err := config.Get("cache-repo-dir", 0)
	if err != nil {
		return nil, err
	}
	safeURL, err := util.SanitizeURLChecked(r.url)
	if err != nil {
		return nil, err
	}
	cacheName, _, err := cacheDirRegex.Replace(safeURL, "-", -1)
	if err != nil {
		return nil, err
	}
	r.cache, err = cache.New(ioi, php.ToString(cacheRepoDir)+"/"+cacheName, "a-z0-9.$~_", nil, false)
	if err != nil {
		return nil, err
	}
	readOnly, err := config.Get("cache-read-only", 0)
	if err != nil {
		return nil, err
	}
	r.cache.SetReadOnly(php.ToBool(readOnly))
	r.versionParser = pkg.NewVersionParser()
	r.loader = loader.NewArrayLoader(r.versionParser, false)
	r.repoConfig = repoConfig
	if async, ok := httpDownloader.(interface{ EnableAsync() }); ok {
		async.EnableAsync()
	}
	filter, _ := repoConfig.Get("filter")
	if err := r.parseUserFilterConfig(filter); err != nil {
		return nil, err
	}

	r.Extend(r, r.initialize)

	return r, nil
}

// parseUserFilterConfig ports parseUserFilterConfig: the `filter` value
// of the repository config.
func (r *ComposerRepository) parseUserFilterConfig(rawFilter any) error {
	switch rawFilter {
	case false:
		r.userFilterDisabled = true

		return nil
	case nil, true:
		return nil
	}

	filter, ok := rawFilter.(*php.Array)
	if !ok {
		return &util.UnexpectedValueError{Site: phperr.At("ComposerRepository.php", 232), Message: `Repository "filter" must be a boolean or an object mapping advertised list names to false.`}
	}

	for k, value := range filter.All() {
		if !k.IsString() || k.String() == "" {
			return &util.UnexpectedValueError{Site: phperr.At("ComposerRepository.php", 238), Message: `Repository "filter" keys must be non-empty list-name strings.`}
		}
		if value == true {
			continue
		}

		if value != false {
			return &util.UnexpectedValueError{Site: phperr.At("ComposerRepository.php", 245), Message: `Repository "filter" entry for "` + k.String() + `" must be a boolean; got ` + php.VarExport(value) + "."}
		}

		if r.userFilterSkipped == nil {
			r.userFilterSkipped = map[string]struct{}{}
		}
		r.userFilterSkipped[k.String()] = struct{}{}
	}

	return nil
}

// RepoName ports getRepoName.
func (r *ComposerRepository) RepoName() string {
	return "composer repo (" + util.SanitizeURL(r.url) + ")"
}

// Class returns the PHP class name.
func (r *ComposerRepository) Class() string { return Class }

// RepoConfig ports getRepoConfig.
func (r *ComposerRepository) RepoConfig() *php.Array { return r.repoConfig }

// FindPackage ports findPackage.
func (r *ComposerRepository) FindPackage(name string, constraint semver.ConstraintInterface) (pkg.PackageInterface, error) {
	// this call initializes loadRootServerFile which is needed for the rest below to work
	hasProviders, err := r.hasProvidersCheck()
	if err != nil {
		return nil, err
	}

	name = php.Strtolower(name)

	if r.lazyProvidersURL != "" {
		hasPartial, err := r.hasPartialPackagesCheck()
		if err != nil {
			return nil, err
		}
		if hasPartial && r.partialPackagesByName.Has(name) {
			packages, err := r.whatProvides(name, nil, nil, nil)
			if err != nil {
				return nil, err
			}

			return firstMatch(nameMapValues(packages), constraint), nil
		}

		if r.hasAvailablePackageList {
			if contains, err := r.lazyProvidersRepoContains(name); err != nil || !contains {
				return nil, err
			}
		}

		result, err := r.loadAsyncPackages(repository.NewConstraintMap(name, constraint), nil, nil, nil)
		if err != nil || len(result.Packages) == 0 {
			return nil, err
		}

		return result.Packages[0], nil
	}

	if hasProviders {
		names, err := r.providerNames()
		if err != nil {
			return nil, err
		}
		for _, providerName := range names {
			if name == providerName {
				packages, err := r.whatProvides(providerName, nil, nil, nil)
				if err != nil {
					return nil, err
				}

				return firstMatch(nameMapValues(packages), constraint), nil
			}
		}

		return nil, nil
	}

	return r.ArrayRepository.FindPackage(name, constraint)
}

// FindPackages ports findPackages.
func (r *ComposerRepository) FindPackages(name string, constraint semver.ConstraintInterface) ([]pkg.PackageInterface, error) {
	// this call initializes loadRootServerFile which is needed for the rest below to work
	hasProviders, err := r.hasProvidersCheck()
	if err != nil {
		return nil, err
	}

	name = php.Strtolower(name)

	if r.lazyProvidersURL != "" {
		hasPartial, err := r.hasPartialPackagesCheck()
		if err != nil {
			return nil, err
		}
		if hasPartial && r.partialPackagesByName.Has(name) {
			packages, err := r.whatProvides(name, nil, nil, nil)
			if err != nil {
				return nil, err
			}

			return filterPackages(nameMapValues(packages), constraint), nil
		}

		if r.hasAvailablePackageList {
			if contains, err := r.lazyProvidersRepoContains(name); err != nil || !contains {
				return nil, err
			}
		}

		result, err := r.loadAsyncPackages(repository.NewConstraintMap(name, constraint), nil, nil, nil)

		return result.Packages, err
	}

	if hasProviders {
		names, err := r.providerNames()
		if err != nil {
			return nil, err
		}
		for _, providerName := range names {
			if name == providerName {
				packages, err := r.whatProvides(providerName, nil, nil, nil)
				if err != nil {
					return nil, err
				}

				return filterPackages(nameMapValues(packages), constraint), nil
			}
		}

		return nil, nil
	}

	return r.ArrayRepository.FindPackages(name, constraint)
}

// filterPackages ports filterPackages without $returnFirstMatch: the
// packages whose version matches constraint (nil: all of them).
func filterPackages(packages []pkg.PackageInterface, constraint semver.ConstraintInterface) []pkg.PackageInterface {
	if constraint == nil {
		return packages
	}

	var filteredPackages []pkg.PackageInterface
	for _, p := range packages {
		if constraint.Matches(semver.NewConstraintOp(semver.OpEQ, p.Version())) {
			filteredPackages = append(filteredPackages, p)
		}
	}

	return filteredPackages
}

// firstMatch ports filterPackages with $returnFirstMatch.
func firstMatch(packages []pkg.PackageInterface, constraint semver.ConstraintInterface) pkg.PackageInterface {
	for _, p := range packages {
		if constraint == nil || constraint.Matches(semver.NewConstraintOp(semver.OpEQ, p.Version())) {
			return p
		}
	}

	return nil
}

// Packages ports getPackages.
func (r *ComposerRepository) Packages() ([]pkg.PackageInterface, error) {
	hasProviders, err := r.hasProvidersCheck()
	if err != nil {
		return nil, err
	}

	if r.lazyProvidersURL != "" {
		if r.availablePackages != nil && r.availablePackagePatterns == nil {
			packageMap := &repository.ConstraintMap{}
			for _, name := range r.availablePackages {
				packageMap.Set(name, semver.NewMatchAllConstraint())
			}

			result, err := r.loadAsyncPackages(packageMap, nil, nil, nil)

			return result.Packages, err
		}

		hasPartial, err := r.hasPartialPackagesCheck()
		if err != nil {
			return nil, err
		}
		if hasPartial {
			if r.partialPackagesByName == nil {
				return nil, &util.LogicError{Site: phperr.At("ComposerRepository.php", 402), Message: "hasPartialPackages failed to initialize $this->partialPackagesByName"}
			}

			// Composer passes the version lists keyed by name as if each
			// was one package's data (which the loader rejects for lack of
			// a name)
			var data []*php.Array
			for _, list := range r.partialPackagesByName.All() {
				versions := php.NewArrayCap(len(list))
				for _, v := range list {
					versions.Append(v)
				}
				data = append(data, versions)
			}

			return r.createPackages(data, "packages.json inline packages")
		}

		return nil, &util.LogicError{Site: phperr.At("ComposerRepository.php", 408), Message: "Composer repositories that have lazy providers and no available-packages list can not load the complete list of packages, use getPackageNames instead."}
	}

	if hasProviders {
		return nil, &util.LogicError{Site: phperr.At("ComposerRepository.php", 412), Message: "Composer repositories that have providers can not load the complete list of packages, use getPackageNames instead."}
	}

	return r.ArrayRepository.Packages()
}

// HasPackage ports ArrayRepository::hasPackage, which lists the packages
// through ComposerRepository::getPackages.
func (r *ComposerRepository) HasPackage(p pkg.PackageInterface) (bool, error) {
	if _, err := r.Packages(); err != nil {
		return false, err
	}

	return r.ArrayRepository.HasPackage(p)
}

// RemovePackage ports ArrayRepository::removePackage, which lists the
// packages through ComposerRepository::getPackages.
func (r *ComposerRepository) RemovePackage(p pkg.PackageInterface) error {
	if _, err := r.Packages(); err != nil {
		return err
	}

	return r.ArrayRepository.RemovePackage(p)
}

// PackageNames ports getPackageNames: the names of the packages this
// repository has, matching packageFilter ("" for null; "*" is a
// wildcard) when given.
func (r *ComposerRepository) PackageNames(packageFilter string) ([]string, error) {
	hasProviders, err := r.hasProvidersCheck()
	if err != nil {
		return nil, err
	}

	filterResults := func(results []string) ([]string, error) { return results, nil }
	if packageFilter != "" {
		packageFilterRegex := pkg.PackageNameToRegexp(packageFilter, "{^%s$}i")
		filterResults = func(results []string) ([]string, error) {
			return pregGrep(packageFilterRegex, results)
		}
	}

	if r.lazyProvidersURL != "" {
		if r.availablePackages != nil {
			return filterResults(r.availablePackages)
		}

		if r.listURL != "" {
			// no need to call $filterResults here as the $packageFilter is applied in the function itself
			return r.loadPackageList(packageFilter)
		}

		hasPartial, err := r.hasPartialPackagesCheck()
		if err != nil {
			return nil, err
		}
		if hasPartial && r.partialPackagesByName != nil {
			return filterResults(r.partialPackagesByName.Keys())
		}

		return nil, nil
	}

	if hasProviders {
		names, err := r.providerNames()
		if err != nil {
			return nil, err
		}

		return filterResults(names)
	}

	packages, err := r.Packages()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(packages))
	for i, p := range packages {
		names[i] = p.PrettyName()
	}

	return filterResults(names)
}

// pregGrep is Preg::grep($pattern, $list) for a list of strings.
func pregGrep(pattern string, list []string) ([]string, error) {
	matched, err := php.PregGrep(pattern, php.StringList(list), 0)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, matched.Len())
	for _, v := range matched.All() {
		out = append(out, php.ToString(v))
	}

	return out, nil
}

// vendorNames ports getVendorNames.
func (r *ComposerRepository) vendorNames() ([]string, error) {
	const cacheKey = "vendor-list.txt"
	if cached, ok, err := r.freshCachedList(cacheKey); err != nil || ok {
		return cached, err
	}

	names, err := r.PackageNames("")
	if err != nil {
		return nil, err
	}

	var vendors []string
	seen := map[string]struct{}{}
	for _, name := range names {
		vendor, _, _ := strings.Cut(name, "/")
		if _, ok := seen[vendor]; !ok {
			seen[vendor] = struct{}{}
			vendors = append(vendors, vendor)
		}
	}

	if !r.cache.IsReadOnly() {
		if _, err := r.cache.Write(cacheKey, strings.Join(vendors, "\n")); err != nil {
			return nil, err
		}
	}

	return vendors, nil
}

// freshCachedList reads a newline separated list cached less than 10
// minutes ago.
func (r *ComposerRepository) freshCachedList(cacheKey string) ([]string, bool, error) {
	cacheAge, ok := r.cache.Age(cacheKey)
	if !ok || cacheAge >= 600 {
		return nil, false, nil
	}
	cachedData, ok, err := r.cache.Read(cacheKey)
	if err != nil || !ok {
		return nil, false, err
	}

	return strings.Split(cachedData, "\n"), true, nil
}

// loadPackageList ports loadPackageList; packageFilter "" is null.
func (r *ComposerRepository) loadPackageList(packageFilter string) ([]string, error) {
	if r.listURL == "" {
		return nil, &util.LogicError{Site: phperr.At("ComposerRepository.php", 514), Message: "Make sure to call loadRootServerFile before loadPackageList"}
	}

	url := r.listURL
	if packageFilter != "" {
		url += "?filter=" + php.Urlencode(packageFilter)

		return r.getPackageNamesList(url)
	}

	const cacheKey = "package-list.txt"
	if cached, ok, err := r.freshCachedList(cacheKey); err != nil || ok {
		return cached, err
	}

	names, err := r.getPackageNamesList(url)
	if err != nil {
		return nil, err
	}

	if !r.cache.IsReadOnly() {
		if _, err := r.cache.Write(cacheKey, strings.Join(names, "\n")); err != nil {
			return nil, err
		}
	}

	return names, nil
}

// getPackageNamesList GETs a list API URL and returns its packageNames.
func (r *ComposerRepository) getPackageNamesList(url string) ([]string, error) {
	result, err := r.getJSON(url, r.options)
	if err != nil {
		return nil, err
	}

	return stringList(result, "packageNames"), nil
}

// getJSON is $this->httpDownloader->get($url, $options)->decodeJson()
// followed by HttpDownloader::outputWarnings.
func (r *ComposerRepository) getJSON(url string, options *php.Array) (*php.Array, error) {
	response, err := r.httpDownloader.Get(url, options)
	if err != nil {
		return nil, err
	}
	data, err := response.DecodeJSONArray()
	if err != nil {
		return nil, err
	}
	if _, err := http.OutputWarnings(r.io, r.url, data); err != nil {
		return nil, err
	}

	return asArray(data), nil
}

// stringList returns the values of the list $data[$key] as strings.
func stringList(data *php.Array, key string) []string {
	list := asArray(get(data, key))
	out := make([]string, 0, list.Len())
	for _, v := range list.All() {
		out = append(out, php.ToString(v))
	}

	return out
}

// LoadPackages ports loadPackages.
func (r *ComposerRepository) LoadPackages(packageNameMap *repository.ConstraintMap, acceptableStabilities, stabilityFlags *php.Array, alreadyLoaded repository.AlreadyLoaded) (repository.LoadResult, error) {
	// this call initializes loadRootServerFile which is needed for the rest below to work
	hasProviders, err := r.hasProvidersCheck()
	if err != nil {
		return repository.LoadResult{}, phperr.Call(err, `Composer\Repository\ComposerRepository->hasProviders`, "ComposerRepository.php", 549)
	}
	hasPartial, err := r.hasPartialPackagesCheck()
	if err != nil {
		return repository.LoadResult{}, err
	}

	if !hasProviders && !hasPartial && r.lazyProvidersURL == "" {
		return r.ArrayRepository.LoadPackages(packageNameMap, acceptableStabilities, stabilityFlags, alreadyLoaded)
	}

	var packages []pkg.PackageInterface
	namesFound := &repository.NameMap[struct{}]{}
	packageNameMap = packageNameMap.Clone()

	if hasProviders || hasPartial {
		for name, constraint := range packageNameMap.Clone().All() {
			// if a repo has no providers but only partial packages and the partial packages are missing
			// then we don't want to call whatProvides as it would try to load from the providers and fail
			if !hasProviders && !r.partialPackagesByName.Has(name) {
				continue
			}

			candidates, err := r.whatProvides(name, acceptableStabilities, stabilityFlags, alreadyLoaded)
			if err != nil {
				return repository.LoadResult{}, err
			}
			matches := newPackageSet()
			for _, candidate := range nameMapValues(candidates) {
				if candidate.Name() != name {
					return repository.LoadResult{}, &util.LogicError{Site: phperr.At("ComposerRepository.php", 571), Message: "whatProvides should never return a package with a different name than the requested one"}
				}
				namesFound.Set(name, struct{}{})

				if constraint == nil || constraint.Matches(semver.NewConstraintOp(semver.OpEQ, candidate.Version())) {
					matches.add(candidate)
					if alias, ok := candidate.(pkg.Alias); ok {
						matches.add(alias.AliasOf())
					}
				}
			}

			// add aliases of matched packages even if they did not match the constraint
			for _, candidate := range nameMapValues(candidates) {
				if alias, ok := candidate.(pkg.Alias); ok && matches.has(alias.AliasOf()) {
					matches.add(candidate)
				}
			}
			packages = append(packages, matches.list...)

			packageNameMap.Delete(name)
		}
	}

	if r.lazyProvidersURL != "" && packageNameMap.Len() > 0 {
		if r.hasAvailablePackageList {
			for name := range packageNameMap.Clone().All() {
				contains, err := r.lazyProvidersRepoContains(php.Strtolower(name))
				if err != nil {
					return repository.LoadResult{}, err
				}
				if !contains {
					packageNameMap.Delete(name)
				}
			}
		}

		result, err := r.loadAsyncPackages(packageNameMap, acceptableStabilities, stabilityFlags, alreadyLoaded)
		if err != nil {
			return repository.LoadResult{}, err
		}
		packages = append(packages, result.Packages...)
		for _, name := range result.NamesFound {
			namesFound.Set(name, struct{}{})
		}
	}

	return repository.LoadResult{NamesFound: namesFound.Keys(), Packages: packages}, nil
}

// packageSet is an array of packages keyed by spl_object_id: insertion
// ordered, without duplicates.
type packageSet struct {
	list []pkg.PackageInterface
	seen map[pkg.PackageInterface]struct{}
}

func newPackageSet() *packageSet {
	return &packageSet{seen: map[pkg.PackageInterface]struct{}{}}
}

func (s *packageSet) has(p pkg.PackageInterface) bool {
	_, ok := s.seen[p]

	return ok
}

func (s *packageSet) add(p pkg.PackageInterface) {
	if !s.has(p) {
		s.seen[p] = struct{}{}
		s.list = append(s.list, p)
	}
}

var (
	whitespaceRegex  = `{\s+}`
	vendorQueryRegex = php.MustCompile(`{^\^(?P<query>(?P<vendor>[a-z0-9_.-]+)/[a-z0-9_.-]*)\*?$}i`)
)

// Search ports search; typ "" is null.
func (r *ComposerRepository) Search(query string, mode int, typ string) ([]repository.SearchResult, error) {
	if _, _, err := r.loadRootServerFile(600); err != nil {
		return nil, err
	}

	if r.searchURL != "" && mode == repository.SearchFulltext {
		url := strings.ReplaceAll(r.searchURL, "%query%", php.Urlencode(query))
		url = strings.ReplaceAll(url, "%type%", typ)

		search, err := r.getJSON(url, r.options)
		if err != nil {
			return nil, err
		}

		resultList, _ := search.GetArray("results")
		if resultList == nil || resultList.Len() == 0 {
			return nil, nil
		}

		var results []repository.SearchResult
		for _, raw := range resultList.All() {
			result := asArray(raw)
			// do not show virtual packages in results as they are not directly useful from a composer perspective
			if v, _ := result.Get("virtual"); php.ToBool(v) {
				continue
			}

			results = append(results, searchResult(result))
		}

		return results, nil
	}

	if mode == repository.SearchVendor {
		regex, err := searchRegex(query)
		if err != nil {
			return nil, err
		}

		vendorNames, err := r.vendorNames()
		if err != nil {
			return nil, err
		}

		return namesResults(regex, vendorNames)
	}

	hasProviders, err := r.hasProvidersCheck()
	if err != nil {
		return nil, err
	}
	if hasProviders || r.lazyProvidersURL != "" {
		// optimize search for "^foo/bar" where at least "^foo/" is present by loading this directly from the listUrl if present
		if m, err := vendorQueryRegex.MatchStrictGroups(query); err != nil {
			return nil, err
		} else if m != nil && r.listURL != "" {
			vendor, _ := m.Named("vendor")
			q, _ := m.Named("query")
			url := r.listURL + "?vendor=" + php.Urlencode(vendor) + "&filter=" + php.Urlencode(q+"*")
			names, err := r.getPackageNamesList(url)
			if err != nil {
				return nil, err
			}

			results := make([]repository.SearchResult, len(names))
			for i, name := range names {
				results[i] = repository.SearchResult{Name: name, Description: pkg.Str("")}
			}

			return results, nil
		}

		regex, err := searchRegex(query)
		if err != nil {
			return nil, err
		}

		packageNames, err := r.PackageNames("")
		if err != nil {
			return nil, err
		}

		return namesResults(regex, packageNames)
	}

	return r.ArrayRepository.Search(query, mode, "")
}

// searchRegex is '{(?:'.implode('|', Preg::split('{\s+}', $query)).')}i'.
func searchRegex(query string) (string, error) {
	parts, err := php.PregSplit(whitespaceRegex, query, -1, 0)
	if err != nil {
		return "", err
	}

	return "{(?:" + strings.Join(parts, "|") + ")}i", nil
}

// namesResults is the ['name' => $name, 'description' => ”] results of
// the names matching regex.
func namesResults(regex string, names []string) ([]repository.SearchResult, error) {
	matched, err := pregGrep(regex, names)
	if err != nil {
		return nil, err
	}

	var results []repository.SearchResult
	for _, name := range matched {
		results = append(results, repository.SearchResult{Name: name, Description: pkg.Str("")})
	}

	return results, nil
}

// searchResult is a result of the search API.
func searchResult(result *php.Array) repository.SearchResult {
	name, _ := result.Get("name")
	out := repository.SearchResult{Name: php.ToString(name), Raw: result}
	if v, ok := result.Get("description"); ok && v != nil {
		out.Description = pkg.Str(php.ToString(v))
	}
	if v, ok := result.Get("abandoned"); ok {
		out.Abandoned = v
	}
	if v, ok := result.Get("url"); ok && v != nil {
		out.URL = pkg.Str(php.ToString(v))
	}

	return out
}

// Providers ports getProviders.
func (r *ComposerRepository) Providers(packageName string) ([]repository.ProviderInfo, error) {
	if _, _, err := r.loadRootServerFile(noMaxAge); err != nil {
		return nil, err
	}
	result := &repository.NameMap[repository.ProviderInfo]{}

	if r.providersAPIURL != "" {
		apiResult, err := r.getJSON(strings.ReplaceAll(r.providersAPIURL, "%package%", packageName), r.options)
		if err != nil {
			if statusCode(err) == 404 {
				return nil, nil
			}

			return nil, err
		}

		for _, raw := range asArray(get(apiResult, "providers")).All() {
			provider, _ := raw.(*php.Array)
			info := providerInfo(provider, nil)
			result.Set(info.Name, info)
		}

		return providerValues(result), nil
	}

	hasPartial, err := r.hasPartialPackagesCheck()
	if err != nil {
		return nil, err
	}
	if hasPartial {
		if r.partialPackagesByName == nil {
			return nil, &util.LogicError{Site: phperr.At("ComposerRepository.php", 1033), Message: "hasPartialPackages failed to initialize $this->partialPackagesByName"}
		}
		for _, versions := range r.partialPackagesByName.All() {
			for _, candidate := range versions {
				name := php.ToString(get(candidate, "name"))
				provide, _ := candidate.GetArray("provide")
				if result.Has(name) || get(provide, packageName) == nil {
					continue
				}
				result.Set(name, providerInfo(candidate, ""))
			}
		}
	}

	if r.initialized {
		if n, _ := r.Count(); n > 0 {
			parent, err := r.ArrayRepository.Providers(packageName)
			if err != nil {
				return nil, err
			}
			for _, info := range parent {
				result.Set(info.Name, info)
			}
		}
	}

	return providerValues(result), nil
}

// providerInfo reads a provider's name, description and type; the
// missing ones get def (nil keeps null, "" is PHP's ?? ”).
func providerInfo(data *php.Array, def any) repository.ProviderInfo {
	info := repository.ProviderInfo{Name: php.ToString(get(data, "name"))}
	description := get(data, "description")
	if description == nil {
		description = def
	}
	if description != nil {
		info.Description = pkg.Str(php.ToString(description))
	}
	typ := get(data, "type")
	if typ == nil {
		typ = def
	}
	info.Type = php.ToString(typ)

	return info
}

func providerValues(m *repository.NameMap[repository.ProviderInfo]) []repository.ProviderInfo {
	var out []repository.ProviderInfo
	for _, v := range m.All() {
		out = append(out, v)
	}

	return out
}

// get is $a[$key] ?? null.
func get(a *php.Array, key string) any {
	if a == nil {
		return nil
	}
	v, _ := a.Get(key)

	return v
}

// configurePackageTransportOptions ports configurePackageTransportOptions:
// packages downloaded from this repository get its transport options.
func (r *ComposerRepository) configurePackageTransportOptions(p pkg.PackageInterface) {
	for _, url := range p.DistURLs() {
		if strings.HasPrefix(url, r.baseURL) {
			p.SetTransportOptions(r.options)

			return
		}
	}
}

// AddPackage ports addPackage.
func (r *ComposerRepository) AddPackage(p pkg.PackageInterface) error {
	if err := r.ArrayRepository.AddPackage(p); err != nil {
		return err
	}
	// an alias added its aliased package too; configuring the alias
	// configures that package, as both use the aliased package's options
	r.configurePackageTransportOptions(p)

	return nil
}

// initialize ports initialize.
func (r *ComposerRepository) initialize() error {
	r.InitializeBase()
	r.initialized = true

	repoData, err := r.loadDataFromServer()
	if err != nil {
		return err
	}

	packages, err := r.createPackages(repoData, "root file ("+util.SanitizeURL(r.packagesJSONURL())+")")
	if err != nil {
		return err
	}
	for _, p := range packages {
		if err := r.AddPackage(p); err != nil {
			return err
		}
	}

	return nil
}
