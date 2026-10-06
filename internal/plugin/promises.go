// Promises crossing the channel (docs/PLUGINS.md §5.6 "Promises"):
// maestro's installer promises handed to PHP become React promises there,
// and PHP's promises handed to maestro become maestro promises, each
// settled when the original settles, so then() callbacks on either side
// run when they would in Composer's single event loop.
// php/src/Maestro/Shim/Promises.php is the PHP half.

package plugin

import (
	"sync"

	"github.com/stubbedev/maestro/internal/installer"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/util"
)

// promiseTable holds the promises one side waits on the other for.
type promiseTable struct {
	mu sync.Mutex
	// last is the last id given to a maestro promise PHP holds.
	last int64
	// rejected are maestro promises PHP holds that were rejected, until
	// PHP asked for their reasons (`promise.rejection`).
	rejected map[int64]error
	// pending are the deferreds standing for PHP's pending promises, by
	// PHP's id.
	pending map[int64]*phpPending
}

// phpPending settles the maestro promise standing for a pending PHP one.
type phpPending struct {
	resolve func(struct{})
	reject  func(error)
}

func newPromiseTable() *promiseTable {
	return &promiseTable{rejected: map[int64]error{}, pending: map[int64]*phpPending{}}
}

func (r *Runtime) registerPromises() {
	r.Handle("promise.settled", r.promiseSettled)
	r.Handle("promise.rejection", func(v any) (any, error) {
		a := argsOf("promise.rejection", v)
		id := int64(a.integer(0))

		r.promises.mu.Lock()
		err, ok := r.promises.rejected[id]
		delete(r.promises.rejected, id)
		r.promises.mu.Unlock()

		if !ok {
			return nil, &rpc.ProtocolError{Message: "promise " + php.ToString(id) + " has no rejection reason"}
		}

		return nil, err
	})
}

// promiseToPHP is a maestro promise as PHP receives it ({id, s}; nil for
// null): PHP builds a React promise from it (Promises::fromMaestro). A
// pending promise settles PHP's when it settles (`promise.settle`), on the
// goroutine settling it, which holds the PHP baton (the one driving the
// event loop).
func (r *Runtime) promiseToPHP(p *installer.Promise) any {
	return promiseValueToPHP(r, p, nil)
}

// promiseValueToPHP is promiseToPHP for a promise with a value, which
// conv makes PHP's (nil: none, the promise resolves with null).
func promiseValueToPHP[T any](r *Runtime, p *util.Promise[T], conv func(T) any) any {
	if p == nil {
		return nil
	}
	value := func() any {
		if conv == nil {
			return nil
		}
		v, _ := p.Wait()

		return conv(v)
	}

	settled, err := p.Result()
	if settled && err == nil {
		return php.ArrayOf("id", 0, "s", "fulfilled", "v", value())
	}

	r.promises.mu.Lock()
	r.promises.last++
	id := r.promises.last
	if settled {
		r.promises.rejected[id] = err
	}
	r.promises.mu.Unlock()

	if settled {
		return php.ArrayOf("id", id, "s", "rejected")
	}

	p.OnSettled(func(err error) {
		var v any
		if err != nil {
			r.promises.mu.Lock()
			r.promises.rejected[id] = err
			r.promises.mu.Unlock()
		} else {
			v = value()
		}
		// What fails here (PHP ended) fails every later call to PHP the
		// same way; there is nobody to hand it to.
		_, _ = r.Call("promise.settle", php.ArrayOf("id", id, "ok", err == nil, "v", v))
	})

	return php.ArrayOf("id", id, "s", "pending")
}

// promiseFromPHP is the maestro promise standing for a PHP promise PHP
// handed over ({id, s} from Promises::watch). A pending one is settled
// when PHP reports it (`promise.settled`); sched is the scheduler of the
// event loop it waits on (nil: none), which Await drives.
func (r *Runtime) promiseFromPHP(v any, sched *util.Scheduler) (*installer.Promise, error) {
	a, ok := v.(*php.Array)
	if !ok {
		return nil, &rpc.ProtocolError{Message: "invalid promise from PHP"}
	}
	idv, _ := a.Get("id")
	id := php.ToInt(idv)
	state, _ := a.GetString("s")

	switch state {
	case "fulfilled":
		return installer.Resolved(), nil
	case "rejected":
		return installer.Rejected(r.phpRejection(id)), nil
	}

	p, resolve, reject := util.NewDeferredOn[struct{}](sched, nil)

	r.promises.mu.Lock()
	r.promises.pending[id] = &phpPending{resolve: resolve, reject: reject}
	r.promises.mu.Unlock()

	return p, nil
}

// phpRejection returns the rejection reason of PHP's promise id, which
// PHP throws (the exception keeps its identity: handed back to PHP, it is
// the same object).
func (r *Runtime) phpRejection(id int64) error {
	_, err := r.Call("promise.reason", php.ArrayOf("id", id))
	if err == nil {
		return &rpc.ProtocolError{Message: "PHP's promise " + php.ToString(id) + " has no rejection reason"}
	}

	return err
}

// promiseSettled serves `promise.settled`: PHP's pending promise id
// settled, so its maestro promise does, running maestro's callbacks now,
// as React runs them while the promise settles.
func (r *Runtime) promiseSettled(v any) (any, error) {
	a := argsOf("promise.settled", v)
	id := int64(a.integer(0))

	r.promises.mu.Lock()
	pending, ok := r.promises.pending[id]
	delete(r.promises.pending, id)
	r.promises.mu.Unlock()

	if !ok {
		return nil, &rpc.ProtocolError{Message: "unknown PHP promise " + php.ToString(id)}
	}

	if a.boolean(1) {
		pending.resolve(struct{}{})
	} else {
		pending.reject(r.phpRejection(id))
	}

	return nil, nil
}
