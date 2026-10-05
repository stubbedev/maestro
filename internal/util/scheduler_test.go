package util

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// TestScheduler_CompletionsRunInStartOrder: work finishing in reverse
// order still has its callbacks run in the order it started, on the
// goroutine driving the scheduler (the race detector checks the shared
// slice is only touched there).
func TestScheduler_CompletionsRunInStartOrder(t *testing.T) {
	s := NewScheduler()

	var (
		order    []int
		promises []Waitable
	)

	const n = 6

	for i := range n {
		job := Go(s, func() (int, error) {
			time.Sleep(time.Duration(n-i) * 3 * time.Millisecond)

			return i, nil
		})

		promises = append(promises, Then(job, func(v int) (struct{}, error) {
			order = append(order, v)

			return struct{}{}, nil
		}))
	}

	if err := AwaitAll(promises); err != nil {
		t.Fatal(err)
	}

	if want := []int{0, 1, 2, 3, 4, 5}; !slices.Equal(order, want) {
		t.Fatalf("order %v, want %v", order, want)
	}

	if s.Pending() != 0 {
		t.Fatalf("%d tickets pending", s.Pending())
	}
}

// TestPromise_SettledCallbacksRunAtOnce: as in React, then() on a settled
// promise runs the callback before returning; on a pending one, when it
// settles, in the order the callbacks were added.
func TestPromise_SettledCallbacksRunAtOnce(t *testing.T) {
	var log []string

	p := Then(Resolved(1), func(v int) (int, error) {
		log = append(log, "first")

		return v + 1, nil
	})

	log = append(log, "returned")

	if settled, err := p.Result(); !settled || err != nil || !p.Immediate() {
		t.Fatalf("settled %v, %v, immediate %v", settled, err, p.Immediate())
	}

	deferred, resolve, _ := NewDeferred[int](nil)

	for _, name := range []string{"a", "b"} {
		Then(deferred, func(int) (int, error) {
			log = append(log, name)

			return 0, nil
		})
	}

	log = append(log, "resolving")
	resolve(1)

	if want := []string{"first", "returned", "resolving", "a", "b"}; !slices.Equal(log, want) {
		t.Fatalf("log %q, want %q", log, want)
	}
}

// TestPromise_ChainFollowsReturnedPromise: a callback returning a promise
// makes the chain follow it, and cancelling the chain cancels it.
func TestPromise_ChainFollowsReturnedPromise(t *testing.T) {
	s := NewScheduler()

	inner, resolveInner, _ := NewAsync[string](s, nil)

	cancelled := false
	outer, resolveOuter, _ := NewDeferred[string](func() { cancelled = true })

	chained := Chain(outer, func(string) (*Promise[string], string, error) {
		return inner, "", nil
	}, nil)

	chained.Cancel()

	if !cancelled {
		t.Fatal("cancelling the chain did not cancel its parent")
	}

	resolveOuter("")

	if chained.Scheduler() != s {
		t.Fatal("the chain does not wait on the followed promise's scheduler")
	}

	resolveInner("done")

	if v, err := chained.Await(); err != nil || v != "done" {
		t.Fatalf("got %q, %v", v, err)
	}
}

// TestPromise_Later settles on the scheduler's next turn, after the work
// started before it.
func TestPromise_Later(t *testing.T) {
	s := NewScheduler()

	var log []string

	slow := Go(s, func() (string, error) {
		time.Sleep(5 * time.Millisecond)

		return "slow", nil
	})
	slow.OnSettled(func(error) { log = append(log, "slow") })

	later := Later(s, func() (string, error) {
		log = append(log, "later")

		return "", errors.New("boom")
	})

	if settled, _ := later.Result(); settled {
		t.Fatal("Later settled at once")
	}

	if err := AwaitAll([]Waitable{slow, later}); err == nil || err.Error() != "boom" {
		t.Fatalf("got %v", err)
	}

	if want := []string{"slow", "later"}; !slices.Equal(log, want) {
		t.Fatalf("log %q, want %q", log, want)
	}
}

// TestScheduler_Interrupt runs an interrupt on the driving goroutine
// before the next completion.
func TestScheduler_Interrupt(t *testing.T) {
	s := NewScheduler()

	var log []string

	release := make(chan struct{})
	job := Go(s, func() (struct{}, error) {
		<-release

		return struct{}{}, nil
	})
	job.OnSettled(func(error) { log = append(log, "job") })

	go func() {
		s.Interrupt(func() {
			log = append(log, "interrupt")
			close(release)
		})
	}()

	if _, err := job.Await(); err != nil {
		t.Fatal(err)
	}

	if want := []string{"interrupt", "job"}; !slices.Equal(log, want) {
		t.Fatalf("log %q, want %q", log, want)
	}
}

// TestFirstRejection is all()'s rejection: the first in time, not in
// order given.
func TestFirstRejection(t *testing.T) {
	a, _, rejectA := NewDeferred[int](nil)
	b, _, rejectB := NewDeferred[int](nil)

	first := FirstRejection([]Waitable{a, b})

	rejectB(errors.New("b"))
	rejectA(errors.New("a"))

	if err := first(); err == nil || err.Error() != "b" {
		t.Fatalf("got %v", err)
	}
}
