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
}

func newPromise[T any]() *Promise[T] {
	return &Promise[T]{done: make(chan struct{})}
}

// Resolved returns a promise already fulfilled with value.
func Resolved[T any](value T) *Promise[T] {
	p := newPromise[T]()
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
