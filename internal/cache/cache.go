// Package cache knows where maestro keeps its files: the package store and
// the unpacked Composer runtime.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Dir falls back to the temp directory rather than failing, so a container
// with no HOME still installs, just without keeping the store between runs.
func Dir() string {
	if dir := os.Getenv("MAESTRO_CACHE_DIR"); dir != "" {
		return dir
	}

	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "maestro")
	}

	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
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

// Package is where a package's extracted tree lives in the store: keyed by
// its dist reference (or, without one, its dist URL) so every project and
// worktree installing the same release reuses one copy, and by flavor, the
// extractor's fingerprint, so a tree is only reused for the same extraction.
// The v1 segment lets a later layout sit next to this one.
func Package(name, reference, url, flavor string) string {
	// A reference is one path segment; a slash in it must not nest directories.
	key := segment(safe(reference))
	if reference == "" {
		key = "url-" + digest(url)
	}

	vendor, pkg, _ := strings.Cut(safe(name), "/")

	return filepath.Join(Dir(), "pkgs", "v1", segment(vendor), segment(pkg), key+"-"+flavor)
}

// segment makes s exactly one non-empty path segment.
func segment(s string) string {
	s = strings.ReplaceAll(s, "/", "-")
	if s == "" || s == "." {
		return "-"
	}

	return s
}

// Runtime is where the Composer runtime with the given content digest is
// unpacked.
func Runtime(digest string) string {
	return filepath.Join(Dir(), "runtime", digest)
}

// Fingerprint condenses an extractor's identity into a short key.
func Fingerprint(path string, size, mtime, umask int64) string {
	return digest(path + "\x00" + strconv.FormatInt(size, 10) + "\x00" + strconv.FormatInt(mtime, 10) + "\x00" + strconv.FormatInt(umask, 8))[:12]
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])[:16]
}

// safe keeps a package name usable as a path on every platform without letting
// it climb out of the cache directory.
func safe(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))

	var b strings.Builder

	b.Grow(len(name))

	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_', r == '.', r == '/':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}

	return strings.ReplaceAll(b.String(), "..", "--")
}
