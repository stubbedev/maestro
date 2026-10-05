// The embedded shim and its extraction (docs/PLUGINS.md §5.1 "Embedding
// and extraction", §5.15).

package plugin

//go:generate go run ../../tools/shimgen -index -shim php

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/stubbedev/maestro/internal/plugin/shimbuild"
)

//go:embed all:php
var embedded embed.FS

// shimFS is the shim's source tree.
func shimFS() fs.FS {
	sub, err := fs.Sub(embedded, "php")
	if err != nil {
		panic(err) // the embed always has php/
	}

	return sub
}

// manifest is the embedded manifest and its sha256, which names the
// shim's cache directory.
var manifest = sync.OnceValues(func() ([]byte, string) {
	data, err := fs.ReadFile(shimFS(), shimbuild.ManifestFile)
	if err != nil {
		panic("plugin: the embedded shim has no manifest: " + err.Error())
	}
	sum := sha256.Sum256(data)

	return data, hex.EncodeToString(sum[:])
})

// ShimDir is where the shim is extracted: <cacheDir>/maestro/shim/<sha256
// of its manifest>. cacheDir is Composer's cache-dir.
func ShimDir(cacheDir string) string {
	_, sum := manifest()

	return filepath.Join(cacheDir, "maestro", "shim", sum)
}

// ComposerBinary is the path of the COMPOSER_BINARY launcher
// (docs/PLUGINS.md D13) in the shim extracted under cacheDir. It extracts
// nothing.
func ComposerBinary(cacheDir string) string {
	return filepath.Join(ShimDir(cacheDir), "bin", "composer")
}

// The environment variables maestro sets at startup (SetProcessEnv).
const (
	// ComposerBinaryEnv is what bin/composer sets to its own path.
	ComposerBinaryEnv = "COMPOSER_BINARY"
	// MaestroBinaryEnv tells the launcher which maestro to run.
	MaestroBinaryEnv = "MAESTRO_BINARY"
)

// SetProcessEnv does at startup what bin/composer does with
// `Platform::putEnv('COMPOSER_BINARY', realpath($_SERVER['argv'][0]))`:
// COMPOSER_BINARY is the launcher's path (nothing is extracted until a
// script needs it, see Runtime.EnsureComposerBinary), and MAESTRO_BINARY
// the running maestro, which the launcher runs.
func SetProcessEnv(cacheDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.Setenv(MaestroBinaryEnv, exe); err != nil {
		return err
	}

	return os.Setenv(ComposerBinaryEnv, ComposerBinary(cacheDir))
}

// extractShim makes sure the shim is extracted under cacheDir and returns
// its directory. An existing directory is used when its manifest equals
// the embedded one; otherwise it is extracted again. Extraction builds a
// temporary directory and renames it into place, so concurrent maestro
// processes never see a partial shim.
func extractShim(cacheDir string) (string, error) {
	dir := ShimDir(cacheDir)
	if validShim(dir) {
		return dir, nil
	}

	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}

	tmp, err := os.MkdirTemp(parent, ".tmp-")
	if err != nil {
		return "", err
	}
	defer removeTree(tmp)

	if err := writeShim(tmp); err != nil {
		return "", err
	}

	if err := os.Rename(tmp, dir); err == nil {
		return dir, nil
	}

	// Another maestro extracted it meanwhile, or a broken copy is in the
	// way: move that aside and try once more.
	if validShim(dir) {
		return dir, nil
	}
	aside := dir + ".broken-" + randomSuffix()
	if err := os.Rename(dir, aside); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	removeTree(aside)

	if err := os.Rename(tmp, dir); err != nil {
		if validShim(dir) {
			return dir, nil
		}

		return "", err
	}

	return dir, nil
}

// validShim reports whether dir holds this maestro's shim: its manifest,
// written last, equals the embedded one.
func validShim(dir string) bool {
	want, _ := manifest()
	got, err := os.ReadFile(filepath.Join(dir, shimbuild.ManifestFile))

	return err == nil && bytes.Equal(got, want)
}

// writeShim writes the shim into dir: directories 0700, files read-only
// (the launcher executable), the manifest last.
func writeShim(dir string) error {
	fsys := shimFS()

	write := func(rel string, data []byte) error {
		file := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
			return err
		}
		mode := os.FileMode(0o444)
		if path.Dir(rel) == "bin" {
			mode = 0o555
		}

		return os.WriteFile(file, data, mode)
	}

	err := fs.WalkDir(fsys, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || rel == shimbuild.ManifestFile {
			return err
		}
		data, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return err
		}

		return write(rel, data)
	})
	if err != nil {
		return err
	}

	for rel, content := range shimbuild.VirtualFiles() {
		if err := write(rel, []byte(content)); err != nil {
			return err
		}
	}

	m, _ := manifest()

	return write(shimbuild.ManifestFile, m)
}

// removeTree removes a directory tree whose files are read-only (its
// directories are writable, so that is no obstacle).
func removeTree(dir string) {
	_ = os.RemoveAll(dir)
}

func randomSuffix() string {
	var b [8]byte
	_, _ = rand.Read(b[:])

	return hex.EncodeToString(b[:])
}
