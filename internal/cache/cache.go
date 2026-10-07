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
