package phperr

import (
	"slices"
	"testing"
)

func TestEnterStack(t *testing.T) {
	if s := Stack(); len(s) != 0 {
		t.Fatalf("stack not empty: %v", s)
	}
	outer := Enter("outer", "a.php", 1)
	inner := Enter("inner", "b.php", 2)
	want := []Frame{{Function: "inner", File: "b.php", Line: 2}, {Function: "outer", File: "a.php", Line: 1}}
	if s := Stack(); !slices.Equal(s, want) {
		t.Errorf("got %v, want %v", s, want)
	}
	// left out of order (another goroutine's call): only its own frame goes
	outer()
	if s := Stack(); !slices.Equal(s, want[:1]) {
		t.Errorf("got %v, want %v", s, want[:1])
	}
	inner()
	if s := Stack(); len(s) != 0 {
		t.Errorf("stack not empty: %v", s)
	}
}
