package util

import (
	"errors"
	"testing"
)

func TestAheadTakeMatchingKey(t *testing.T) {
	cleaned := 0
	a := StartAhead(1, func() (string, error) { return "v", nil }, func(string) { cleaned++ })
	defer a.Discard()
	v, ok := a.Take(1)
	if !ok || v != "v" {
		t.Fatalf("Take(1) = %q, %v; want v, true", v, ok)
	}
	if v, ok := a.Take(1); ok || v != "" {
		t.Fatalf("second Take = %q, %v; want nothing", v, ok)
	}
	a.Discard()
	if cleaned != 0 {
		t.Fatalf("cleanup ran %d times for a taken result", cleaned)
	}
}

func TestAheadTakeOtherKeyCleansUp(t *testing.T) {
	var cleaned []string
	a := StartAhead(1, func() (string, error) { return "v", nil }, func(v string) { cleaned = append(cleaned, v) })
	if _, ok := a.Take(2); ok {
		t.Fatal("Take(2) took a result started from 1")
	}
	a.Discard()
	if _, ok := a.Take(1); ok {
		t.Fatal("Take after a refused Take took the result")
	}
	if len(cleaned) != 1 || cleaned[0] != "v" {
		t.Fatalf("cleanup ran for %q, want once for v", cleaned)
	}
}

func TestAheadDiscard(t *testing.T) {
	cleaned := 0
	a := StartAhead("k", func() (int, error) { return 1, nil }, func(int) { cleaned++ })
	a.Discard()
	a.Discard()
	if _, ok := a.Take("k"); ok {
		t.Fatal("Take after Discard took the result")
	}
	if cleaned != 1 {
		t.Fatalf("cleanup ran %d times, want 1", cleaned)
	}
}

func TestAheadFailedWork(t *testing.T) {
	cleaned := 0
	a := StartAhead("k", func() (int, error) { return 1, errors.New("failed") }, func(int) { cleaned++ })
	if _, ok := a.Take("k"); ok {
		t.Fatal("Take took the result of failed work")
	}
	a.Discard()
	if cleaned != 0 {
		t.Fatalf("cleanup ran %d times for failed work", cleaned)
	}
}

func TestAheadFuncKey(t *testing.T) {
	a := StartAheadFunc([]int{1, 2}, func(x, y []int) bool { return len(x) == len(y) && x[0] == y[0] && x[1] == y[1] },
		func() (int, error) { return 3, nil }, nil)
	if _, ok := a.Take([]int{1, 3}); ok {
		t.Fatal("Take took the result for another key")
	}
	b := StartAheadFunc([]int{1, 2}, func(x, y []int) bool { return len(x) == len(y) && x[0] == y[0] && x[1] == y[1] },
		func() (int, error) { return 3, nil }, nil)
	if v, ok := b.Take([]int{1, 2}); !ok || v != 3 {
		t.Fatalf("Take = %d, %v; want 3, true", v, ok)
	}
}

func TestAheadNil(t *testing.T) {
	var a *Ahead[int, int]
	a.Wait()
	a.Discard()
	if _, ok := a.Take(0); ok {
		t.Fatal("nil Ahead took a result")
	}
	if a.Key() != 0 {
		t.Fatal("nil Ahead has a key")
	}
}
