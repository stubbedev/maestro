// Ports the vendor half of src/Composer/Compiler.php (compile(): the
// Finder over vendor/ and the extra files), which decides what the phar
// ships of Composer's runtime dependencies (docs/PLUGINS.md D6, §5.1).

package shimbuild

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// LibDir is where the vendored libraries live inside the shim. It is not
// "vendor": Go module zips drop every file below a nested vendor/
// directory, which would empty the embed of a released maestro.
const LibDir = "lib"

// The metadata files of vendor/composer/ the shim keeps: installed.json
// (versions and autoload rules, for the drift test and the index),
// installed.php (the phar's InstalledVersions data), autoload_classmap.php
// (the class map the phar's ClassLoader consults before its PSR rules) and
// autoload_files.php (the order and identifiers of the `files` the phar
// requires).
var metadataFiles = []string{"installed.json", "installed.php", "autoload_classmap.php", "autoload_files.php"}

// Compiler::compile's notPath patterns, verbatim.
var notPath = []*php.Regexp{
	php.MustCompile(`/\/(composer\.(?:json|lock)|[A-Z]+\.md(?:own)?|\.gitignore|appveyor.yml|phpunit\.xml\.dist|phpstan\.neon\.dist|phpstan-config\.neon|phpstan-baseline\.neon|UPGRADE.*\.(?:md|txt))$/`),
	php.MustCompile(`/bin\/(jsonlint|validate-json|simple-phpunit|phpstan|phpstan\.phar)(\.bat)?$/`),
}

// Compiler::compile's plain notPath strings (Finder matches them as
// substrings of the relative path).
var notPathStrings = []string{
	"justinrainbow/json-schema/demo/",
	"justinrainbow/json-schema/dist/",
	"justinrainbow/json-schema/bin/",
	"composer/pcre/extension.neon",
	"composer/LICENSE",
}

// Compiler::compile's exclude() directories, any depth.
var excludedDirs = map[string]bool{"Tests": true, "tests": true, "docs": true}

// Compiler::compile's $extraFiles (the CA bundle is ca-bundle's res/).
var extraFiles = []string{
	"composer/installed.json",
	"composer/spdx-licenses/res/spdx-exceptions.json",
	"composer/spdx-licenses/res/spdx-licenses.json",
	"composer/ca-bundle/res/cacert.pem",
	"symfony/console/Resources/bin/hiddeninput.exe",
	"symfony/console/Resources/completion.bash",
}

var (
	expectedName = php.MustCompile(`{(^LICENSE(?:\.txt)?$|\.php$)}`)
	vcsDirs      = map[string]bool{".git": true, ".svn": true, ".hg": true, "_darcs": true, "CVS": true, ".arch-params": true, ".monotone": true, ".bzr": true}
)

// Installed is vendor/composer/installed.json.
type Installed struct {
	Packages []InstalledPackage `json:"packages"`
	Dev      bool               `json:"dev"`
}

// InstalledPackage is one package of installed.json.
type InstalledPackage struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	InstallPath string   `json:"install-path"`
	Autoload    Autoload `json:"autoload"`
}

// Autoload is a package's autoload section, as the index needs it.
type Autoload struct {
	PSR4                map[string]json.RawMessage `json:"psr-4"`
	PSR0                map[string]json.RawMessage `json:"psr-0"`
	Classmap            []string                   `json:"classmap"`
	Files               []string                   `json:"files"`
	ExcludeFromClassmap []string                   `json:"exclude-from-classmap"`
}

// ReadInstalled reads an installed.json.
func ReadInstalled(file string) (*Installed, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	var in Installed
	if err := json.Unmarshal(data, &in); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}

	return &in, nil
}

// LockedVersions returns name => version of the non-dev packages of a
// composer.lock.
func LockedVersions(lockFile string) (map[string]string, error) {
	data, err := os.ReadFile(lockFile)
	if err != nil {
		return nil, err
	}

	var lock struct {
		Packages []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("%s: %w", lockFile, err)
	}

	out := make(map[string]string, len(lock.Packages))
	for _, p := range lock.Packages {
		out[p.Name] = p.Version
	}

	return out, nil
}

// VendorFiles returns the files of vendorDir (Composer's vendor/, with
// its runtime dependencies installed) that Compiler::compile puts into
// the phar, relative to vendorDir and sorted, restricted to the installed
// packages plus the vendor/composer/ metadata the shim keeps. Like the
// Compiler, it fails on a file that is neither PHP, a LICENSE nor a listed
// extra file, and on a missing extra file.
func VendorFiles(vendorDir string) ([]string, error) {
	in, err := ReadInstalled(filepath.Join(vendorDir, "composer", "installed.json"))
	if err != nil {
		return nil, err
	}
	if in.Dev {
		return nil, errors.New(vendorDir + ": dev dependencies are installed; the phar ships only the runtime ones (composer install --no-dev)")
	}

	var files []string
	for _, p := range in.Packages {
		root := filepath.Join(vendorDir, filepath.FromSlash(p.Name))
		err := filepath.WalkDir(root, func(file string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(vendorDir, file)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)

			if d.IsDir() {
				if vcsDirs[d.Name()] || excludedDirs[d.Name()] {
					return filepath.SkipDir
				}

				return nil
			}
			if vcsDirs[d.Name()] || !d.Type().IsRegular() {
				return nil
			}

			keep, err := keepVendorFile(rel)
			if err != nil || !keep {
				return err
			}
			files = append(files, rel)

			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	for _, name := range metadataFiles {
		rel := "composer/" + name
		if _, err := os.Stat(filepath.Join(vendorDir, filepath.FromSlash(rel))); err != nil {
			return nil, err
		}
		files = append(files, rel)
	}

	for _, extra := range extraFiles {
		if !slices.Contains(files, extra) {
			return nil, errors.New("These files were expected but not added to the phar, they might be excluded or gone from the source package: " + extra)
		}
	}

	slices.Sort(files)

	return slices.Compact(files), nil
}

// keepVendorFile applies the Finder's notPath filters to rel and the
// Compiler's check of what may be added.
func keepVendorFile(rel string) (bool, error) {
	// Finder matches notPath against the relative path.
	for _, re := range notPath {
		m, err := re.IsMatch(rel)
		if err != nil || m {
			return false, err
		}
	}
	for _, s := range notPathStrings {
		if strings.Contains(rel, s) {
			return false, nil
		}
	}

	if slices.Contains(extraFiles, rel) {
		return true, nil
	}

	ok, err := expectedName.IsMatch(path.Base(rel))
	if err != nil {
		return false, err
	}
	if !ok {
		return false, errors.New("These files were unexpectedly added to the phar, make sure they are excluded or listed in $extraFiles: " + rel)
	}

	return true, nil
}

// Vendor replaces libDir with the files VendorFiles selects from
// vendorDir, copied verbatim.
func Vendor(vendorDir, libDir string) error {
	files, err := VendorFiles(vendorDir)
	if err != nil {
		return err
	}

	if err := os.RemoveAll(libDir); err != nil {
		return err
	}

	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(vendorDir, filepath.FromSlash(rel)))
		if err != nil {
			return err
		}
		dst := filepath.Join(libDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return err
		}
	}

	return nil
}
