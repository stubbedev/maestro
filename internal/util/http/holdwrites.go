// Ports nothing: requests leave in as few TCP segments as they fit in
// (deliberate deviation 3, speed).

package http

import (
	"net"
	"sync"
	"time"
)

// maxWriteHold bounds how long a hold (holdWrites) holds writes back,
// whatever happens to the burst it was asked for.
const maxWriteHold = 20 * time.Millisecond

// burstLinger is how long a request started ahead along with others
// (transferRequest.burst) holds its connection's writes back once
// written, for the requests started right after it to join it: each
// would otherwise leave in a segment of its own, and a connection sends
// only about ten segments a round trip until the server acknowledges
// them.
const burstLinger = time.Millisecond

// writeHold holds a TCP socket's small writes back (TCP_CORK) while it
// has holders, so that what they write leaves together, in full
// segments.
type writeHold struct {
	mu   sync.Mutex
	held int
}

// hold holds the writes to conn back until release is called, or for
// maxWriteHold at most. Nothing is held for a connection that is not
// over TCP, or where the socket cannot (corkSocket).
func (h *writeHold) hold(conn net.Conn) (release func()) {
	h.mu.Lock()
	if h.held == 0 && !corkSocket(conn, true) {
		h.mu.Unlock()

		return func() {}
	}
	h.held++
	h.mu.Unlock()

	var once sync.Once
	release = func() {
		once.Do(func() {
			h.mu.Lock()
			if h.held--; h.held == 0 {
				corkSocket(conn, false)
			}
			h.mu.Unlock()
		})
	}
	time.AfterFunc(maxWriteHold, release)

	return release
}

// holdWrites holds the writes to conn back until flush is called (at
// most maxWriteHold), along with the other holds of an HTTP/2
// connection's socket: it is released once the last of them is.
func holdWrites(conn net.Conn) (flush func()) {
	if hc, ok := conn.(*h2HeadConn); ok {
		return hc.writes.hold(conn)
	}

	return new(writeHold).hold(conn)
}
