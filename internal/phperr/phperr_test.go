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
	phperr.Site
}

func (e *exc) Error() string      { return e.msg }
func (e *exc) PHPPrevious() error { return e.prev }

// wrapper keeps the message of the error it wraps.
type wrapper struct{ err error }

func (w *wrapper) Error() string { return w.err.Error() }
func (w *wrapper) Unwrap() error { return w.err }

func TestSiteOf(t *testing.T) {
	prev := &exc{msg: "inner", Site: phperr.At("VersionParser.php", 526)}
	e := &exc{msg: "outer", prev: prev, Site: phperr.At("ArrayLoader.php", 412)}

	if s, ok := phperr.SiteOf(e); !ok || s != phperr.At("ArrayLoader.php", 412) {
		t.Errorf("SiteOf(e) = %v, %v", s, ok)
	}
	if !phperr.Is(e, "ArrayLoader.php", 412) {
		t.Error("Is(e, ArrayLoader.php, 412) = false")
	}
	if p := phperr.PreviousOf(e); !errors.Is(p, prev) || p == nil {
		t.Errorf("PreviousOf(e) = %v", p)
	}

	// a wrapper keeping the message is transparent
	w := &wrapper{err: e}
	if s, ok := phperr.SiteOf(w); !ok || s.Line != 412 {
		t.Errorf("SiteOf(wrapper) = %v, %v", s, ok)
	}
	if p := phperr.PreviousOf(w); !errors.Is(p, prev) || p == nil {
		t.Errorf("PreviousOf(wrapper) = %v", p)
	}

	// one that changes the message is a different exception
	ctx := fmt.Errorf("context: %w", e)
	if s, ok := phperr.SiteOf(ctx); ok {
		t.Errorf("SiteOf(fmt wrapper) = %v, want none", s)
	}
	if phperr.PreviousOf(ctx) != nil {
		t.Error("PreviousOf(fmt wrapper) != nil")
	}

	if _, ok := phperr.SiteOf(errors.New("plain")); ok {
		t.Error("SiteOf(plain error) found a site")
	}
	if _, ok := phperr.SiteOf(&exc{msg: "unsited"}); ok {
		t.Error("SiteOf(zero site) found a site")
	}
}
