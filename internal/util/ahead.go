// Ports nothing: work started ahead of the code path that needs its
// result (deliberate deviation 3, speed).

package util

import "sync/atomic"

// Ahead is work started early from a key, for a later code path to take
// its result instead of doing the work itself, but only when what it
// would work from is still what the work started from. The key is
// captured when the work starts and compared with the taker's when it
// takes: it must describe the state the work reads (contents, revision
// counters), not only which objects it read.
//
// An Ahead ends exactly once, by the first Take or Discard: a later Take
// gets nothing and a later Discard does nothing, so `defer a.Discard()`
// right after starting it covers every path that does not take it. Both
// wait for the work to end. The cleanup given to StartAhead runs once for
// a result that succeeded and nobody took. A nil *Ahead is one that never
// started.
type Ahead[K, V any] struct {
	key     K
	same    func(a, b K) bool
	cleanup func(V)

	done  chan struct{}
	val   V
	err   error
	ended atomic.Bool
}

// StartAhead starts work in the background, from key.
func StartAhead[K comparable, V any](key K, work func() (V, error), cleanup func(V)) *Ahead[K, V] {
	return StartAheadFunc(key, func(a, b K) bool { return a == b }, work, cleanup)
}

// StartAheadFunc is StartAhead for a key that is not comparable with ==:
// same reports whether two keys describe the same state.
func StartAheadFunc[K, V any](key K, same func(a, b K) bool, work func() (V, error), cleanup func(V)) *Ahead[K, V] {
	a := &Ahead[K, V]{key: key, same: same, cleanup: cleanup, done: make(chan struct{})}
	go func() {
		defer close(a.done)
		a.val, a.err = work()
	}()

	return a
}

// Key is the key the work started from. It must not be modified.
func (a *Ahead[K, V]) Key() K {
	if a == nil {
		var zero K

		return zero
	}

	return a.key
}

// Wait waits for the work to end, without ending a.
func (a *Ahead[K, V]) Wait() {
	if a != nil {
		<-a.done
	}
}

// Take ends a, waiting for the work, and hands its result over when the
// work succeeded and key is the one it started from; the result is the
// caller's then. It returns false otherwise, a having been discarded.
func (a *Ahead[K, V]) Take(key K) (V, bool) {
	var zero V
	if a == nil || !a.ended.CompareAndSwap(false, true) {
		return zero, false
	}
	<-a.done
	if a.err != nil {
		return zero, false
	}
	if !a.same(a.key, key) {
		a.drop()

		return zero, false
	}
	val := a.val
	a.val = zero

	return val, true
}

// Discard ends a, waiting for the work, unless it already ended.
func (a *Ahead[K, V]) Discard() {
	if a == nil || !a.ended.CompareAndSwap(false, true) {
		return
	}
	<-a.done
	if a.err == nil {
		a.drop()
	}
}

// drop cleans up the result nobody took.
func (a *Ahead[K, V]) drop() {
	if a.cleanup != nil {
		a.cleanup(a.val)
	}
	var zero V
	a.val = zero
}
