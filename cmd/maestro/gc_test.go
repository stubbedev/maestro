package main

import (
	"math"
	"runtime"
	"runtime/debug"
	"testing"
	"time"
)

// gcSettings reads the collector's settings.
func gcSettings() (percent int, limit int64) {
	percent = debug.SetGCPercent(-1)
	debug.SetGCPercent(percent)

	return percent, debug.SetMemoryLimit(-1)
}

// The collector stays off until the first collection, after which the
// settings the run started with are back; GOGC or GOMEMLIMIT in the
// environment leave them alone.
func TestStartGCFloor(t *testing.T) {
	percent, limit := gcSettings()
	if percent != 100 || limit != math.MaxInt64 {
		t.Skipf("the test runs with GOGC=%d, GOMEMLIMIT=%d", percent, limit)
	}

	for _, name := range []string{"GOGC", "GOMEMLIMIT"} {
		startGCFloor(func(v string) string {
			if v == name {
				return "200"
			}

			return ""
		})

		if p, l := gcSettings(); p != percent || l != limit {
			t.Fatalf("with %s set: GOGC %d, limit %d", name, p, l)
		}
	}

	startGCFloor(func(string) string { return "" })

	if p, l := gcSettings(); p != -1 || l != gcFloor {
		t.Fatalf("before the first collection: GOGC %d, limit %d", p, l)
	}

	runtime.GC()

	deadline := time.Now().Add(10 * time.Second)

	for {
		p, l := gcSettings()
		if p == percent && l == limit {
			return
		}

		if time.Now().After(deadline) {
			t.Fatalf("after the first collection: GOGC %d, limit %d", p, l)
		}

		time.Sleep(time.Millisecond)
	}
}
