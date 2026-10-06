// Work posted to the goroutine driving Composer's flow while it waits.

package util

import "sync"

// Composer runs on one thread: everything its asynchronous work does (a
// download's or a process's output, ...) happens on it, while Loop::wait()
// or Process::wait() runs. maestro runs that work on goroutines, and some
// of what it does must happen on the goroutine driving the flow: the
// plugin runtime calls PHP only from there (docs/PLUGINS.md §5.14), so a
// call to an IO written in PHP made from parallel work is posted to it.
// The driving goroutine runs what was posted whenever it waits for
// parallel work: the waits of Scheduler (Loop.Wait, Promise.Await, ...)
// and of Process run the wait hooks when NotifyWaitHooks is called.

var waitHooks struct {
	mu    sync.Mutex
	hooks map[int]func()
	next  int
	// wake is closed (and replaced) by NotifyWaitHooks
	wake chan struct{}
}

// AddWaitHook makes the waits run fn when NotifyWaitHooks is called; fn
// runs on the waiting goroutine and must return at once when it has
// nothing to do there. It returns the function removing the hook.
func AddWaitHook(fn func()) (remove func()) {
	waitHooks.mu.Lock()
	defer waitHooks.mu.Unlock()

	if waitHooks.hooks == nil {
		waitHooks.hooks = map[int]func(){}
	}
	id := waitHooks.next
	waitHooks.next++
	waitHooks.hooks[id] = fn

	return func() {
		waitHooks.mu.Lock()
		defer waitHooks.mu.Unlock()

		delete(waitHooks.hooks, id)
	}
}

// NotifyWaitHooks wakes the waiting goroutines to run the wait hooks.
func NotifyWaitHooks() {
	waitHooks.mu.Lock()
	defer waitHooks.mu.Unlock()

	if waitHooks.wake != nil {
		close(waitHooks.wake)
		waitHooks.wake = nil
	}
}

// waitHookWake returns the channel closed by the next NotifyWaitHooks.
func waitHookWake() <-chan struct{} {
	waitHooks.mu.Lock()
	defer waitHooks.mu.Unlock()

	if waitHooks.wake == nil {
		waitHooks.wake = make(chan struct{})
	}

	return waitHooks.wake
}

// RunWaitHooks runs the wait hooks on the calling goroutine.
func RunWaitHooks() {
	waitHooks.mu.Lock()
	if len(waitHooks.hooks) == 0 {
		waitHooks.mu.Unlock()

		return
	}
	hooks := make([]func(), 0, len(waitHooks.hooks))
	for _, fn := range waitHooks.hooks {
		hooks = append(hooks, fn)
	}
	waitHooks.mu.Unlock()

	for _, fn := range hooks {
		fn()
	}
}

// WaitServing blocks until done is closed, running the wait hooks
// meanwhile (a wait of the driving goroutine for parallel work outside a
// Scheduler).
func WaitServing(done <-chan struct{}) {
	for {
		// what was posted before the channel was taken runs now
		wake := waitHookWake()
		RunWaitHooks()
		select {
		case <-done:
			return
		case <-wake:
		}
	}
}
