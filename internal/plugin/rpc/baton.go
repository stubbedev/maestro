// The PHP baton (docs/PLUGINS.md §5.14).

package rpc

import (
	"bytes"
	"errors"
	"runtime"
	"strconv"
	"sync"

	"github.com/stubbedev/maestro/internal/util"
)

// ErrBaton is returned by a call made on a goroutine that does not hold
// the PHP baton while another goroutine does.
var ErrBaton = errors.New("maestro: PHP called from a goroutine that does not hold the plugin runtime's baton (docs/PLUGINS.md §5.14)")

// baton is the right to call PHP: free, or held by one goroutine, which
// may nest calls.
type baton struct {
	mu    sync.Mutex
	owner int64 // goroutine id; 0 when free
	depth int
	// guest is set while the owner took the free baton for a call of
	// parallel work (Conn.Run): other goroutines' calls wait for it to
	// end instead of failing
	guest bool
	freed *sync.Cond
	// posted are the calls of parallel work for the holder (Conn.Run),
	// oldest first
	posted []func()
}

// enter takes or re-enters the baton for the calling goroutine.
func (b *baton) enter() error {
	id := goid()

	b.mu.Lock()
	defer b.mu.Unlock()

	b.waitGuest(id)

	switch b.owner {
	case 0:
		b.owner = id
	case id:
	default:
		return ErrBaton
	}
	b.depth++

	return nil
}

// exitUnlessPosted leaves the baton entered last, unless that gives it up
// with calls posted for its holder, which must run first (unless force):
// it then reports false.
func (b *baton) exitUnlessPosted(force bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !force && b.depth == 1 && len(b.posted) > 0 {
		return false
	}
	b.depth--
	if b.depth == 0 {
		b.owner = 0
		if b.guest {
			b.guest = false
			b.cond().Broadcast()
		}
	}

	return true
}

// waitGuest waits while a guest other than id holds the baton; b.mu is
// held.
func (b *baton) waitGuest(id int64) {
	for b.guest && b.owner != id {
		b.cond().Wait()
	}
}

// cond is the condition guests' ends are broadcast on; b.mu is held.
func (b *baton) cond() *sync.Cond {
	if b.freed == nil {
		b.freed = sync.NewCond(&b.mu)
	}

	return b.freed
}

// run is Conn.Run's part on the baton: it reports whether the calling
// goroutine holds the baton (as is), took the free baton as a guest
// (entered once), or posted fn for the holder.
func (b *baton) run(fn func()) (holds, guest bool) {
	id := goid()

	b.mu.Lock()
	defer b.mu.Unlock()

	switch {
	case b.owner == id:
		return true, false
	case b.owner == 0 && !b.guest:
		b.owner, b.depth, b.guest = id, 1, true

		return true, true
	}
	b.posted = append(b.posted, fn)

	return false, false
}

// takePosted removes and returns the oldest posted call, if the calling
// goroutine holds the baton.
func (b *baton) takePosted() (func(), bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.posted) == 0 || b.owner != goid() {
		return nil, false
	}
	fn := b.posted[0]
	b.posted = b.posted[1:]

	return fn, true
}

// postedForCaller reports whether calls are posted and the calling
// goroutine holds the baton.
func (b *baton) postedForCaller() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	return len(b.posted) > 0 && b.owner == goid()
}

// lend makes the goroutine running fn the holder while fn runs, then gives
// the baton back to the calling holder.
func (b *baton) lend(fn func() error) error {
	b.mu.Lock()
	b.waitGuest(goid())
	holder, depth := b.owner, b.depth
	if holder != 0 && holder != goid() {
		b.mu.Unlock()

		return ErrBaton
	}
	b.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		b.mu.Lock()
		b.owner, b.depth = goid(), 0
		b.mu.Unlock()

		var err error
		defer func() {
			b.mu.Lock()
			b.owner, b.depth = holder, depth
			b.mu.Unlock()
			// what was posted to the lent goroutine is the holder's now
			util.NotifyWaitHooks()
			done <- err
		}()
		err = fn()
	}()

	return <-done
}

// goid returns the id of the calling goroutine, from the first line of
// its stack trace ("goroutine 18 [running]:").
func goid() int64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	line := bytes.TrimPrefix(buf[:n], []byte("goroutine "))
	if i := bytes.IndexByte(line, ' '); i > 0 {
		line = line[:i]
	}
	id, err := strconv.ParseInt(string(line), 10, 64)
	if err != nil {
		panic("rpc: cannot read the goroutine id from " + strconv.Quote(string(buf[:n])))
	}

	return id
}
