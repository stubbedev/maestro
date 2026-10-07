// Ports nothing: a class map scan run ahead of the dump that needs it
// (deliberate deviation 3, speed).

package autoload

import (
	"io/fs"
	"os"
	"slices"

	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// speculation is a class map scan started by Speculate.
type speculation struct {
	parser               classmap.Parser
	scanPsrPackages      bool
	basePath, vendorPath string
	autoloads            *Autoloads

	done     chan struct{}
	classMap *classmap.ClassMap
	err      error

	// devMode is installed.json's "dev" as the speculation read it, from
	// the file installedJSON describes (nil when it was not read, or
	// changed while it was read).
	devMode       bool
	installedPath string
	installedJSON fs.FileInfo
}

// Speculate starts, in the background, the class map scan of the Dump
// with these arguments that the caller expects to run next, for that
// Dump to take its result instead of scanning (the installer starts it
// before it waits on the network, when the lock file asks for no package
// operation). Speculate prints nothing and writes nothing; the Dump scans
// itself unless what it would scan is what was scanned here.
//
// The caller must call DiscardSpeculation as soon as anything may have
// changed the files since: installing, updating or removing packages,
// or running a listener of an event (a script or a plugin).
func (g *Generator) Speculate(config Config, localRepo InstalledRepository, rootPackage pkg.RootPackageInterface, im InstallationManager, scanPsrPackages bool) {
	g.DiscardSpeculation()
	if g.classMapAuthoritative {
		scanPsrPackages = true
	}
	devMode := g.devMode
	var installedPath string
	var installedJSON fs.FileInfo
	if !g.devModeSet {
		var value any
		var err error
		if devMode, value, installedPath, installedJSON, err = installedDevModeStamped(config); err != nil || value != nil {
			return
		}
	}
	vendorDir, err := vendorDirConfig(config)
	if err != nil {
		return
	}
	cwd, err := util.GetCwd(false)
	if err != nil {
		return
	}
	d := &dump{}
	if d.basePath, err = realpath(cwd, 0); err != nil {
		return
	}
	d.basePath = util.NormalizePath(d.basePath)
	if d.vendorPath, err = realpath(vendorDir, 0); err != nil {
		return
	}
	d.vendorPath = util.NormalizePath(d.vendorPath)

	packageMap, err := g.BuildPackageMap(im, rootPackage, localRepo.CanonicalPackages())
	if err != nil {
		return
	}
	autoloads, err := g.parseAutoloads(packageMap, rootPackage, devFilter(devMode, localRepo.DevPackageNames()), devMode)
	if err != nil {
		return
	}

	s := &speculation{
		parser:          g.Parser,
		scanPsrPackages: scanPsrPackages,
		basePath:        d.basePath,
		vendorPath:      d.vendorPath,
		autoloads:       autoloads,
		done:            make(chan struct{}),
		devMode:         devMode,
		installedPath:   installedPath,
		installedJSON:   installedJSON,
	}
	g.speculation = s
	go func() {
		defer close(s.done)
		s.classMap, s.err = g.scanClassMap(d, autoloads, packageMap, scanPsrPackages)
	}()
}

// installedDevModeStamped is installedDevMode, also returning the path of
// installed.json and its file's description when it was there and did not
// change while it was read.
func installedDevModeStamped(config Config) (devMode bool, value any, path string, info fs.FileInfo, err error) {
	vendorDir, err := vendorDirConfig(config)
	if err != nil {
		return false, nil, "", nil, err
	}
	path = vendorDir + "/composer/installed.json"
	before, beforeErr := os.Stat(path)
	if devMode, value, err = installedDevMode(config); err != nil {
		return devMode, value, path, nil, err
	}
	if after, afterErr := os.Stat(path); beforeErr == nil && afterErr == nil && sameFileStamp(before, after) {
		info = after
	}

	return devMode, value, path, info, nil
}

// sameFileStamp reports whether a and b describe the same file with the
// same size, mode and modification time.
func sameFileStamp(a, b fs.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && a.Mode() == b.Mode()
}

// takeSpeculatedDevMode sets devMode as detectDevMode would, from what the
// speculation read of installed.json, when that file is still the one it
// read (the dump of a no-op install reads it again otherwise: 400 KB of
// JSON for symfony's); false when it cannot.
func (g *Generator) takeSpeculatedDevMode(config Config) bool {
	s := g.speculation
	if s == nil || s.installedJSON == nil {
		return false
	}
	vendorDir, err := vendorDirConfig(config)
	if err != nil || vendorDir+"/composer/installed.json" != s.installedPath {
		return false
	}
	now, err := os.Stat(s.installedPath)
	if err != nil || !sameFileStamp(s.installedJSON, now) {
		return false
	}
	g.devModeSet, g.devMode, g.devModeValue = true, s.devMode, nil

	return true
}

// DiscardSpeculation drops the scan Speculate started, once it ended.
func (g *Generator) DiscardSpeculation() {
	if s := g.speculation; s != nil {
		g.speculation = nil
		<-s.done
	}
}

// takeSpeculation is the class map of the speculated scan when it scanned
// what a Dump with these values scans, nil otherwise.
func (g *Generator) takeSpeculation(d *dump, autoloads *Autoloads, scanPsrPackages bool) *classmap.ClassMap {
	s := g.speculation
	if s == nil {
		return nil
	}
	g.DiscardSpeculation()
	if s.err != nil || s.parser != g.Parser || s.scanPsrPackages != scanPsrPackages || s.basePath != d.basePath || s.vendorPath != d.vendorPath || !sameScans(s.autoloads, autoloads) {
		return nil
	}

	return s.classMap
}

// sameScans reports whether a and b give the same scans: the same
// classmap rules, exclusions and PSR-0/4 rules.
func sameScans(a, b *Autoloads) bool {
	return slices.Equal(a.Classmap, b.Classmap) &&
		slices.Equal(a.ExcludeFromClassmap, b.ExcludeFromClassmap) &&
		a.classmapValue == nil && b.classmapValue == nil &&
		sameArrays(a.PSR0, b.PSR0) && sameArrays(a.PSR4, b.PSR4)
}

// sameArrays is a === b.
func sameArrays(a, b *php.Array) bool {
	if a == nil || b == nil {
		return a == b
	}

	return php.StrictEquals(a, b)
}
