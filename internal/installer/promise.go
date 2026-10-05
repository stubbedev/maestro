// The React\Promise\PromiseInterface the installers return.

package installer

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Promise is the ?PromiseInterface an installer method returns (a nil
// *Promise is PHP's null): a util.Promise whose value is dropped, as the
// installers only chain on completion. Its callbacks run as React's do, at
// once when it is settled, else when the loop settles it, on the goroutine
// waiting (see util.Scheduler).
type Promise = util.Promise[struct{}]

// Resolved is \React\Promise\resolve(null).
func Resolved() *Promise { return util.Resolved(struct{}{}) }

// Rejected is \React\Promise\reject($e).
func Rejected(err error) *Promise { return util.Rejected[struct{}](err) }

// Of wraps a util.Promise (a downloader or process promise), dropping its
// value. A nil promise is PHP's null and gives nil.
func Of[T any](p *util.Promise[T]) *Promise {
	if p == nil {
		return nil
	}

	return util.Then(p, func(T) (struct{}, error) { return struct{}{}, nil })
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

	var (
		fulfilled func(struct{}) (*Promise, struct{}, error)
		rejected  func(error) (*Promise, struct{}, error)
	)

	if onFulfilled != nil {
		fulfilled = func(struct{}) (*Promise, struct{}, error) {
			next, err := onFulfilled()

			return next, struct{}{}, err
		}
	}

	if onRejected != nil {
		rejected = func(e error) (*Promise, struct{}, error) {
			next, err := onRejected(e)

			return next, struct{}{}, err
		}
	}

	return util.Chain(p, fulfilled, rejected)
}

// Await is SyncHelper::await($loop, $promise) for an installer promise: it
// waits for p, running the loop's callbacks on the calling goroutine, and
// returns its rejection. A nil loop drives the promise's own scheduler
// (tests).
func Await(loop *http.Loop, p *Promise) error {
	if p == nil {
		return nil
	}

	return wait(loop, []*Promise{p}, nil)
}

// wait is Loop::wait($promises, $progress) for installer promises.
func wait(loop *http.Loop, promises []*Promise, progress *console.ProgressBar) error {
	waitables := make([]http.Waitable, len(promises))
	for i, p := range promises {
		waitables[i] = p
	}

	return loop.Wait(waitables, progress)
}
