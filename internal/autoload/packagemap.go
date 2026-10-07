// Ports AutoloadGenerator::buildPackageMap, validatePackage, parseAutoloads,
// parseAutoloadsType, getFileIdentifier, filterPackageMap and
// sortPackageMap (src/Composer/Autoload/AutoloadGenerator.php).

package autoload

import (
	"crypto/md5" //nolint:gosec // Composer identifies autoloaded files by md5, not for security.
	"encoding/hex"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// PackageMapEntry is one [package, install path] pair of a package map.
// Installed is false for a null install path (a metapackage).
type PackageMapEntry struct {
	Package     pkg.PackageInterface
	InstallPath string
	Installed   bool
}

// Autoloads is what parseAutoloads returns.
type Autoloads struct {
	// PSR0 and PSR4 map namespaces to lists of paths, in krsort order.
	PSR0 *php.Array
	PSR4 *php.Array
	// Classmap lists the paths to scan.
	Classmap []string
	// Files maps file identifiers to paths.
	Files *php.Array
	// ExcludeFromClassmap lists regexes (without delimiters) of paths to
	// leave out of the class map.
	ExcludeFromClassmap []string

	// classmapValue is the first classmap entry that is not a string (a
	// root package's path given as a scalar), at classmapIndex: dump()
	// passes it to buildExclusionRegex(string $dir)
	classmapValue any
	classmapIndex int
}

// DevFilter is parseAutoloads' $filteredDevPackages: which packages to
// leave out as dev dependencies.
type DevFilter struct {
	names  []string
	legacy bool
	list   bool
}

// NoDevFilter keeps every package (false).
var NoDevFilter = DevFilter{}

// LegacyDevFilter works out the dev packages from the root package's
// requirements (true).
var LegacyDevFilter = DevFilter{legacy: true}

// DevPackageNames leaves out the named packages (a list).
func DevPackageNames(names []string) DevFilter { return DevFilter{names: names, list: true} }

// BuildPackageMap ports buildPackageMap: the root package with an empty
// install path, then each non-alias package with its install path.
func (g *Generator) BuildPackageMap(im InstallationManager, rootPackage pkg.PackageInterface, packages []pkg.PackageInterface) ([]PackageMapEntry, error) {
	packageMap := make([]PackageMapEntry, 1, len(packages)+1)
	packageMap[0] = PackageMapEntry{Package: rootPackage, Installed: true}

	for _, p := range packages {
		if _, ok := p.(pkg.Alias); ok {
			continue
		}
		if err := validatePackage(p); err != nil {
			return nil, err
		}
		path, ok, err := im.InstallPath(p)
		if err != nil {
			return nil, err
		}
		packageMap = append(packageMap, PackageMapEntry{Package: p, InstallPath: path, Installed: ok})
	}

	return packageMap, nil
}

// validatePackage ports validatePackage: PSR-4 rules must not be combined
// with target-dir, and their namespaces must end with a separator.
func validatePackage(p pkg.PackageInterface) error {
	psr4, _ := p.Autoload().Get("psr-4")
	if !php.ToBool(psr4) {
		return nil
	}
	if p.TargetDir().Valid {
		return &util.InvalidArgumentError{Message: "PSR-4 autoloading is incompatible with the target-dir property, remove the target-dir in package '" + p.Name() + "'."}
	}
	rules, ok := psr4.(*php.Array)
	if !ok {
		return foreachError(psr4)
	}
	for k := range rules.All() {
		if k.IsInt() {
			return typeError("substr", "int")
		}
		namespace := k.String()
		if namespace != "" && namespace[len(namespace)-1] != '\\' {
			return &util.InvalidArgumentError{Message: "psr-4 namespaces must end with a namespace separator, '" + namespace + "' does not, use '" + namespace + "\\'."}
		}
	}

	return nil
}

// ParseAutoloads ports parseAutoloads: compiles the ordered autoload rules
// of a package map (from BuildPackageMap; its first entry is the root
// package if it is a RootPackageInterface).
func (g *Generator) ParseAutoloads(packageMap []PackageMapEntry, rootPackage pkg.PackageInterface, filter DevFilter) (*Autoloads, error) {
	return g.parseAutoloads(packageMap, rootPackage, filter, g.devMode)
}

// parseAutoloads is ParseAutoloads with the root package's autoload-dev
// rules included for rootDev instead of by $this->devMode: a warm-up or
// a speculation runs before the dump sets it.
func (g *Generator) parseAutoloads(packageMap []PackageMapEntry, rootPackage pkg.PackageInterface, filter DevFilter, rootDev bool) (*Autoloads, error) {
	var rootEntry *PackageMapEntry
	if len(packageMap) > 0 {
		if _, ok := packageMap[0].Package.(pkg.RootPackageInterface); ok {
			rootEntry = &packageMap[0]
			packageMap = packageMap[1:]
		}
	}

	switch {
	case filter.list:
		packageMap = slices.DeleteFunc(slices.Clone(packageMap), func(e PackageMapEntry) bool {
			return slices.Contains(filter.names, e.Package.Name())
		})
	case filter.legacy:
		packageMap = filterPackageMap(packageMap, rootPackage)
	}

	sortedPackageMap := sortPackageMap(packageMap)
	if rootEntry != nil {
		sortedPackageMap = append(sortedPackageMap, *rootEntry)
	}
	reverseSortedMap := slices.Clone(sortedPackageMap)
	slices.Reverse(reverseSortedMap)

	// reverse-sorted means root first, then dependents, then their
	// dependents, etc. which makes sense to allow root to override
	// classmap or psr-0/4 entries with higher precedence rules
	a := &Autoloads{PSR0: php.NewArray(), PSR4: php.NewArray(), Files: php.NewArray()}
	if err := g.parseAutoloadsType(reverseSortedMap, typePSR0, rootPackage, a, rootDev); err != nil {
		return nil, err
	}
	if err := g.parseAutoloadsType(reverseSortedMap, typePSR4, rootPackage, a, rootDev); err != nil {
		return nil, err
	}
	if err := g.parseAutoloadsType(reverseSortedMap, typeClassmap, rootPackage, a, rootDev); err != nil {
		return nil, err
	}
	// sorted (i.e. dependents first) for files to ensure that dependencies
	// are loaded/available once a file is included
	if err := g.parseAutoloadsType(sortedPackageMap, typeFiles, rootPackage, a, rootDev); err != nil {
		return nil, err
	}
	// using sorted here but it does not really matter as all are excluded
	// equally
	if err := g.parseAutoloadsType(sortedPackageMap, typeExclude, rootPackage, a, rootDev); err != nil {
		return nil, err
	}

	php.Krsort(a.PSR0, 0)
	php.Krsort(a.PSR4, 0)

	return a, nil
}

// autoloadType is one of the autoload keys parseAutoloadsType handles.
type autoloadType string

const (
	typePSR0     autoloadType = "psr-0"
	typePSR4     autoloadType = "psr-4"
	typeClassmap autoloadType = "classmap"
	typeFiles    autoloadType = "files"
	typeExclude  autoloadType = "exclude-from-classmap"
)

var (
	// {/+}
	reSlashes = php.MustCompile(`{/+}`)
	// {^((?:(?:\\\.){1,2}+/)+)}: leading ./ and ../ segments, preg_quoted.
	reUpDirs = php.MustCompile(`{^((?:(?:\\\.){1,2}+/)+)}`)
)

// parseAutoloadsType ports parseAutoloadsType, adding the rules of type
// typ to a; the root package's autoload-dev rules are added for rootDev.
func (g *Generator) parseAutoloadsType(packageMap []PackageMapEntry, typ autoloadType, rootPackage pkg.PackageInterface, a *Autoloads, rootDev bool) error {
	for _, item := range packageMap {
		// packages that are not installed cannot autoload anything
		if !item.Installed {
			continue
		}
		p, installPath := item.Package, item.InstallPath

		autoload := p.Autoload()
		if rootDev && p == rootPackage {
			autoload = php.ArrayMergeRecursive(autoload, p.DevAutoload())
		}

		// skip misconfigured packages
		v, _ := autoload.Get(string(typ))
		rules, ok := v.(*php.Array)
		if !ok {
			continue
		}
		targetDir := p.TargetDir()
		if targetDir.Valid && p != rootPackage {
			installPath = php.SubstrLen(installPath, 0, -len("/"+targetDir.S))
		}

		for k, paths := range rules.All() {
			var namespace php.Key
			if typ == typePSR4 || typ == typePSR0 {
				if k.IsInt() {
					return typeError("ltrim", "int")
				}
				// normalize namespaces to ensure "\" becomes "" and others
				// do not have leading separators as they are not needed
				namespace = php.StrKey(php.LtrimSet(k.String(), `\`))
			}
			for _, pv := range castArray(paths) {
				// a path that is not a string is cast where PHP
				// concatenates it and fails where it reaches a string
				// parameter (strict_types)
				path, isString := pv.(string)
				_, isArray := pv.(*php.Array)
				if !isString && !isArray {
					path = php.ToString(pv)
				}
				if (typ == typeFiles || typ == typeClassmap || typ == typeExclude) && php.ToBool(targetDir.Value()) {
					if isArray {
						return &util.ErrorException{Message: "Array to string conversion"}
					}
					if !util.IsReadable(installPath + "/" + path) {
						if p == rootPackage {
							if !isString {
								return typeError("ltrim", php.ZvalValueName(pv))
							}
							// remove target-dir from file paths of the root package
							var err error
							if path, err = stripTargetDir(targetDir.S, path); err != nil {
								return err
							}
						} else {
							// add target-dir from file paths that don't have it
							path = targetDir.S + "/" + path
						}
						pv, isString = path, true
					}
				}

				if typ == typeExclude && !isString {
					return typeError("strtr", php.ZvalValueName(pv))
				}

				if typ == typeExclude {
					pattern, resolved, ok, err := excludePattern(path, &installPath)
					if err != nil {
						return err
					}
					if ok {
						a.ExcludeFromClassmap = append(a.ExcludeFromClassmap, php.PregQuote(strings.ReplaceAll(resolved, `\`, "/"), "")+"/"+pattern+"($|/)")
					}

					continue
				}

				relativePath := installPath + "/" + path
				var rawRelative any // the value when it is not a string
				if empty(installPath) {
					relativePath = path
					if !isString {
						rawRelative = pv
					}
					if !php.ToBool(pv) {
						relativePath, rawRelative = ".", nil
					}
				} else if isArray {
					return &util.ErrorException{Message: "Array to string conversion"}
				}

				switch typ {
				case typeFiles:
					if !isString {
						return pkg.ArgumentTypeError(`Composer\Autoload\AutoloadGenerator::getFileIdentifier`, 2, "path", "string", pv)
					}
					a.Files.Set(fileIdentifier(p, path), relativePath)
				case typeClassmap:
					if rawRelative != nil && a.classmapValue == nil {
						a.classmapValue, a.classmapIndex = rawRelative, len(a.Classmap)
					}
					a.Classmap = append(a.Classmap, relativePath)
				default:
					psr := a.PSR0
					if typ == typePSR4 {
						psr = a.PSR4
					}
					subArray(psr, namespace).Append(relativePath)
				}
			}
		}
	}

	return nil
}

// stripTargetDir removes the root package's target dir from the start of
// path.
func stripTargetDir(targetDir, path string) (string, error) {
	quoted := strings.ReplaceAll(php.PregQuote(strings.NewReplacer("/", "<dirsep>", `\`, "<dirsep>").Replace(targetDir), ""), `\<dirsep\>`, `[\\/]`)
	path, _, err := php.PregReplace("{^"+quoted+"}", "", php.LtrimSet(path, `\/`), -1)
	if err != nil {
		return "", err
	}

	return php.LtrimSet(path, `\/`), nil
}

// excludePattern turns an exclude-from-classmap path into the regex of
// what it excludes below resolved, the realpath of the directory it is
// relative to; ok is false when that does not exist. An empty installPath
// is set to the working directory, as PHP does.
func excludePattern(path string, installPath *string) (pattern, resolved string, ok bool, err error) {
	// first escape user input
	pattern, _, err = reSlashes.Replace(php.PregQuote(php.TrimSet(strings.ReplaceAll(path, `\`, "/"), "/"), ""), "/", -1)
	if err != nil {
		return "", "", false, err
	}

	// add support for wildcards * and **
	pattern = php.StrtrPairs(pattern, map[string]string{`\*\*`: ".+?", `\*`: "[^/]+?"})

	// add support for up-level relative paths
	updir := ""
	pattern, _, err = reUpDirs.ReplaceCallback(pattern, func(m *php.Match) string {
		// undo preg_quote for the matched string
		updir = strings.ReplaceAll(m.Get(1), `\.`, ".")

		return ""
	}, -1)
	if err != nil {
		return "", "", false, err
	}
	if empty(*installPath) {
		cwd, err := util.GetCwd(false)
		if err != nil {
			return "", "", false, err
		}
		*installPath = strings.ReplaceAll(cwd, `\`, "/")
	}

	resolved, ok = util.RealpathOK(*installPath + "/" + updir)

	return pattern, resolved, ok, nil
}

// fileIdentifier ports getFileIdentifier.
func fileIdentifier(p pkg.PackageInterface, path string) string {
	sum := md5.Sum([]byte(p.Name() + ":" + path)) //nolint:gosec // as above

	return hex.EncodeToString(sum[:])
}

// filterPackageMap ports filterPackageMap: the packages the root package
// requires, directly or not, following replacements.
func filterPackageMap(packageMap []PackageMapEntry, rootPackage pkg.PackageInterface) []PackageMapEntry {
	packages := make(map[string]pkg.PackageInterface, len(packageMap))
	include := map[string]bool{}
	replacedBy := map[string]string{}

	for _, item := range packageMap {
		name := item.Package.Name()
		packages[name] = item.Package
		for replace := range item.Package.Replaces().Values() {
			replacedBy[replace.Target()] = name
		}
	}

	var add func(p pkg.PackageInterface)
	add = func(p pkg.PackageInterface) {
		for link := range p.Requires().Values() {
			target := link.Target()
			if by, ok := replacedBy[target]; ok {
				target = by
			}
			if !include[target] {
				include[target] = true
				if dep, ok := packages[target]; ok {
					add(dep)
				}
			}
		}
	}
	add(rootPackage)

	filtered := make([]PackageMapEntry, 0, len(packageMap))
	for _, item := range packageMap {
		if slices.ContainsFunc(item.Package.Names(true), func(name string) bool { return include[name] }) {
			filtered = append(filtered, item)
		}
	}

	return filtered
}

// sortPackageMap ports sortPackageMap: packages by dependency weight, those
// of equal weight alphabetically. A name seen twice keeps its first
// position with its last entry, as PHP's name-keyed array does.
func sortPackageMap(packageMap []PackageMapEntry) []PackageMapEntry {
	index := make(map[string]int, len(packageMap))
	entries := make([]PackageMapEntry, 0, len(packageMap))
	for _, item := range packageMap {
		name := item.Package.Name()
		if i, ok := index[name]; ok {
			entries[i] = item

			continue
		}
		index[name] = len(entries)
		entries = append(entries, item)
	}

	packages := make([]pkg.PackageInterface, len(entries))
	for i, e := range entries {
		packages[i] = e.Package
	}

	sorted := pkg.SortPackages(packages, nil)
	sortedPackageMap := make([]PackageMapEntry, len(sorted))
	for i, p := range sorted {
		sortedPackageMap[i] = entries[index[p.Name()]]
	}

	return sortedPackageMap
}

// castArray is (array) $v for the values of autoload rules.
func castArray(v any) []any {
	switch v := v.(type) {
	case *php.Array:
		return v.Values()
	case nil:
		return nil
	}

	return []any{v}
}

// empty is PHP's empty() on a string.
func empty(s string) bool { return s == "" || s == "0" }

// typeError is the TypeError a strict_types call of fn in
// AutoloadGenerator.php throws for an argument #1 ($string) of the given
// type.
func typeError(fn, given string) error {
	return &php.EngineError{Class: "TypeError", Message: fn + "(): Argument #1 ($string) must be of type string, " + given + " given"}
}

// foreachError is the warning (an ErrorException under Composer's error
// handler) foreach emits for a value that is not iterable.
func foreachError(v any) error {
	return &util.ErrorException{Message: "foreach() argument must be of type array|object, " + php.TypeName(v) + " given"}
}
