// Ports src/Composer/Repository/FilesystemRepository.php and
// InstalledFilesystemRepository.php.

package repository

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fspath"
)

// FilesystemRepository ports Composer\Repository\FilesystemRepository: a
// repository stored in a JSON file (vendor/composer/installed.json).
type FilesystemRepository struct {
	WritableArrayRepository
	file         JSONFile
	dumpVersions bool
	rootPackage  pkg.RootPackageInterface
	// FilesystemRepository's own private $devMode, read from the file.
	fsDevMode, fsDevModeKnown bool
	fsDevModeValue            any
	installedVersionsSink     func(versions *php.Array)
	// deferWrites and pending: see DeferWrites.
	deferWrites bool
	pending     *pendingWrite
	// preparedWrite: see PrepareWrite.
	preparedWrite *preparedWrite
}

// pendingWrite is a deferred Write: its arguments and the repository's
// packages when it was asked for.
type pendingWrite struct {
	devMode   bool
	im        InstallationManager
	canonical []pkg.PackageInterface
	packages  []pkg.PackageInterface
}

var _ WritableRepository = (*FilesystemRepository)(nil)

// NewFilesystemRepository ports new FilesystemRepository($repositoryFile,
// $dumpVersions, $rootPackage): with dumpVersions, write also dumps
// installed.php and InstalledVersions.php, which needs the root package.
func NewFilesystemRepository(file JSONFile, dumpVersions bool, rootPackage pkg.RootPackageInterface) (*FilesystemRepository, error) {
	r := &FilesystemRepository{}
	r.bind(r, r)
	if err := r.init(file, dumpVersions, rootPackage); err != nil {
		return nil, err
	}

	return r, nil
}

func (r *FilesystemRepository) init(file JSONFile, dumpVersions bool, rootPackage pkg.RootPackageInterface) error {
	r.file = file
	r.dumpVersions = dumpVersions
	r.rootPackage = rootPackage
	if dumpVersions && rootPackage == nil {
		return &util.InvalidArgumentError{Message: "Expected a root package instance if $dumpVersions is true"}
	}

	return nil
}

// SetInstalledVersionsSink sets the function write hands the installed.php
// data to, where Composer reloads its InstalledVersions class with it (the
// plugin runtime's copy of that class).
func (r *FilesystemRepository) SetInstalledVersionsSink(sink func(versions *php.Array)) {
	r.installedVersionsSink = sink
}

// Class returns the PHP class name.
func (r *FilesystemRepository) Class() string { return `Composer\Repository\FilesystemRepository` }

// RepoName ports getRepoName (ArrayRepository's).
func (r *FilesystemRepository) RepoName() string { return r.ArrayRepository.RepoName() }

// File returns the repository's JSON file.
func (r *FilesystemRepository) File() JSONFile { return r.file }

// DevMode ports FilesystemRepository::getDevMode: the "dev" flag of the
// file; ok false is null.
func (r *FilesystemRepository) DevMode() (devMode, ok bool) {
	return r.fsDevMode, r.fsDevModeKnown
}

// DevModeValue is getDevMode() when installed.json's "dev" is not a bool
// (DevMode gives its truthiness); nil otherwise.
func (r *FilesystemRepository) DevModeValue() any { return r.fsDevModeValue }

// initialize ports FilesystemRepository::initialize: it reads the file.
func (r *FilesystemRepository) initialize() error {
	r.baseInitialize()

	if !r.file.Exists() {
		return nil
	}

	packages, err := r.readPackageList()
	if err != nil {
		// catch (\Exception $e): a PHP \Error goes through
		if !phperr.InstanceOf(err, "Exception") {
			return err
		}

		return &InvalidRepositoryError{Message: "Invalid repository data in " + r.file.Path() + ", packages could not be loaded: [" + phperr.Class(err) + "] " + err.Error()}
	}

	arrayLoader := loader.NewArrayLoader(nil, true)
	for _, packageData := range packages.All() {
		data, ok := packageData.(*php.Array)
		if !ok {
			return pkg.ArgumentTypeError(`Composer\Package\Loader\ArrayLoader::load`, 1, "config", "array", packageData)
		}
		p, err := arrayLoader.Load(data, pkg.ClassCompletePackage)
		if err != nil {
			return err
		}
		if err := r.hooks.addPackage(p); err != nil {
			return err
		}
	}

	return nil
}

// readPackageList is the try block of initialize.
func (r *FilesystemRepository) readPackageList() (*php.Array, error) {
	decoded, err := r.file.Read()
	if err != nil {
		return nil, err
	}
	packages := decoded
	if data, ok := decoded.(*php.Array); ok {
		if v, _ := data.Get("packages"); v != nil {
			packages = v
		}
		if v, _ := data.Get("dev-package-names"); v != nil {
			names, ok := v.(*php.Array)
			if !ok {
				return nil, pkg.ArgumentTypeError(`Composer\Repository\WritableArrayRepository::setDevPackageNames`, 1, "devPackageNames", "array", v)
			}
			r.SetDevPackageNames(php.ToStrings(names))
		}
		if v, _ := data.Get("dev"); v != nil {
			// $this->devMode = $data['dev'] (an untyped property): a
			// value that is not a bool is kept for DevModeValue
			r.fsDevMode, r.fsDevModeKnown = php.ToBool(v), true
			if _, ok := v.(bool); !ok {
				r.fsDevModeValue = v
			}
		}
	}
	list, ok := packages.(*php.Array)
	if !ok {
		return nil, &util.UnexpectedValueError{Message: "Could not parse package list from the repository"}
	}

	return list, nil
}

// Reload ports FilesystemRepository::reload: the file is read again.
func (r *FilesystemRepository) Reload() error {
	r.unload()

	return r.hooks.initialize()
}

// Write ports FilesystemRepository::write: installed.json, and with
// dumpVersions installed.php and InstalledVersions.php next to it.
func (r *FilesystemRepository) Write(devMode bool, im InstallationManager) error {
	canonical, err := r.CanonicalPackages()
	if err != nil {
		return err
	}
	packages, err := r.Packages()
	if err != nil {
		return err
	}
	if r.deferWrites {
		r.pending = &pendingWrite{devMode, im, slices.Clone(canonical), slices.Clone(packages)}

		return nil
	}

	return r.write(devMode, im, canonical, packages)
}

// DeferWrites makes Write only record what it would write, until
// FlushWrites writes the last of it (deliberate deviation 3, speed).
// InstallationManager::executeBatch writes the repository after every
// operation, so installing n packages writes installed.json and
// installed.php n times, each with every package: O(n²) work whose
// intermediate states only code running between two of those writes can
// see. The installation manager defers them while it waits for a batch
// whose installers are all maestro's own (no plugin code runs until it
// flushes) and flushes when the wait ends, before anything else can look:
// the files end up exactly as the last write would have left them, as
// that write's packages are kept.
func (r *FilesystemRepository) DeferWrites() { r.deferWrites = true }

// FlushWrites ends DeferWrites, performing the last deferred Write.
func (r *FilesystemRepository) FlushWrites() error {
	r.deferWrites = false
	p := r.pending
	if p == nil {
		return nil
	}
	r.pending = nil

	return r.write(p.devMode, p.im, p.canonical, p.packages)
}

// write is Write with the repository's packages given.
func (r *FilesystemRepository) write(devMode bool, im InstallationManager, canonical, repoPackages []pkg.PackageInterface) error {
	// make sure the directory is created so we can realpath it
	// as realpath() does some additional normalizations with network paths that normalizePath does not
	// and we need to find shortest path correctly
	repoDir := php.Dirname(r.file.Path())
	if err := util.EnsureDirectoryExists(repoDir); err != nil {
		return err
	}
	repoDir = fspath.NormalizePath(util.Realpath(repoDir))

	in := writeInput{devMode, canonical, repoPackages, slices.Clone(r.devPackageNames), r.rootPackage, r.dumpVersions, repoDir}
	if out := r.takePreparedWrite(in, im); out != nil {
		return r.writeOutput(out)
	}
	out, err := in.build(func(p pkg.PackageInterface) (pkg.NullString, error) {
		return r.relativeInstallPath(im, p, repoDir)
	}, nil)
	if err != nil {
		return err
	}

	return r.writeOutput(out)
}

// writeInput is everything write's files are made of, but the install
// paths.
type writeInput struct {
	devMode         bool
	canonical       []pkg.PackageInterface
	repoPackages    []pkg.PackageInterface
	devPackageNames []string
	rootPackage     pkg.RootPackageInterface
	dumpVersions    bool
	repoDir         string
}

// writeOutput is what write writes: installed.json's data (encoded when
// it was built for a prepared write) and installed.php's.
type writeOutput struct {
	repoDir      string
	data         *php.Array
	encoded      string
	encodedOK    bool
	versions     *php.Array
	code         string
	versionsErr  error
	unchangedFns [2]func() bool // installed.json and installed.php hold encoded and code (prepared writes)
}

// build makes write's output. installPath is the install path to record
// for a package; encode, when given, encodes installed.json's data.
func (in writeInput) build(installPath func(pkg.PackageInterface) (pkg.NullString, error), encode func(data *php.Array) (string, error)) (*writeOutput, error) {
	pkgArrays := make([]*php.Array, 0, len(in.canonical))
	devNames := php.NewArray()
	installPaths := make(map[string]pkg.NullString, len(in.canonical))
	var arrayDumper dumper.ArrayDumper
	for _, p := range in.canonical {
		pkgArray, err := arrayDumper.Dump(p)
		if err != nil {
			return nil, err
		}
		installPath, err := installPath(p)
		if err != nil {
			return nil, err
		}
		installPaths[p.Name()] = installPath

		pkgArray.Set("install-path", installPath.Value())
		pkgArrays = append(pkgArrays, pkgArray)

		// only write to the files the names which are really installed, as we receive the full list
		// of dev package names before they get installed during composer install
		if slices.Contains(in.devPackageNames, p.Name()) {
			devNames.Append(p.Name())
		}
	}

	php.Sort(devNames, php.SortRegular)
	php.SortSlice(pkgArrays, func(a, b *php.Array) int {
		an, _ := a.GetString("name")
		bn, _ := b.GetString("name")

		return php.Strcmp(an, bn)
	})
	packages := php.NewArrayCap(len(pkgArrays))
	for _, a := range pkgArrays {
		packages.Append(a)
	}

	out := &writeOutput{repoDir: in.repoDir, data: php.ArrayOf("packages", packages, "dev", in.devMode, "dev-package-names", devNames)}
	if encode != nil {
		var err error
		out.encoded, err = encode(out.data)
		out.encodedOK = err == nil
	}

	if !in.dumpVersions {
		return out, nil
	}

	// errors here come after installed.json is written (writeOutput)
	versions, err := generateInstalledVersions(in.repoPackages, installPaths, in.devMode, in.repoDir, in.devPackageNames, in.rootPackage)
	if err != nil {
		out.versionsErr = err

		return out, nil
	}
	if out.code, err = dumpToPhpCode(versions, 0); err != nil {
		out.versionsErr = err

		return out, nil
	}
	out.code = "<?php return " + out.code + ";\n"
	out.versions = versions

	return out, nil
}

// writeOutput writes out's files.
func (r *FilesystemRepository) writeOutput(out *writeOutput) error {
	if out.unchangedFns[0] == nil || !out.unchangedFns[0]() {
		if err := r.file.Write(out.data, json.DefaultEncodeFlags); err != nil {
			return err
		}
	}

	if !r.dumpVersions {
		return nil
	}
	if out.versionsErr != nil {
		return out.versionsErr
	}

	if out.unchangedFns[1] == nil || !out.unchangedFns[1]() {
		if _, err := util.FilePutContentsIfModified(out.repoDir+"/installed.php", []byte(out.code)); err != nil {
			return err
		}
	}
	if _, err := util.FilePutContentsIfModified(out.repoDir+"/InstalledVersions.php", []byte(autoload.InstalledVersionsPHP)); err != nil {
		return err
	}

	// make sure the in memory state is up to date with on disk
	if r.installedVersionsSink != nil {
		r.installedVersionsSink(out.versions)
	}

	return nil
}

// relativeInstallPath is the install path write records for p: relative
// to repoDir, null when the package has none.
func (r *FilesystemRepository) relativeInstallPath(im InstallationManager, p pkg.PackageInterface, repoDir string) (pkg.NullString, error) {
	path, ok, err := im.InstallPath(p)
	if err != nil || !ok || path == "" {
		return pkg.NullString{}, err
	}
	if !fspath.IsAbsolutePath(path) {
		cwd, err := util.GetCwd(false)
		if err != nil {
			return pkg.NullString{}, err
		}
		path = cwd + "/" + path
	}
	shortest, err := util.FindShortestPath(repoDir, fspath.NormalizePath(path), true, false)
	if err != nil {
		return pkg.NullString{}, err
	}

	return pkg.Str(shortest), nil
}

// dumpToPhpCode ports FilesystemRepository::dumpToPhpCode: the array as
// PHP code, install paths relative to __DIR__.
func dumpToPhpCode(array *php.Array, level int) (string, error) {
	var b strings.Builder
	if err := appendPhpCode(&b, array, level); err != nil {
		return "", err
	}

	return b.String(), nil
}

func appendPhpCode(b *strings.Builder, array *php.Array, level int) error {
	b.WriteString("array(\n")
	level++

	for key, value := range array.All() {
		b.WriteString(strings.Repeat("    ", level))
		if key.IsInt() {
			b.WriteString(strconv.FormatInt(key.Int(), 10))
		} else {
			b.Write(php.AppendVarExport(nil, key.String()))
		}
		b.WriteString(" => ")

		switch v := value.(type) {
		case *php.Array:
			if v.Len() > 0 {
				if err := appendPhpCode(b, v, level); err != nil {
					return err
				}
			} else {
				b.WriteString("array(),\n")
			}
		case string:
			if key.IsString() && key.String() == "install_path" && !fspath.IsAbsolutePath(v) {
				b.WriteString("__DIR__ . ")
				v = "/" + v
			}
			b.Write(php.AppendVarExport(nil, v))
			b.WriteString(",\n")
		case bool:
			if v {
				b.WriteString("true,\n")
			} else {
				b.WriteString("false,\n")
			}
		case nil:
			b.WriteString("null,\n")
		default:
			return &util.UnexpectedValueError{Message: "Unexpected type " + php.TypeName(value)}
		}
	}

	b.WriteString(strings.Repeat("    ", level-1))
	b.WriteByte(')')
	if level-1 != 0 {
		b.WriteString(",\n")
	}

	return nil
}

// generateInstalledVersions ports FilesystemRepository::generateInstalledVersions:
// the data of installed.php.
// devPackageNames and rootPkg are the repository's.
func generateInstalledVersions(repoPackages []pkg.PackageInterface, installPaths map[string]pkg.NullString, devMode bool, repoDir string, devPackageNames []string, rootPkg pkg.RootPackageInterface) (*php.Array, error) {
	devPackages := make(map[string]struct{}, len(devPackageNames))
	for _, name := range devPackageNames {
		devPackages[name] = struct{}{}
	}
	if rootPkg == nil {
		return nil, &util.LogicError{Message: "It should not be possible to dump packages if no root package is given"}
	}
	packages := append(slices.Clone(repoPackages), pkg.PackageInterface(rootPkg))
	var rootPackage pkg.PackageInterface = rootPkg
	for {
		alias, ok := rootPackage.(*pkg.RootAliasPackage)
		if !ok {
			break
		}
		rootPackage = alias.AliasOf()
		packages = append(packages, rootPackage)
	}
	root, ok := rootPackage.(pkg.RootPackageInterface)
	if !ok {
		return nil, &util.LogicError{Message: "The root package alias does not alias a root package"}
	}

	rootData, err := dumpRootPackage(root, installPaths, devMode, repoDir, devPackages)
	if err != nil {
		return nil, err
	}
	all := php.NewArray()
	versions := php.ArrayOf("root", rootData, "versions", all)

	// add real installed packages
	for _, p := range packages {
		if _, ok := p.(pkg.Alias); ok {
			continue
		}
		data, err := dumpInstalledPackage(p, installPaths, repoDir, devPackages)
		if err != nil {
			return nil, err
		}
		all.Set(p.Name(), data)
	}

	// add provided/replaced packages
	for _, p := range packages {
		_, isDevPackage := devPackages[p.Name()]
		for _, l := range [...]struct {
			links pkg.Links
			key   string
		}{{p.Replaces(), "replaced"}, {p.Provides(), "provided"}} {
			for link := range l.links.Values() {
				// exclude platform replaces/provides as when they are really there we can not check for their presence
				if pkg.IsPlatformPackage(link.Target()) {
					continue
				}
				entry := all.ArrayAtOrCreate(link.Target())
				if v, _ := entry.Get("dev_requirement"); v == nil {
					entry.Set("dev_requirement", isDevPackage)
				} else if !isDevPackage {
					entry.Set("dev_requirement", false)
				}
				constraint, err := link.PrettyConstraint()
				if err != nil {
					return nil, err
				}
				if constraint == "self.version" {
					constraint = p.PrettyVersion()
				}
				list, _ := entry.GetArray(l.key)
				if list == nil || !php.InArray(constraint, list, true) {
					entry.ArrayAtOrCreate(l.key).Append(constraint)
				}
			}
		}
	}

	// add aliases
	for _, p := range packages {
		if _, ok := p.(pkg.Alias); !ok {
			continue
		}
		all.ArrayAtOrCreate(p.Name()).ArrayAtOrCreate("aliases").Append(p.PrettyVersion())
		if _, ok := p.(pkg.RootPackageInterface); ok {
			rootData.ArrayAtOrCreate("aliases").Append(p.PrettyVersion())
		}
	}

	php.Ksort(all, php.SortRegular)
	php.Ksort(versions, php.SortRegular)

	for _, entry := range all.All() {
		entry, ok := entry.(*php.Array)
		if !ok {
			continue
		}
		for _, key := range [...]string{"aliases", "replaced", "provided"} {
			if list, ok := entry.GetArray(key); ok {
				php.Sort(list, php.SortNatural)
			}
		}
	}

	return versions, nil
}

// dumpInstalledPackage ports FilesystemRepository::dumpInstalledPackage.
func dumpInstalledPackage(p pkg.PackageInterface, installPaths map[string]pkg.NullString, repoDir string, devPackages map[string]struct{}) (*php.Array, error) {
	var reference pkg.NullString
	if source := p.InstallationSource(); php.ToBool(source.S) {
		if source.S == "source" {
			reference = p.SourceReference()
		} else {
			reference = p.DistReference()
		}
	}
	if !reference.Valid {
		reference = p.SourceReference()
		if !php.ToBool(reference.S) {
			reference = p.DistReference()
		}
		if !php.ToBool(reference.S) {
			reference = pkg.NullString{}
		}
	}

	var installPath pkg.NullString
	if _, ok := p.(pkg.RootPackageInterface); ok {
		cwd, err := util.GetCwd(false)
		if err != nil {
			return nil, err
		}
		path, err := util.FindShortestPath(repoDir, fspath.NormalizePath(util.Realpath(cwd)), true, false)
		if err != nil {
			return nil, err
		}
		installPath = pkg.Str(path)
	} else {
		installPath = installPaths[p.Name()]
	}

	_, isDev := devPackages[p.Name()]

	return php.ArrayOf(
		"pretty_version", p.PrettyVersion(),
		"version", p.Version(),
		"reference", reference.Value(),
		"type", p.Type(),
		"install_path", installPath.Value(),
		"aliases", php.NewArray(),
		"dev_requirement", isDev,
	), nil
}

// dumpRootPackage ports FilesystemRepository::dumpRootPackage.
func dumpRootPackage(p pkg.RootPackageInterface, installPaths map[string]pkg.NullString, devMode bool, repoDir string, devPackages map[string]struct{}) (*php.Array, error) {
	data, err := dumpInstalledPackage(p, installPaths, repoDir, devPackages)
	if err != nil {
		return nil, err
	}
	get := func(k string) any { v, _ := data.Get(k); return v }

	return php.ArrayOf(
		"name", p.Name(),
		"pretty_version", get("pretty_version"),
		"version", get("version"),
		"reference", get("reference"),
		"type", get("type"),
		"install_path", get("install_path"),
		"aliases", get("aliases"),
		"dev", devMode,
	), nil
}

// InstalledFilesystemRepository ports
// Composer\Repository\InstalledFilesystemRepository: the installed
// packages (vendor/composer/installed.json).
type InstalledFilesystemRepository struct {
	FilesystemRepository
}

var _ InstalledRepositoryInterface = (*InstalledFilesystemRepository)(nil)

// NewInstalledFilesystemRepository ports new
// InstalledFilesystemRepository($repositoryFile, $dumpVersions, $rootPackage).
func NewInstalledFilesystemRepository(file JSONFile, dumpVersions bool, rootPackage pkg.RootPackageInterface) (*InstalledFilesystemRepository, error) {
	r := &InstalledFilesystemRepository{}
	r.bind(r, &r.FilesystemRepository)
	if err := r.init(file, dumpVersions, rootPackage); err != nil {
		return nil, err
	}

	return r, nil
}

// Class returns the PHP class name.
func (r *InstalledFilesystemRepository) Class() string {
	return `Composer\Repository\InstalledFilesystemRepository`
}

// RepoName ports InstalledFilesystemRepository::getRepoName.
func (r *InstalledFilesystemRepository) RepoName() string {
	return "installed " + r.FilesystemRepository.RepoName()
}

// IsFresh ports InstalledFilesystemRepository::isFresh: whether the file
// does not exist.
func (r *InstalledFilesystemRepository) IsFresh() (bool, error) { return !r.file.Exists(), nil }
