// Package cache knows where maestro keeps its own files, and ports
// Composer\Cache (composer.go), which keeps repository metadata and VCS
// mirrors in Composer's cache directories.
package cache

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/stubbedev/maestro/internal/php"
)

// Dir is maestro's cache directory: MAESTRO_CACHE_DIR, else the XDG (or
// platform) cache directory. It falls back to the temp directory rather
// than failing, so a container with no HOME still installs, just without
// keeping anything between runs.
func Dir() string {
	if dir := os.Getenv("MAESTRO_CACHE_DIR"); dir != "" {
		return dir
	}

	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "maestro")
	}

	if php.IsWindows() || runtime.GOOS == "darwin" {
		if dir, err := os.UserCacheDir(); err == nil {
			return filepath.Join(dir, "maestro")
		}

		return filepath.Join(os.TempDir(), "maestro")
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "maestro")
	}

	return filepath.Join(os.TempDir(), "maestro")
}

// Store is the package store's directory (internal/store). The version
// segment lets a later layout live next to this one.
func Store() string {
	return filepath.Join(Dir(), "store", "v1")
}

// DecodedMetadata is where the repository metadata files read from
// Composer's repository cache are kept decoded (internal/repository/
// composerrepo, deliberate deviation 3), one directory per version of
// their form.
func DecodedMetadata() string {
	return filepath.Join(Dir(), "p2")
}
