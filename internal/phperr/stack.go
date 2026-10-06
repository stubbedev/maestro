package phperr

import (
	"slices"
	"sync"
)

// The PHP calls in progress, for what reads the call stack while nothing
// is thrown: ErrorHandler's deprecation notices list debug_backtrace()
// under -v. An exception's trace is built as the error goes back up
// (Call); a notice is printed on the way down, so the ports along its
// paths record the calls they make with Enter.
var calls struct {
	sync.Mutex
	frames []*Frame
}

// Enter records that the PHP call of function at file:line is in progress
// until the returned function is called:
//
//	leave := phperr.Enter(`Composer\Factory->createComposer`, "Factory.php", 631)
//	full, err := f.CreateComposer(...)
//	leave()
//
// The stack is process-wide, as PHP's is; a call left out of order (a
// goroutine's) removes only its own frame.
func Enter(function, file string, line int) (leave func()) {
	fr := &Frame{Function: function, File: file, Line: line}
	calls.Lock()
	calls.frames = append(calls.frames, fr)
	calls.Unlock()

	return func() {
		calls.Lock()
		defer calls.Unlock()
		for i, f := range slices.Backward(calls.frames) {
			if f == fr {
				calls.frames = slices.Delete(calls.frames, i, i+1)

				return
			}
		}
	}
}

// Stack returns the calls in progress (Enter), innermost first.
func Stack() []Frame {
	calls.Lock()
	defer calls.Unlock()
	frames := make([]Frame, 0, len(calls.frames))
	for _, f := range slices.Backward(calls.frames) {
		frames = append(frames, *f)
	}

	return frames
}
