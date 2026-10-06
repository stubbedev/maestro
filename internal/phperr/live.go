// The call stack in progress (docs/PLUGINS.md §5.12).

package phperr

import (
	"slices"
	"sync"
)

// An exception's trace is recorded as the error goes up (Call), but PHP
// code can also ask for the stack while it runs: Composer's ErrorHandler
// lists debug_backtrace() under a deprecation notice at -v, which in the
// plugin runtime runs inside maestro's calls of plugin code. The calls that
// lead into PHP code therefore also record themselves while they are in
// progress (Enter, EnterCode), on one stack for the process: Composer runs
// on one thread, and maestro calls PHP code only from the flow holding the
// plugin runtime's baton.
//
// A call PHP code makes into maestro (the plugin runtime serving it) is a
// mark on the stack (Callback): what is above it ran for that call, what is
// below it called the PHP code making it.
//
// Since #13 deprecation notices are maestro's own, without a stack, and
// nothing reads Live any more; the stack stays until the error path is
// simplified (#13 step 3).

// liveFrame is an entry of the stack: a frame (Call's), a call into code
// whose callee maestro cannot name (Locate's, Function ""), or a mark.
type liveFrame struct {
	Frame
	mark bool
	// id identifies a Push's entry, which its leave removes alone.
	id uint64
}

var live struct {
	mu     sync.Mutex
	frames []liveFrame
	nextID uint64
}

// push adds an entry and returns the function removing it with what was
// pushed above it (left there by a panic).
func push(f liveFrame) func() {
	live.mu.Lock()
	n := len(live.frames)
	live.frames = append(live.frames, f)
	live.mu.Unlock()

	return func() {
		live.mu.Lock()
		if len(live.frames) > n {
			live.frames = live.frames[:n]
		}
		live.mu.Unlock()
	}
}

// Enter records that Composer's call of function at file:line (as Call
// takes them) is in progress. The returned function ends it and records
// it in the trace of the error the call returned (Call), returning that
// error:
//
//	done := phperr.Enter(`Composer\EventDispatcher\EventDispatcher->doDispatch`, "EventDispatcher.php", 142)
//	ret, err := d.doDispatch(event)
//
//	return ret, done(err)
func Enter(function, file string, line int) func(err error) error {
	pop := push(liveFrame{Function: function, File: file, Line: line})

	return func(err error) error {
		pop()

		return Call(err, function, file, line)
	}
}

// EnterCode is Enter for a call into PHP code whose callee maestro cannot
// name (a listener, a script, an object written in PHP): the returned
// function records the call's location in the error's trace (Locate).
func EnterCode(file string, line int) func(err error) error {
	pop := push(liveFrame{File: file, Line: line})

	return func(err error) error {
		pop()

		return Locate(err, file, line)
	}
}

// Within records that the calls of frames (innermost first, as Calls
// takes them) are in progress, for a caller recording them in errors'
// traces itself; the returned function ends them.
func Within(frames ...Frame) func() {
	live.mu.Lock()
	n := len(live.frames)
	for _, f := range slices.Backward(frames) {
		live.frames = append(live.frames, liveFrame{Frame: f})
	}
	live.mu.Unlock()

	return func() {
		live.mu.Lock()
		if len(live.frames) > n {
			live.frames = live.frames[:n]
		}
		live.mu.Unlock()
	}
}

// Callback marks the start of a call PHP code made into maestro; the
// returned function ends it.
func Callback() func() { return push(liveFrame{mark: true}) }

// Live returns the calls in progress, innermost first, in segments: the
// first holds those above the innermost Callback mark, each next one those
// between two marks, the last those below the outermost. A frame with an
// empty Function is the location of a call into PHP code (EnterCode).
func Live() [][]Frame {
	live.mu.Lock()
	defer live.mu.Unlock()

	segments := [][]Frame{nil}
	for _, f := range slices.Backward(live.frames) {
		if f.mark {
			segments = append(segments, nil)

			continue
		}
		segments[len(segments)-1] = append(segments[len(segments)-1], f.Frame)
	}

	return segments
}

// Push records that the PHP call of function at file:line is in progress
// until the returned function is called, for ports that record the call
// in errors' traces themselves (or have none to record), as the stacks
// plugin code asks for list the calls in progress (Live):
//
//	leave := phperr.Push(`Composer\Factory->createComposer`, "Factory.php", 631)
//	full, err := f.CreateComposer(...)
//	leave()
//
// A call left out of order (a goroutine's) removes only its own entry.
func Push(function, file string, line int) (leave func()) {
	live.mu.Lock()
	live.nextID++
	id := live.nextID
	f := liveFrame{id: id}
	f.Function, f.File, f.Line = function, file, line
	live.frames = append(live.frames, f)
	live.mu.Unlock()

	return func() {
		live.mu.Lock()
		defer live.mu.Unlock()
		for i, f := range slices.Backward(live.frames) {
			if f.id == id {
				live.frames = slices.Delete(live.frames, i, i+1)

				return
			}
		}
	}
}
