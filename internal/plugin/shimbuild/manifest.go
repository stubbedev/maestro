// The shim manifest (docs/PLUGINS.md §5.1 "Embedding and extraction",
// §5.15): every file of the extracted shim with its sha256. The shim's
// cache directory is named after the manifest's own sha256, and a cached
// shim is valid when its manifest equals the embedded one.

package shimbuild

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/autoload"
	"github.com/stubbedev/maestro/internal/json/res"
)

// ManifestFile is the manifest's path, relative to the shim root.
const ManifestFile = "MANIFEST"

// VirtualFiles are the files the extracted shim holds that are not in its
// source tree, by path: the verbatim classes (see VirtualClasses) and
// Composer's res/ schemas, which the phar ships and JsonFile::COMPOSER_SCHEMA_PATH
// and LOCK_SCHEMA_PATH point to, from internal/json's embed.
func VirtualFiles() map[string]string {
	return map[string]string{
		"res/composer-schema.json":                      res.ComposerSchema(),
		"res/composer-lock-schema.json":                 res.LockSchema(),
		"res/composer-repository-schema.json":           res.RepositorySchema(),
		VirtualClasses[`Composer\Autoload\ClassLoader`]: autoload.ClassLoaderPHP,
		VirtualClasses[`Composer\InstalledVersions`]:    autoload.InstalledVersionsPHP,
	}
}

// Manifest returns the manifest of the shim whose source tree is fsys:
// one "<sha256> <path>" line per file (the virtual files included, the
// manifest itself excluded), sorted by path.
func Manifest(fsys fs.FS) ([]byte, error) {
	sums := map[string]string{}

	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == ManifestFile {
			return err
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}
		sums[path] = sum(data)

		return nil
	})
	if err != nil {
		return nil, err
	}

	for path, content := range VirtualFiles() {
		sums[path] = sum([]byte(content))
	}

	var b strings.Builder
	for _, path := range slices.Sorted(maps.Keys(sums)) {
		b.WriteString(sums[path] + " " + path + "\n")
	}

	return []byte(b.String()), nil
}

func sum(data []byte) string {
	h := sha256.Sum256(data)

	return hex.EncodeToString(h[:])
}
