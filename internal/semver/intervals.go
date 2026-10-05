// Ports src/Intervals.php.

package semver

import (
	"sync"

	"github.com/stubbedev/maestro/internal/php"
)

// IntervalSet is what Intervals::get() returns: array{'numeric':
// Interval[], 'branches': array{'names': string[], 'exclude': bool}}.
//
// An empty Numeric means the constraint matches nothing in the numeric
// range (0 - +inf); Branches says which dev-* versions it matches. The
// slices are shared with the memoization cache and must not be modified.
type IntervalSet struct {
	Numeric  []Interval
	Branches DevBranches
}

// intervals is the namespace for Intervals' static methods.
type intervals struct{}

// Intervals ports Composer\Semver\Intervals: helpers generating intervals
// from constraints, to compact constraints and check whether one is a
// subset of another.
//
// Call Clear to free the memoization cache when done.
var Intervals intervals

var intervalsCache = struct {
	sync.RWMutex
	m map[string]IntervalSet
}{m: map[string]IntervalSet{}}

// opSortOrder ports Intervals::$opSortOrder.
func opSortOrder(op Op) int {
	switch op {
	case OpGE:
		return -3
	case OpLT:
		return -2
	case OpGT:
		return 2
	default: // OpLE: borders are never == or !=
		return 3
	}
}

// Clear ports Intervals::clear(): it clears the memoization cache.
func (intervals) Clear() {
	c := &intervalsCache
	c.Lock()
	c.m = map[string]IntervalSet{}
	c.Unlock()
}

// IsSubsetOf ports Intervals::isSubsetOf(): whether candidate is a subset
// of constraint.
func (intervals) IsSubsetOf(candidate, constraint ConstraintInterface) bool {
	if _, ok := constraint.(*MatchAllConstraint); ok {
		return true
	}

	if isMatchNone(candidate) || isMatchNone(constraint) {
		return false
	}

	intersectionIntervals := Intervals.Get(newMultiConstraint([]ConstraintInterface{candidate, constraint}, true))
	candidateIntervals := Intervals.Get(candidate)
	if len(intersectionIntervals.Numeric) != len(candidateIntervals.Numeric) {
		return false
	}

	for index, interval := range intersectionIntervals.Numeric {
		if !candidateIntervals.Numeric[index].start.sameString(interval.start) {
			return false
		}

		if !candidateIntervals.Numeric[index].end.sameString(interval.end) {
			return false
		}
	}

	intersection, candidates := intersectionIntervals.Branches, candidateIntervals.Branches
	if intersection.Exclude != candidates.Exclude {
		return false
	}
	if len(intersection.Names) != len(candidates.Names) {
		return false
	}
	for i, name := range intersection.Names {
		// The names are compared by array key, which an undefined key
		// never matches.
		if other, ok := candidates.lookup(intersection.key(i)); !ok || name != other {
			return false
		}
	}

	return true
}

func isMatchNone(c ConstraintInterface) bool {
	_, ok := c.(*MatchNoneConstraint)

	return ok
}

// HaveIntersections ports Intervals::haveIntersections(): whether a and b
// have any intersection, equivalent to a.Matches(b).
func (intervals) HaveIntersections(a, b ConstraintInterface) bool {
	if _, ok := a.(*MatchAllConstraint); ok {
		return true
	}
	if _, ok := b.(*MatchAllConstraint); ok {
		return true
	}

	if isMatchNone(a) || isMatchNone(b) {
		return false
	}

	intersectionIntervals := generateIntervals(newMultiConstraint([]ConstraintInterface{a, b}, true), true)

	return len(intersectionIntervals.Numeric) > 0 || intersectionIntervals.Branches.Exclude ||
		len(intersectionIntervals.Branches.Names) > 0
}

// CompactConstraint ports Intervals::compactConstraint(): it attempts to
// optimize a MultiConstraint.
//
// When merging MultiConstraints together they can get very large, this
// compacts it by looking at the real intervals covered by all the
// constraints and then creates a new constraint containing only the
// smallest amount of rules to match the same intervals.
func (intervals) CompactConstraint(constraint ConstraintInterface) ConstraintInterface {
	multi, ok := constraint.(*MultiConstraint)
	if !ok {
		return constraint
	}

	intervals := generateIntervals(multi, false)
	var constraints []ConstraintInterface
	hasNumericMatchAll := false

	if len(intervals.Numeric) == 1 && intervals.Numeric[0].start.sameString(intervalZero) &&
		intervals.Numeric[0].end.sameString(intervalPositiveInfinity) {
		constraints = append(constraints, intervals.Numeric[0].start)
		hasNumericMatchAll = true
	} else {
		var unEqualConstraints []ConstraintInterface
		for i, count := 0, len(intervals.Numeric); i < count; i++ {
			interval := intervals.Numeric[i]

			// if current interval ends with < N and next interval begins with > N we can swap this out for != N
			// but this needs to happen as a conjunctive expression together with the start of the current interval
			// and end of next interval, so [>=M, <N] || [>N, <P] => [>=M, !=N, <P] but M/P can be skipped if
			// they are zero/+inf
			if interval.end.operator == OpLT && i+1 < count {
				nextInterval := intervals.Numeric[i+1]
				if interval.end.version == nextInterval.start.version && nextInterval.start.operator == OpGT {
					// only add a start if we didn't already do so, can be skipped if we're looking at second
					// interval in [>=M, <N] || [>N, <P] || [>P, <Q] where unEqualConstraints currently contains
					// [>=M, !=N] already and we only want to add !=P right now
					if len(unEqualConstraints) == 0 && !interval.start.sameString(intervalZero) {
						unEqualConstraints = append(unEqualConstraints, interval.start)
					}
					unEqualConstraints = append(unEqualConstraints, NewConstraintOp(OpNE, interval.end.version))

					continue
				}
			}

			if len(unEqualConstraints) > 0 {
				// this is where the end of the following interval of a != constraint is added as explained above
				if !interval.end.sameString(intervalPositiveInfinity) {
					unEqualConstraints = append(unEqualConstraints, interval.end)
				}

				// count is 1 if entire constraint is just one != expression
				if len(unEqualConstraints) > 1 {
					constraints = append(constraints, newMultiConstraint(unEqualConstraints, true))
				} else {
					constraints = append(constraints, unEqualConstraints[0])
				}

				unEqualConstraints = nil

				continue
			}

			// convert back >= x - <= x intervals to == x
			if interval.start.version == interval.end.version && interval.start.operator == OpGE &&
				interval.end.operator == OpLE {
				constraints = append(constraints, NewConstraintOp(OpEQ, interval.start.version))

				continue
			}

			switch {
			case interval.start.sameString(intervalZero):
				constraints = append(constraints, interval.end)
			case interval.end.sameString(intervalPositiveInfinity):
				constraints = append(constraints, interval.start)
			default:
				constraints = append(constraints, newMultiConstraint([]ConstraintInterface{interval.start, interval.end}, true))
			}
		}
	}

	branches := intervals.Branches
	if len(branches.Names) == 0 {
		if branches.Exclude && hasNumericMatchAll {
			return NewMatchAllConstraint()
		}
		// otherwise constraint should contain a != operator and already cover this
	} else {
		devOp := OpEQ
		if branches.Exclude {
			devOp = OpNE
		}
		devConstraints := make([]ConstraintInterface, len(branches.Names))
		for i, branchName := range branches.Names {
			devConstraints[i] = NewConstraintOp(devOp, branchName)
		}

		// excluded branches, e.g. != dev-foo are conjunctive with the interval, so
		// > 2.0 != dev-foo must return a conjunctive constraint
		if branches.Exclude {
			if len(constraints) > 1 {
				return newMultiConstraint(append(
					[]ConstraintInterface{newMultiConstraint(constraints, false)},
					devConstraints...,
				), true)
			}

			if c, ok := constraints[0].(*Constraint); len(constraints) == 1 && ok && c.sameString(intervalZero) {
				if len(devConstraints) > 1 {
					return newMultiConstraint(devConstraints, true)
				}

				return devConstraints[0]
			}

			// array_merge($constraints, $devConstraints) has at least two
			// entries: one numeric constraint at most, but the names
			// are not empty.
			return newMultiConstraint(append(constraints, devConstraints...), true)
		}

		// otherwise devConstraints contains a list of == operators for branches which are disjunctive with the
		// rest of the constraint
		constraints = append(constraints, devConstraints...)
	}

	if len(constraints) > 1 {
		return newMultiConstraint(constraints, false)
	}

	if len(constraints) == 1 {
		return constraints[0]
	}

	return NewMatchNoneConstraint()
}

// Get ports Intervals::get(): the numeric intervals and branch constraints
// representing a constraint, memoized by its string form.
func (intervals) Get(constraint ConstraintInterface) IntervalSet {
	key := constraint.String()

	c := &intervalsCache
	c.RLock()
	set, ok := c.m[key]
	c.RUnlock()
	if ok {
		return set
	}

	set = generateIntervals(constraint, false)
	c.Lock()
	if cached, ok := c.m[key]; ok {
		// Another goroutine got there first; PHP would have kept that one.
		set = cached
	} else {
		c.m[key] = set
	}
	c.Unlock()

	return set
}

// generateIntervals ports Intervals::generateIntervals().
func generateIntervals(constraint ConstraintInterface, stopOnFirstValidInterval bool) IntervalSet {
	switch c := constraint.(type) {
	case *MatchAllConstraint:
		return IntervalSet{Numeric: []Interval{IntervalAny()}, Branches: AnyDev()}
	case *MatchNoneConstraint:
		return IntervalSet{Branches: NoDev()}
	case *Constraint:
		return generateSingleConstraintIntervals(c)
	case *MultiConstraint:
		return generateMultiConstraintIntervals(c, stopOnFirstValidInterval)
	}

	panic("semver: unknown ConstraintInterface implementation")
}

// border is one end of an interval, as generateIntervals() sorts them.
type border struct {
	version string
	op      Op
	start   bool
}

func generateMultiConstraintIntervals(constraint *MultiConstraint, stopOnFirstValidInterval bool) IntervalSet {
	constraints := constraint.constraints

	numericGroups := make([][]Interval, len(constraints))
	var branches DevBranches
	if constraint.conjunctive {
		branches = AnyDev()
	} else {
		branches = NoDev()
	}
	numericCount := 0
	for i, c := range constraints {
		res := Intervals.Get(c)
		numericGroups[i] = res.Numeric
		numericCount += len(res.Numeric)
		branches = mergeBranches(branches, res.Branches, constraint.conjunctive)
	}

	branches.Names, branches.keys = arrayUnique(branches)

	if len(numericGroups) == 1 {
		return IntervalSet{Numeric: numericGroups[0], Branches: branches}
	}

	borders := make([]border, 0, 2*numericCount)
	for _, group := range numericGroups {
		for _, interval := range group {
			borders = append(borders,
				border{version: interval.start.version, op: interval.start.operator, start: true},
				border{version: interval.end.version, op: interval.end.operator})
		}
	}

	php.SortSlice(borders, func(a, b border) int {
		if order := VersionCompare(a.version, b.version); order != 0 {
			return order
		}

		return opSortOrder(a.op) - opSortOrder(b.op)
	})

	activeIntervals := 0
	var intervals []Interval
	activationThreshold := 1
	if constraint.conjunctive {
		activationThreshold = len(numericGroups)
	}
	var start *Constraint
	for _, border := range borders {
		if border.start {
			activeIntervals++
		} else {
			activeIntervals--
		}
		if start == nil && activeIntervals >= activationThreshold {
			start = NewConstraintOp(border.op, border.version)
		} else if start != nil && activeIntervals < activationThreshold {
			// filter out invalid intervals like > x - <= x, or >= x - < x
			if versionCompareOp(start.version, border.version, OpEQ) &&
				((start.operator == OpGT && border.op == OpLE) || (start.operator == OpGE && border.op == OpLT)) {
				// unset($intervals[$index]) of an index not set yet: no-op
			} else {
				intervals = append(intervals, NewInterval(start, NewConstraintOp(border.op, border.version)))

				if stopOnFirstValidInterval {
					break
				}
			}

			start = nil
		}
	}

	return IntervalSet{Numeric: intervals, Branches: branches}
}

// mergeBranches folds the branches of one more constraint into those of a
// disjunctive or conjunctive multi constraint.
func mergeBranches(branches, b DevBranches, conjunctive bool) DevBranches {
	if !conjunctive {
		if b.Exclude {
			if branches.Exclude {
				// disjunctive constraint, so only exclude what's excluded in all constraints
				// !=a,!=b || !=b,!=c => !=b
				branches.Names, branches.keys = arrayIntersect(branches, b)
			} else {
				// disjunctive constraint so exclude all names which are not explicitly included in the alternative
				// (==b || ==c) || !=a,!=b => !=a
				branches.Exclude = true
				branches.Names, branches.keys = arrayDiff(b, branches)
			}
		} else {
			if branches.Exclude {
				// disjunctive constraint so exclude all names which are not explicitly included in the alternative
				// !=a,!=b || (==b || ==c) => !=a
				branches.Names, branches.keys = arrayDiff(branches, b)
			} else {
				// disjunctive constraint, so just add all the other branches
				// (==a || ==b) || ==c => ==a || ==b || ==c
				branches.Names, branches.keys = arrayMerge(branches, b), nil
			}
		}

		return branches
	}

	if b.Exclude {
		if branches.Exclude {
			// conjunctive, so just add all branch names to be excluded
			// !=a && !=b => !=a,!=b
			branches.Names, branches.keys = arrayMerge(branches, b), nil
		} else {
			// conjunctive, so only keep included names which are not excluded
			// (==a||==c) && !=a,!=b => ==c
			branches.Names, branches.keys = arrayDiff(branches, b)
		}
	} else {
		if branches.Exclude {
			// conjunctive, so only keep included names which are not excluded
			// !=a,!=b && (==a||==c) => ==c
			branches.Names, branches.keys = arrayDiff(b, branches)
			branches.Exclude = false
		} else {
			// conjunctive, so only keep names that are included in both
			// (==a||==b) && (==a||==c) => ==a
			branches.Names, branches.keys = arrayIntersect(branches, b)
		}
	}

	return branches
}

// generateSingleConstraintIntervals ports
// Intervals::generateSingleConstraintIntervals().
func generateSingleConstraintIntervals(constraint *Constraint) IntervalSet {
	op := constraint.operator

	// handle branch constraints first
	if isBranch(constraint.version) {
		// != dev-foo means any numeric version may match, we treat >/< like != they are not really defined for branches
		switch op {
		case OpNE:
			return IntervalSet{
				Numeric:  []Interval{IntervalAny()},
				Branches: DevBranches{Names: []string{constraint.version}, Exclude: true},
			}
		case OpEQ:
			return IntervalSet{Branches: DevBranches{Names: []string{constraint.version}}}
		}

		return IntervalSet{Branches: NoDev()}
	}

	switch op {
	case OpGT, OpGE:
		return IntervalSet{Numeric: []Interval{NewInterval(constraint, intervalPositiveInfinity)}, Branches: NoDev()}
	case OpLT, OpLE:
		return IntervalSet{Numeric: []Interval{NewInterval(intervalZero, constraint)}, Branches: NoDev()}
	case OpNE:
		// convert !=x to intervals of 0 - <x && >x - +inf + dev*
		return IntervalSet{Numeric: []Interval{
			NewInterval(intervalZero, NewConstraintOp(OpLT, constraint.version)),
			NewInterval(NewConstraintOp(OpGT, constraint.version), intervalPositiveInfinity),
		}, Branches: AnyDev()}
	}

	// convert ==x to an interval of >=x - <=x
	return IntervalSet{Numeric: []Interval{
		NewInterval(NewConstraintOp(OpGE, constraint.version), NewConstraintOp(OpLE, constraint.version)),
	}, Branches: NoDev()}
}
