// Ports src/Composer/Package/Archiver/ArchiveManager.php and
// ArchiverInterface.php.

package archiver

import (
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // hash('sha1') names archives, as in Composer
	"encoding/hex"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Archiver ports ArchiverInterface.
type Archiver interface {
	// Archive creates an archive of the sources directory at target in
	// format, leaving out the files the exclude patterns match (and,
	// unless ignoreFilters, .gitattributes export-ignore'd files), and
	// returns the path of the written archive.
	Archive(sources, target, format string, excludes []string, ignoreFilters bool) (string, error)
	// Supports reports whether the archiver writes format for packages
	// of the source type (null for none).
	Supports(format string, sourceType pkg.NullString) bool
}

// DownloadManager is the part of Composer\Downloader\DownloadManager the
// manager uses, as SyncHelper takes it: internal/downloader's
// `DownloadManager.Sync()` (that package imports this one).
type DownloadManager interface {
	Download(p pkg.PackageInterface, path string, prevPackage pkg.PackageInterface) (http.Waitable, error)
	Install(p pkg.PackageInterface, path string) (http.Waitable, error)
}

// Loop is SyncHelper::await's loop (*http.Loop).
type Loop interface {
	Await(promise http.Waitable, err error) error
}

// ArchiveManager ports ArchiveManager.
type ArchiveManager struct {
	downloadManager DownloadManager
	loop            Loop
	archivers       []Archiver
	overwriteFiles  bool
}

// NewArchiveManager ports ArchiveManager::__construct.
func NewArchiveManager(downloadManager DownloadManager, loop Loop) *ArchiveManager {
	return &ArchiveManager{downloadManager: downloadManager, loop: loop, overwriteFiles: true}
}

// AddArchiver ports ArchiveManager::addArchiver.
func (m *ArchiveManager) AddArchiver(archiver Archiver) { m.archivers = append(m.archivers, archiver) }

// SetOverwriteFiles ports ArchiveManager::setOverwriteFiles: whether
// existing archives are overwritten.
func (m *ArchiveManager) SetOverwriteFiles(overwriteFiles bool) *ArchiveManager {
	m.overwriteFiles = overwriteFiles

	return m
}

// FilenamePart is an entry of getPackageFilenameParts()'s array.
type FilenamePart struct {
	Key, Value string
}

var (
	archiveNameSanitizer = php.MustCompile(`#[^a-z0-9-_]#i`)
	sha1Reference        = php.MustCompile(`{^[a-f0-9]{40}$}`)
)

// PackageFilenameParts ports ArchiveManager::getPackageFilenameParts: the
// base name, then either the dist reference and type (for a 40 character
// hex reference) or the version and dist reference, then a hash of the
// source reference; null parts are left out and slashes become dashes.
func (m *ArchiveManager) PackageFilenameParts(p pkg.CompletePackageInterface) []FilenamePart {
	// Neither pattern runs in UTF mode or can backtrack catastrophically,
	// so they cannot fail.
	baseName := p.ArchiveName()
	if !baseName.Valid {
		sanitized, _, _ := archiveNameSanitizer.Replace(p.Name(), "-", -1)
		baseName = pkg.Str(sanitized)
	}

	type part struct {
		key   string
		value pkg.NullString
	}

	parts := []part{{"base", baseName}}

	distReference := p.DistReference()
	if isSha1, _ := sha1Reference.IsMatch(distReference.S); distReference.Valid && isSha1 {
		parts = append(parts, part{"dist_reference", distReference}, part{"dist_type", p.DistType()})
	} else {
		parts = append(parts, part{"version", pkg.Str(p.PrettyVersion())}, part{"dist_reference", distReference})
	}

	if sourceReference := p.SourceReference(); sourceReference.Valid {
		sum := sha1.Sum([]byte(sourceReference.S)) //nolint:gosec // see import
		parts = append(parts, part{"source_reference", pkg.Str(hex.EncodeToString(sum[:])[:6])})
	}

	out := make([]FilenamePart, 0, len(parts))

	for _, part := range parts {
		if part.value.Valid {
			out = append(out, FilenamePart{Key: part.key, Value: strings.ReplaceAll(part.value.S, "/", "-")})
		}
	}

	return out
}

// PackageFilenameFromParts ports ArchiveManager::getPackageFilenameFromParts.
func (m *ArchiveManager) PackageFilenameFromParts(parts []FilenamePart) string {
	values := make([]string, len(parts))
	for i, part := range parts {
		values[i] = part.Value
	}

	return strings.Join(values, "-")
}

// PackageFilename ports ArchiveManager::getPackageFilename: a distinct
// file name, without extension, for a version of a package.
func (m *ArchiveManager) PackageFilename(p pkg.CompletePackageInterface) string {
	return m.PackageFilenameFromParts(m.PackageFilenameParts(p))
}

// Archive ports ArchiveManager::archive: it creates an archive of the
// package in format in targetDir, named fileName (null for the
// PackageFilename) plus the format, and returns its path. The root
// package is archived from the working directory; any other package is
// downloaded into a temporary directory first, and its composer.json's
// archive name and excludes apply.
//
// As in Composer, the temporary directory is left behind when archiving
// fails.
func (m *ArchiveManager) Archive(p pkg.CompletePackageInterface, format, targetDir string, fileName pkg.NullString, ignoreFilters bool) (string, error) {
	if !php.ToBool(format) {
		return "", &util.InvalidArgumentError{Site: phperr.At("ArchiveManager.php", 150), Message: "Format must be specified"}
	}

	// Search for the most appropriate archiver
	var usableArchiver Archiver

	for _, archiver := range m.archivers {
		if archiver.Supports(format, p.SourceType()) {
			usableArchiver = archiver

			break
		}
	}

	// Checks the format/source type are supported before downloading the
	// package
	if usableArchiver == nil {
		return "", &util.RuntimeError{Site: phperr.At("ArchiveManager.php", 164), Message: "No archiver found to support " + format + " format"}
	}

	filesystem := util.NewFilesystem(nil)

	_, isRoot := p.(pkg.RootPackageInterface)

	var sourcePath string

	if isRoot {
		sourcePath = util.Realpath(".")
	} else {
		// Directory used to download the sources
		sourcePath = php.SysGetTempDir() + "/composer_archive" + randomHex(5)
		if err := util.EnsureDirectoryExists(sourcePath); err != nil {
			return "", err
		}

		// Download sources
		err := m.loop.Await(m.downloadManager.Download(p, sourcePath, nil))
		if err == nil {
			err = m.loop.Await(m.downloadManager.Install(p, sourcePath))
		}

		if err != nil {
			_, _ = filesystem.RemoveDirectory(sourcePath)

			return "", err
		}

		// Check exclude from downloaded composer.json
		if err := applyArchiveConfig(p, sourcePath+"/composer.json"); err != nil {
			return "", err
		}
	}

	supportedFormats := m.supportedFormats()

	packageNameParts := []FilenamePart{{Key: "base", Value: fileName.S}}
	if !fileName.Valid {
		packageNameParts = m.PackageFilenameParts(p)
	}

	packageName := m.PackageFilenameFromParts(packageNameParts)
	excludePatterns := buildExcludePatterns(packageNameParts, supportedFormats)

	// Archive filename
	if err := util.EnsureDirectoryExists(targetDir); err != nil {
		return "", err
	}

	target := util.Realpath(targetDir) + "/" + packageName + "." + format
	if err := util.EnsureDirectoryExists(util.Dirname(target)); err != nil {
		return "", err
	}

	if !m.overwriteFiles && fileExists(target) {
		return target, nil
	}

	// Create the archive
	tempTarget := php.SysGetTempDir() + "/composer_archive" + randomHex(5) + "." + format
	if err := util.EnsureDirectoryExists(util.Dirname(tempTarget)); err != nil {
		return "", err
	}

	archiveExcludes, err := stringList(p.ArchiveExcludes())
	if err != nil {
		return "", err
	}

	archivePath, err := usableArchiver.Archive(sourcePath, tempTarget, format, append(excludePatterns, archiveExcludes...), ignoreFilters)
	if err != nil {
		return "", err
	}

	if err := filesystem.Rename(archivePath, target); err != nil {
		return "", err
	}

	// cleanup temporary download
	if !isRoot {
		if _, err := filesystem.RemoveDirectory(sourcePath); err != nil {
			return "", err
		}
	}

	if _, err := filesystem.Remove(tempTarget); err != nil {
		return "", err
	}

	return target, nil
}

// applyArchiveConfig applies the archive name and excludes of a downloaded
// package's composer.json, when there is one.
func applyArchiveConfig(p pkg.CompletePackageInterface, composerJSONPath string) error {
	if !fileExists(composerJSONPath) {
		return nil
	}

	jsonFile, err := json.NewFile(composerJSONPath, nil, nil)
	if err != nil {
		return err
	}

	jsonData, err := jsonFile.Read()
	if err != nil {
		return err
	}

	// $jsonData['archive'][...] under empty(): anything that is not an
	// array reads as empty
	archive := arrayGet(jsonData, "archive")

	class := `Composer\Package\CompletePackage`
	if _, ok := p.(pkg.Alias); ok {
		class = `Composer\Package\CompleteAliasPackage`
	}

	if name := arrayGet(archive, "name"); php.ToBool(name) {
		s, ok := name.(string)
		if !ok {
			return pkg.ArgumentTypeError(class+"::setArchiveName", 1, "name", "?string", name)
		}

		p.SetArchiveName(pkg.Str(s))
	}

	if exclude := arrayGet(archive, "exclude"); php.ToBool(exclude) {
		a, ok := exclude.(*php.Array)
		if !ok {
			return pkg.ArgumentTypeError(class+"::setArchiveExcludes", 1, "excludes", "array", exclude)
		}

		p.SetArchiveExcludes(a)
	}

	return nil
}

// arrayGet is $a[$key] for an array a, else null.
func arrayGet(a any, key string) any {
	arr, ok := a.(*php.Array)
	if !ok {
		return nil
	}

	v, _ := arr.GetKey(php.StrKey(key))

	return v
}

// stringList is the exclude rules array as BaseExcludeFilter's strictly
// typed generatePattern(string $rule) accepts it.
func stringList(a *php.Array) ([]string, error) {
	var out []string

	for _, v := range a.Values() {
		s, ok := v.(string)
		if !ok {
			return nil, pkg.ArgumentTypeError(`Composer\Package\Archiver\BaseExcludeFilter::generatePattern`, 1, "rule", "string", v)
		}

		out = append(out, s)
	}

	return out, nil
}

// buildExcludePatterns ports ArchiveManager::buildExcludePatterns: the
// archives of this package (any version, when the name has more parts than
// the base) in each supported format.
func buildExcludePatterns(parts []FilenamePart, formats []string) []string {
	base := parts[0].Value
	if len(parts) > 1 {
		base += "-*"
	}

	patterns := make([]string, 0, len(formats))
	for _, format := range formats {
		patterns = append(patterns, base+"."+format)
	}

	return patterns
}

// supportedFormats ports ArchiveManager::getSupportedFormats: the formats
// of the archivers Composer knows, by class.
func (m *ArchiveManager) supportedFormats() []string {
	var formats []string

	for _, archiver := range m.archivers {
		switch archiver.(type) {
		case *ZipArchiver:
			formats = append(formats, "zip")
		case *PharArchiver:
			formats = append(formats, "zip", "tar", "tar.gz", "tar.bz2")
		}
	}

	// array_unique
	var unique []string

	for _, format := range formats {
		if !slices.Contains(unique, format) {
			unique = append(unique, format)
		}
	}

	return unique
}

// randomHex is bin2hex(random_bytes(n)).
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)

	return hex.EncodeToString(b)
}
