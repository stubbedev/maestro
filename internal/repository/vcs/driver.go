// Ports src/Composer/Repository/Vcs/VcsDriverInterface.php and
// src/Composer/Repository/Vcs/VcsDriver.php.

package vcs

import (
	"time"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	uvcs "github.com/stubbedev/maestro/internal/util/vcs"
)

// Driver ports Composer\Repository\Vcs\VcsDriverInterface.
//
// Branches and tags are *php.Array maps of names (int keys for numeric
// names, as PHP coerces them) to identifiers. Arrays the driver returns
// may be cached by it: clone them before modifying.
type Driver interface {
	// Initialize is initialize(): clone, fetch the repository data, ...
	Initialize() error
	// ComposerInformation is getComposerInformation(): the composer.json
	// of identifier, nil when there is none.
	ComposerInformation(identifier string) (*php.Array, error)
	// FileContent is getFileContent(); ok false is null (no such file).
	FileContent(file, identifier string) (content string, ok bool, err error)
	// ChangeDate is getChangeDate(); ok false is null.
	ChangeDate(identifier string) (date time.Time, ok bool, err error)
	// RootIdentifier is getRootIdentifier(): trunk, master, default, ...
	RootIdentifier() (string, error)
	// Branches is getBranches().
	Branches() (*php.Array, error)
	// Tags is getTags().
	Tags() (*php.Array, error)
	// Dist is getDist(): type, url, reference and shasum, nil for null.
	Dist(identifier string) *php.Array
	// Source is getSource(): type, url and reference.
	Source(identifier string) *php.Array
	// URL is getUrl().
	URL() string
	// HasComposerFile is hasComposerFile().
	HasComposerFile(identifier string) (bool, error)
	// Cleanup is cleanup().
	Cleanup() error
	// Class is the PHP class name (get_class()).
	Class() string
}

// Deps are the collaborators of a driver, the constructor arguments of
// VcsDriver after the repository config ($io, $config, $httpDownloader,
// $process).
type Deps struct {
	IO     io.IO
	Config http.Config
	// HTTPDownloader may be nil for drivers that do not use the API
	// (git, hg, svn, fossil, perforce).
	HTTPDownloader http.Getter
	// Process runs the VCS binaries, also for the static supports()
	// checks, where Composer creates a ProcessExecutor of its own.
	Process uvcs.Process
}

// DriverType is a driver class: its constructor (`new $class(...)`) and
// static supports().
type DriverType struct {
	// Class is the PHP class name.
	Class string
	// New is new $class($repoConfig, $io, $config, $httpDownloader,
	// $process); it does not initialize the driver.
	New func(repoConfig *php.Array, deps Deps) Driver
	// Supports is $class::supports($io, $config, $url, $deep).
	Supports func(deps Deps, url string, deep bool) (bool, error)
}

// NamedDriver is an entry of VcsRepository's $drivers: a repository type
// and its driver.
type NamedDriver struct {
	Name string
	Type DriverType
}

// The driver classes (DefaultDrivers lists them).
var (
	gitHubDriverType       = DriverType{Class: `Composer\Repository\Vcs\GitHubDriver`, New: newGitHubDriver, Supports: gitHubSupports}
	gitLabDriverType       = DriverType{Class: `Composer\Repository\Vcs\GitLabDriver`, New: newGitLabDriver, Supports: gitLabSupports}
	gitBitbucketDriverType = DriverType{Class: `Composer\Repository\Vcs\GitBitbucketDriver`, New: newGitBitbucketDriver, Supports: gitBitbucketSupports}
	forgejoDriverType      = DriverType{Class: `Composer\Repository\Vcs\ForgejoDriver`, New: newForgejoDriver, Supports: forgejoSupports}
	gitDriverType          = DriverType{Class: `Composer\Repository\Vcs\GitDriver`, New: newGitDriver, Supports: gitSupports}
	hgDriverType           = DriverType{Class: `Composer\Repository\Vcs\HgDriver`, New: newHgDriver, Supports: hgSupports}
	perforceDriverType     = DriverType{Class: `Composer\Repository\Vcs\PerforceDriver`, New: newPerforceDriver, Supports: perforceSupports}
	fossilDriverType       = DriverType{Class: `Composer\Repository\Vcs\FossilDriver`, New: newFossilDriver, Supports: fossilSupports}
	svnDriverType          = DriverType{Class: `Composer\Repository\Vcs\SvnDriver`, New: newSvnDriver, Supports: svnSupports}
)

// DefaultDrivers returns VcsRepository's default $drivers, in the order
// supports() is tried.
func DefaultDrivers() []NamedDriver {
	return []NamedDriver{
		{"github", gitHubDriverType},
		{"gitlab", gitLabDriverType},
		{"bitbucket", gitBitbucketDriverType},
		{"git-bitbucket", gitBitbucketDriverType},
		{"forgejo", forgejoDriverType},
		{"git", gitDriverType},
		{"hg", hgDriverType},
		{"perforce", perforceDriverType},
		{"fossil", fossilDriverType},
		// svn must be last because identifying a subversion server for sure is practically impossible
		{"svn", svnDriverType},
	}
}

// driverHooks are the methods of VcsDriver its subclasses override and
// the base class calls through $this.
type driverHooks interface {
	ComposerInformation(identifier string) (*php.Array, error)
	FileContent(file, identifier string) (string, bool, error)
	ChangeDate(identifier string) (time.Time, bool, error)
	shouldCache(identifier string) bool
}

// vcsDriver ports Composer\Repository\Vcs\VcsDriver, embedded by every
// driver.
type vcsDriver struct {
	self           driverHooks
	url            string
	originURL      string
	repoConfig     *php.Array
	io             io.IO
	config         http.Config
	process        uvcs.Process
	httpDownloader http.Getter
	// infoCache holds the composer.json data by identifier; a nil value
	// is not cached (isset() is false for null).
	infoCache map[string]*php.Array
	cache     *cache.Cache
}

// init ports VcsDriver::__construct; self receives the overridable calls.
func (d *vcsDriver) init(self driverHooks, repoConfig *php.Array, deps Deps) {
	repoConfig = repoConfig.Clone()

	url, _ := repoConfig.GetString("url")
	if util.IsLocalPath(url) {
		url = util.GetPlatformPath(url)
		repoConfig.Set("url", url)
	}

	d.self = self
	d.url = url
	d.originURL = url
	d.repoConfig = repoConfig
	d.io = deps.IO
	d.config = deps.Config
	d.httpDownloader = deps.HTTPDownloader
	d.process = deps.Process
	d.infoCache = map[string]*php.Array{}
}

var sha1Identifier = php.MustCompile(`{^[a-f0-9]{40}$}iD`)

// shouldCache is shouldCache(): whether composer.json of identifier is
// cached.
func (d *vcsDriver) shouldCache(identifier string) bool {
	// Anchored and fixed-length: Preg::isMatch cannot throw.
	isSha, _ := sha1Identifier.IsMatch(identifier)

	return d.cache != nil && isSha
}

// ComposerInformation ports VcsDriver::getComposerInformation.
func (d *vcsDriver) ComposerInformation(identifier string) (*php.Array, error) {
	if composer, ok := d.infoCache[identifier]; ok {
		return composer, nil
	}

	composer, err := d.cachedBaseComposerInformation(identifier, true)
	if err != nil {
		return nil, err
	}

	d.storeInfo(identifier, composer)

	return composer, nil
}

// storeInfo is $this->infoCache[$identifier] = $composer.
func (d *vcsDriver) storeInfo(identifier string, composer *php.Array) {
	if composer != nil {
		d.infoCache[identifier] = composer
	}
}

// cachedBaseComposerInformation is the cache lookup of
// getComposerInformation: the cached composer.json of identifier, else
// getBaseComposerInformation's (written to the cache). truthy is
// `$res = $this->cache->read(...)`; Forgejo tests `false !== $res`.
func (d *vcsDriver) cachedBaseComposerInformation(identifier string, truthy bool) (*php.Array, error) {
	shouldCache := d.self.shouldCache(identifier)

	if shouldCache {
		res, ok, err := d.cache.Read(identifier)
		if err != nil {
			return nil, err
		}

		if ok && (!truthy || php.ToBool(res)) {
			return parseJSONArray(res, "")
		}
	}

	composer, err := d.baseComposerInformation(identifier)
	if err != nil {
		return nil, err
	}

	if shouldCache {
		if err := d.writeCache(identifier, composer); err != nil {
			return nil, err
		}
	}

	return composer, nil
}

// writeCache is $this->cache->write($file, JsonFile::encode($composer,
// JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES)).
func (d *vcsDriver) writeCache(file string, composer *php.Array) error {
	var data any
	if composer != nil {
		data = composer
	}

	encoded, err := json.Encode(data, php.JSONUnescapedUnicode|php.JSONUnescapedSlashes, json.IndentDefault)
	if err != nil {
		return err
	}

	_, err = d.cache.Write(file, encoded)

	return err
}

// parseJSONArray is JsonFile::parseJson for data that must be an array:
// nil for anything else.
func parseJSONArray(content, file string) (*php.Array, error) {
	data, err := json.ParseJSON(content, file)
	if err != nil {
		return nil, err
	}

	a, _ := data.(*php.Array)

	return a, nil
}

// baseComposerInformation ports getBaseComposerInformation.
func (d *vcsDriver) baseComposerInformation(identifier string) (*php.Array, error) {
	composerFileContent, ok, err := d.self.FileContent("composer.json", identifier)
	if err != nil {
		return nil, err
	}

	if !ok || !php.ToBool(composerFileContent) {
		return nil, nil
	}

	composer, err := parseJSONArray(composerFileContent, identifier+":composer.json")
	if err != nil || composer == nil || composer.Len() == 0 {
		return nil, err
	}

	if v, _ := composer.Get("time"); !php.ToBool(v) {
		changeDate, ok, err := d.self.ChangeDate(identifier)
		if err != nil {
			return nil, err
		}

		if ok {
			composer.Set("time", changeDate.Format(dateRFC3339))
		}
	}

	return composer, nil
}

// dateRFC3339 is PHP's DATE_RFC3339 ("Y-m-d\TH:i:sP").
const dateRFC3339 = "2006-01-02T15:04:05-07:00"

// HasComposerFile ports VcsDriver::hasComposerFile.
func (d *vcsDriver) HasComposerFile(identifier string) (bool, error) {
	composer, err := d.self.ComposerInformation(identifier)
	if err != nil {
		if isTransportError(err) {
			return false, nil
		}

		return false, err
	}

	return composer != nil, nil
}

// getContents ports VcsDriver::getContents: a GET of url with the
// repository's options.
func (d *vcsDriver) getContents(url string) (*http.Response, error) {
	options, _ := d.repoConfig.GetArray("options")
	if options == nil {
		options = php.NewArray()
	}

	return d.httpDownloader.Get(url, options)
}

// newCache is new Cache($this->io, $dir) with setReadOnly(cache-read-only).
func (d *vcsDriver) newCache(dir string) error {
	c, err := cache.New(d.io, dir, "", nil, false)
	if err != nil {
		return err
	}

	c.SetReadOnly(php.ToBool(d.config.Get("cache-read-only")))
	d.cache = c

	return nil
}

// Cleanup ports VcsDriver::cleanup.
func (d *vcsDriver) Cleanup() error { return nil }

// parseDate is new \DateTimeImmutable($date) (UTC unless the string names
// a zone).
func parseDate(date string) (time.Time, error) {
	return loader.ParseDateTime(date)
}
