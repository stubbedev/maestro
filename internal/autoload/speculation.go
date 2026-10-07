// Ports nothing: a class map scan run ahead of the dump that needs it
// (deliberate deviation 3, speed).

package autoload

import (
	"slices"

	"github.com/stubbedev/maestro/internal/classmap"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// speculation is a class map scan started by Speculate.
type speculation struct {
	scan *util.Ahead[scanKey, *scanResult]

	// devMode is installed.json's "dev" as the speculation read it, from
	// the file installedJSON stamps (the zero Stamp when it was not
	// read, or changed while it was read).
	devMode       devMode
	installedPath string
	installedJSON fsstate.Stamp
}

// scanKey is what a class map scan is made of: the Dump that takes a
// speculated scan is one whose key is the same.
type scanKey struct {
	scanInputs
	autoloads *Autoloads
}

// scanInputs are the comparable part of a scanKey. strictAmbiguous is
// how the scan's result was analysed.
type scanInputs struct {
	parser                           classmap.Parser
	scanPsrPackages, strictAmbiguous bool
	basePath, vendorPath             slashPath
}

// scanKey is the key of the scan of autoloads for the dump d.
func (g *Generator) scanKey(d *dump, autoloads *Autoloads, scanPsrPackages, strictAmbiguous bool) scanKey {
	return scanKey{scanInputs{g.Parser, scanPsrPackages, strictAmbiguous, d.basePath, d.vendorPath}, autoloads}
}

// same reports whether a and b give the same scan.
func (a scanKey) same(b scanKey) bool {
	return a.scanInputs == b.scanInputs && sameScans(a.autoloads, b.autoloads)
}

// scanResult is the result of a speculated scan.
type scanResult struct {
	classMap *classmap.ClassMap
	// warnings are what scan prints about classMap, which is completed
	// as scan completes it (analyseClassMap, without strictAmbiguous).
	warnings []string
	// ahead is the dump of a target dir "composer" with the class map's
	// files built (dump.classmap), nil without one; current are the
	// files the dump writes as they were then (readCurrent).
	ahead   *dump
	current map[string]fsstate.Snapshot
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
	var installedJSON fsstate.Stamp
	if !devMode.known {
		var err error
		if devMode, installedPath, installedJSON, err = installedDevModeStamped(config); err != nil || devMode.raw != nil {
			return
		}
	}
	d, err := lookAheadDump(config)
	if err != nil {
		return
	}

	packageMap, err := g.BuildPackageMap(im, rootPackage, localRepo.CanonicalPackages())
	if err != nil {
		return
	}
	autoloads, err := g.parseAutoloads(packageMap, rootPackage, devFilter(devMode.on, localRepo.DevPackageNames()), devMode.on)
	if err != nil {
		return
	}

	ahead := aheadDump(d, "composer")
	key := g.scanKey(d, autoloads, scanPsrPackages, false)
	g.speculation = &speculation{
		scan: util.StartAheadFunc(key, scanKey.same, func() (*scanResult, error) {
			s := &scanResult{}
			var err error
			if s.classMap, err = g.scanClassMap(d, autoloads, packageMap, scanPsrPackages); err != nil {
				return nil, err
			}
			if s.warnings, err = analyseClassMap(s.classMap, string(d.vendorPath), false); err != nil {
				return nil, err
			}
			if ahead != nil && ahead.classmap(s.classMap) == nil {
				s.ahead = ahead
				s.current = readCurrent(ahead)
			}

			return s, nil
		}, nil),
		devMode:       devMode,
		installedPath: installedPath,
		installedJSON: installedJSON,
	}
}

// aheadDump is the dump newDump(config, targetDir) makes for the base and
// vendor paths of d, when vendor-dir/targetDir exists (newDump would
// create it); nil otherwise.
func aheadDump(d *dump, targetDir string) *dump {
	paths, err := pathsFor(d.basePath, d.vendorPath, targetDir, false)
	if err != nil {
		return nil
	}

	return &dump{dumpPaths: paths}
}

// readCurrent reads the files of d a dump writes with putIfModified, for
// it to compare them with what it writes without reading them again
// while they did not change (unchanged).
func readCurrent(d *dump) map[string]fsstate.Snapshot {
	current := make(map[string]fsstate.Snapshot, len(dumpFiles)+1)
	read := func(path string) {
		if s, ok := fsstate.ReadStable(path); ok {
			current[path] = s
		}
	}
	for _, name := range dumpFiles {
		read(string(d.targetDir) + "/" + name)
	}
	read(string(d.vendorPath + "/autoload.php"))

	return current
}

// dumpFiles are the files of the target dir write writes.
var dumpFiles = []string{"autoload_namespaces.php", "autoload_psr4.php", "autoload_classmap.php", "include_paths.php", "autoload_files.php", "autoload_static.php", "platform_check.php", "autoload_real.php", "ClassLoader.php", "LICENSE"}

// unchanged reports whether path holds content, as the speculation read
// it, when it did not change since.
func (s *scanResult) unchanged(path, content string) bool {
	f, ok := s.current[path]

	return ok && f.Holds(content)
}

// takeClassmap sets d's class map files to those the speculation built,
// when it built them for d's paths (all dump.classmap reads besides the
// class map).
func (s *scanResult) takeClassmap(d *dump) bool {
	a := s.ahead
	if a == nil || a.dumpPaths != d.dumpPaths {
		return false
	}
	d.classes, d.classPaths, d.classmapFile, d.staticClassMap = a.classes, a.classPaths, a.classmapFile, a.staticClassMap

	return true
}

// installedDevModeStamped is installedDevMode, also returning the path of
// installed.json and its stamp when it was there and did not change while
// it was read (the zero Stamp otherwise).
func installedDevModeStamped(config Config) (dev devMode, path string, stamp fsstate.Stamp, err error) {
	vendorDir, err := vendorDirConfig(config)
	if err != nil {
		return devMode{}, "", fsstate.Stamp{}, err
	}
	path = vendorDir + "/composer/installed.json"
	before, _ := fsstate.StatStamp(path)
	if dev, err = installedDevMode(config); err != nil {
		return dev, path, fsstate.Stamp{}, err
	}
	if after, _ := fsstate.StatStamp(path); before.Same(after) {
		stamp = after
	}

	return dev, path, stamp, nil
}

// takeSpeculatedDevMode sets devMode as detectDevMode would, from what the
// speculation read of installed.json, when that file is still the one it
// read (the dump of a no-op install reads it again otherwise: 400 KB of
// JSON for symfony's); false when it cannot.
func (g *Generator) takeSpeculatedDevMode(config Config) bool {
	s := g.speculation
	if s == nil {
		return false
	}
	vendorDir, err := vendorDirConfig(config)
	if err != nil || vendorDir+"/composer/installed.json" != s.installedPath {
		return false
	}
	if !s.installedJSON.Unchanged(s.installedPath) {
		return false
	}
	g.devMode = s.devMode

	return true
}

// DiscardSpeculation drops the scan Speculate started, once it ended.
func (g *Generator) DiscardSpeculation() {
	if s := g.speculation; s != nil {
		g.speculation = nil
		s.scan.Discard()
	}
}

// takeSpeculation is the speculated scan when it scanned what a Dump with
// these values scans, and analysed it as that Dump does; nil otherwise.
// The speculation is dropped either way.
func (g *Generator) takeSpeculation(d *dump, autoloads *Autoloads, scanPsrPackages, strictAmbiguous bool) *scanResult {
	s := g.speculation
	if s == nil {
		return nil
	}
	g.speculation = nil
	taken, _ := s.scan.Take(g.scanKey(d, autoloads, scanPsrPackages, strictAmbiguous))

	return taken
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
