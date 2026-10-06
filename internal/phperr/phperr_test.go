package phperr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stubbedev/maestro/internal/phperr"
)

type exc struct {
	msg  string
	prev error
}

func (e *exc) Error() string      { return e.msg }
func (e *exc) PHPPrevious() error { return e.prev }

// wrapper keeps the message of the error it wraps.
type wrapper struct{ err error }

func (w *wrapper) Error() string { return w.err.Error() }
func (w *wrapper) Unwrap() error { return w.err }

func TestPreviousOf(t *testing.T) {
	prev := &exc{msg: "inner"}
	e := &exc{msg: "outer", prev: prev}

	if p := phperr.PreviousOf(e); !errors.Is(p, prev) || p == nil {
		t.Errorf("PreviousOf(e) = %v", p)
	}

	// a wrapper keeping the message is transparent
	if p := phperr.PreviousOf(&wrapper{err: e}); !errors.Is(p, prev) || p == nil {
		t.Errorf("PreviousOf(wrapper) = %v", p)
	}

	// one that changes the message is a different exception
	if phperr.PreviousOf(fmt.Errorf("context: %w", e)) != nil {
		t.Error("PreviousOf(fmt wrapper) != nil")
	}

	if phperr.PreviousOf(errors.New("plain")) != nil {
		t.Error("PreviousOf(plain error) != nil")
	}
}

func TestSetRoot(t *testing.T) {
	old := phperr.Root()
	t.Cleanup(func() { phperr.SetRoot(old) })

	phperr.SetRoot("phar:///usr/local/bin/maestro")
	if got := phperr.Root(); got != "phar:///usr/local/bin/maestro" {
		t.Errorf("Root() = %q", got)
	}
}
