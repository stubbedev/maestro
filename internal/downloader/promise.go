package downloader

import "github.com/stubbedev/maestro/internal/util"

// resolved is \React\Promise\resolve($value).
func resolved(value string) *Promise { return util.Resolved(value) }

// rejected is \React\Promise\reject($e).
func rejected(err error) *Promise { return util.Rejected[string](err) }

// then is $p->then($onFulfilled, $onRejected) where the callbacks may return
// a promise (nil for a plain value, carried by the string) or throw. A nil
// onRejected passes the rejection through. As in React, the callbacks run
// at once when p is settled, else when it settles. Cancelling the result
// cancels p.
func then[T any](p *util.Promise[T], onFulfilled func(T) (*Promise, string, error), onRejected func(error) (*Promise, string, error)) *Promise {
	return util.Chain(p, onFulfilled, onRejected)
}

// await is SyncHelper::await on a bare promise: it drives the promise's
// scheduler until it settled and returns its rejection, or the synchronous
// error.
func await(p *Promise, err error) error {
	if err != nil {
		return err
	}

	_, err = p.Await()

	return err
}
