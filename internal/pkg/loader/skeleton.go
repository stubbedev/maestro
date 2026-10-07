// Ports nothing: skeleton packages, loaded with what the solver reads and
// completed on first use (deliberate deviation 3, speed).

package loader

import (
	"iter"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// SkeletonConfig is the part of a version's config a skeleton package is
// loaded from (LoadSkeleton): what createObject, the links,
// configureType, configureExtra, configureDefaultBranch,
// configureAbandoned and GetBranchAlias read of it.
func SkeletonConfig(config *php.Array) *php.Array {
	skeleton := php.NewArrayCap(len(SkeletonKeys))
	for key, v := range SkeletonFields(config) {
		SetSkeletonField(skeleton, key, v)
	}

	return skeleton
}

// SkeletonKeys are the keys of a SkeletonConfig, in its order; the links'
// are in pkg.SupportedLinkTypes' order.
var SkeletonKeys = [...]string{
	"name", "version", "version_normalized", "type", "default-branch", "abandoned",
	"require", "conflict", "provide", "replace", "require-dev", "extra",
}

// SkeletonFields yields the keys of SkeletonConfig(config), in its order,
// as their indexes in SkeletonKeys, and their values (SetSkeletonField
// sets them), found in one pass.
func SkeletonFields(config *php.Array) iter.Seq2[int, any] {
	return func(yield func(int, any) bool) {
		var (
			values [len(SkeletonKeys)]any
			found  [len(SkeletonKeys)]bool
		)
		for k, v := range config.All() {
			if key := skeletonKey(k); key >= 0 {
				values[key], found[key] = v, true
			}
		}
		for key, v := range values {
			if found[key] && !yield(key, v) {
				return
			}
		}
	}
}

// skeletonKey is the index in SkeletonKeys of k; -1 for any other key.
func skeletonKey(k php.Key) int {
	if !k.IsString() {
		return -1
	}
	switch k.String() {
	case "name":
		return 0
	case "version":
		return 1
	case "version_normalized":
		return 2
	case "type":
		return 3
	case "default-branch":
		return 4
	case "abandoned":
		return 5
	case "require":
		return 6
	case "conflict":
		return 7
	case "provide":
		return 8
	case "replace":
		return 9
	case "require-dev":
		return 10
	case "extra":
		return 11
	}

	return -1
}

// SetSkeletonField sets the key at index key in SkeletonKeys of skeleton,
// a SkeletonConfig, to v, its value as SkeletonFields yields it.
func SetSkeletonField(skeleton *php.Array, key int, v any) {
	skeleton.Set(SkeletonKeys[key], v)
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
	configureExtra(cp, skeleton)
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
	plain := NewArrayLoader(l.versionParser, false)

	return &SkeletonChecker{l: plain, batch: plain.Batch()}
}

// Fits reports whether a version, config (with the notification-url it
// is loaded with, or any string standing for it), loads as a skeleton.
func (c *SkeletonChecker) Fits(config *php.Array) bool {
	if !LoadedFits(config) {
		return false
	}
	if _, err := c.batch.Load(config); err != nil {
		// a batch is not used after an error
		c.batch = c.l.Batch()

		return false
	}

	return true
}

// LoadedFits is SkeletonChecker.Fits for a version, config, that a
// PackageBatch.Load of any loader loaded without error: Load succeeds
// with it with or without transport options alike but for them (they
// are configured last), and the version fits unless its transport
// options fail or loading the rest of it later would not give what
// loading it now gives.
func LoadedFits(config *php.Array) bool {
	if scripts := config.ArrayAt("scripts"); scripts != nil {
		for _, reserved := range [...]string{"composer", "php", "putenv"} {
			if scripts.Has(reserved) {
				return false
			}
		}
	}
	if config.Isset("transport-options") {
		if _, err := transportOptions(config); err != nil {
			return false
		}
	}

	return releaseDateIsAbsolute(config)
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

	// every field given: Packagist's form
	return isoWithOffset(s) || parsesAlike(s)
}

// parsesAlike reports whether the release date configureFields reads
// from s is the same whatever the current time.
func parsesAlike(s string) bool {
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

// isoWithOffset reports whether s is a date and time with every field
// and a UTC offset, in the form 2006-01-02T15:04:05+07:00, which parses
// alike whatever the current time.
func isoWithOffset(s string) bool {
	const form = "dddd-dd-ddTdd:dd:dd+dd:dd"
	if len(s) != len(form) {
		return false
	}
	for i := range len(form) {
		switch c := s[i]; form[i] {
		case 'd':
			if c < '0' || c > '9' {
				return false
			}
		case '+':
			if c != '+' && c != '-' {
				return false
			}
		default:
			if c != form[i] {
				return false
			}
		}
	}

	return true
}
