// Command shimvendor vendors the third-party PHP libraries Composer
// 2.10.3's phar ships into the plugin shim (docs/PLUGINS.md D6, §5.1):
// the files Compiler.php puts into the phar from Composer's vendor/
// (installed with --no-dev, as `ref-sync` does), copied verbatim into
// internal/plugin/php/lib/ with installed.json recording their versions.
// It then rebuilds the shim's autoload index and manifest.
//
// Usage, from the repository root in the dev shell:
//
//	go run ./tools/shimvendor
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stubbedev/maestro/internal/plugin/shimbuild"
)

func main() {
	vendor := flag.String("vendor", ".ref/composer/vendor", "Composer 2.10.3's vendor directory (runtime dependencies only)")
	shim := flag.String("shim", "internal/plugin/php", "the shim source tree")
	flag.Parse()

	if err := run(*vendor, *shim); err != nil {
		fmt.Fprintln(os.Stderr, "shimvendor:", err)
		os.Exit(1)
	}
}

func run(vendor, shim string) error {
	if err := shimbuild.Vendor(vendor, filepath.Join(shim, shimbuild.LibDir)); err != nil {
		return err
	}

	files, err := shimbuild.VendorFiles(vendor)
	if err != nil {
		return err
	}
	fmt.Printf("vendored %d files into %s\n", len(files), filepath.Join(shim, shimbuild.LibDir))

	return shimbuild.WriteIndex(shim)
}
