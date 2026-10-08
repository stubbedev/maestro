// Ports nothing: transfers to a host wait for its first connection
// (deliberate deviation 3, speed).

package http

import (
	"context"
	"net"
	"net/url"
	"sync"
)

// firstConnKey names the connections of one transport to one address.
type firstConnKey struct {
	key  transportKey
	addr string
}

// firstConn is the first connection attempt of a transport to an address.
type firstConn struct {
	// settled is set once the first transfer got a connection, or failed.
	settled bool
	// h2 tells that the connection it got speaks HTTP/2.
	h2 bool
	// waiters are the transfers waiting for that, in arrival order.
	waiters []*connWaiter
}

// connWaiter is a transfer waiting for the first connection.
type connWaiter struct {
	ready chan struct{}
	// next is the waiter released once this one sent its request (over
	// HTTP/2), set before ready closes.
	next *connWaiter
}

// release lets the waiter go ahead.
func (w *connWaiter) release() {
	if w != nil {
		close(w.ready)
	}
}

// awaitFirstConn holds a transfer to an https address back while the
// first transfer to that address is still connecting, as curl's
// CURLOPT_PIPEWAIT does: over HTTP/2 the transfers then share that
// connection instead of each opening its own (net/http dials one per
// waiting transfer, up to MaxConnsPerHost, each with its TCP and TLS
// handshakes), and send their requests in the order they were made
// (the root file a command waits for before the files prefetched after
// it). Over HTTP/1, or when the first transfer failed, they go ahead
// together.
//
// The transfer calls connected once it has its connection, with that
// connection when it speaks HTTP/2 (nil otherwise), sent once it sent its
// request or starts opening a connection of its own, and done when it
// ends.
//
// The requests released one after the other over HTTP/2 leave in as few
// TCP segments as they fit in (holdWrites, until the last of them was
// sent), as curl's do, which writes the frames of all of them at once: a
// new connection may send about ten segments before the server
// acknowledges them, and a segment per request would hold the requests
// after the first few back for a round trip.
func (p *transportPool) awaitFirstConn(ctx context.Context, key transportKey, u *url.URL) (connected func(h2 net.Conn), sent, done func()) {
	noop := func() {}
	fk, ok := firstConnKeyOf(key, u)
	if !ok {
		return func(net.Conn) {}, noop, noop
	}

	p.mu.Lock()
	if p.first == nil {
		p.first = map[firstConnKey]*firstConn{}
	}

	fc, ok := p.first[fk]
	if !ok {
		// the first transfer
		fc = &firstConn{}
		p.first[fk] = fc
		p.mu.Unlock()

		var once sync.Once
		settle := func(h2 net.Conn) {
			once.Do(func() {
				p.mu.Lock()
				fc.settled = true
				fc.h2 = h2 != nil
				waiters := fc.waiters
				fc.waiters = nil
				p.mu.Unlock()

				if h2 == nil {
					for _, w := range waiters {
						w.release()
					}

					return
				}

				if len(waiters) == 0 {
					return
				}

				// the last waiter releases the writes held back
				end := &connWaiter{ready: make(chan struct{})}
				flush := holdWrites(h2)
				go func() {
					<-end.ready
					flush()
				}()

				waiters = append(waiters, end)
				for i := 1; i < len(waiters); i++ {
					waiters[i-1].next = waiters[i]
				}

				waiters[0].release()
			})
		}

		return settle, noop, func() { settle(nil) }
	}

	if fc.settled {
		p.mu.Unlock()

		return func(net.Conn) {}, noop, noop
	}

	w := &connWaiter{ready: make(chan struct{})}
	fc.waiters = append(fc.waiters, w)
	p.mu.Unlock()

	select {
	case <-w.ready:
	case <-ctx.Done():
		// the transfer fails on its context; the next waiter goes when
		// this one would have
		go func() {
			<-w.ready
			w.next.release()
		}()

		return func(net.Conn) {}, noop, noop
	}

	var once sync.Once
	releaseNext := func() { once.Do(func() { w.next.release() }) }

	return func(h2 net.Conn) {
		if h2 == nil {
			releaseNext()
		}
	}, releaseNext, releaseNext
}

// firstConnKeyOf is the key of the first connection a transfer with key
// to u waits for; ok is false when it waits for none: not https, through
// a proxy, HTTP/1 only or on a fresh connection.
func firstConnKeyOf(key transportKey, u *url.URL) (firstConnKey, bool) {
	if u.Scheme != "https" || key.proxy != "" || key.http1 || key.fresh {
		return firstConnKey{}, false
	}

	port := u.Port()
	if port == "" {
		port = "443"
	}

	return firstConnKey{key: key, addr: u.Hostname() + ":" + port}, true
}

// mayOpenLane tells whether r may go over a connection of a lane of its
// own (prefetchLanes): it waits for a first connection, and the first
// connection of its own transport to that host, if settled, speaks
// HTTP/2 (an HTTP/1 server takes a request per connection: a lane would
// only open more of them).
func (p *transportPool) mayOpenLane(r *transferRequest) bool {
	u, err := url.Parse(r.url)
	if err != nil {
		return false
	}
	fk, ok := firstConnKeyOf(r.key, u)
	if !ok {
		return false
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	fc := p.first[fk]

	return fc == nil || !fc.settled || fc.h2
}
