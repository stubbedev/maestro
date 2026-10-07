// Ports src/CompilingMatcher.php.

package semver

import (
	"strconv"
	"strings"
	"sync"
)

// compiledMatcher is what ConstraintInterface::compile() returns, as an
// evaluable function instead of PHP source. kind distinguishes the literal
// 'true' and 'false' results, which MultiConstraint::compile() folds.
type compiledMatcher struct {
	kind matcherKind
	fn   func(v string, b bool) bool
}

type matcherKind uint8

const (
	matcherKindExpr matcherKind = iota
	matcherKindTrue
	matcherKindFalse
)

var (
	matcherTrue  = compiledMatcher{kind: matcherKindTrue}
	matcherFalse = compiledMatcher{kind: matcherKindFalse}
)

func matcherExpr(fn func(v string, b bool) bool) compiledMatcher {
	return compiledMatcher{kind: matcherKindExpr, fn: fn}
}

// eval runs the compiled code with $v = v and $b = b.
func (m compiledMatcher) eval(v string, b bool) bool {
	switch m.kind {
	case matcherKindTrue:
		return true
	case matcherKindFalse:
		return false
	default:
		return m.fn(v, b)
	}
}

// compilingMatcher is the namespace for CompilingMatcher's static methods.
type compilingMatcher struct{}

// CompilingMatcher ports Composer\Semver\CompilingMatcher: it compiles a
// constraint for an operator once and caches every result. Its results are
// those of the compiled code, which differ from Matches() in a few corner
// cases, exactly as in PHP.
var CompilingMatcher compilingMatcher

var compilingMatcherCache = struct {
	sync.RWMutex
	compiled map[string]compiledMatcher
	results  map[string]bool
}{
	compiled: map[string]compiledMatcher{},
	results:  map[string]bool{},
}

// Clear ports CompilingMatcher::clear().
func (compilingMatcher) Clear() {
	c := &compilingMatcherCache
	c.Lock()
	c.compiled = map[string]compiledMatcher{}
	c.results = map[string]bool{}
	c.Unlock()
}

// Match ports CompilingMatcher::match($constraint, $operator, $version):
// whether version, with the given operator, matches constraint. operator
// must be one of the Op constants.
//
// Cache hits do not allocate.
func (compilingMatcher) Match(constraint ConstraintInterface, operator Op, version string) bool {
	// $resultCacheKey = $operator.$constraint.';'.$version
	var buf [128]byte
	key := strconv.AppendInt(buf[:0], int64(operator), 10)
	key = appendConstraintString(key, constraint)
	checkerKeyLen := len(key) // $cacheKey = $operator.$constraint
	key = append(key, ';')
	key = append(key, version...)

	c := &compilingMatcherCache
	c.RLock()
	result, ok := c.results[string(key)]
	if ok {
		c.RUnlock()

		return result
	}
	checker, ok := c.compiled[string(key[:checkerKeyLen])]
	c.RUnlock()

	if !ok {
		checker = constraint.compile(operator)
		c.Lock()
		c.compiled[string(key[:checkerKeyLen])] = checker
		c.Unlock()
	}

	result = checker.eval(version, strings.HasPrefix(version, "dev-"))
	c.Lock()
	c.results[string(key)] = result
	c.Unlock()

	return result
}

// Matcher returns Match(constraint, operator, version) as a function of
// version, for matching one constraint against many versions: it finds
// (or compiles) the constraint's checker once, and leaves the result
// cache alone, whose entries are what the checker gives. It is safe to
// call the function from several goroutines at once.
func (compilingMatcher) Matcher(constraint ConstraintInterface, operator Op) func(version string) bool {
	var buf [128]byte
	key := strconv.AppendInt(buf[:0], int64(operator), 10)
	key = appendConstraintString(key, constraint)

	c := &compilingMatcherCache
	c.RLock()
	checker, ok := c.compiled[string(key)]
	c.RUnlock()

	if !ok {
		checker = constraint.compile(operator)
		c.Lock()
		c.compiled[string(key)] = checker
		c.Unlock()
	}

	return func(version string) bool {
		return checker.eval(version, strings.HasPrefix(version, "dev-"))
	}
}

// appendConstraintString appends (string) $constraint. It dispatches
// statically so that dst can stay on the caller's stack.
func appendConstraintString(dst []byte, constraint ConstraintInterface) []byte {
	switch c := constraint.(type) {
	case *Constraint:
		return c.appendString(dst)
	case *MultiConstraint:
		return c.appendString(dst)
	default:
		return append(dst, c.String()...)
	}
}
