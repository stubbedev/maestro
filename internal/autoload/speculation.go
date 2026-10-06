// Ports nothing: a class map scan run ahead of the dump that needs it
// (deliberate deviation 3, speed).

package autoload

import (
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
	if !g.devModeSet {
		var value any
		var err error
		if devMode, value, err = installedDevMode(config); err != nil || value != nil {
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
	// parseAutoloads reads the root's autoload-dev by $this->devMode
	saved := g.devMode
	g.devMode = devMode
	autoloads, err := g.ParseAutoloads(packageMap, rootPackage, devFilter(devMode, localRepo.DevPackageNames()))
	g.devMode = saved
	if err != nil {
		return
	}
	g.addReleases(packageMap)

	s := &speculation{
		parser:          g.Parser,
		scanPsrPackages: scanPsrPackages,
		basePath:        d.basePath,
		vendorPath:      d.vendorPath,
		autoloads:       autoloads,
		done:            make(chan struct{}),
	}
	g.speculation = s
	go func() {
		defer close(s.done)
		s.classMap, s.err = g.scanClassMap(d, autoloads, scanPsrPackages)
	}()
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
