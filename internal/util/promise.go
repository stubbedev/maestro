package util

import "sync"

// Promise is the subset of React\Promise\PromiseInterface the async
// ProcessExecutor and Filesystem APIs return: a value or an error delivered
// once, with optional cancellation.
type Promise[T any] struct {
	done   chan struct{}
	once   sync.Once
	value  T
	err    error
	cancel func()
	// immediate is set by Resolved and Rejected: the promise was settled
	// when it was created.
	immediate bool
}

func newPromise[T any]() *Promise[T] {
	return &Promise[T]{done: make(chan struct{})}
}

// Resolved returns a promise already fulfilled with value.
func Resolved[T any](value T) *Promise[T] {
	p := newPromise[T]()
	p.immediate = true
	p.resolve(value)

	return p
}

func (p *Promise[T]) resolve(value T) {
	p.once.Do(func() {
		p.value = value
		close(p.done)
	})
}

func (p *Promise[T]) reject(err error) {
	p.once.Do(func() {
		p.err = err
		close(p.done)
	})
}

// Done is closed once the promise settled.
func (p *Promise[T]) Done() <-chan struct{} {
	return p.done
}

// Wait blocks until the promise settled and returns its outcome.
func (p *Promise[T]) Wait() (T, error) {
	<-p.done

	return p.value, p.err
}

// Cancel runs the promise's canceller, if any.
func (p *Promise[T]) Cancel() {
	if p.cancel != nil {
		p.cancel()
	}
}

// Then returns a promise settled by f applied to p's value, or with p's
// error. Cancelling it cancels p.
func Then[T, U any](p *Promise[T], f func(T) (U, error)) *Promise[U] {
	next := newPromise[U]()
	next.cancel = p.Cancel

	go func() {
		value, err := p.Wait()
		if err != nil {
			next.reject(err)

			return
		}

		result, err := f(value)
		if err != nil {
			next.reject(err)

			return
		}

		next.resolve(result)
	}()

	return next
}

// Rejected returns a promise already rejected with err.
func Rejected[T any](err error) *Promise[T] {
	p := newPromise[T]()
	p.immediate = true
	p.reject(err)

	return p
}

// NewDeferred is React\Promise\Deferred (or new Promise($resolver,
// $canceller)): a pending promise and the functions settling it. Only the
// first settlement counts. cancel, when not nil, is the canceller Cancel
// runs.
func NewDeferred[T any](cancel func()) (p *Promise[T], resolve func(T), reject func(error)) {
	p = newPromise[T]()
	p.cancel = cancel

	return p, p.resolve, p.reject
}

// Err blocks until the promise settled and returns its rejection reason,
// nil when it was fulfilled.
func (p *Promise[T]) Err() error {
	<-p.done

	return p.err
}

// Immediate reports whether the promise was created settled (Resolved,
// Rejected): React runs then() callbacks of such a promise synchronously,
// which callers that drive promise chains themselves (internal/installer)
// reproduce without depending on goroutine timing.
func (p *Promise[T]) Immediate() bool { return p.immediate }
