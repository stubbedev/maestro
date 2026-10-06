// Ports nothing: loading speculated ahead of the pool builder (deliberate
// deviation 3, speed).

package repository

import "github.com/stubbedev/maestro/internal/php"

// LoadSpeculator is a repository that can start, in the background and
// without output, the work its LoadPackages calls are about to need: the
// metadata of the names in roots (with their constraints) and,
// transitively, of what the acceptable versions of those require, which
// a pool builder asks for wave after wave. Later LoadPackages calls print,
// load and return what they would without it; a guess that turns out
// unneeded costs work, never a different result.
//
// skip tells which names the pool builder will not load (fixed or locked
// ones, names outside a restricted list); it is called on other
// goroutines. stop ends the speculation and frees what it kept but did
// not hand over.
type LoadSpeculator interface {
	SpeculateLoads(roots *ConstraintMap, skip func(name string) bool, acceptableStabilities, stabilityFlags *php.Array) (stop func())
}
