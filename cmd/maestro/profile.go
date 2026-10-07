//go:build maestro_profile

// Profiling hooks for performance work, compiled in only with the
// maestro_profile build tag (`go build -tags maestro_profile`), never in a
// release: MAESTRO_CPUPROFILE=<file> writes a CPU profile,
// MAESTRO_MEMPROFILE=<file> a heap (allocation) profile and
// MAESTRO_TRACE=<file> an execution trace (`go tool trace`) of the run.
package main

import (
	"os"
	"runtime"
	"runtime/pprof"
	"runtime/trace"

	"github.com/stubbedev/maestro/internal/switches"
)

func init() {
	startProfiling = func() func() {
		var stops []func()

		if path := os.Getenv(switches.CPUProfile); path != "" {
			if f, err := os.Create(path); err == nil {
				if pprof.StartCPUProfile(f) == nil {
					stops = append(stops, func() { pprof.StopCPUProfile(); _ = f.Close() })
				}
			}
		}

		if path := os.Getenv(switches.Trace); path != "" {
			if f, err := os.Create(path); err == nil {
				if trace.Start(f) == nil {
					stops = append(stops, func() { trace.Stop(); _ = f.Close() })
				}
			}
		}

		if path := os.Getenv(switches.MemProfile); path != "" {
			runtime.MemProfileRate = 4096
			stops = append(stops, func() {
				if f, err := os.Create(path); err == nil {
					_ = pprof.Lookup("allocs").WriteTo(f, 0)
					_ = f.Close()
				}
			})
		}

		return func() {
			for _, stop := range stops {
				stop()
			}
		}
	}
}
