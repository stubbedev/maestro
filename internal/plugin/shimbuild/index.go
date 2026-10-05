// Builds the shim's autoload index (docs/PLUGINS.md §5.1 "Shim
// autoloading"): one static class map over the hand-written shim, the
// generated stubs and the vendored libraries, plus the vendored
// libraries' `files`, in the phar's order.

package shimbuild

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/php"
)

// IndexFile is the generated autoload index, relative to the shim root.
const IndexFile = "autoload.php"

// The shim's own trees, scanned in full.
const (
	SrcDir   = "src"
	StubsDir = "stubs"
)

// VirtualClasses are the classes whose files the shim does not keep in
// its source tree: they are Composer's own files, embedded once by
// internal/autoload and written into the shim when it is extracted.
var VirtualClasses = map[string]string{
	`Composer\Autoload\ClassLoader`: "src/Composer/Autoload/ClassLoader.php",
	`Composer\InstalledVersions`:    "src/Composer/InstalledVersions.php",
}

// An entry of autoload_files.php / autoload_classmap.php.
var (
	filesEntry  = php.MustCompile(`{^\s*'([0-9a-f]{32})' => \$vendorDir \. '/([^']+)',$}m`)
	vendorEntry = php.MustCompile(`{^\s*'((?:[^'\\]|\\.)+)' => \$vendorDir \. '/([^']+)',$}m`)
)

// unquote reverses var_export's escaping of a single-quoted string.
func unquote(s string) string {
	return strings.NewReplacer(`\\`, `\`, `\'`, `'`).Replace(s)
}

// Index is the shim's autoload index.
type Index struct {
	// Classmap maps a class to its file, relative to the shim root.
	Classmap map[string]string
	// Files are the `files` to require, in order, with Composer's file
	// identifiers.
	Files []IndexedFile
}

// IndexedFile is one autoload `files` entry.
type IndexedFile struct {
	ID, Path string
}

// BuildIndex scans the shim rooted at shimDir.
func BuildIndex(shimDir string) (*Index, error) {
	shimDir, err := filepath.Abs(shimDir)
	if err != nil {
		return nil, err
	}

	idx := &Index{Classmap: map[string]string{}}

	addOne := func(class, rel string) error {
		if prev, ok := idx.Classmap[class]; ok && prev != rel {
			return errors.New("class " + class + " is defined by both " + prev + " and " + rel)
		}
		idx.Classmap[class] = rel

		return nil
	}
	add := func(m *classmap.ClassMap) error {
		for class, file := range m.Map() {
			rel, err := filepath.Rel(shimDir, file)
			if err != nil {
				return err
			}
			if err := addOne(class, filepath.ToSlash(rel)); err != nil {
				return err
			}
		}

		return nil
	}

	for _, dir := range []string{SrcDir, StubsDir} {
		root := filepath.Join(shimDir, dir)
		if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
			continue
		}
		m, err := classmap.CreateMap(root)
		if err != nil {
			return nil, err
		}
		if err := add(m); err != nil {
			return nil, err
		}
	}

	for class, file := range VirtualClasses {
		if prev, ok := idx.Classmap[class]; ok {
			return nil, errors.New("class " + class + " is written at extraction but also defined by " + prev)
		}
		idx.Classmap[class] = file
	}

	lib := filepath.Join(shimDir, LibDir)

	// The phar's ClassLoader looks a class up in its class map first, so
	// that wins over the PSR rules (marc-mabe/php-enum's Stringable stub
	// over symfony/polyfill-php80's).
	cm, err := os.ReadFile(filepath.Join(lib, "composer", "autoload_classmap.php"))
	if err != nil {
		return nil, err
	}
	entries, err := vendorEntry.MatchAll(string(cm))
	if err != nil {
		return nil, err
	}
	fromClassmap := map[string]bool{}
	for _, m := range entries {
		class, file := unquote(m.Get(1)), m.Get(2)
		if strings.HasPrefix(file, "composer/") {
			continue // vendor/composer's own InstalledVersions: a virtual class here
		}
		fromClassmap[class] = true
		if err := addOne(class, LibDir+"/"+file); err != nil {
			return nil, err
		}
	}

	in, err := ReadInstalled(filepath.Join(lib, "composer", "installed.json"))
	if err != nil {
		return nil, err
	}

	for _, p := range in.Packages {
		g := classmap.NewGenerator(nil)
		pkgDir := filepath.Join(lib, filepath.FromSlash(p.Name))

		scan := func(paths json.RawMessage, typ classmap.AutoloadType, namespace string) error {
			for _, rel := range rawPaths(paths) {
				if err := g.ScanPaths(filepath.Join(pkgDir, filepath.FromSlash(rel)), nil, typ, namespace, nil); err != nil {
					return err
				}
			}

			return nil
		}

		for _, ns := range sortedKeys(p.Autoload.PSR4) {
			if err := scan(p.Autoload.PSR4[ns], classmap.PSR4, ns); err != nil {
				return nil, err
			}
		}
		for _, ns := range sortedKeys(p.Autoload.PSR0) {
			if err := scan(p.Autoload.PSR0[ns], classmap.PSR0, ns); err != nil {
				return nil, err
			}
		}
		for class, file := range g.ClassMap().Map() {
			if fromClassmap[class] {
				continue
			}
			rel, err := filepath.Rel(shimDir, file)
			if err != nil {
				return nil, err
			}
			if err := addOne(class, filepath.ToSlash(rel)); err != nil {
				return nil, err
			}
		}
	}

	files, err := os.ReadFile(filepath.Join(lib, "composer", "autoload_files.php"))
	if err != nil {
		return nil, err
	}
	matches, err := filesEntry.MatchAll(string(files))
	if err != nil {
		return nil, err
	}
	for _, m := range matches {
		idx.Files = append(idx.Files, IndexedFile{ID: m.Get(1), Path: LibDir + "/" + m.Get(2)})
	}

	return idx, nil
}

// rawPaths decodes an autoload rule's path or list of paths.
func rawPaths(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}

	var many []string
	_ = json.Unmarshal(raw, &many)

	return many
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}

// PHP renders the index as the PHP file the shim's autoloader requires.
func (idx *Index) PHP() []byte {
	var b strings.Builder

	b.WriteString("<?php\n\n// @generated by tools/shimgen -index; do not edit.\n// The shim's static class map and the vendored libraries' autoload\n// files, relative to the shim root (docs/PLUGINS.md §5.1).\n\nreturn array(\n    'classmap' => array(\n")

	classes := sortedKeys(idx.Classmap)
	for _, class := range classes {
		b.WriteString("        " + php.VarExport(class) + " => " + php.VarExport(idx.Classmap[class]) + ",\n")
	}

	b.WriteString("    ),\n    'files' => array(\n")
	for _, f := range idx.Files {
		b.WriteString("        " + php.VarExport(f.ID) + " => " + php.VarExport(f.Path) + ",\n")
	}
	b.WriteString("    ),\n);\n")

	return []byte(b.String())
}
