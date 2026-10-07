// Ports nothing: skeleton packages, loaded with what the solver reads and
// completed on first use (deliberate deviation 3, speed).

package loader

import (
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// SkeletonConfig is the part of a version's config a skeleton package is
// loaded from (LoadSkeleton): what createObject, the links,
// configureType, configureDefaultBranch, configureAbandoned and
// GetBranchAlias read of it.
func SkeletonConfig(config *php.Array) *php.Array {
	skeleton := php.NewArrayCap(12)
	for _, key := range [...]string{"name", "version", "version_normalized", "type", "default-branch", "abandoned"} {
		if v, ok := config.Get(key); ok {
			skeleton.Set(key, v)
		}
	}
	for _, t := range pkg.SupportedLinkTypes() {
		if v, ok := config.Get(t.Type); ok {
			skeleton.Set(t.Type, v)
		}
	}
	if aliases, ok := config.ArrayAt("extra").Get("branch-alias"); ok {
		skeleton.Set("extra", php.ArrayOf("branch-alias", aliases))
	}

	return skeleton
}

// LoadSkeleton is Load for a version a SkeletonChecker tells fits, from its
// SkeletonConfig: the package (or its branch alias) has the properties
// the solver reads, and takes the others from full, the version's whole
// config, when one of them is first used (pkg.NewSkeletonPackage).
func (b *PackageBatch) LoadSkeleton(skeleton *php.Array, full func() *php.Array) (pkg.PackageInterface, error) {
	p, err := b.l.createObject(skeleton, pkg.ClassCompletePackage)
	if err != nil {
		return nil, err
	}

	if err := b.l.configureCachedLinks(b.cache, p, skeleton); err != nil {
		return nil, err
	}

	cp, _ := pkg.AsCompletePackage(p)
	if err := configureType(cp, skeleton); err != nil {
		return nil, err
	}
	configureDefaultBranch(cp, skeleton)
	configureAbandoned(cp, skeleton)

	l := b.l
	pkg.NewSkeletonPackage(cp, func() (*pkg.CompletePackage, error) { return l.loadFull(full()) })

	return l.aliased(cp, skeleton)
}

// loadFull is the package Load builds from config, without its links
// (which a skeleton has).
func (l *ArrayLoader) loadFull(config *php.Array) (*pkg.CompletePackage, error) {
	p, err := l.createObject(config, pkg.ClassCompletePackage)
	if err != nil {
		return nil, err
	}

	if _, err := l.configureObject(p, config); err != nil {
		return nil, err
	}

	cp, _ := pkg.AsCompletePackage(p)

	return cp, nil
}

// SkeletonChecker tells which versions load as skeletons, in place of
// Load: a version does when Load succeeds with it (also with transport
// options), and loading the rest of it later gives what loading it now
// gives: Load has no side effect (the deprecation of a reserved script
// name), and the release date does not depend on the current time. The
// skeleton is then the package Load builds: LoadSkeleton runs the steps
// of Load that set a skeleton's properties on SkeletonConfig, which holds
// all they read of the version. The versions of a list share their links,
// as LoadPackages has them share.
type SkeletonChecker struct {
	l     *ArrayLoader
	batch *PackageBatch
}

// SkeletonChecker returns a SkeletonChecker for versions loaded with l's
// parser.
func (l *ArrayLoader) SkeletonChecker() *SkeletonChecker {
	strict := NewArrayLoader(l.versionParser, true)

	return &SkeletonChecker{l: strict, batch: strict.Batch()}
}

// Fits reports whether a version, config (with the notification-url it
// is loaded with, or any string standing for it), loads as a skeleton.
func (c *SkeletonChecker) Fits(config *php.Array) bool {
	if scripts := config.ArrayAt("scripts"); scripts != nil {
		for _, reserved := range [...]string{"composer", "php", "putenv"} {
			if scripts.Has(reserved) {
				return false
			}
		}
	}
	if !releaseDateIsAbsolute(config) {
		return false
	}
	if _, err := c.batch.Load(config); err != nil {
		// a batch is not used after an error
		c.batch = c.l.Batch()

		return false
	}

	return true
}

// releaseDateIsAbsolute reports whether the release date configureFields
// reads from config is the same whatever the current time.
func releaseDateIsAbsolute(config *php.Array) bool {
	t := config.At("time")
	if empty(t) {
		return true
	}
	s, ok := t.(string)
	if !ok {
		// configureFields fails
		return false
	}
	if mustMatch(digitsOnly, s) {
		s = "@" + s
	}

	// two current times that differ in every field
	a, errA := parseDateTimeAt(s, time.Date(2001, 2, 3, 4, 5, 6, 7, time.UTC))
	b, errB := parseDateTimeAt(s, time.Date(2012, 11, 22, 13, 24, 35, 46, time.UTC))
	if errA != nil || errB != nil {
		return errA != nil && errB != nil
	}

	return a.Equal(b) && a.Location().String() == b.Location().String()
}
