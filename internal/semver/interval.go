// Ports src/Interval.php.

package semver

// Interval ports Composer\Semver\Interval: a numeric range between two
// constraints.
type Interval struct {
	start, end *Constraint
}

// NewInterval ports new Interval($start, $end).
func NewInterval(start, end *Constraint) Interval { return Interval{start: start, end: end} }

// Start ports getStart().
func (i Interval) Start() *Constraint { return i.start }

// End ports getEnd().
func (i Interval) End() *Constraint { return i.end }

// The shared instances behind Interval::fromZero() and
// Interval::untilPositiveInfinity(), which PHP keeps in static variables.
var (
	intervalZero             = NewConstraintOp(OpGE, zeroVersion)
	intervalPositiveInfinity = NewConstraintOp(OpLT, positiveInfinityVersion)
)

// IntervalFromZero ports Interval::fromZero(): the shared >= 0.0.0.0-dev
// constraint.
func IntervalFromZero() *Constraint { return intervalZero }

// IntervalUntilPositiveInfinity ports Interval::untilPositiveInfinity(): the
// shared < PHP_INT_MAX.0.0.0 constraint.
func IntervalUntilPositiveInfinity() *Constraint { return intervalPositiveInfinity }

// IntervalAny ports Interval::any().
func IntervalAny() Interval { return NewInterval(intervalZero, intervalPositiveInfinity) }

// DevBranches is the 'branches' part of an interval set: array{'names':
// string[], 'exclude': bool}. With Exclude, every dev branch except Names
// matches; without it, only Names do.
type DevBranches struct {
	Names   []string
	Exclude bool

	// keys are the PHP array keys of Names, which array_diff(),
	// array_intersect() and array_unique() preserve; nil means 0..n-1.
	// Intervals::isSubsetOf() looks names up by key.
	keys []int
}

// AnyDev ports Interval::anyDev(): any dev branch (exclude nothing).
func AnyDev() DevBranches { return DevBranches{Exclude: true} }

// NoDev ports Interval::noDev(): no dev branch.
func NoDev() DevBranches { return DevBranches{} }

// key returns the PHP array key of Names[i].
func (d DevBranches) key(i int) int {
	if d.keys == nil {
		return i
	}

	return d.keys[i]
}

// lookup returns the name at PHP array key k.
func (d DevBranches) lookup(k int) (string, bool) {
	if d.keys == nil {
		if k >= 0 && k < len(d.Names) {
			return d.Names[k], true
		}

		return "", false
	}
	for i, key := range d.keys {
		if key == k {
			return d.Names[i], true
		}
	}

	return "", false
}

func (d DevBranches) contains(name string) bool {
	for _, n := range d.Names {
		if n == name {
			return true
		}
	}

	return false
}

// filterNames keeps the names of a for which keep returns true, with their
// keys, as array_intersect() and array_diff() do.
func filterNames(a DevBranches, keep func(string) bool) ([]string, []int) {
	var names []string
	var keys []int
	sequential := true
	for i, n := range a.Names {
		if keep(n) {
			k := a.key(i)
			sequential = sequential && k == len(names)
			names = append(names, n)
			keys = append(keys, k)
		}
	}
	if sequential {
		keys = nil
	}

	return names, keys
}

// arrayIntersect ports array_intersect($a['names'], $b['names']).
func arrayIntersect(a, b DevBranches) ([]string, []int) {
	return filterNames(a, b.contains)
}

// arrayDiff ports array_diff($a['names'], $b['names']).
func arrayDiff(a, b DevBranches) ([]string, []int) {
	return filterNames(a, func(n string) bool { return !b.contains(n) })
}

// arrayMerge ports array_merge($a['names'], $b['names']), which renumbers.
func arrayMerge(a, b DevBranches) ([]string, []int) {
	names := make([]string, 0, len(a.Names)+len(b.Names))
	names = append(names, a.Names...)

	return append(names, b.Names...), nil
}

// arrayUnique ports array_unique($names): the first occurrence of each name
// is kept, with its key.
func arrayUnique(d DevBranches) ([]string, []int) {
	if len(d.Names) <= 1 {
		return d.Names, d.keys
	}
	seen := make(map[string]struct{}, len(d.Names))

	return filterNames(d, func(n string) bool {
		if _, ok := seen[n]; ok {
			return false
		}
		seen[n] = struct{}{}

		return true
	})
}
