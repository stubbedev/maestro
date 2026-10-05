// Ports src/Composer/Repository/VcsRepository.php.

package vcs

import (
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	uvcs "github.com/stubbedev/maestro/internal/util/vcs"
)

// VcsRepositoryClass is the PHP class of VcsRepository, which the Svn and
// Perforce downloaders test packages' repositories against.
const VcsRepositoryClass = `Composer\Repository\VcsRepository`

// TransportErrors are VcsRepository's $versionTransportExceptions: the
// TransportExceptions that made tags and branches be skipped, by name.
type TransportErrors struct {
	Tags     *repository.NameMap[*util.TransportError]
	Branches *repository.NameMap[*util.TransportError]
}

// Options are the optional arguments of VcsRepository's constructor.
type Options struct {
	// Drivers are $drivers: nil is DefaultDrivers().
	Drivers []NamedDriver
	// VersionCache is $versionCache: nil for none.
	VersionCache repository.VersionCache
}

// VcsRepository ports Composer\Repository\VcsRepository: the packages of
// the tags and branches of a VCS repository, read by the driver that
// handles its url when first used.
type VcsRepository struct {
	repository.ArrayRepository

	url                        string
	packageName                string
	isVerbose                  bool
	isVeryVerbose              bool
	io                         io.IO
	config                     http.Config
	versionParser              *pkg.VersionParser
	typ                        string
	loader                     loader.LoaderInterface
	repoConfig                 *php.Array
	httpDownloader             http.Getter
	processExecutor            uvcs.Process
	branchErrorOccurred        bool
	drivers                    []NamedDriver
	driver                     Driver
	versionCache               repository.VersionCache
	emptyReferences            []string
	versionTransportExceptions TransportErrors
}

var (
	_ repository.RepositoryInterface    = (*VcsRepository)(nil)
	_ repository.ConfigurableRepository = (*VcsRepository)(nil)
)

// NewVcsRepository is new VcsRepository($repoConfig, $io, $config,
// $httpDownloader, $dispatcher, $process, $drivers, $versionCache). A nil
// process is a new ProcessExecutor; httpDownloader may be nil for
// repositories whose driver does not use the API.
func NewVcsRepository(repoConfig *php.Array, ioi io.IO, config http.Config, httpDownloader http.Getter, process uvcs.Process, opts Options) (*VcsRepository, error) {
	r := &VcsRepository{
		drivers:      opts.Drivers,
		versionCache: opts.VersionCache,
		versionTransportExceptions: TransportErrors{
			Tags:     &repository.NameMap[*util.TransportError]{},
			Branches: &repository.NameMap[*util.TransportError]{},
		},
	}
	r.Extend(r, r.initialize)

	if len(r.drivers) == 0 {
		r.drivers = DefaultDrivers()
	}

	repoConfig = repoConfig.Clone()

	url, err := util.ExpandPath(pathString(repoConfig, "url"))
	if err != nil {
		return nil, err
	}

	repoConfig.Set("url", url)
	r.url = url
	r.io = ioi

	r.typ = "vcs"
	if v, _ := repoConfig.Get("type"); v != nil {
		r.typ = php.ToString(v)
	}

	r.isVerbose = ioi.IsVerbose()
	r.isVeryVerbose = ioi.IsVeryVerbose()
	r.config = config
	r.repoConfig = repoConfig
	r.httpDownloader = httpDownloader

	r.processExecutor = process
	if r.processExecutor == nil {
		r.processExecutor = http.NewProcessExecutor(ioi)
	}

	return r, nil
}

// NewRepository is the repository.Constructor of the "vcs" type and its
// aliases (git, github, gitlab, ...), for RepositoryManager's registry.
func NewRepository(config *php.Array, deps repository.Deps) (repository.RepositoryInterface, error) {
	var (
		httpDownloader http.Getter
		process        uvcs.Process
	)

	if deps.HTTPDownloader != nil {
		httpDownloader = deps.HTTPDownloader
	}

	if deps.Process != nil {
		process = deps.Process
	}

	return NewVcsRepository(config, deps.IO, deps.Config.ForHTTP(), httpDownloader, process, Options{})
}

// Class returns the PHP class name.
func (r *VcsRepository) Class() string { return VcsRepositoryClass }

// RepoName ports VcsRepository::getRepoName. When no driver can be set
// up, where PHP throws, the configured type names the driver.
func (r *VcsRepository) RepoName() string {
	driverType := r.typ

	if driver, err := r.Driver(); err == nil && driver != nil {
		driverType = driver.Class()

		for _, d := range r.drivers {
			if d.Type.Class == driverType {
				driverType = d.Name

				break
			}
		}
	}

	return "vcs repo (" + driverType + " " + util.SanitizeURL(r.url) + ")"
}

// RepoConfig ports getRepoConfig.
func (r *VcsRepository) RepoConfig() *php.Array { return r.repoConfig }

// SetLoader ports setLoader.
func (r *VcsRepository) SetLoader(l loader.LoaderInterface) { r.loader = l }

// driverDeps are the collaborators handed to the drivers.
func (r *VcsRepository) driverDeps() Deps {
	return Deps{IO: r.io, Config: r.config, HTTPDownloader: r.httpDownloader, Process: r.processExecutor}
}

// Driver ports getDriver: the driver of the repository, initialized; nil
// when no driver supports its url.
func (r *VcsRepository) Driver() (Driver, error) {
	if r.driver != nil {
		return r.driver, nil
	}

	deps := r.driverDeps()

	for _, d := range r.drivers {
		if d.Name == r.typ {
			return r.setDriver(d.Type, deps)
		}
	}

	for _, deep := range []bool{false, true} {
		for _, d := range r.drivers {
			ok, err := d.Type.Supports(deps, r.url, deep)
			if err != nil {
				return nil, err
			}

			if ok {
				return r.setDriver(d.Type, deps)
			}
		}
	}

	return nil, nil
}

// setDriver creates and initializes the driver of typ. As in PHP, the
// driver is kept when its initialization fails.
func (r *VcsRepository) setDriver(typ DriverType, deps Deps) (Driver, error) {
	r.driver = typ.New(r.repoConfig, deps)

	if err := r.driver.Initialize(); err != nil {
		return nil, err
	}

	return r.driver, nil
}

// HadInvalidBranches ports hadInvalidBranches.
func (r *VcsRepository) HadInvalidBranches() bool { return r.branchErrorOccurred }

// EmptyReferences ports getEmptyReferences: the identifiers found to have
// no composer.json.
func (r *VcsRepository) EmptyReferences() []string { return r.emptyReferences }

// VersionTransportExceptions ports getVersionTransportExceptions.
func (r *VcsRepository) VersionTransportExceptions() TransportErrors {
	return r.versionTransportExceptions
}

// label is the name of the package in messages.
func (r *VcsRepository) label() string {
	if r.packageName != "" {
		return r.packageName
	}

	return util.SanitizeURL(r.url)
}

// writeVerbose writes msg at -vv, or overwrites the current line with it
// at -v.
func (r *VcsRepository) writeVerbose(msg string) {
	if r.isVeryVerbose {
		r.io.WriteError(msg, true, io.Normal)
	} else if r.isVerbose {
		r.io.OverwriteError(msg, false, -1, io.Normal)
	}
}

// writeVeryVerbose writes msg at -vv.
func (r *VcsRepository) writeVeryVerbose(msg string) {
	if r.isVeryVerbose {
		r.io.WriteError(msg, true, io.Normal)
	}
}

// initialize ports VcsRepository::initialize: the packages of every tag
// and branch.
func (r *VcsRepository) initialize() error {
	r.InitializeBase()

	driver, err := r.Driver()
	if err != nil {
		return err
	}

	if driver == nil {
		return &util.InvalidArgumentError{Message: "No driver found to handle VCS repository " + util.SanitizeURL(r.url)}
	}

	r.versionParser = pkg.NewVersionParser()
	if r.loader == nil {
		r.loader = loader.NewArrayLoader(r.versionParser, false)
	}

	hasRootIdentifierComposerJSON, err := r.readRootComposerJSON(driver)
	if err != nil {
		return err
	}

	if err := r.loadTags(driver); err != nil {
		return err
	}

	if !r.isVeryVerbose {
		r.io.OverwriteError("", false, -1, io.Normal)
	}

	if err := r.loadBranches(driver, hasRootIdentifierComposerJSON); err != nil {
		return err
	}

	if err := driver.Cleanup(); err != nil {
		return err
	}

	if !r.isVeryVerbose {
		r.io.OverwriteError("", false, -1, io.Normal)
	}

	if n, err := r.Count(); err != nil || n == 0 {
		if err != nil {
			return err
		}

		return &repository.InvalidRepositoryError{Message: "No valid composer.json was found in any branch or tag of " + util.SanitizeURL(r.url) + ", could not load a package from it."}
	}

	return nil
}

// readRootComposerJSON reads the package name from the composer.json of
// the root identifier, and reports whether there is one.
func (r *VcsRepository) readRootComposerJSON(driver Driver) (bool, error) {
	has, err := func() (bool, error) {
		root, err := driver.RootIdentifier()
		if err != nil {
			return false, err
		}

		has, err := driver.HasComposerFile(root)
		if err != nil || !has {
			return false, err
		}

		data, err := driver.ComposerInformation(root)
		if err != nil {
			return true, err
		}

		r.packageName = ""
		if name, _ := data.Get("name"); php.ToBool(name) {
			r.packageName = php.ToString(name)
		}

		return true, nil
	}()
	if err == nil {
		return has, nil
	}

	if e, ok := asTransportError(err); ok && shouldRethrowTransportException(e) {
		return has, err
	}

	if r.isVeryVerbose {
		root, rerr := driver.RootIdentifier()
		if rerr != nil {
			return has, rerr
		}

		r.io.WriteError("<error>Skipped parsing "+root+", "+err.Error()+"</error>", true, io.Normal)
	}

	return has, nil
}

var (
	devSuffix       = php.MustCompile(`{[.-]?dev$}i`)
	devPrefixSuffix = php.MustCompile(`{(^dev-|[.-]?dev$)}i`)
	nines           = php.MustCompile(`{(\.9{7})+}`)
)

// loadTags adds the packages of the driver's tags.
func (r *VcsRepository) loadTags(driver Driver) error {
	tags, err := driver.Tags()
	if err != nil {
		return err
	}

	for k, v := range tags.All() {
		tag := k.String()
		identifier := php.ToString(v)
		msg := "Reading composer.json of <info>" + r.label() + "</info> (<comment>" + tag + "</comment>)"

		// strip the release- prefix from tags if present
		tag = strings.ReplaceAll(tag, "release-", "")

		cachedPackage, absent, err := r.cachedPackageVersion(tag, identifier, false)
		if err != nil {
			return err
		}

		if cachedPackage != nil {
			if err := r.AddPackage(cachedPackage); err != nil {
				return err
			}

			continue
		}

		if absent {
			r.emptyReferences = append(r.emptyReferences, identifier)

			continue
		}

		parsedTag, err := r.versionParser.Normalize(tag)
		if err != nil || parsedTag == "" {
			r.writeVeryVerbose("<warning>Skipped tag " + tag + ", invalid tag name</warning>")

			continue
		}

		r.writeVerbose(msg)

		if err := r.loadTag(driver, tag, identifier, parsedTag); err != nil {
			e, isTransport := asTransportError(err)
			if isTransport {
				r.versionTransportExceptions.Tags.Set(tag, e)
				if e.Code == 404 {
					r.emptyReferences = append(r.emptyReferences, identifier)
				}

				if shouldRethrowTransportException(e) {
					return err
				}

				r.writeVeryVerbose("<warning>Skipped tag " + tag + ", no composer file was found (" + strconv.Itoa(e.Code) + " HTTP status code)</warning>")
			} else {
				r.writeVeryVerbose("<warning>Skipped tag " + tag + ", " + err.Error() + "</warning>")
			}
		}
	}

	return nil
}

// loadTag is the try block of initialize()'s tag loop: it adds the
// package of the tag, unless it is skipped.
func (r *VcsRepository) loadTag(driver Driver, tag, identifier, parsedTag string) error {
	data, err := driver.ComposerInformation(identifier)
	if err != nil {
		return err
	}

	if data == nil {
		r.writeVeryVerbose("<warning>Skipped tag " + tag + ", no composer file</warning>")
		r.emptyReferences = append(r.emptyReferences, identifier)

		return nil
	}

	data = data.Clone()

	// manually versioned package
	if v, _ := data.Get("version"); v != nil {
		normalized, err := r.versionParser.Normalize(php.ToString(v))
		if err != nil {
			return err
		}

		data.Set("version_normalized", normalized)
	} else {
		// auto-versioned package, read value from tag
		data.Set("version", tag)
		data.Set("version_normalized", parsedTag)
	}

	// make sure tag packages have no -dev flag
	data.Set("version", replaceInfallible(devSuffix, "", pathString(data, "version")))
	versionNormalized := replaceInfallible(devPrefixSuffix, "", pathString(data, "version_normalized"))
	data.Set("version_normalized", versionNormalized)

	// make sure tag do not contain the default-branch marker
	data.Delete("default-branch")

	// broken package, version doesn't match tag
	if versionNormalized != parsedTag {
		// Constant work per start position: Preg::isMatch cannot throw.
		if hasDev, _ := devPrefixSuffix.IsMatch(parsedTag); hasDev {
			r.writeVeryVerbose("<warning>Skipped tag " + tag + ", invalid tag name, tags can not use dev prefixes or suffixes</warning>")
		} else {
			r.writeVeryVerbose("<warning>Skipped tag " + tag + ", tag (" + parsedTag + ") does not match version (" + versionNormalized + ") in composer.json</warning>")
		}

		return nil
	}

	tagPackageName := r.packageName
	if tagPackageName == "" {
		tagPackageName = pathString(data, "name")
	}

	constraint, err := r.versionParser.ParseConstraints(versionNormalized)
	if err != nil {
		return err
	}

	existingPackage, err := r.FindPackage(tagPackageName, constraint)
	if err != nil {
		return err
	}

	if existingPackage != nil {
		r.writeVeryVerbose("<warning>Skipped tag " + tag + ", it conflicts with an another tag (" + existingPackage.PrettyVersion() + ") as both resolve to " + versionNormalized + " internally</warning>")

		return nil
	}

	r.writeVeryVerbose("Importing tag " + tag + " (" + versionNormalized + ")")

	p, err := r.loader.Load(r.preProcess(driver, data, identifier), pkg.ClassCompletePackage)
	if err != nil {
		return err
	}

	return r.AddPackage(p)
}

// loadBranches adds the packages of the driver's branches, the root
// identifier's first when it has a composer.json.
func (r *VcsRepository) loadBranches(driver Driver, hasRootIdentifierComposerJSON bool) error {
	branches, err := driver.Branches()
	if err != nil {
		return err
	}

	// make sure the root identifier branch gets loaded first
	if hasRootIdentifierComposerJSON {
		root, err := driver.RootIdentifier()
		if err != nil {
			return err
		}

		if v, _ := branches.Get(root); v != nil {
			first := php.NewArray()
			first.Set(root, v)

			plus, err := php.Add(first, branches)
			if err != nil {
				return err
			}

			branches, _ = plus.(*php.Array)
		}
	}

	for k, v := range branches.All() {
		branch := k.String()
		identifier := php.ToString(v)
		msg := "Reading composer.json of <info>" + r.label() + "</info> (<comment>" + branch + "</comment>)"
		r.writeVerbose(msg)

		parsedBranch, ok := r.validateBranch(branch)
		if !ok {
			r.writeVeryVerbose("<warning>Skipped branch " + branch + ", invalid name</warning>")

			continue
		}

		var version string

		// make sure branch packages have a dev flag
		if strings.HasPrefix(parsedBranch, "dev-") || parsedBranch == pkg.DefaultBranchAlias {
			version = "dev-" + strings.ReplaceAll(branch, "#", "+")
			parsedBranch = strings.ReplaceAll(parsedBranch, "#", "+")
		} else {
			prefix := ""
			if strings.HasPrefix(branch, "v") {
				prefix = "v"
			}

			replaced, err := replace(nines, ".x", parsedBranch)
			if err != nil {
				return err
			}
			version = prefix + replaced
		}

		root, err := driver.RootIdentifier()
		if err != nil {
			return err
		}

		isDefaultBranch := root == branch

		cachedPackage, absent, err := r.cachedPackageVersion(version, identifier, isDefaultBranch)
		if err != nil {
			return err
		}

		if cachedPackage != nil {
			if err := r.AddPackage(cachedPackage); err != nil {
				return err
			}

			continue
		}

		if absent {
			r.emptyReferences = append(r.emptyReferences, identifier)

			continue
		}

		err = r.loadBranch(driver, branch, identifier, version, parsedBranch, isDefaultBranch)
		if err == nil {
			continue
		}

		if e, ok := asTransportError(err); ok {
			r.versionTransportExceptions.Branches.Set(branch, e)
			if e.Code == 404 {
				r.emptyReferences = append(r.emptyReferences, identifier)
			}

			if shouldRethrowTransportException(e) {
				return err
			}

			r.writeVeryVerbose("<warning>Skipped branch " + branch + ", no composer file was found (" + strconv.Itoa(e.Code) + " HTTP status code)</warning>")

			continue
		}

		if !r.isVeryVerbose {
			r.io.WriteError("", true, io.Normal)
		}

		r.branchErrorOccurred = true
		r.io.WriteError("<error>Skipped branch "+branch+", "+err.Error()+"</error>", true, io.Normal)
		r.io.WriteError("", true, io.Normal)
	}

	return nil
}

// loadBranch is the try block of initialize()'s branch loop: it adds the
// package of the branch, unless it has no composer.json.
func (r *VcsRepository) loadBranch(driver Driver, branch, identifier, version, parsedBranch string, isDefaultBranch bool) error {
	data, err := driver.ComposerInformation(identifier)
	if err != nil {
		return err
	}

	if data == nil {
		r.writeVeryVerbose("<warning>Skipped branch " + branch + ", no composer file</warning>")
		r.emptyReferences = append(r.emptyReferences, identifier)

		return nil
	}

	data = data.Clone()

	// branches are always auto-versioned, read value from branch name
	data.Set("version", version)
	data.Set("version_normalized", parsedBranch)

	data.Delete("default-branch")
	if isDefaultBranch {
		data.Set("default-branch", true)
	}

	r.writeVeryVerbose("Importing branch " + branch + " (" + version + ")")

	packageData := r.preProcess(driver, data, identifier)

	p, err := r.loader.Load(packageData, pkg.ClassCompletePackage)
	if err != nil {
		return err
	}

	if l, ok := r.loader.(*loader.ValidatingArrayLoader); ok && len(l.Warnings()) > 0 {
		return loader.NewInvalidPackageError(l.Errors(), l.Warnings(), packageData)
	}

	return r.AddPackage(p)
}

// preProcess ports preProcess: the name of the root identifier's package
// for every package (so that a package can be renamed in one place and
// all old tags will still be installable using that new name without
// requiring re-tagging), and the driver's dist and source unless
// composer.json has them.
func (r *VcsRepository) preProcess(driver Driver, data *php.Array, identifier string) *php.Array {
	if r.packageName != "" {
		data.Set("name", r.packageName)
	} else {
		name, _ := data.Get("name")
		data.Set("name", name)
	}

	if !isset(data, "dist") {
		data.Set("dist", nullable(driver.Dist(identifier)))
	}

	if !isset(data, "source") {
		data.Set("source", nullable(driver.Source(identifier)))
	}

	// if custom dist info is provided but does not provide a reference, copy the source reference to it
	if dist, ok := data.GetArray("dist"); ok && !isset(dist, "reference") && isset(data, "source", "reference") {
		dist.Set("reference", arrayPath(data, "source", "reference"))
	}

	return data
}

// nullable keeps a nil array PHP's null in an array value.
func nullable(a *php.Array) any {
	if a == nil {
		return nil
	}

	return a
}

// validateBranch ports validateBranch: the normalized branch, false when
// its name conflicts with constraints.
func (r *VcsRepository) validateBranch(branch string) (string, bool) {
	normalizedBranch := r.versionParser.NormalizeBranch(branch)

	// validate that the branch name has no weird characters conflicting with constraints
	if _, err := r.versionParser.ParseConstraints(normalizedBranch); err != nil || normalizedBranch == "" {
		return "", false
	}

	return normalizedBranch, true
}

// cachedPackageVersion ports getCachedPackageVersion: the package of the
// version cache, nil without one; absent is PHP's false (the cache knows
// there is no composer.json).
func (r *VcsRepository) cachedPackageVersion(version, identifier string, isDefaultBranch bool) (p pkg.PackageInterface, absent bool, err error) {
	if r.versionCache == nil {
		return nil, false, nil
	}

	cached := r.versionCache.VersionPackage(version, identifier)
	if cached == false {
		r.writeVeryVerbose("<warning>Skipped " + version + ", no composer file (cached from ref " + identifier + ")</warning>")

		return nil, true, nil
	}

	cachedPackage, _ := cached.(*php.Array)
	if cachedPackage == nil || cachedPackage.Len() == 0 {
		return nil, false, nil
	}

	r.writeVerbose("Found cached composer.json of <info>" + r.label() + "</info> (<comment>" + version + "</comment>)")

	cachedPackage = cachedPackage.Clone()
	cachedPackage.Delete("default-branch")

	if isDefaultBranch {
		cachedPackage.Set("default-branch", true)
	}

	versionNormalized := pathString(cachedPackage, "version_normalized")

	constraint, err := semver.NewConstraint("=", versionNormalized)
	if err != nil {
		return nil, false, err
	}

	existingPackage, err := r.FindPackage(pathString(cachedPackage, "name"), constraint)
	if err != nil {
		return nil, false, err
	}

	if existingPackage != nil {
		r.writeVeryVerbose("<warning>Skipped cached version " + version + ", it conflicts with an another tag (" + existingPackage.PrettyVersion() + ") as both resolve to " + versionNormalized + " internally</warning>")

		return nil, false, nil
	}

	p, err = r.loader.Load(cachedPackage, pkg.ClassCompletePackage)

	return p, false, err
}

// shouldRethrowTransportException ports shouldRethrowTransportException:
// authentication, rate limit and server errors abort the scan.
func shouldRethrowTransportException(e *util.TransportError) bool {
	return e.Code == 401 || e.Code == 403 || e.Code == 429 || e.Code >= 500
}
