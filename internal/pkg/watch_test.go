package pkg

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// ChangeClock moves with the changes of watched packages only, an alias
// watching the package it aliases.
func TestChangeClock(t *testing.T) {
	p := NewCompletePackage("acme/lib", "1.0.0.0", "1.0.0")
	other := NewCompletePackage("acme/other", "1.0.0.0", "1.0.0")
	a := NewCompleteAliasPackage(p, "2.0.0.0", "2.0.0")

	moved := func(change func()) bool {
		before := ChangeClock()
		change()

		return ChangeClock() != before
	}

	if moved(func() { p.SetExtra(php.ArrayOf("a", int64(1))) }) {
		t.Error("an unwatched package moved the clock")
	}
	Watch(a)
	if !moved(func() { p.SetExtra(php.ArrayOf("a", int64(2))) }) || !moved(func() { p.SetID(3) }) {
		t.Error("the aliased package of a watched alias did not move the clock")
	}
	if !moved(func() { a.SetID(4) }) {
		t.Error("a watched alias did not move the clock")
	}
	if moved(func() { other.SetID(5) }) {
		t.Error("an unwatched package moved the clock")
	}
}
