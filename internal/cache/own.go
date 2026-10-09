// Ports nothing: the caches maestro keeps of its own under Dir (deliberate
// deviations 1 and 3). docs/PORTING.md ("maestro's own caches") describes
// each; a test fails when that section and owned disagree.

package cache

import (
	"os"
	"path/filepath"
)

// Where each of maestro's own caches lives, relative to Dir. Every path
// maestro keeps under Dir is one of these, and each is in owned.
const (
	storePath           = "store/v1"
	decodedMetadataPath = "p2"
	decodedFilesPath    = "decoded"
	classMapParsePath   = "classmap/v1.bin"
	classMapRecordsPath = "classmap/records"
	platformProbesPath  = "platform"
	gitVersionPath      = "git-version"
	schemaMemoPath      = "schema/validated"
	caBundlesPath       = "cacert"
)

// Own is one of maestro's own caches.
type Own struct {
	// Path is where it lives, relative to Dir, with "/" separators.
	Path string
	// With is the cache directory setting whose clear-cache clears it too:
	// cache-files-dir, cache-repo-dir, or cache-dir for every other cache.
	With string
}

// owned are maestro's own caches.
var owned = []Own{
	{storePath, "cache-files-dir"},
	{decodedMetadataPath, "cache-repo-dir"},
	{decodedFilesPath, "cache-dir"},
	{classMapParsePath, "cache-dir"},
	{classMapRecordsPath, "cache-dir"},
	{platformProbesPath, "cache-dir"},
	{gitVersionPath, "cache-dir"},
	{schemaMemoPath, "cache-dir"},
	{caBundlesPath, "cache-dir"},
}

// Owned returns maestro's own caches.
func Owned() []Own { return append([]Own(nil), owned...) }

func path(rel string) string { return filepath.Join(Dir(), filepath.FromSlash(rel)) }

// Store is the package store's directory (internal/store, deviation 1).
// The version segment lets a later layout live next to this one.
func Store() string { return path(storePath) }

// DecodedMetadata is where the repository metadata files read from
// Composer's repository cache are kept decoded (internal/repository/
// composerrepo), one directory per version of their form.
func DecodedMetadata() string { return path(decodedMetadataPath) }

// DecodedFiles is where the large local JSON files read on most runs are
// kept decoded (vendor/composer/installed.json; json.UseDecodedFiles).
func DecodedFiles() string { return path(decodedFilesPath) }

// ClassMapParse is the file of the class map scans' parse results, by file
// contents (autoload.Generator.UseParseCacheFile).
func ClassMapParse() string { return path(classMapParsePath) }

// ClassMapRecords is the directory of the class maps of earlier scans,
// records of a project's dumps and class loaders
// (autoload.Generator.UseScanRecords).
func ClassMapRecords() string { return path(classMapRecordsPath) }

// PlatformProbes is the directory of php's probed platform, one entry per
// php binary and what it depends on (internal/platform, Linux and
// Windows).
func PlatformProbes() string { return path(platformProbesPath) }

// GitVersion is the directory of git's version, by git binary
// (vcs.UseVersionCache, Linux).
func GitVersion() string { return path(gitVersionPath) }

// SchemaMemo is the file of the documents that validated against
// Composer's schemas (json.UseSchemaMemo).
func SchemaMemo() string { return path(schemaMemoPath) }

// CABundles is the directory the embedded CA bundle is written to, one
// file per bundle (http.BundledCaBundlePath).
func CABundles() string { return path(caBundlesPath) }

// ClearOwned removes maestro's own caches that clear-cache clears with the
// cache directory setting with, all but the package store: the store is
// shared by concurrent installs, so it is pruned through internal/store,
// which locks what it removes. A cache that does not exist is no error.
func ClearOwned(with string) error {
	var first error
	for _, o := range owned {
		if o.With != with || o.Path == storePath {
			continue
		}
		if err := os.RemoveAll(path(o.Path)); err != nil && first == nil {
			first = err
		}
	}

	return first
}
