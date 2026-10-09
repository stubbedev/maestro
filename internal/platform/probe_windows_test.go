//go:build windows

package platform

import (
	"os"
	"testing"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

func TestHasResultLine(t *testing.T) {
	result := probeMarker + `{"x":1}` + "\n"

	for _, c := range []struct {
		out  string
		want bool
	}{
		{result, true},
		{"PHP Warning: ...\n" + result, true},
		{"", false},
		{probeMarker, false},
		{probeMarker + `{"x":1}`, false},
		{"PHP Warning: ...\n" + probeMarker, false},
		// the marker of an earlier result is not the end of a later one
		{result[:len(result)-1], false},
	} {
		if got := hasResultLine([]byte(c.out)); got != c.want {
			t.Errorf("hasResultLine(%q) = %v, want %v", c.out, got, c.want)
		}
	}
}

// TestProcessModules checks that a process's own executable is among the
// modules listed for it.
func TestProcessModules(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	selfID, ok := fsstate.Stat(self)
	if !ok {
		t.Fatal("no ID for the test binary")
	}

	modules, ok := processModules(uint32(os.Getpid()))
	if !ok || len(modules) == 0 {
		t.Fatalf("processModules = %q, %v", modules, ok)
	}

	for _, m := range modules {
		if id, ok := fsstate.Stat(m); ok && id == selfID {
			return
		}
	}

	t.Errorf("the process's own executable is not among %d modules", len(modules))
}
