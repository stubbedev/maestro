// AutoloadGenerator's protected methods, for the plugin shim: what a
// subclass written in PHP calls on itself (parent::sortPackageMap(),
// $this->getPathCode(), ...). maestro's dump does not call a subclass's
// overrides of them.

package autoload

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// SortPackageMap is sortPackageMap($packageMap).
func SortPackageMap(packageMap []PackageMapEntry) []PackageMapEntry {
	return sortPackageMap(packageMap)
}

// ParseAutoloadsType is parseAutoloadsType($packageMap, $type,
// $rootPackage): the rules of type typ ("psr-0", "psr-4", "classmap",
// "files" or "exclude-from-classmap") in the field of Autoloads holding
// them.
func (g *Generator) ParseAutoloadsType(packageMap []PackageMapEntry, typ string, rootPackage pkg.PackageInterface) (*Autoloads, error) {
	a := &Autoloads{PSR0: php.NewArray(), PSR4: php.NewArray(), Files: php.NewArray()}
	if err := g.parseAutoloadsType(packageMap, autoloadType(typ), rootPackage, a, g.devMode); err != nil {
		return nil, err
	}

	return a, nil
}

// AutoloadFile is getAutoloadFile($vendorPathToTargetDirCode, $suffix).
func AutoloadFile(vendorPathToTargetDirCode, suffix string) string {
	return autoloadFile(vendorPathToTargetDirCode, suffix)
}

// AutoloadRealFile is getAutoloadRealFile(...): targetDirLoader "" is
// null.
func (g *Generator) AutoloadRealFile(useIncludePath bool, targetDirLoader string, useIncludeFiles bool, suffix string, useGlobalIncludePath, prependAutoloader, checkPlatform bool) string {
	return g.autoloadRealFile(useIncludePath, targetDirLoader, useIncludeFiles, suffix, useGlobalIncludePath, prependAutoloader, checkPlatform)
}

// PlatformCheck is getPlatformCheck($packageMap, $checkPlatform,
// $devPackageNames): "" for null.
func (g *Generator) PlatformCheck(packageMap []PackageMapEntry, checkPlatform any, devPackageNames []string) (string, error) {
	return g.platformCheck(packageMap, checkPlatform, devPackageNames)
}

// IO is the generator's $io (getIncludeFilesFile() warns through it).
func (g *Generator) IO() io.IO { return g.io }
