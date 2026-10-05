// Package shimbuild holds what the shim's build tools (tools/shimvendor,
// tools/shimgen) and their freshness tests share: which vendored library
// files the shim ships (Compiler.php's selection), the static autoload
// index over the shim, and the shim manifest (docs/PLUGINS.md §5.1).
//
// The generated files are committed: internal/plugin/php/lib/ (vendored
// libraries), internal/plugin/php/stubs/ (presence-parity stubs),
// internal/plugin/php/autoload.php (the index) and
// internal/plugin/php/MANIFEST. `go generate ./internal/plugin` rebuilds
// the index and the manifest after a hand-written shim file changes.
package shimbuild

import (
	"os"
	"path/filepath"
)

// WriteIndex regenerates the autoload index and then the manifest of the
// shim source tree rooted at shimDir.
func WriteIndex(shimDir string) error {
	idx, err := BuildIndex(shimDir)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(shimDir, IndexFile), idx.PHP(), 0o644); err != nil {
		return err
	}

	manifest, err := Manifest(os.DirFS(shimDir))
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(shimDir, ManifestFile), manifest, 0o644)
}
