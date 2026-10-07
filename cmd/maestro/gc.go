package main

import (
	"runtime"
	"runtime/debug"
)

// gcFloor is the memory (the Go runtime's total, as debug.SetMemoryLimit
// counts it) a run grows to before its first garbage collection. Most
// commands finish below it and never collect; one that reaches it
// collects from then on as Go's defaults have it.
const gcFloor = 64 << 20

// startGCFloor turns the collector off until the heap reaches gcFloor
// (debug.SetMemoryLimit triggers that collection), then puts back the
// settings the run started with once the first collection is done. A
// GOGC or GOMEMLIMIT set in the environment is left alone.
func startGCFloor(getenv func(string) string) {
	if getenv("GOGC") != "" || getenv("GOMEMLIMIT") != "" {
		return
	}

	percent := debug.SetGCPercent(-1)
	limit := debug.SetMemoryLimit(gcFloor)

	// the sentinel is unreachable at once: its cleanup runs after the
	// first collection (32 bytes keeps it out of the tiny allocator,
	// whose blocks outlive their objects)
	runtime.AddCleanup(new([32]byte), func(struct{}) {
		debug.SetGCPercent(percent)
		debug.SetMemoryLimit(limit)
	}, struct{}{})
}
