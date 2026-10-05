// The PHP baton (docs/PLUGINS.md §5.14).

package rpc

import (
	"bytes"
	"errors"
	"runtime"
	"strconv"
	"sync"
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
}

// enter takes or re-enters the baton for the calling goroutine.
func (b *baton) enter() (leave func(), err error) {
	id := goid()

	b.mu.Lock()
	defer b.mu.Unlock()

	switch b.owner {
	case 0:
		b.owner = id
	case id:
	default:
		return nil, ErrBaton
	}
	b.depth++

	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		b.depth--
		if b.depth == 0 {
			b.owner = 0
		}
	}, nil
}

// lend makes the goroutine running fn the holder while fn runs, then gives
// the baton back to the calling holder.
func (b *baton) lend(fn func() error) error {
	b.mu.Lock()
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
