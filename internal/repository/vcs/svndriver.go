// Ports src/Composer/Repository/Vcs/SvnDriver.php.

package vcs

import (
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fspath"
	uvcs "github.com/stubbedev/maestro/internal/util/vcs"
)

// svnPath is one of SvnDriver's string|false layout paths (trunk-path,
// branches-path, tags-path): disabled is false. As in PHP, a disabled
// path reads as "" where it is concatenated.
type svnPath struct {
	path     string
	disabled bool
}

// SvnDriver ports Composer\Repository\Vcs\SvnDriver: a Subversion
// repository read with `svn ls`/`svn cat`, with the trunk/branches/tags
// layout of its config.
type SvnDriver struct {
	vcsDriver
	baseURL string
	// tags and branches are nil until loaded.
	tags, branches *php.Array
	// rootIdentifier is "" for null.
	rootIdentifier   string
	trunkPath        svnPath
	branchesPath     svnPath
	tagsPath         svnPath
	packagePath      string
	cacheCredentials bool
	util             *uvcs.Svn
}

func newSvnDriver(repoConfig *php.Array, deps Deps) Driver { return NewSvnDriver(repoConfig, deps) }

// NewSvnDriver is new SvnDriver($repoConfig, $io, $config,
// $httpDownloader, $process).
func NewSvnDriver(repoConfig *php.Array, deps Deps) *SvnDriver {
	d := &SvnDriver{
		trunkPath:        svnPath{path: "trunk"},
		branchesPath:     svnPath{path: "branches"},
		tagsPath:         svnPath{path: "tags"},
		cacheCredentials: true,
	}
	d.init(d, repoConfig, deps)

	return d
}

// PHPClass returns the PHP class name.
func (d *SvnDriver) PHPClass() string { return svnDriverType.Class }

// configPath reads a layout path from the repository config (isset()).
func (d *SvnDriver) configPath(key string, p *svnPath) {
	v, _ := d.repoConfig.Get(key)
	if v == nil {
		return
	}

	if v == false {
		*p = svnPath{disabled: true}
	} else {
		*p = svnPath{path: php.ToString(v)}
	}
}

// Initialize ports SvnDriver::initialize.
func (d *SvnDriver) Initialize() error {
	d.url = php.RtrimSet(svnNormalizeURL(d.url), "/")
	d.baseURL = d.url

	uvcs.SvnCleanEnv()

	d.configPath("trunk-path", &d.trunkPath)
	d.configPath("branches-path", &d.branchesPath)
	d.configPath("tags-path", &d.tagsPath)

	if v, ok := d.repoConfig.Get("svn-cache-credentials"); ok {
		d.cacheCredentials = php.ToBool(v)
	}

	if v, _ := d.repoConfig.Get("package-path"); v != nil {
		d.packagePath = "/" + php.TrimSet(php.ToString(v), "/")
	}

	if pos := strings.LastIndex(d.url, "/"+d.trunkPath.path); pos >= 0 {
		d.baseURL = d.url[:pos]
	}

	safeBaseURL, err := util.SanitizeURLChecked(d.baseURL)
	if err != nil {
		return err
	}

	if err := d.newCache(php.ToString(d.config.Get("cache-repo-dir")) + "/" + replaceInfallible(cacheNameChars, "-", safeBaseURL)); err != nil {
		return err
	}

	if _, err := d.Branches(); err != nil {
		return err
	}

	_, err = d.Tags()

	return err
}

// RootIdentifier ports SvnDriver::getRootIdentifier.
func (d *SvnDriver) RootIdentifier() (string, error) {
	if d.rootIdentifier != "" {
		return d.rootIdentifier, nil
	}

	return d.trunkPath.path, nil
}

// URL ports SvnDriver::getUrl.
func (d *SvnDriver) URL() string { return d.url }

// Source ports SvnDriver::getSource.
func (d *SvnDriver) Source(identifier string) *php.Array {
	return php.ArrayOf("type", "svn", "url", d.baseURL, "reference", identifier)
}

// Dist ports SvnDriver::getDist.
func (d *SvnDriver) Dist(string) *php.Array { return nil }

var svnRevision = php.MustCompile(`{@\d+$}`)

// shouldCache ports SvnDriver::shouldCache.
func (d *SvnDriver) shouldCache(identifier string) bool {
	// \d+ is possessive before $, so each start position does constant
	// work: Preg::isMatch cannot throw.
	hasRevision, _ := svnRevision.IsMatch(identifier)

	return d.cache != nil && hasRevision
}

// ComposerInformation ports SvnDriver::getComposerInformation.
func (d *SvnDriver) ComposerInformation(identifier string) (*php.Array, error) {
	if composer, ok := d.infoCache[identifier]; ok {
		return composer, nil
	}

	if d.shouldCache(identifier) {
		res, ok, err := d.cache.Read(identifier + ".json")
		if err != nil {
			return nil, err
		}

		if ok && php.ToBool(res) {
			// old cache files had '' stored instead of null due to af3783b5f40bae32a23e353eaf0a00c9b8ce82e2, so we make sure here that we always return null or array
			// and fix outdated invalid cache files
			if res == `""` {
				res = "null"
				if _, err := d.cache.Write(identifier+".json", res); err != nil {
					return nil, err
				}
			}

			composer, err := parseJSONArray(res, "")
			if err != nil {
				return nil, err
			}

			d.storeInfo(identifier, composer)

			return composer, nil
		}
	}

	composer, err := d.baseComposerInformation(identifier)
	if err != nil {
		transport, ok := asTransportError(err)
		if !ok {
			return nil, err
		}

		message := php.Strtolower(transport.Message)
		if !strings.Contains(message, "path not found") && !strings.Contains(message, "svn: warning: w160013") {
			return nil, err
		}

		// remember a not-existent composer.json
		composer = nil
	}

	if d.shouldCache(identifier) {
		if err := d.writeCache(identifier+".json", composer); err != nil {
			return nil, err
		}
	}

	d.storeInfo(identifier, composer)

	return composer, nil
}

var svnIdentifier = php.MustCompile(`{^(.+?)(@\d+)?/$}`)

// splitIdentifier splits an identifier into its path and "@rev" (or "").
func splitIdentifier(identifier string) (path, rev string, err error) {
	identifier = "/" + php.TrimSet(identifier, "/") + "/"

	m, err := match(svnIdentifier, identifier)
	if err != nil {
		return "", "", err
	}
	if m != nil {
		if r, ok := m.Group(2); ok {
			return m.Get(1), r, nil
		}
	}

	return identifier, "", nil
}

// FileContent ports SvnDriver::getFileContent.
func (d *SvnDriver) FileContent(file, identifier string) (string, bool, error) {
	path, rev, err := splitIdentifier(identifier)
	if err != nil {
		return "", false, err
	}

	output, err := d.execute([]string{"svn", "cat"}, d.baseURL+path+file+rev)
	if err != nil {
		if phperr.InstanceOf(err, "RuntimeException") {
			return "", false, util.NewTransportError(err.Error(), 400)
		}

		return "", false, err
	}

	if php.Trim(output) == "" {
		return "", false, nil
	}

	return output, true, nil
}

var lastChangedDate = php.MustCompile(`{^Last Changed Date: ([^(]+)}`)

// ChangeDate ports SvnDriver::getChangeDate.
func (d *SvnDriver) ChangeDate(identifier string) (time.Time, bool, error) {
	path, rev, err := splitIdentifier(identifier)
	if err != nil {
		return time.Time{}, false, err
	}

	output, err := d.execute([]string{"svn", "info"}, d.baseURL+path+rev)
	if err != nil {
		return time.Time{}, false, err
	}

	for _, line := range util.SplitLines(output) {
		if line == "" {
			continue
		}

		m, err := match(lastChangedDate, line)
		if err != nil {
			return time.Time{}, false, err
		}
		if m != nil {
			date, err := parseDate(m.Get(1))

			return date, err == nil, err
		}
	}

	return time.Time{}, false, nil
}

var svnListEntry = php.MustCompile(`{^\s*(\S+).*?(\S+)\s*$}`)

// listEntries runs `svn ls --verbose` on url and calls fn with the
// revision and name of each entry ("./" for the directory itself).
func (d *SvnDriver) listEntries(url string, trimOutput bool, fn func(rev int, name string) bool) error {
	output, err := d.execute([]string{"svn", "ls", "--verbose"}, url)
	if err != nil || output == "" {
		return err
	}

	if trimOutput {
		output = php.Trim(output)
	}

	for _, line := range util.SplitLines(output) {
		line = php.Trim(line)
		if line == "" {
			continue
		}

		m, err := match(svnListEntry, line)
		if err != nil {
			return err
		}
		if m != nil && !fn(php.ToNativeInt(m.Get(1)), m.Get(2)) {
			break
		}
	}

	return nil
}

// Tags ports SvnDriver::getTags.
func (d *SvnDriver) Tags() (*php.Array, error) {
	if d.tags != nil {
		return d.tags, nil
	}

	tags := php.NewArray()

	if !d.tagsPath.disabled {
		if err := d.collectRefs(tags, d.tagsPath.path, false); err != nil {
			return nil, err
		}
	}

	d.tags = tags

	return tags, nil
}

// collectRefs adds the entries of the dir directory (tags or branches) to
// refs, at the latest of their revision and the directory's.
func (d *SvnDriver) collectRefs(refs *php.Array, dir string, trimOutput bool) error {
	lastRev := 0

	return d.listEntries(d.baseURL+"/"+dir, trimOutput, func(rev int, name string) bool {
		if name == "./" {
			lastRev = rev
		} else {
			refs.Set(php.RtrimSet(name, "/"), d.buildIdentifier("/"+dir+"/"+name, max(lastRev, rev)))
		}

		return true
	})
}

// Branches ports SvnDriver::getBranches.
func (d *SvnDriver) Branches() (*php.Array, error) {
	if d.branches != nil {
		return d.branches, nil
	}

	branches := php.NewArray()

	trunkParent := d.baseURL + "/" + d.trunkPath.path
	if d.trunkPath.disabled {
		trunkParent = d.baseURL + "/"
	}

	err := d.listEntries(trunkParent, false, func(rev int, name string) bool {
		if name != "./" {
			return true
		}

		trunk := d.buildIdentifier("/"+d.trunkPath.path, rev)
		branches.Set("trunk", trunk)
		d.rootIdentifier = trunk

		return false
	})
	if err != nil {
		return nil, err
	}

	if !d.branchesPath.disabled {
		if err := d.collectRefs(branches, d.branchesPath.path, true); err != nil {
			return nil, err
		}
	}

	d.branches = branches

	return branches, nil
}

var svnURL = php.MustCompile(`#(^svn://|^svn\+ssh://|svn\.)#i`)

// svnSupports ports SvnDriver::supports.
func svnSupports(deps Deps, url string, deep bool) (bool, error) {
	url = svnNormalizeURL(url)
	if ok, err := matches(svnURL, url); err != nil || ok {
		return ok, err
	}

	// proceed with deep check for local urls since they are fast to process
	if !deep && !util.IsLocalPath(url) {
		return false, nil
	}

	var ignoredOutput string

	exit, err := deps.Process.Execute(util.Cmd("svn", "info", "--non-interactive", "--", url), &ignoredOutput, "")
	if err != nil {
		return false, err
	}

	if exit == 0 {
		// This is definitely a Subversion repository.
		return true, nil
	}

	errorOutput := php.Strtolower(deps.Process.GetErrorOutput())

	// Subversion client 1.7 and older: this is likely a remote Subversion
	// repository that requires authentication. We will handle actual
	// authentication later.
	// Subversion client 1.8 and newer: this is likely a remote Subversion or
	// newer repository that requires authentication.
	return strings.Contains(errorOutput, "authorization failed:") || strings.Contains(errorOutput, "authentication failed"), nil
}

// svnNormalizeURL ports SvnDriver::normalizeUrl: an absolute path becomes
// a file:// url.
func svnNormalizeURL(url string) string {
	if fspath.IsAbsolutePath(url) {
		return "file://" + strings.ReplaceAll(url, `\`, "/")
	}

	return url
}

// execute ports SvnDriver::execute: an svn command with credentials
// handling.
func (d *SvnDriver) execute(command []string, url string) (string, error) {
	if d.util == nil {
		d.util = uvcs.NewSvn(d.baseURL, d.io, d.config, d.process)
		d.util.SetCacheCredentials(d.cacheCredentials)
	}

	output, err := d.util.Execute(command, url, "", "", false)
	if err == nil || !phperr.InstanceOf(err, "RuntimeException") {
		return output, err
	}

	if _, ok, verr := d.util.BinaryVersion(); verr != nil {
		return "", verr
	} else if !ok {
		return "", &util.RuntimeError{Message: "Failed to load " + util.SanitizeURL(d.url) + ", svn was not found, check that it is installed and in your PATH env." + "\n\n" + d.process.GetErrorOutput()}
	}

	return "", &util.RuntimeError{Message: "Repository " + util.SanitizeURL(d.url) + " could not be processed, " + util.SanitizeURL(err.Error())}
}

// buildIdentifier ports SvnDriver::buildIdentifier.
func (d *SvnDriver) buildIdentifier(baseDir string, revision int) string {
	return php.RtrimSet(baseDir, "/") + d.packagePath + "/@" + strconv.Itoa(revision)
}
