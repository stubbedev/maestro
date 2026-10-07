// Ports src/Composer/Autoload/AutoloadGenerator.php: the generator's state,
// dump() and createLoader().

// Package autoload is a port of Composer\Autoload: it writes
// vendor/autoload.php and vendor/composer/autoload_*.php, platform_check.php,
// ClassLoader.php and LICENSE exactly as Composer's AutoloadGenerator does,
// and builds the class loader contents plugins and scripts are run with.
package autoload

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
)

// Generator is Composer\Autoload\AutoloadGenerator.
type Generator struct {
	// Parser decides how scanned files are tokenized: set its
	// ShortOpenTag and PHPVersionID from the short_open_tag and
	// PHP_VERSION_ID of the user's php (internal/platform); the defaults
	// are PHP's built-in On and PHP 8.4's scanner.
	Parser classmap.Parser

	eventDispatcher EventDispatcher
	io              io.IO
	parseCache      *classmap.ParseCache
	store           *store.Store
	devMode         bool
	devModeSet      bool
	// devModeValue is a $devMode that is not a bool (installed.json's
	// "dev", stored in the untyped property as is); devMode is its
	// truthiness
	devModeValue any
	// suffixValue is a configured autoloader-suffix that is not a string
	suffixValue               any
	classMapAuthoritative     bool
	apcu                      bool
	apcuPrefix                *string
	dryRun                    bool
	runScripts                bool
	platformRequirementFilter version.PlatformRequirementFilter
	// speculation is the class map scan Speculate started, nil without
	// one.
	speculation *speculation
	// ahead is the speculated scan the running Dump took, nil without one:
	// write compares with the files as it read them.
	ahead *scanResult
	// recordDir keeps the class maps of the scans (UseScanRecords), ""
	// for none.
	recordDir string
}

// NewGenerator ports new AutoloadGenerator($eventDispatcher, $io); a nil
// io is a NullIO.
func NewGenerator(eventDispatcher EventDispatcher, ioi io.IO) *Generator {
	if ioi == nil {
		ioi = io.NewNullIO()
	}

	return &Generator{
		Parser:                    classmap.DefaultParser,
		eventDispatcher:           eventDispatcher,
		io:                        ioi,
		parseCache:                classmap.NewParseCache(),
		platformRequirementFilter: ignoreNothing{},
	}
}

// ignoreNothing is PlatformRequirementFilterFactory::ignoreNothing().
type ignoreNothing struct{}

func (ignoreNothing) IsIgnored(string) bool { return false }

// SetDevMode ports setDevMode.
func (g *Generator) SetDevMode(devMode bool) {
	g.devMode, g.devModeSet, g.devModeValue = devMode, true, nil
}

// SetClassMapAuthoritative ports setClassMapAuthoritative: whether the
// generated autoloader considers the class map authoritative.
func (g *Generator) SetClassMapAuthoritative(classMapAuthoritative bool) {
	g.classMapAuthoritative = classMapAuthoritative
}

// SetApcu ports setApcu: whether the generated autoloader uses APCu, with
// the given prefix (nil for a random one).
func (g *Generator) SetApcu(apcu bool, apcuPrefix *string) { g.apcu, g.apcuPrefix = apcu, apcuPrefix }

// SetRunScripts ports setRunScripts.
func (g *Generator) SetRunScripts(runScripts bool) { g.runScripts = runScripts }

// SetDryRun ports setDryRun.
func (g *Generator) SetDryRun(dryRun bool) { g.dryRun = dryRun }

// SetPlatformRequirementFilter ports setPlatformRequirementFilter.
func (g *Generator) SetPlatformRequirementFilter(filter version.PlatformRequirementFilter) {
	g.platformRequirementFilter = filter
}

// classMapExtensions are the extensions the generator scans.
var classMapExtensions = []string{"php", "inc", "hh"}

func (g *Generator) newClassMapGenerator() *classmap.Generator {
	gen := classmap.NewGenerator(classMapExtensions).AvoidDuplicateScans(nil).SetParseCache(g.parseCache)
	gen.Parser = g.Parser

	return gen
}

// Dump ports dump(): it writes the autoloader into vendor-dir/targetDir
// and vendor-dir/autoload.php and returns the class map. suffix "" is
// null; locker may be nil.
func (g *Generator) Dump(config Config, localRepo InstalledRepository, rootPackage pkg.RootPackageInterface, im InstallationManager, targetDir string, scanPsrPackages bool, suffix string, locker Locker, strictAmbiguous bool) (*classmap.ClassMap, error) {
	if g.classMapAuthoritative {
		// Force scanPsrPackages when classmap is authoritative
		scanPsrPackages = true
	}

	// a warm-up (Warm) ends before anything else happens, scripts included
	g.parseCache.Wait()

	// auto-set devMode based on whether dev dependencies are installed or not
	if !g.devModeSet && !g.takeSpeculatedDevMode(config) {
		if err := g.detectDevMode(config); err != nil {
			return nil, err
		}
	}

	flags := php.ArrayOf("optimize", scanPsrPackages)
	if g.runScripts {
		// set COMPOSER_DEV_MODE in case not set yet so it is available in
		// the dump-autoload event listeners
		if _, ok := util.GetEnv("COMPOSER_DEV_MODE"); !ok {
			util.PutEnv("COMPOSER_DEV_MODE", map[bool]string{true: "1", false: "0"}[g.devMode])
		}

		if err := g.devModeArg(); err != nil {
			return nil, err
		}
		if _, err := g.eventDispatcher.DispatchScript(PreAutoloadDump, g.devMode, nil, flags); err != nil {
			return nil, err
		}
	}

	d, err := newDump(config, targetDir)
	if err != nil {
		return nil, err
	}

	// Collect information from all packages.
	devPackageNames := localRepo.DevPackageNames()
	packageMap, err := g.BuildPackageMap(im, rootPackage, localRepo.CanonicalPackages())
	if err != nil {
		return nil, err
	}
	autoloads, err := g.ParseAutoloads(packageMap, rootPackage, devFilter(g.devMode, devPackageNames))
	if err != nil {
		return nil, err
	}

	if err := d.namespaces(autoloads); err != nil {
		return nil, err
	}

	// add custom psr-0 autoloading if the root package has a target dir
	targetDirLoader, err := d.targetDirLoader(rootPackage)
	if err != nil {
		return nil, err
	}

	ahead := g.takeSpeculation(d, autoloads, scanPsrPackages, strictAmbiguous)
	classMap, err := g.scan(d, autoloads, packageMap, scanPsrPackages, strictAmbiguous, ahead)
	if err != nil {
		return nil, err
	}
	g.parseCache.Save()
	if ahead == nil || !ahead.takeClassmap(d) {
		if err := d.classmap(classMap); err != nil {
			return nil, err
		}
	}
	g.ahead = ahead
	defer func() { g.ahead = nil }()

	g.suffixValue = nil
	if suffix, err = g.suffix(config, d.vendorPath, suffix, locker); err != nil {
		return nil, err
	}

	if g.dryRun {
		return classMap, nil
	}

	if err := g.write(d, config, packageMap, autoloads, devPackageNames, targetDirLoader, suffix); err != nil {
		return nil, err
	}

	if g.runScripts {
		if err := g.devModeArg(); err != nil {
			return nil, err
		}
		if _, err := g.eventDispatcher.DispatchScript(PostAutoloadDump, g.devMode, nil, flags); err != nil {
			return nil, err
		}
	}

	return classMap, nil
}

// UseParseCacheFile makes the generator keep the classes it finds in files
// in the file at path, by file contents, and use the classes kept there by
// earlier runs instead of parsing the same contents again (deliberate
// deviation 3, speed). The file is written after each scan.
func (g *Generator) UseParseCacheFile(path string) { g.parseCache.UseFile(path) }

// UseScanRecords makes the generator keep the class map of its scans in
// dir, one record per project, and take it from there instead of scanning
// when the scans are the same and none of the files and directories they
// depend on changed since (deliberate deviation 3, speed;
// classmap.Record). The ambiguous classes and PSR violations are kept
// with it and reported as after a scan.
func (g *Generator) UseScanRecords(dir string) { g.recordDir = dir }

// Warm starts parsing, in the background, the files a Dump with these
// arguments would scan (every package's, dev or not), so that the Dump that
// follows finds most of them parsed (deliberate deviation 3, speed: the
// installer calls it before it waits on the network). It changes nothing a
// Dump does or prints: the Dump parses again whatever changed since, and
// any problem here only means less is parsed ahead.
func (g *Generator) Warm(config Config, localRepo InstalledRepository, rootPackage pkg.RootPackageInterface, im InstallationManager, scanPsrPackages bool) {
	if g.classMapAuthoritative {
		scanPsrPackages = true
	}
	vendorDir, err := vendorDirConfig(config)
	if err != nil {
		return
	}
	cwd, err := util.GetCwd(false)
	if err != nil {
		return
	}
	d := &dump{}
	if d.basePath, err = realpath(cwd); err != nil {
		return
	}
	d.basePath = util.NormalizePath(d.basePath)
	if d.vendorPath, err = realpath(vendorDir); err != nil {
		return
	}
	d.vendorPath = util.NormalizePath(d.vendorPath)

	packageMap, err := g.BuildPackageMap(im, rootPackage, localRepo.CanonicalPackages())
	if err != nil {
		return
	}
	autoloads, err := g.parseAutoloads(packageMap, rootPackage, NoDevFilter, true)
	if err != nil {
		return
	}
	g.addReleases(packageMap)
	requests := make([]classmap.ScanRequest, 0, len(autoloads.Classmap))
	for _, dir := range autoloads.Classmap {
		requests = append(requests, classmap.ScanRequest{Path: dir, Excluded: d.exclusions.build(dir, autoloads.ExcludeFromClassmap)})
	}
	if scanPsrPackages {
		for _, s := range d.psrScans(autoloads, autoloads.ExcludeFromClassmap) {
			requests = append(requests, classmap.ScanRequest{Path: s.dir, Excluded: s.excluded})
		}
	}
	g.parseCache.Warm(g.Parser, classMapExtensions, requests)
}

// detectDevMode sets devMode from vendor/composer/installed.json; it is
// false if no vendor dir is present or it is too old to contain dev
// information.
func (g *Generator) detectDevMode(config Config) error {
	g.devModeSet = true
	var err error
	g.devMode, g.devModeValue, err = installedDevMode(config)

	return err
}

// installedDevMode is the devMode and devModeValue detectDevMode reads.
func installedDevMode(config Config) (devMode bool, value any, err error) {
	vendorDir, err := vendorDirConfig(config)
	if err != nil {
		return false, nil, err
	}

	installedJSON, err := json.NewFile(vendorDir+"/composer/installed.json", nil, nil)
	if err != nil || !installedJSON.Exists() {
		return false, nil, err
	}
	data, err := installedJSON.Read()
	if err != nil {
		return false, nil, err
	}
	a, ok := data.(*php.Array)
	if !ok {
		return false, nil, nil
	}
	// if (isset($installedJson['dev'])) $this->devMode = $installedJson['dev'];
	switch dev, _ := a.Get("dev"); dev := dev.(type) {
	case nil:
		return false, nil, nil
	case bool:
		return dev, nil, nil
	default:
		return php.ToBool(dev), dev, nil
	}
}

// devFilter is the dev packages a dump leaves out: none in dev mode,
// else the list of dev package names when it is available, otherwise
// the legacy algo's.
func devFilter(devMode bool, devPackageNames []string) DevFilter {
	if devMode {
		return NoDevFilter
	}
	if len(devPackageNames) > 0 {
		return DevPackageNames(devPackageNames)
	}

	return LegacyDevFilter
}

// devModeArg checks $this->devMode passed to EventDispatcher::dispatchScript
// (bool $devMode) in AutoloadGenerator.php: a value of
// installed.json's "dev" that is not a bool is the TypeError of
// strict_types.
func (g *Generator) devModeArg() error {
	if g.devModeValue == nil {
		return nil
	}

	return pkg.ArgumentTypeError(`Composer\EventDispatcher\EventDispatcher::dispatchScript`, 2, "devMode", "bool", g.devModeValue)
}

// scan builds the class map: the classmap rules, plus the PSR-0/4 dirs
// with scanPsrPackages, reporting ambiguous classes and PSR violations.
// ahead is what a speculation found of it already, nil to scan now.
func (g *Generator) scan(d *dump, autoloads *Autoloads, packageMap []PackageMapEntry, scanPsrPackages, strictAmbiguous bool, ahead *scanResult) (*classmap.ClassMap, error) {
	if ahead != nil {
		// the speculation analysed its scan as this would (takeSpeculation)
		for _, msg := range ahead.warnings {
			g.io.WriteError(msg, true, io.Normal)
		}

		return ahead.classMap, nil
	}
	classMap, err := g.scanClassMap(d, autoloads, packageMap, scanPsrPackages)
	if err != nil {
		return nil, err
	}
	warnings, err := analyseClassMap(classMap, d.vendorPath, strictAmbiguous)
	for _, msg := range warnings {
		g.io.WriteError(msg, true, io.Normal)
	}
	if err != nil {
		return nil, err
	}

	return classMap, nil
}

// analyseClassMap is the part of scan after the scans: it returns the
// warnings about ambiguous classes and PSR violations scan prints (each
// with WriteError at Normal verbosity; on error, those printed before it),
// and completes the class map. It reads and changes nothing but classMap.
func analyseClassMap(classMap *classmap.ClassMap, vendorPath string, strictAmbiguous bool) ([]string, error) {
	var warnings []string
	filter := classmap.DefaultDuplicatesFilter
	if strictAmbiguous {
		filter = nil
	}
	ambiguousClasses, err := classMap.AmbiguousClasses(filter)
	if err != nil {
		return nil, err
	}
	for _, ambiguous := range ambiguousClasses {
		classPath, err := classMap.ClassPath(ambiguous.Class)
		if err != nil {
			return warnings, err
		}
		paths := strings.Join(ambiguous.Paths, `", "`)
		if len(ambiguous.Paths) > 1 {
			warnings = append(warnings, `<warning>Warning: Ambiguous class resolution, "`+ambiguous.Class+`"`+
				` was found `+php.ToString(int64(len(ambiguous.Paths)+1))+`x: in "`+classPath+`" and "`+paths+`", the first will be used.</warning>`)
		} else {
			warnings = append(warnings, `<warning>Warning: Ambiguous class resolution, "`+ambiguous.Class+`"`+
				` was found in both "`+classPath+`" and "`+paths+`", the first will be used.</warning>`)
		}
	}
	if len(ambiguousClasses) > 0 {
		warnings = append(warnings, "<info>To resolve ambiguity in classes not under your control you can ignore them by path using <href="+console.Escape("https://getcomposer.org/doc/04-schema.md#exclude-files-from-classmaps")+">exclude-from-classmap</>")
	}

	// output PSR violations which are not coming from the vendor dir
	classMap.ClearPsrViolationsByPath(vendorPath)
	for _, msg := range classMap.PsrViolations() {
		warnings = append(warnings, "<warning>"+msg+"</warning>")
	}

	classMap.AddClass(`Composer\InstalledVersions`, vendorPath+"/composer/InstalledVersions.php")
	classMap.Sort()

	return warnings, nil
}

// scanClassMap is the part of scan that scans: it prints nothing, and
// reads only d's basePath and vendorPath. The class map comes from the
// scans' record when it is still valid (UseScanRecords); packageMap's
// store releases are looked up only for a scan.
func (g *Generator) scanClassMap(d *dump, autoloads *Autoloads, packageMap []PackageMapEntry, scanPsrPackages bool) (*classmap.ClassMap, error) {
	excluded := autoloads.ExcludeFromClassmap

	// every scan, planned first so that their files are walked and parsed
	// together (classmap.Generator.Prefetch), then run in order
	scans := make([]psrScan, 0, len(autoloads.Classmap))
	for i, dir := range autoloads.Classmap {
		if autoloads.classmapValue != nil && i == autoloads.classmapIndex {
			return nil, pkg.ArgumentTypeError(`Composer\Autoload\AutoloadGenerator::buildExclusionRegex`, 1, "dir", "string", autoloads.classmapValue)
		}
		scans = append(scans, psrScan{dir, d.exclusions.build(dir, excluded), classmap.Classmap, ""})
	}
	if scanPsrPackages {
		scans = append(scans, d.psrScans(autoloads, excluded)...)
	}
	record, recorded := g.scanRecord(d, scans, scanPsrPackages)
	if recorded {
		if classMap, ok := record.Load(); ok {
			return classMap, nil
		}
	}
	g.addReleases(packageMap)
	gen := g.newClassMapGenerator()
	if recorded {
		gen.StartRecording()
	}

	requests := make([]classmap.ScanRequest, len(scans))
	for i, s := range scans {
		requests[i] = classmap.ScanRequest{Path: s.dir, Excluded: s.excluded}
	}
	gen.Prefetch(requests)

	for _, s := range scans {
		if err := gen.ScanPaths(s.dir, s.excluded, s.typ, s.namespace, nil); err != nil {
			return nil, err
		}
	}
	if recorded {
		gen.SaveRecord(record)
	}

	return gen.ClassMap(), nil
}

// scanRecord is the record of these scans (UseScanRecords), ok false
// without one.
func (g *Generator) scanRecord(d *dump, scans []psrScan, scanPsrPackages bool) (*classmap.Record, bool) {
	if g.recordDir == "" {
		return nil, false
	}
	recordScans := make([]classmap.RecordScan, len(scans))
	for i, s := range scans {
		recordScans[i] = classmap.RecordScan{Path: s.dir, Excluded: s.excluded, Type: s.typ, Namespace: s.namespace}
	}
	id := d.basePath + "\x00" + strconv.FormatBool(scanPsrPackages)

	return classmap.NewRecord(g.recordDir, id, []string{d.basePath, d.vendorPath}, g.Parser, classMapExtensions, recordScans)
}

var (
	// {ComposerAutoloaderInit([^:\s]+)::}
	reExistingSuffix = php.MustCompile(`{ComposerAutoloaderInit([^:\s]+)::}`)
	// {^[a-f0-9]+$}
	reContentHash = php.MustCompile(`{^[a-f0-9]+$}`)
)

// suffix picks the class name suffix: the given one, else the configured
// autoloader-suffix, else the one of the existing autoload.php, else the
// lock file's content-hash, else a random one.
func (g *Generator) suffix(config Config, vendorPath, suffix string, locker Locker) (string, error) {
	if suffix != "" {
		return suffix, nil
	}

	configured, err := config.Get("autoloader-suffix", 0)
	if err != nil {
		return "", err
	}
	if configured != nil {
		if str, ok := configured.(string); ok {
			return str, nil
		}
		// kept as is: getStaticFile(string $suffix) rejects it once the
		// other files are written (write)
		g.suffixValue = configured

		return "", nil
	}

	// carry over existing autoload.php's suffix if possible and none is
	// configured
	if util.IsReadable(vendorPath + "/autoload.php") {
		content, _ := os.ReadFile(vendorPath + "/autoload.php")
		m, err := reExistingSuffix.Match(string(content))
		if err != nil {
			return "", err
		}
		if m != nil {
			return m.Get(1), nil
		}
	}

	// a lock file with an unresolved merge conflict has its content-hash
	// replaced by a human readable message, which would end up inside the
	// autoloader class names
	if locker != nil {
		locked, err := locker.IsLocked()
		if err != nil {
			return "", err
		}
		if locked {
			data, err := locker.LockData()
			if err != nil {
				return "", err
			}
			// $locker->getLockData()['content-hash'] is read without isset
			if !data.Has("content-hash") {
				return "", &util.ErrorException{Message: `Undefined array key "content-hash"`}
			}
			if hash, ok := data.GetString("content-hash"); ok {
				if ok, err := reContentHash.IsMatch(hash); err != nil {
					return "", err
				} else if ok {
					return hash, nil
				}
			}
		}
	}

	return randomHex(16), nil
}

// randomHex is bin2hex(random_bytes(n)).
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)

	return hex.EncodeToString(b)
}

// write writes the autoloader files.
func (g *Generator) write(d *dump, config Config, packageMap []PackageMapEntry, autoloads *Autoloads, devPackageNames []string, targetDirLoader, suffix string) error {
	if err := g.put(d.targetDir+"/autoload_namespaces.php", d.namespacesFile); err != nil {
		return err
	}
	if err := g.put(d.targetDir+"/autoload_psr4.php", d.psr4File); err != nil {
		return err
	}
	if err := g.put(d.targetDir+"/autoload_classmap.php", d.classmapFile); err != nil {
		return err
	}

	includePathsFile, err := d.includePathsFile(packageMap)
	if err != nil {
		return err
	}
	if err := g.putOrRemove(d.targetDir+"/include_paths.php", includePathsFile); err != nil {
		return err
	}

	includeFilesFile, err := g.includeFilesFile(d, autoloads.Files)
	if err != nil {
		return err
	}
	if err := g.putOrRemove(d.targetDir+"/autoload_files.php", includeFilesFile); err != nil {
		return err
	}

	if g.suffixValue != nil {
		return pkg.ArgumentTypeError(`Composer\Autoload\AutoloadGenerator::getStaticFile`, 1, "suffix", "string", g.suffixValue)
	}
	staticFile, err := d.staticFile(suffix)
	if err != nil {
		return err
	}
	if err := g.put(d.targetDir+"/autoload_static.php", staticFile); err != nil {
		return err
	}

	platformCheck, err := config.Get("platform-check", 0)
	if err != nil {
		return err
	}
	checkPlatform := platformCheck != false
	if _, ignoreAll := g.platformRequirementFilter.(version.IgnoreAllPlatformRequirementFilter); ignoreAll {
		checkPlatform = false
	}
	platformCheckContent := ""
	if checkPlatform {
		if platformCheckContent, err = g.platformCheck(packageMap, platformCheck, devPackageNames); err != nil {
			return err
		}
		checkPlatform = platformCheckContent != ""
	}
	if err := g.putOrRemove(d.targetDir+"/platform_check.php", platformCheckContent); err != nil {
		return err
	}

	if err := g.put(d.vendorPath+"/autoload.php", autoloadFile(d.vendorPathToTargetDirCode, suffix)); err != nil {
		return err
	}

	useGlobalIncludePath, err := config.Get("use-include-path", 0)
	if err != nil {
		return err
	}
	prependAutoloader, err := config.Get("prepend-autoloader", 0)
	if err != nil {
		return err
	}
	realFile := g.autoloadRealFile(includePathsFile != "", targetDirLoader, includeFilesFile != "", suffix, php.ToBool(useGlobalIncludePath), prependAutoloader != false, checkPlatform)
	if err := g.put(d.targetDir+"/autoload_real.php", realFile); err != nil {
		return err
	}

	if err := g.put(d.targetDir+"/ClassLoader.php", ClassLoaderPHP); err != nil {
		return err
	}

	return g.put(d.targetDir+"/LICENSE", License)
}

// putIfModified is Filesystem::filePutContentsIfModified.
func putIfModified(path, content string) error {
	_, err := util.FilePutContentsIfModified(path, []byte(content))

	return err
}

// put is putIfModified, without reading path when the speculation the
// dump took read it and it did not change since.
func (g *Generator) put(path, content string) error {
	if g.ahead != nil && g.ahead.unchanged(path, content) {
		return nil
	}

	return putIfModified(path, content)
}

// putOrRemove writes content to path, or removes path for no content.
func (g *Generator) putOrRemove(path, content string) error {
	if content != "" {
		return g.put(path, content)
	}
	if _, err := os.Lstat(path); err == nil {
		return util.Unlink(path)
	}

	return nil
}

// vendorDirConfig returns the vendor-dir config value as a string.
func vendorDirConfig(config Config) (string, error) {
	v, err := config.Get("vendor-dir", 0)
	if err != nil {
		return "", err
	}

	return php.ToString(v), nil
}

// CreateLoader ports createLoader: a class loader registering the PSR-0,
// PSR-4 and (scanned) classmap rules of autoloads. Classmap paths that
// cannot be scanned are reported as warnings.
func (g *Generator) CreateLoader(autoloads *Autoloads, vendorDir string) (*ClassLoader, error) {
	loader := NewClassLoader(vendorDir)

	for namespace, paths := range autoloads.PSR0.All() {
		list, _ := paths.(*php.Array)
		if err := loader.Add(namespace, list, false); err != nil {
			return nil, err
		}
	}

	for namespace, paths := range autoloads.PSR4.All() {
		list, _ := paths.(*php.Array)
		if err := loader.AddPsr4(namespace, list, false); err != nil {
			return nil, err
		}
	}

	gen := g.newClassMapGenerator()
	for _, dir := range autoloads.Classmap {
		err := gen.ScanPaths(dir, buildExclusionRegex(dir, autoloads.ExcludeFromClassmap), classmap.Classmap, "", nil)
		if err != nil {
			if !isRuntimeException(err) {
				return nil, err
			}
			g.io.WriteError("<warning>"+err.Error()+"</warning>", true, io.Normal)
		}
	}

	m := gen.ClassMap()
	classes := php.NewArrayCap(m.Count())
	for class, path := range m.Map() {
		classes.Set(class, path)
	}
	loader.AddClassMap(classes)

	return loader, nil
}

// isRuntimeException reports whether err is a PHP \RuntimeException: one
// the class map generator throws as such, or a PcreException.
func isRuntimeException(err error) bool {
	if e, ok := errors.AsType[*classmap.Exception](err); ok {
		return e.IsRuntimeException()
	}
	_, ok := errors.AsType[*php.PcreError](err)

	return ok
}
