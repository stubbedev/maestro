package downloader

import "github.com/stubbedev/maestro/internal/util"

// resolved is \React\Promise\resolve($value).
func resolved(value string) *Promise { return util.Resolved(value) }

// rejected is \React\Promise\reject($e).
func rejected(err error) *Promise { return util.Rejected[string](err) }

// then is $p->then($onFulfilled, $onRejected) where the callbacks may return
// a promise (nil for a plain value, carried by the string) or throw. A nil
// onRejected passes the rejection through. Cancelling the result cancels p.
func then[T any](p *util.Promise[T], onFulfilled func(T) (*Promise, string, error), onRejected func(error) (*Promise, string, error)) *Promise {
	next, resolve, reject := util.NewDeferred[string](p.Cancel)

	go func() {
		value, err := p.Wait()

		var (
			promise *Promise
			result  string
		)

		switch {
		case err != nil && onRejected == nil:
			reject(err)

			return
		case err != nil:
			promise, result, err = onRejected(err)
		default:
			promise, result, err = onFulfilled(value)
		}

		if err == nil && promise != nil {
			result, err = promise.Wait()
		}

		if err != nil {
			reject(err)

			return
		}

		resolve(result)
	}()

	return next
}

// await is SyncHelper::await on a bare promise: its rejection, or the
// synchronous error.
func await(p *Promise, err error) error {
	if err != nil {
		return err
	}

	return p.Err()
}
