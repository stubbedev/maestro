// Ports src/Composer/Package/Locker.php.

// Package locker ports Composer\Package\Locker: reading and writing the
// project's lock file (composer.lock). It lives outside internal/pkg
// because it needs the repositories.
package locker

import (
	"crypto/md5" //nolint:gosec // the lock file's hashes are md5, as Composer computes them
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/json/jsonlint"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
	"github.com/stubbedev/maestro/internal/util/vcs"
)

// Locker ports Composer\Package\Locker. It is not safe for concurrent
// use.
type Locker struct {
	lockFile            repository.JSONFile
	installationManager repository.InstallationManager
	hash                string
	contentHash         string
	loader              *loader.ArrayLoader
	process             vcs.Process
	lockDataCache       *php.Array
	virtualFileWritten  bool
	// lockRead is what the lock file held when LockData decoded it, with
	// the hashes IsFresh compares; nil when the file cannot tell.
	lockRead *lockRead
}

// lockRead is a lock file's content and its hashes ("content-hash",
// "hash").
type lockRead struct {
	content           string
	contentHash, hash any
}

// rereadableFile is a lock file that can tell what it last decoded and
// skip decoding it again (*json.File).
type rereadableFile interface {
	Content() (string, bool)
	ReadIfChanged(content string) (data any, changed bool, err error)
}

// New ports new Locker($io, $lockFile, $installationManager,
// $composerFileContents, $process): composerFileContents is composer.json's
// content, whose hashes the lock file records. A nil process is a new
// ProcessExecutor; it runs git and hg to date dev packages.
func New(out io.IO, lockFile repository.JSONFile, installationManager repository.InstallationManager, composerFileContents string, process vcs.Process) (*Locker, error) {
	contentHash, err := GetContentHash(composerFileContents)
	if err != nil {
		return nil, err
	}
	if process == nil {
		process = http.NewProcessExecutor(out)
	}

	return &Locker{
		lockFile:            lockFile,
		installationManager: installationManager,
		hash:                md5Hex(composerFileContents),
		contentHash:         contentHash,
		loader:              loader.NewArrayLoader(nil, true),
		process:             process,
	}, nil
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // see the import

	return hex.EncodeToString(sum[:])
}

// JSONFile ports getJsonFile.
func (l *Locker) JSONFile() repository.JSONFile { return l.lockFile }

// relevantKeys are the composer.json keys the content hash covers.
var relevantKeys = [...]string{
	"name",
	"version",
	"require",
	"require-dev",
	"conflict",
	"replace",
	"provide",
	"minimum-stability",
	"prefer-stable",
	"repositories",
	"extra",
}

// GetContentHash ports Locker::getContentHash: the md5 of the
// composer.json keys that matter for dependency resolution, sorted.
func GetContentHash(composerFileContents string) (string, error) {
	decoded, err := json.ParseJSON(composerFileContents, "composer.json")
	if err != nil {
		return "", err
	}
	content, ok := decoded.(*php.Array)
	if !ok {
		return "", &php.EngineError{Class: php.ClassTypeError, Message: "array_keys(): Argument #1 ($array) must be of type array, " + php.ZvalValueName(decoded) + " given"}
	}

	relevantContent := php.NewArray()
	for _, key := range relevantKeys {
		if v, ok := content.Get(key); ok {
			relevantContent.Set(key, v)
		}
	}
	if cfg, ok := content.GetArray("config"); ok {
		if platform, _ := cfg.Get("platform"); platform != nil {
			relevantContent.Set("config", php.ArrayOf("platform", platform))
		}
	}

	php.Ksort(relevantContent, php.SortRegular)

	encoded, err := json.Encode(relevantContent, 0, json.IndentDefault)
	if err != nil {
		return "", err
	}

	return md5Hex(encoded), nil
}

// IsLocked ports Locker::isLocked: whether a lock file with packages
// exists.
func (l *Locker) IsLocked() (bool, error) {
	if !l.virtualFileWritten && !l.lockFile.Exists() {
		return false, nil
	}

	data, err := l.LockData()
	if err != nil {
		return false, err
	}
	v, _ := data.Get("packages")

	return v != nil, nil
}

// IsFresh ports Locker::isFresh: whether the lock file matches
// composer.json, by content hash (or, in old lock files, file hash).
func (l *Locker) IsFresh() (bool, error) {
	contentHash, hash, err := l.lockHashes()
	if err != nil {
		return false, err
	}

	if php.ToBool(contentHash) {
		// There is a content hash key, use that instead of the file hash
		return contentHash == l.contentHash, nil
	}

	// BC support for old lock files without content-hash
	if php.ToBool(hash) {
		return hash == l.hash, nil
	}

	// should not be reached unless the lock file is corrupted, so assume it's out of date
	return false, nil
}

// lockHashes reads the lock file for IsFresh: its "content-hash" and
// "hash". The content LockData decoded is not decoded again (deliberate
// deviation 3): the file is read and compared with it.
func (l *Locker) lockHashes() (contentHash, hash any, err error) {
	var decoded any

	if f, ok := l.lockFile.(rereadableFile); ok && l.lockRead != nil {
		data, changed, err := f.ReadIfChanged(l.lockRead.content)
		if err != nil {
			return nil, nil, err
		}

		if !changed {
			return l.lockRead.contentHash, l.lockRead.hash, nil
		}

		decoded = data
	} else if decoded, err = l.lockFile.Read(); err != nil {
		return nil, nil, err
	}

	lock, _ := decoded.(*php.Array)

	return lockValue(lock, "content-hash"), lockValue(lock, "hash"), nil
}

func lockValue(lock *php.Array, key string) any {
	if lock == nil {
		return nil
	}
	v, _ := lock.Get(key)

	return v
}

// LockedRepository ports Locker::getLockedRepository: the locked
// packages, with the dev ones when withDevReqs is set.
func (l *Locker) LockedRepository(withDevReqs bool) (*repository.LockArrayRepository, error) {
	lockData, err := l.LockData()
	if err != nil {
		return nil, err
	}
	packages, err := repository.NewLockArrayRepository(nil)
	if err != nil {
		return nil, err
	}

	// $lockedPackages = $lockData['packages'] (untyped: a value that is
	// not an array fails array_merge() or is no valid lock)
	rawPackages, ok := lockData.Get("packages")
	if !ok {
		return nil, &util.ErrorException{Message: `Undefined array key "packages"`}
	}
	if withDevReqs {
		dev, _ := lockData.Get("packages-dev")
		if dev == nil {
			return nil, &util.RuntimeError{Message: "The lock file does not contain require-dev information, run install with the --no-dev option or delete it and run composer update to generate a new lock file."}
		}
		for i, v := range []any{rawPackages, dev} {
			if _, ok := v.(*php.Array); !ok {
				return nil, &php.EngineError{Class: php.ClassTypeError, Message: "array_merge(): Argument #" + strconv.Itoa(i+1) + " must be of type array, " + php.ZvalValueName(v) + " given"}
			}
		}
		a, _ := rawPackages.(*php.Array) // both checked above
		b, _ := dev.(*php.Array)
		rawPackages = php.ArrayMerge(a, b)
	}

	// empty($lockedPackages)
	if !php.ToBool(rawPackages) {
		return packages, nil
	}

	// isset($lockedPackages[0]['name']) is false for anything but an array
	lockedPackages, isArray := rawPackages.(*php.Array)
	var first *php.Array
	if isArray {
		first, _ = lockedPackages.GetArray(0)
	}
	if lockValue(first, "name") == nil {
		return nil, &util.RuntimeError{Message: `Your composer.lock is invalid. Run "composer update" to generate a new one.`}
	}

	packageByName := make(map[string]pkg.PackageInterface)
	for _, info := range lockedPackages.All() {
		data, ok := info.(*php.Array)
		if !ok {
			return nil, &php.EngineError{Class: php.ClassTypeError, Message: `Composer\Package\Loader\ArrayLoader::load(): Argument #1 ($config) must be of type array, ` + php.ZvalValueName(info) + " given"}
		}
		p, err := l.loader.Load(data, pkg.ClassCompletePackage)
		if err != nil {
			return nil, err
		}
		if err := packages.AddPackage(p); err != nil {
			return nil, err
		}
		packageByName[p.Name()] = p

		if alias, ok := p.(pkg.Alias); ok {
			packageByName[alias.AliasOf().Name()] = alias.AliasOf()
		}
	}

	if aliases, _ := lockData.Get("aliases"); aliases != nil {
		list, ok := aliases.(*php.Array)
		if !ok {
			return nil, &util.ErrorException{Message: "foreach() argument must be of type array|object, " + php.ZvalValueName(aliases) + " given"}
		}
		for _, alias := range list.All() {
			// isset($packageByName[$alias['package']])
			name, err := lockOffset(alias, "package")
			if err != nil {
				return nil, err
			}
			if _, isArray := name.(*php.Array); isArray {
				return nil, &php.EngineError{Class: php.ClassTypeError, Message: "Cannot access offset of type array in isset or empty"}
			}
			p, ok := packageByName[php.ToKey(name).String()]
			if !ok {
				continue
			}
			// new CompleteAliasPackage($packageByName[...],
			// $alias['alias_normalized'], $alias['alias'])
			var args [2]string
			for i, key := range []string{"alias_normalized", "alias"} {
				v, err := lockOffset(alias, key)
				if err != nil {
					return nil, err
				}
				str, ok := v.(string)
				if !ok {
					param := [2]string{"version", "prettyVersion"}[i]
					return nil, pkg.ArgumentTypeError(`Composer\Package\CompleteAliasPackage::__construct`, i+2, param, "string", v)
				}
				args[i] = str
			}
			complete, ok := p.(pkg.CompletePackageInterface)
			if !ok {
				return nil, &php.EngineError{Class: php.ClassTypeError, Message: `Composer\Package\CompleteAliasPackage::__construct(): Argument #1 ($aliasOf) must be of type Composer\Package\CompletePackage, ` + p.Class() + " given"}
			}
			aliasPkg := pkg.NewCompleteAliasPackage(complete, args[0], args[1])
			aliasPkg.SetRootPackageAlias(true)
			if err := packages.AddPackage(aliasPkg); err != nil {
				return nil, err
			}
		}
	}

	return packages, nil
}

// DevPackageNames ports Locker::getDevPackageNames: the names of the
// packages installed through require-dev.
func (l *Locker) DevPackageNames() ([]string, error) {
	lockData, err := l.LockData()
	if err != nil {
		return nil, err
	}
	var names []string
	dev, _ := lockData.Get("packages-dev")
	if dev == nil {
		return names, nil
	}
	list, ok := dev.(*php.Array)
	if !ok {
		return nil, &util.ErrorException{Message: "foreach() argument must be of type array|object, " + php.ZvalValueName(dev) + " given"}
	}
	for _, p := range list.All() {
		// strtolower($package['name']) under strict_types
		name, err := lockOffset(p, "name")
		if err != nil {
			return nil, err
		}
		s, ok := name.(string)
		if !ok {
			return nil, pkg.ArgumentTypeError("strtolower", 1, "string", "string", name)
		}
		names = append(names, php.Strtolower(s))
	}

	return names, nil
}

// PlatformRequirements ports Locker::getPlatformRequirements: the
// platform requirements recorded in the lock file, with require-dev's
// when withDevReqs is set.
func (l *Locker) PlatformRequirements(withDevReqs bool) (pkg.Links, error) {
	lockData, err := l.LockData()
	if err != nil {
		return pkg.Links{}, err
	}
	var requirements pkg.Links

	if platform, ok := lockData.GetArray("platform"); ok && platform.Len() > 0 {
		if requirements, err = l.loader.ParseLinks("__root__", "1.0.0", pkg.TypeRequire, platform); err != nil {
			return pkg.Links{}, err
		}
	}

	if platformDev, ok := lockData.GetArray("platform-dev"); withDevReqs && ok && platformDev.Len() > 0 {
		devRequirements, err := l.loader.ParseLinks("__root__", "1.0.0", pkg.TypeRequire, platformDev)
		if err != nil {
			return pkg.Links{}, err
		}

		requirements = repository.MergeLinks(requirements, devRequirements)
	}

	return requirements, nil
}

// MinimumStability ports Locker::getMinimumStability.
func (l *Locker) MinimumStability() (string, error) {
	lockData, err := l.LockData()
	if err != nil {
		return "", err
	}
	v, _ := lockData.Get("minimum-stability")
	switch v := v.(type) {
	case nil:
		return "stable", nil
	case string:
		return v, nil
	}

	return "", returnTypeError("getMinimumStability", "string", v)
}

// returnTypeError is the TypeError of Locker::<method>() returning a value
// of the lock file that its return type (under strict_types) rejects.
func returnTypeError(method, typ string, v any) error {
	return &php.EngineError{Class: php.ClassTypeError, Message: `Composer\Package\Locker::` + method + "(): Return value must be of type " + typ + ", " + php.ZvalValueName(v) + " returned"}
}

// StabilityFlags ports Locker::getStabilityFlags.
func (l *Locker) StabilityFlags() (*php.Array, error) {
	return l.arrayOrEmpty("getStabilityFlags", "stability-flags")
}

// PreferStable ports Locker::getPreferStable; ok false is null (old lock
// files lack the key).
func (l *Locker) PreferStable() (value, ok bool, err error) {
	return l.optionalBool("getPreferStable", "prefer-stable")
}

// PreferLowest ports Locker::getPreferLowest; ok false is null.
func (l *Locker) PreferLowest() (value, ok bool, err error) {
	return l.optionalBool("getPreferLowest", "prefer-lowest")
}

// PlatformOverrides ports Locker::getPlatformOverrides.
func (l *Locker) PlatformOverrides() (*php.Array, error) {
	return l.arrayOrEmpty("getPlatformOverrides", "platform-overrides")
}

// Aliases ports Locker::getAliases: a list of ['package' => ...,
// 'version' => ..., 'alias' => ..., 'alias_normalized' => ...].
func (l *Locker) Aliases() (*php.Array, error) {
	return l.arrayOrEmpty("getAliases", "aliases")
}

// PluginAPI ports Locker::getPluginApi: the lock file's
// plugin-api-version as it is, which need not be a string (the method has
// no return type), or "1.1.0" when it is missing or null.
func (l *Locker) PluginAPI() (any, error) {
	lockData, err := l.LockData()
	if err != nil {
		return nil, err
	}
	if v, ok := lockData.Get("plugin-api-version"); ok && v != nil {
		return v, nil
	}

	return "1.1.0", nil
}

// arrayOrEmpty is `return $lockData[key] ?? [];` of an array-typed
// method.
func (l *Locker) arrayOrEmpty(method, key string) (*php.Array, error) {
	lockData, err := l.LockData()
	if err != nil {
		return nil, err
	}
	v, _ := lockData.Get(key)
	switch v := v.(type) {
	case nil:
		return php.NewArray(), nil
	case *php.Array:
		return v, nil
	}

	return nil, returnTypeError(method, "array", v)
}

// optionalBool is `return $lockData[key] ?? null;` of a ?bool method.
func (l *Locker) optionalBool(method, key string) (value, ok bool, err error) {
	lockData, err := l.LockData()
	if err != nil {
		return false, false, err
	}
	v, _ := lockData.Get(key)
	switch v := v.(type) {
	case nil:
		return false, false, nil
	case bool:
		return v, true, nil
	}

	return false, false, returnTypeError(method, "?bool", v)
}

// LockData ports Locker::getLockData: the decoded lock file, cached.
func (l *Locker) LockData() (*php.Array, error) {
	if l.lockDataCache != nil {
		return l.lockDataCache, nil
	}

	if !l.lockFile.Exists() {
		return nil, &util.LogicError{Message: "No lockfile found. Unable to read locked packages"}
	}

	decoded, err := l.lockFile.Read()
	if err != nil {
		return nil, err
	}
	data, ok := decoded.(*php.Array)
	if !ok {
		// a return type error is raised at the return statement
		return nil, &php.EngineError{Class: php.ClassTypeError, Message: `Composer\Package\Locker::getLockData(): Return value must be of type array, ` + php.ZvalValueName(decoded) + " returned"}
	}
	l.lockDataCache = data

	l.lockRead = nil
	if f, ok := l.lockFile.(rereadableFile); ok {
		if content, ok := f.Content(); ok {
			l.lockRead = &lockRead{content: content, contentHash: lockValue(data, "content-hash"), hash: lockValue(data, "hash")}
		}
	}

	return data, nil
}

// LockDataInput are the arguments of Locker::setLockData.
type LockDataInput struct {
	Packages []pkg.PackageInterface
	// DevPackages are the dev packages, or null (installed without --dev),
	// which writes "packages-dev": null where [] writes an empty list.
	DevPackages php.Nullable[[]pkg.PackageInterface]
	// PlatformReqs and PlatformDevReqs are package name => constraint.
	PlatformReqs    *php.Array
	PlatformDevReqs *php.Array
	// Aliases are the root aliases (RootPackage::getAliases).
	Aliases          *php.Array
	MinimumStability string
	// StabilityFlags are package name => BasePackage::STABILITY_*.
	StabilityFlags *php.Array
	PreferStable   bool
	PreferLowest   bool
	// PlatformOverrides is config.platform: name => version or false.
	PlatformOverrides *php.Array
}

func orEmpty(a *php.Array) *php.Array {
	if a == nil {
		return php.NewArray()
	}

	return a
}

// SetLockData ports Locker::setLockData: it writes the lock file unless
// it already holds this data; without write (tests, --dry-run) the data is
// only kept in memory. It reports whether the lock data changed.
func (l *Locker) SetLockData(in LockDataInput, write bool) (bool, error) {
	// keep old default branch names normalized to DEFAULT_BRANCH_ALIAS for BC as that is how Composer 1 outputs the lock file
	// when loading the lock file the version is anyway ignored in Composer 2, so it has no adverse effect
	aliases := php.NewArray()
	for _, v := range orEmpty(in.Aliases).All() {
		if alias, ok := v.(*php.Array); ok {
			if version, _ := alias.GetString("version"); version == "dev-master" || version == "dev-trunk" || version == "dev-default" {
				alias = alias.Clone()
				alias.Set("version", pkg.DefaultBranchAlias)
			}
			v = alias
		}
		aliases.Append(v)
	}

	packages, err := l.lockPackages(in.Packages)
	if err != nil {
		return false, err
	}

	lock := php.ArrayOf(
		"_readme", php.ListOf(
			"This file locks the dependencies of your project to a known state",
			"Read more about it at https://getcomposer.org/doc/01-basic-usage.md#installing-dependencies",
			"This file is @gener"+"ated automatically",
		),
		"content-hash", l.contentHash,
		"packages", packages,
		"packages-dev", nil,
		"aliases", aliases,
		"minimum-stability", in.MinimumStability,
		"stability-flags", orEmpty(in.StabilityFlags).Clone(),
		"prefer-stable", in.PreferStable,
		"prefer-lowest", in.PreferLowest,
	)

	if dev, ok := in.DevPackages.Get(); ok {
		devPackages, err := l.lockPackages(dev)
		if err != nil {
			return false, err
		}
		lock.Set("packages-dev", devPackages)
	}

	lock.Set("platform", orEmpty(in.PlatformReqs))
	lock.Set("platform-dev", orEmpty(in.PlatformDevReqs))
	if overrides := orEmpty(in.PlatformOverrides); overrides.Len() > 0 {
		lock.Set("platform-overrides", overrides)
	}
	lock.Set("plugin-api-version", repository.PluginAPIVersion)

	lock = fixupJSONDataType(lock)

	isLocked, err := l.IsLocked()
	if err != nil {
		if _, ok := errors.AsType[*jsonlint.ParsingError](err); !ok {
			return false, err
		}
		isLocked = false
	}
	if isLocked {
		current, err := l.LockData()
		if err != nil {
			return false, err
		}
		if php.StrictEquals(lock, current) {
			return false, nil
		}
	}

	if write {
		if err := l.lockFile.Write(lock, json.DefaultEncodeFlags); err != nil {
			return false, err
		}
		l.lockDataCache = nil
		l.virtualFileWritten = false
	} else {
		l.virtualFileWritten = true
		encoded, err := json.EncodeDefault(lock)
		if err != nil {
			return false, err
		}
		decoded, err := json.ParseJSON(encoded, "")
		if err != nil {
			return false, err
		}
		l.lockDataCache, _ = decoded.(*php.Array)
	}

	return true, nil
}

// UpdateHash ports Locker::updateHash: it records the content hash of the
// composer.json at composerJSONPath in the lock file, in place, keeping the
// lock file's mtime. dataProcessor, when set, can change the lock data
// before it is written. Use it only after composer.json changes that
// cannot affect dependency resolution.
func (l *Locker) UpdateHash(composerJSONPath string, dataProcessor func(lockData *php.Array) *php.Array) error {
	contents, err := os.ReadFile(composerJSONPath)
	if err != nil {
		return &util.RuntimeError{Message: "Unable to read " + composerJSONPath + " contents to update the lock file hash."}
	}

	var lockMtime time.Time
	info, statErr := os.Stat(l.lockFile.Path())
	if statErr == nil {
		lockMtime = info.ModTime()
	}
	decoded, err := l.lockFile.Read()
	if err != nil {
		return err
	}
	lockData, ok := decoded.(*php.Array)
	if !ok {
		return &util.ErrorException{Message: "Cannot use a scalar value as an array"}
	}
	contentHash, err := GetContentHash(string(contents))
	if err != nil {
		return err
	}
	lockData.Set("content-hash", contentHash)
	if dataProcessor != nil {
		lockData = dataProcessor(lockData)
	}

	if err := l.lockFile.Write(fixupJSONDataType(lockData), json.DefaultEncodeFlags); err != nil {
		return err
	}
	l.lockDataCache = nil
	l.virtualFileWritten = false
	if statErr == nil {
		// touch() sets both times; a failure is silenced as with @touch
		_ = os.Chtimes(l.lockFile.Path(), lockMtime.Truncate(time.Second), lockMtime.Truncate(time.Second))
	}

	return nil
}

// fixupJSONDataType ports Locker::fixupJsonDataType: empty maps become
// objects and the stability flags are sorted.
func fixupJSONDataType(lockData *php.Array) *php.Array {
	for _, key := range [...]string{"stability-flags", "platform", "platform-dev"} {
		if v, ok := lockData.GetArray(key); ok && v.Len() == 0 {
			lockData.Set(key, php.NewObject())
		}
	}

	if flags, ok := lockData.GetArray("stability-flags"); ok {
		php.Ksort(flags, php.SortRegular)
	}

	return lockData
}

// lockPackages ports Locker::lockPackages: the lock file entries of the
// packages (aliases left out), sorted by name and version.
func (l *Locker) lockPackages(packages []pkg.PackageInterface) (*php.Array, error) {
	var locked []*php.Array
	var arrayDumper dumper.ArrayDumper

	for _, p := range packages {
		if _, ok := p.(pkg.Alias); ok {
			continue
		}

		if p.PrettyName() == "" || p.PrettyName() == "0" || p.PrettyVersion() == "" || p.PrettyVersion() == "0" {
			return nil, &util.LogicError{Message: `Package "` + p.String() + `" has no version or name and can not be locked`}
		}

		spec, err := arrayDumper.Dump(p)
		if err != nil {
			return nil, err
		}
		spec.Delete("version_normalized")

		// always move time to the end of the package definition
		t, _ := spec.Get("time")
		spec.Delete("time")
		if p.IsDev() && p.InstallationSource().S == "source" {
			// use the exact commit time of the current reference if it's a dev package
			packageTime, err := l.packageTime(p)
			if err != nil {
				return nil, err
			}
			if packageTime != "" {
				t = packageTime
			}
		}
		if t != nil {
			spec.Set("time", t)
		}

		spec.Delete("installation-source")

		locked = append(locked, spec)
	}

	php.SortSlice(locked, func(a, b *php.Array) int {
		an, _ := a.Get("name")
		bn, _ := b.Get("name")
		if c := php.Strcmp(php.ToString(an), php.ToString(bn)); c != 0 {
			return c
		}

		// If it is the same package, compare the versions to make the order deterministic
		av, _ := a.Get("version")
		bv, _ := b.Get("version")

		return php.Strcmp(php.ToString(av), php.ToString(bv))
	})

	list := php.NewArrayCap(len(locked))
	for _, spec := range locked {
		list.Append(spec)
	}

	return list, nil
}

var (
	gitTimestamp = php.MustCompile(`{^\s*\d+\s*$}`)
	hgTimestamp  = php.MustCompile(`{^\s*(\d+)\s*}`)
)

// packageTime ports Locker::getPackageTime: the date of the package's
// source reference in its VCS checkout (DATE_RFC3339, UTC), "" for null.
func (l *Locker) packageTime(p pkg.PackageInterface) (string, error) {
	path, ok, err := l.installationManager.InstallPath(p)
	if err != nil || !ok {
		return "", err
	}
	path, ok = util.RealpathOK(path)
	sourceType := p.SourceType().S
	if !ok || path == "" || (sourceType != "git" && sourceType != "hg") {
		return "", nil
	}

	sourceRef := p.SourceReference().S
	if !php.ToBool(sourceRef) {
		sourceRef = p.DistReference().S
	}

	var timestamp string
	switch sourceType {
	case "git":
		if err := vcs.CleanEnv(l.process); err != nil {
			return "", err
		}

		flags, err := vcs.GetNoShowSignatureFlags(l.process)
		if err != nil {
			return "", err
		}
		command, err := vcs.BuildRevListCommand(l.process, append([]string{"-n1", "--format=%ct", sourceRef}, flags...))
		if err != nil {
			return "", err
		}
		var output string
		code, err := l.process.Execute(util.Cmd(command...), &output, path)
		if err != nil {
			return "", err
		}
		if code == 0 {
			parsed, err := vcs.ParseRevListOutput(output, l.process)
			if err != nil {
				return "", err
			}
			parsed = php.Trim(parsed)
			if matched, err := gitTimestamp.IsMatch(parsed); err != nil {
				return "", err
			} else if matched {
				timestamp = php.Trim(parsed)
			}
		}

	case "hg":
		var output string
		code, err := l.process.Execute(util.Cmd("hg", "log", "--template", "{date|hgdate}", "-r", sourceRef), &output, path)
		if err != nil {
			return "", err
		}
		if code == 0 {
			if m, err := hgTimestamp.Match(output); err != nil {
				return "", err
			} else if m != nil {
				timestamp = m.Get(1)
			}
		}
	}

	if timestamp == "" {
		return "", nil
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return "", &loader.DateTimeError{Time: "@" + timestamp, Message: "Failed to parse time string (@" + timestamp + ")"}
	}

	return time.Unix(seconds, 0).UTC().Format(dumper.RFC3339), nil
}

// MissingRequirementInfo ports Locker::getMissingRequirementInfo: the
// messages explaining which of the root package's requirements the lock
// file does not satisfy (none when it satisfies them all).
func (l *Locker) MissingRequirementInfo(root pkg.RootPackageInterface, includeDev bool) ([]string, error) {
	var info []string
	missingRequirements := false

	type set struct {
		withDev     bool
		method      string
		description string
	}
	sets := []set{{false, pkg.TypeRequire, "Required"}}
	if includeDev {
		sets = append(sets, set{true, pkg.TypeDevRequire, "Required (in require-dev)"})
	}
	clone, ok := pkg.Clone(root).(pkg.RootPackageInterface)
	if !ok {
		return nil, &util.LogicError{Message: "a clone of the root package is a root package"}
	}
	rootRepo, err := repository.NewRootPackageRepository(clone)
	if err != nil {
		return nil, err
	}

	for _, s := range sets {
		lockedRepo, err := l.LockedRepository(s.withDev)
		if err != nil {
			return nil, err
		}
		installedRepo, err := repository.NewInstalledRepository([]repository.RepositoryInterface{lockedRepo, rootRepo})
		if err != nil {
			return nil, err
		}

		for link := range pkg.LinksByMethod(root, s.method).Values() {
			if pkg.IsPlatformPackage(link.Target()) {
				continue
			}
			prettyConstraint, err := link.PrettyConstraint()
			if err != nil {
				return nil, err
			}
			if prettyConstraint == "self.version" {
				continue
			}
			matching, err := installedRepo.FindPackagesWithReplacersAndProviders(link.Target(), link.Constraint())
			if err != nil {
				return nil, err
			}
			if len(matching) > 0 {
				continue
			}
			results, err := installedRepo.FindPackagesWithReplacersAndProviders(link.Target(), nil)
			if err != nil {
				return nil, err
			}

			if len(results) > 0 {
				description, err := providerDescription(results[0], link.Target())
				if err != nil {
					return nil, err
				}
				info = append(info, "- "+s.description+` package "`+link.Target()+`" is in the lock file as "`+description+`" but that does not satisfy your constraint "`+prettyConstraint+`".`)
			} else {
				info = append(info, "- "+s.description+` package "`+link.Target()+`" is not present in the lock file.`)
			}
			missingRequirements = true
		}
	}

	if missingRequirements {
		info = append(info,
			"This usually happens when composer files are incorrectly merged or the composer.json file is manually edited.",
			"Read more about correctly resolving merge conflicts https://getcomposer.org/doc/articles/resolving-merge-conflicts.md",
			"and prefer using the \"require\" command over editing the composer.json file directly https://getcomposer.org/doc/03-cli.md#require-r",
		)
	}

	return info, nil
}

// providerDescription is how getMissingRequirementInfo describes the
// package found for target: its version, or how it replaces or provides
// target.
func providerDescription(provider pkg.PackageInterface, target string) (string, error) {
	description := provider.PrettyVersion()
	if provider.Name() == target {
		return description, nil
	}
	for _, m := range [...]struct {
		links pkg.Links
		text  string
	}{{provider.Replaces(), "replaced as "}, {provider.Provides(), "provided as "}} {
		for providerLink := range m.links.Values() {
			if providerLink.Target() == target {
				constraint, err := providerLink.PrettyConstraint()
				if err != nil {
					return "", err
				}

				return m.text + constraint + " by " + provider.PrettyName() + " " + provider.PrettyVersion(), nil
			}
		}
	}

	return description, nil
}

// lockOffset is $entry[$key] read (not in isset()) in Locker.php:
// a missing key is the "Undefined array key" warning, an entry that is a
// string the TypeError of a string offset, any other scalar PHP's "Trying
// to access array offset" warning (null for it).
func lockOffset(entry any, key string) (any, error) {
	switch e := entry.(type) {
	case *php.Array:
		v, ok := e.Get(key)
		if !ok {
			return nil, &util.ErrorException{Message: `Undefined array key "` + key + `"`}
		}

		return v, nil
	case string:
		return nil, &php.EngineError{Class: php.ClassTypeError, Message: "Cannot access offset of type string on string"}
	}

	return nil, &util.ErrorException{Message: "Trying to access array offset on " + php.ZvalValueName(entry)}
}
