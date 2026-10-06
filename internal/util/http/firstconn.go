// Ports nothing: transfers to a host wait for its first connection
// (deliberate deviation 3, speed).

package http

import (
	"context"
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
// The transfer calls connected once it has its connection, with whether
// that speaks HTTP/2, sent once it sent its request or starts opening a
// connection of its own, and done when it ends.
func (p *transportPool) awaitFirstConn(ctx context.Context, key transportKey, u *url.URL) (connected func(h2 bool), sent, done func()) {
	noop := func() {}
	if u.Scheme != "https" || key.proxy != "" || key.http1 || key.fresh {
		return func(bool) {}, noop, noop
	}

	port := u.Port()
	if port == "" {
		port = "443"
	}

	fk := firstConnKey{key: key, addr: u.Hostname() + ":" + port}

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
		settle := func(h2 bool) {
			once.Do(func() {
				p.mu.Lock()
				fc.settled = true
				waiters := fc.waiters
				fc.waiters = nil
				p.mu.Unlock()

				if !h2 {
					for _, w := range waiters {
						w.release()
					}

					return
				}

				for i := 1; i < len(waiters); i++ {
					waiters[i-1].next = waiters[i]
				}

				if len(waiters) > 0 {
					waiters[0].release()
				}
			})
		}

		return settle, noop, func() { settle(false) }
	}

	if fc.settled {
		p.mu.Unlock()

		return func(bool) {}, noop, noop
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

		return func(bool) {}, noop, noop
	}

	var once sync.Once
	releaseNext := func() { once.Do(func() { w.next.release() }) }

	return func(h2 bool) {
		if !h2 {
			releaseNext()
		}
	}, releaseNext, releaseNext
}
