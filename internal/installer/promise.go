// The React\Promise\PromiseInterface the installers return, driven the way
// Composer's single-threaded event loop drives it.

package installer

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Promise is the ?PromiseInterface an installer method returns (a nil
// *Promise is PHP's null). It is a chain of then() callbacks over leaf
// promises (the downloader's, the process executor's), and it is driven on
// the goroutine that waits for it, as PHP runs promise callbacks on its one
// thread:
//
//   - Then on a promise that is already settled runs the callback at once,
//     as React does for a fulfilled or rejected promise. A leaf counts as
//     settled from the start only when it was created settled
//     (util.Resolved, util.Rejected), so whether a callback runs inline
//     never depends on goroutine timing.
//   - Other callbacks run when the promise is waited for (Manager's
//     waitOnPromises, Await), on the waiting goroutine, in the order of the
//     promises waited for. Only the leaves' own work (downloads,
//     extraction, removals) runs in parallel.
//
// A Promise is not safe for concurrent use: create, chain and wait for it on
// the main flow.
type Promise struct {
	// leaf is set for a promise wrapping a util.Promise.
	leaf http.Waitable
	// ready is set once the leaf was waited for (or it was created
	// settled).
	ready bool

	parent      *Promise
	onFulfilled func() (*Promise, error)
	onRejected  func(error) (*Promise, error)
	ran         bool
	// adopted is the promise a callback returned, which this one follows.
	adopted *Promise

	settled bool
	err     error
}

// Resolved is \React\Promise\resolve(null).
func Resolved() *Promise { return &Promise{settled: true} }

// Rejected is \React\Promise\reject($e).
func Rejected(err error) *Promise { return &Promise{settled: true, err: err} }

// Of wraps a util.Promise (a downloader or process promise); its value is
// dropped, as the installers only chain on completion. A nil promise is
// PHP's null and gives nil.
func Of[T any](p *util.Promise[T]) *Promise {
	if p == nil {
		return nil
	}

	return &Promise{leaf: p, ready: p.Immediate()}
}

// wrap is Of applied to a call returning (promise, synchronous error).
func wrap[T any](p *util.Promise[T], err error) (*Promise, error) {
	if err != nil {
		return nil, err
	}

	return Of(p), nil
}

// Then is $promise->then($onFulfilled, $onRejected), p nil standing for a
// resolved promise. A callback returns the promise to follow (nil for a
// plain value) or throws (error). A nil onRejected passes the rejection on.
func Then(p *Promise, onFulfilled func() (*Promise, error), onRejected func(error) (*Promise, error)) *Promise {
	if p == nil {
		p = Resolved()
	}

	next := &Promise{parent: p, onFulfilled: onFulfilled, onRejected: onRejected}
	next.advance()

	return next
}

// Result reports whether the promise settled so far, and its rejection.
func (p *Promise) Result() (settled bool, err error) {
	return p.settled, p.err
}

// advance runs the callbacks whose inputs settled and reports whether p is
// settled.
func (p *Promise) advance() bool {
	if p.settled {
		return true
	}

	if p.leaf != nil {
		if !p.ready {
			return false
		}

		p.settle(p.leaf.Err())

		return true
	}

	if !p.ran {
		if !p.parent.advance() {
			return false
		}

		p.ran = true

		var (
			next *Promise
			err  error
		)

		switch {
		case p.parent.err != nil && p.onRejected == nil:
			err = p.parent.err
		case p.parent.err != nil:
			next, err = p.onRejected(p.parent.err)
		case p.onFulfilled != nil:
			next, err = p.onFulfilled()
		}

		if err != nil || next == nil {
			p.settle(err)

			return true
		}

		p.adopted = next
	}

	if !p.adopted.advance() {
		return false
	}

	p.settle(p.adopted.err)

	return true
}

func (p *Promise) settle(err error) {
	p.settled = true
	p.err = err
	// drop the chain so settled promises do not keep closures alive
	p.parent, p.onFulfilled, p.onRejected, p.adopted = nil, nil, nil, nil
}

// blocking returns the unsettled leaf p waits for, nil when p settled.
func (p *Promise) blocking() *Promise {
	switch {
	case p.settled:
		return nil
	case p.leaf != nil:
		return p
	case !p.ran:
		return p.parent.blocking()
	default:
		return p.adopted.blocking()
	}
}

// Await is SyncHelper::await($loop, $promise) for an installer promise: it
// waits for p, running its callbacks on the calling goroutine, and returns
// its rejection. A nil loop waits for the leaves without driving HTTP jobs
// (tests).
func Await(loop *http.Loop, p *Promise) error {
	if p == nil {
		return nil
	}

	return wait(loop, []*Promise{p}, nil)
}

// wait is Loop::wait($promises, $progress) for installer promises: it
// drives the loop until every promise settled and returns the first
// rejection in the order given (PHP's all() rejects with the first one in
// time; the order of the promises is the deterministic equivalent).
//
// Leaves are waited for in rounds: the loop runs until every leaf the
// promises are blocked on settled, then the callbacks run in promise
// order, which may chain new leaves for the next round. The progress bar,
// as in PHP, is driven once, by the first round.
func wait(loop *http.Loop, promises []*Promise, progress *console.ProgressBar) error {
	for first := true; ; first = false {
		var frontier []*Promise

		seen := map[*Promise]bool{}

		for _, p := range promises {
			p.advance()

			if b := p.blocking(); b != nil && !seen[b] {
				seen[b] = true
				frontier = append(frontier, b)
			}
		}

		if len(frontier) == 0 && !first {
			break
		}

		if first {
			waitLeaves(loop, frontier, progress)
		} else {
			waitLeaves(loop, frontier, nil)
		}

		for _, b := range frontier {
			b.ready = true
		}

		if len(frontier) == 0 {
			break
		}
	}

	for _, p := range promises {
		if p.err != nil {
			return p.err
		}
	}

	return nil
}

// waitLeaves runs the loop until every leaf settled. Loop.Wait returns at
// the first rejection; PHP's loop keeps running until no job is left, so
// the remaining leaves are waited for too.
func waitLeaves(loop *http.Loop, leaves []*Promise, progress *console.ProgressBar) {
	if loop == nil {
		for _, l := range leaves {
			<-l.leaf.Done()
		}

		return
	}

	waitables := make([]http.Waitable, len(leaves))
	for i, l := range leaves {
		waitables[i] = l.leaf
	}

	_ = loop.Wait(waitables, progress)

	for {
		var pending []http.Waitable

		for _, w := range waitables {
			select {
			case <-w.Done():
			default:
				pending = append(pending, w)
			}
		}

		if len(pending) == 0 {
			return
		}

		_ = loop.Wait(pending, nil)

		waitables = pending
	}
}
