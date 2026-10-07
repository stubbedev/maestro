// Ports nothing: how maestro's in-process memos of the outside world
// (deliberate deviation 3, speed) learn that code they cannot see into
// ran, and may have changed that world.

package util

import "sync/atomic"

// foreignCode counts the runs of code maestro does not control (scripts,
// the plugin runtime's PHP) started and finished, and those under way.
var foreignCode struct {
	generation atomic.Uint64
	running    atomic.Int64
}

// RunForeignCode notes that code maestro does not control starts to run:
// a script, or PHP in the plugin runtime, which may change anything a
// memo of the process saw (a checkout's branch, a file). Call done when it
// returned.
func RunForeignCode() (done func()) {
	foreignCode.running.Add(1)
	foreignCode.generation.Add(1)

	return func() {
		foreignCode.generation.Add(1)
		foreignCode.running.Add(-1)
	}
}

// ForeignCodeGeneration tells what foreign code ran so far: two calls
// return the same generation, with quiet true, only when no such code ran
// in between, nor was running at either.
func ForeignCodeGeneration() (generation uint64, quiet bool) {
	generation = foreignCode.generation.Load()

	return generation, foreignCode.running.Load() == 0
}
