package eventdispatcher

import "testing"

// checker is a ListenerChecker answering listened.
type checker struct{ listened bool }

func (c checker) WillDispatchTo(Event) bool { return c.listened }

// MayListen is the one answer every speculation takes: no dispatcher has
// no listener, and a dispatcher that cannot tell may have one.
func TestMayListen(t *testing.T) {
	event := NewEvent("post-install-cmd", nil, nil)
	for _, tc := range []struct {
		name       string
		dispatcher any
		want       bool
	}{
		{"no dispatcher", nil, false},
		{"cannot tell", struct{}{}, true},
		{"no listener", checker{false}, false},
		{"listener", checker{true}, true},
	} {
		if got := MayListen(tc.dispatcher, event); got != tc.want {
			t.Errorf("%s: MayListen = %v, want %v", tc.name, got, tc.want)
		}
	}
}
