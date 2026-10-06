// React\Promise (react/promise 3, which Composer uses) on Go: promises
// whose then() callbacks run synchronously when they settle, settled by the
// Scheduler that drives the asynchronous work behind them.

package util

import (
	"sync"
)

// Promise is React\Promise\PromiseInterface: a value or an error delivered
// once, with optional cancellation.
//
// As in React, a promise's callbacks (Then, Chain, OnSettled) run
// synchronously: at once when it is already settled, else when it settles,
// on the goroutine settling it, in the order they were added. Promises of
// asynchronous work (HTTP requests, processes, Go) are settled by their
// Scheduler on the goroutine driving it, so their callbacks run there; see
// Scheduler. A promise from NewDeferred settles on whatever goroutine
// resolves it.
//
// Wait, Err and Done block without driving anything; Await drives the
// scheduler the promise waits on.
type Promise[T any] struct {
	mu       sync.Mutex
	settled  bool
	value    T
	err      error
	handlers []func()
	done     chan struct{}
	// canceller is Cancel's action; after a callback returned a promise to
	// follow, it cancels that one.
	canceller func()
	// sched settles the promise (asynchronous work); upstream is the
	// promise this one waits for (the parent of a then(), or the promise
	// a callback returned).
	sched    *Scheduler
	upstream interface{ Scheduler() *Scheduler }
	// immediate: the promise was settled when it was created.
	immediate bool
}

func newPromise[T any]() *Promise[T] {
	return &Promise[T]{done: make(chan struct{})}
}

// Resolved returns a promise already fulfilled with value
// (\React\Promise\resolve($value)).
func Resolved[T any](value T) *Promise[T] {
	p := newPromise[T]()
	p.immediate = true
	p.settle(value, nil)

	return p
}

// Rejected returns a promise already rejected with err
// (\React\Promise\reject($e)).
func Rejected[T any](err error) *Promise[T] {
	p := newPromise[T]()
	p.immediate = true

	var zero T

	p.settle(zero, err)

	return p
}

// NewDeferred is React\Promise\Deferred (or new Promise($resolver,
// $canceller)): a pending promise and the functions settling it. Only the
// first settlement counts; the callbacks run on the goroutine calling
// resolve or reject. cancel, when not nil, is the canceller Cancel runs.
func NewDeferred[T any](cancel func()) (p *Promise[T], resolve func(T), reject func(error)) {
	p = newPromise[T]()
	p.canceller = cancel

	return p, p.resolve, p.reject
}

// NewDeferredOn is NewDeferred for a promise settled by the goroutine
// driving s (as the HttpDownloader settles its requests'): resolve and
// reject must be called there. Await and Loop.Wait drive s to settle it.
func NewDeferredOn[T any](s *Scheduler, cancel func()) (p *Promise[T], resolve func(T), reject func(error)) {
	p, resolve, reject = NewDeferred[T](cancel)
	p.sched = s

	return p, resolve, reject
}

// NewAsync is NewDeferred for work finishing on another goroutine: resolve
// and reject may be called from any goroutine (exactly one of them, once),
// and the promise settles when s runs the completion, in the order the
// asynchronous work started (it takes its Ticket now).
func NewAsync[T any](s *Scheduler, cancel func()) (p *Promise[T], resolve func(T), reject func(error)) {
	return newAsync[T](s, cancel, s.Ticket())
}

func newAsync[T any](s *Scheduler, cancel func(), t *Ticket) (p *Promise[T], resolve func(T), reject func(error)) {
	p = newPromise[T]()
	p.canceller = cancel
	p.sched = s

	return p, func(value T) {
			t.Complete(func() { p.resolve(value) })
		}, func(err error) {
			t.Complete(func() { p.reject(err) })
		}
}

// Go runs fn on a new goroutine and returns the promise of its result,
// settled by s (see NewAsync): parallel work whose outcome callbacks see on
// the driving goroutine.
func Go[T any](s *Scheduler, fn func() (T, error)) *Promise[T] {
	return goOn(s, fn, s.Ticket())
}

// GoBackground is Go for work that is not one of Composer's jobs (it takes
// a BackgroundTicket): Loop's progress bar does not count it.
func GoBackground[T any](s *Scheduler, fn func() (T, error)) *Promise[T] {
	return goOn(s, fn, s.BackgroundTicket())
}

func goOn[T any](s *Scheduler, fn func() (T, error), t *Ticket) *Promise[T] {
	p, resolve, reject := newAsync[T](s, nil, t)

	go func() {
		value, err := fn()
		if err != nil {
			reject(err)

			return
		}

		resolve(value)
	}()

	return p
}

// Later returns a promise settled with fn's result on s's next turn: fn
// runs on the driving goroutine after the completions of the work started
// before. It is the promise of work Composer runs asynchronously (and
// whose callbacks therefore run on a later loop tick) that maestro does at
// once.
func Later[T any](s *Scheduler, fn func() (T, error)) *Promise[T] {
	p := newPromise[T]()
	p.sched = s

	s.Ticket().Complete(func() {
		value, err := fn()
		p.settle(value, err)
	})

	return p
}

func (p *Promise[T]) resolve(value T) { p.settle(value, nil) }

func (p *Promise[T]) reject(err error) {
	var zero T

	p.settle(zero, err)
}

// settle settles the promise unless it is settled already, then runs its
// callbacks.
func (p *Promise[T]) settle(value T, err error) {
	p.mu.Lock()

	if p.settled {
		p.mu.Unlock()

		return
	}

	p.settled = true
	p.value, p.err = value, err
	handlers := p.handlers
	p.handlers = nil
	// React drops the canceller of a settled promise, and so the chain
	p.canceller, p.upstream = nil, nil
	close(p.done)
	p.mu.Unlock()

	for _, h := range handlers {
		h()
	}
}

// subscribe runs h once the promise settled: at once if it is settled.
func (p *Promise[T]) subscribe(h func()) {
	p.mu.Lock()

	if !p.settled {
		p.handlers = append(p.handlers, h)
		p.mu.Unlock()

		return
	}

	p.mu.Unlock()
	h()
}

// follow settles p as next settles (a callback returned next).
func (p *Promise[T]) follow(next *Promise[T]) {
	if next == p {
		p.reject(&LogicError{Message: "Cannot resolve a promise with itself."})

		return
	}

	p.mu.Lock()
	if !p.settled {
		p.upstream = next
		p.canceller = next.Cancel
	}
	p.mu.Unlock()

	next.subscribe(func() { p.settle(next.value, next.err) })
}

// Done is closed once the promise settled.
func (p *Promise[T]) Done() <-chan struct{} {
	return p.done
}

// Wait blocks until the promise settled and returns its outcome. It does
// not drive the promise's scheduler: someone else must (see Await).
func (p *Promise[T]) Wait() (T, error) {
	<-p.done

	return p.value, p.err
}

// Err blocks until the promise settled and returns its rejection reason,
// nil when it was fulfilled. Like Wait, it does not drive anything.
func (p *Promise[T]) Err() error {
	<-p.done

	return p.err
}

// Await drives the scheduler the promise waits on until it settled
// (SyncHelper::await without a Loop), and returns its outcome. A promise
// waiting on no scheduler is waited for as Wait does.
func (p *Promise[T]) Await() (T, error) {
	await(p)

	return p.value, p.err
}

// await drives the schedulers w waits on until it settled.
func await(w Waitable) {
	for {
		select {
		case <-w.Done():
			return
		default:
		}

		s := w.Scheduler()
		if s == nil {
			<-w.Done()

			return
		}

		s.step(w.Done())
	}
}

// Result reports whether the promise settled, and its rejection reason,
// without blocking.
func (p *Promise[T]) Result() (settled bool, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.settled, p.err
}

// OnSettled runs fn with the rejection reason (nil when fulfilled) once the
// promise settled, as a then() callback: at once if it settled, else on
// the goroutine settling it.
func (p *Promise[T]) OnSettled(fn func(err error)) {
	p.subscribe(func() { fn(p.err) })
}

// Scheduler returns the scheduler that settles the promise or what it
// waits for, nil when it is settled or waits on no scheduler.
func (p *Promise[T]) Scheduler() *Scheduler {
	p.mu.Lock()
	settled, upstream, sched := p.settled, p.upstream, p.sched
	p.mu.Unlock()

	if settled {
		return nil
	}

	if upstream != nil {
		if s := upstream.Scheduler(); s != nil {
			return s
		}
	}

	return sched
}

// Cancel is cancel(): it runs the canceller of a pending promise (that of
// the promise it follows, if any).
func (p *Promise[T]) Cancel() {
	p.mu.Lock()
	canceller := p.canceller
	p.mu.Unlock()

	if canceller != nil {
		canceller()
	}
}

// Immediate reports whether the promise was settled when it was created
// (Resolved, Rejected, or a Then/Chain of such a promise whose callback
// settled it at once).
func (p *Promise[T]) Immediate() bool { return p.immediate }

// Then returns a promise settled by f applied to p's value, or with p's
// error. Cancelling it cancels p.
func Then[T, U any](p *Promise[T], f func(T) (U, error)) *Promise[U] {
	return Chain(p, func(value T) (*Promise[U], U, error) {
		result, err := f(value)

		return nil, result, err
	}, nil)
}

// Chain is $p->then($onFulfilled, $onRejected) where a callback returns
// either a promise to follow (non-nil) or a plain value, or throws (error).
// A nil onRejected passes the rejection on; a nil onFulfilled resolves
// with U's zero value. Cancelling the result cancels p (or the promise it
// follows).
func Chain[T, U any](p *Promise[T], onFulfilled func(T) (*Promise[U], U, error), onRejected func(error) (*Promise[U], U, error)) *Promise[U] {
	next := newPromise[U]()
	next.upstream = p
	next.canceller = p.Cancel

	p.subscribe(func() {
		var (
			follow *Promise[U]
			value  U
			err    error
		)

		switch {
		case p.err != nil && onRejected == nil:
			err = p.err
		case p.err != nil:
			follow, value, err = onRejected(p.err)
		case onFulfilled != nil:
			follow, value, err = onFulfilled(p.value)
		}

		switch {
		case err != nil:
			next.reject(err)
		case follow != nil:
			next.follow(follow)
		default:
			next.resolve(value)
		}
	})

	next.immediate, _ = next.Result()

	return next
}

// Waitable is a promise of any type, as Loop::wait and the helpers driving
// promises take them; every *Promise is one.
type Waitable interface {
	Done() <-chan struct{}
	Err() error
	Cancel()
	OnSettled(fn func(err error))
	Scheduler() *Scheduler
}

var _ Waitable = (*Promise[struct{}])(nil)

// FirstRejection is \React\Promise\all($promises)'s rejection: it returns
// a function giving the first rejection reason among the promises, in the
// order they settled (nil while none rejected).
func FirstRejection(promises []Waitable) func() error {
	var (
		mu    sync.Mutex
		first error
	)

	for _, p := range promises {
		p.OnSettled(func(err error) {
			mu.Lock()
			defer mu.Unlock()

			if err != nil && first == nil {
				first = err
			}
		})
	}

	return func() error {
		mu.Lock()
		defer mu.Unlock()

		return first
	}
}

// AwaitAll drives the schedulers the promises wait on until every one
// settled, and returns the first rejection (as FirstRejection). It is
// Loop::wait for promises not tied to a Loop.
func AwaitAll(promises []Waitable) error {
	rejection := FirstRejection(promises)

	for _, p := range promises {
		await(p)
	}

	return rejection()
}
