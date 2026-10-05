// The event loop Composer's asynchronous code runs on (the scheduling part
// of src/Composer/Util/Loop.php, HttpDownloader::countActiveJobs and
// ProcessExecutor::countActiveJobs), made deterministic.

package util

import (
	"sync"
	"time"
)

// Scheduler is the run queue of Composer's single-threaded event loop.
//
// Composer runs on one thread: a promise's then() callbacks run when it
// settles, and asynchronous work (curl transfers, processes) settles its
// promises while Loop::wait ticks, on that same thread. Here the work
// itself runs on worker goroutines (transfers, processes, extraction into
// the package store), but its completion does not settle anything there:
// the worker hands it to the scheduler, and the goroutine driving the
// scheduler (Loop.Wait, HttpDownloader.Wait, ProcessExecutor.Wait,
// Promise.Await) runs it. Every callback that prints, dispatches events or
// changes shared state therefore runs on the driving goroutine.
//
// Completions run in a fixed order: each piece of asynchronous work takes a
// Ticket when it starts (on the driving goroutine, so in a deterministic
// order), and completions run in ticket order, a completion that arrived
// early waiting for the ones before it. Composer runs them in the order the
// work finishes, which is not deterministic; this is the deterministic
// equivalent (docs: internal/util/http's Loop, internal/installer).
//
// Only one goroutine may drive a scheduler at a time; completions may run
// nested (a callback waiting for more work drives the scheduler itself, as
// PHP's nested Loop::wait does).
type Scheduler struct {
	mu sync.Mutex
	// next is the next ticket to issue, head the next one to run.
	next, head int
	// ready holds the completions that arrived, by ticket.
	ready map[int]func()
	// interrupts run before the next completion (signal handlers).
	interrupts []func()
	// wake is signalled whenever a completion arrives.
	wake chan struct{}
}

// NewScheduler returns an idle scheduler.
func NewScheduler() *Scheduler {
	return &Scheduler{ready: map[int]func(){}, wake: make(chan struct{}, 1)}
}

// Ticket is a place in the order completions run in, taken by one piece of
// asynchronous work.
type Ticket struct {
	s    *Scheduler
	id   int
	once sync.Once
}

// Ticket reserves the next place in the order. The work must Complete it
// eventually: until then no later completion runs.
func (s *Scheduler) Ticket() *Ticket {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := &Ticket{s: s, id: s.next}
	s.next++

	s.signal()

	return t
}

// signal wakes the driving goroutine.
func (s *Scheduler) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Complete hands the work's completion to the scheduler: fn (nil for
// nothing) runs on the driving goroutine once every earlier ticket's
// completion ran. It may be called from any goroutine; only the first call
// counts.
func (t *Ticket) Complete(fn func()) {
	t.once.Do(func() {
		s := t.s

		s.mu.Lock()
		s.ready[t.id] = fn
		s.mu.Unlock()

		s.signal()
	})
}

// Pending is the number of tickets whose completion has not run yet: the
// asynchronous work still active.
func (s *Scheduler) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.next - s.head
}

// Interrupt makes the driving goroutine run fn before the next completion
// (or at once if it is waiting), as PHP runs a signal handler between two
// statements of its one thread. It may be called from any goroutine.
func (s *Scheduler) Interrupt(fn func()) {
	s.mu.Lock()
	s.interrupts = append(s.interrupts, fn)
	s.mu.Unlock()

	s.signal()
}

// RunInterrupts runs the pending interrupts on the calling goroutine (for
// a driver that stops driving) and reports whether there were any.
func (s *Scheduler) RunInterrupts() bool {
	ran := false

	for {
		s.mu.Lock()

		if len(s.interrupts) == 0 {
			s.mu.Unlock()

			return ran
		}

		fn := s.interrupts[0]
		s.interrupts = s.interrupts[1:]
		s.mu.Unlock()

		fn()

		ran = true
	}
}

// RunReady runs the pending interrupts, else the next completion if it
// arrived, and reports whether it ran anything.
func (s *Scheduler) RunReady() bool {
	if s.RunInterrupts() {
		return true
	}

	s.mu.Lock()

	fn, ok := s.ready[s.head]
	if !ok {
		s.mu.Unlock()

		return false
	}

	delete(s.ready, s.head)
	// advance first: fn may drive the scheduler itself
	s.head++
	s.mu.Unlock()

	if fn != nil {
		fn()
	}

	return true
}

// WaitReady blocks until the next completion (or an interrupt) arrived,
// timeout elapsed (none when <= 0), stop is closed (stop may be nil) or no
// ticket is pending.
func (s *Scheduler) WaitReady(timeout time.Duration, stop <-chan struct{}) {
	var expired <-chan time.Time

	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		expired = timer.C
	}

	for {
		s.mu.Lock()
		_, ready := s.ready[s.head]
		ready = ready || len(s.interrupts) > 0
		idle := s.head == s.next
		s.mu.Unlock()

		if ready || idle {
			return
		}

		select {
		case <-s.wake:
		case <-stop:
			return
		case <-expired:
			return
		}
	}
}

// Run drives the scheduler until done reports true or no ticket is
// pending.
func (s *Scheduler) Run(done func() bool) {
	for !done() {
		if s.RunReady() {
			continue
		}

		if s.Pending() == 0 {
			return
		}

		s.WaitReady(0, nil)
	}
}

// step runs the next completion, or waits for it (or for done to close).
// When nothing is pending it waits for new work, done, or a moment, so
// that the caller can look again at what it waits on.
func (s *Scheduler) step(done <-chan struct{}) {
	if s.RunReady() {
		return
	}

	if s.Pending() > 0 {
		s.WaitReady(0, done)

		return
	}

	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()

	select {
	case <-done:
	case <-s.wake:
	case <-timer.C:
	}
}
