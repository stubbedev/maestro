package rpc

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// familyBox is a testBox of a family whose clock the test moves.
type familyBox struct {
	testBox
	family *MirrorFamily
}

func (b *familyBox) MirrorFamily() *MirrorFamily { return b.family }

// The mirrors of a family are looked at only in a message after its
// clock moved.
func TestSync_FamilyMirror(t *testing.T) {
	c, f, _ := newFake(t, Options{})
	var clock uint64
	family := &MirrorFamily{Clock: func() uint64 { return clock }}
	box := &familyBox{testBox{value: "a", label: "l"}, family}

	roundTrip := func(id string) string {
		t.Helper()
		wait := call(t, c, "m"+id, nil)
		m := f.recvRaw()
		f.send(`{"k":"ret","id":` + id + `}`)
		if _, err := wait(); err != nil {
			t.Fatal(err)
		}

		return m
	}

	wait := call(t, c, "m1", php.ListOf(box))
	f.recv()
	f.send(`{"k":"ret","id":1}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}
	// the first message after looks
	box.setValue("b")
	if m := roundTrip("2"); !strings.Contains(m, `"o":[{"h":1,"r":1,"full":{"value":"b","label":"l"}}]`) {
		t.Errorf("first: %s", m)
	}

	// a change that does not move the clock is not looked for
	box.setValue("c")
	if m := roundTrip("3"); strings.Contains(m, `"s"`) {
		t.Errorf("clock still: %s", m)
	}

	// once it moved, every change is
	clock++
	if m := roundTrip("4"); !strings.Contains(m, `"o":[{"h":1,"r":2,"full":{"value":"c","label":"l"}}]`) {
		t.Errorf("clock moved: %s", m)
	}
}
