// Ports nothing: requests started ahead of time (deliberate deviation 3,
// speed).

package http

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// prefetchKey is everything that makes two transfers the same exchange:
// a prefetched result is only handed to a transfer whose request matches
// it field for field, so a prefetch that guessed wrong costs a request,
// never a different result.
type prefetchKey struct {
	url, method                          string
	headers                              string
	timeout, readTimeout, connectTimeout time.Duration
	key                                  transportKey
	decode, curlStatusLines              bool
	limit, maxFileSize                   int64
}

// prefetchKeyOf is r's key, if r may be prefetched: a plain GET whose
// response is kept in memory and whose connections no callback vets.
func prefetchKeyOf(r *transferRequest) (prefetchKey, bool) {
	if r.body != nil || r.content != nil || r.preventIP != nil || (r.method != "" && r.method != "GET") {
		return prefetchKey{}, false
	}

	return prefetchKey{
		url: r.url, method: r.method, headers: strings.Join(r.headers, "\n"),
		timeout: r.timeout, readTimeout: r.readTimeout, connectTimeout: r.connectTimeout,
		key: r.key, decode: r.decode, curlStatusLines: r.curlStatusLines,
		limit: r.limit, maxFileSize: r.maxFileSize,
	}, true
}

// prefetchedTransfer is a transfer started ahead; done closes when res is
// set.
type prefetchedTransfer struct {
	r    *transferRequest
	done chan struct{}
	res  *transferResult
	// started is set by whoever runs the transfer: a prefetch worker, or
	// the request taking it before a worker got to it.
	started atomic.Bool
}

// run makes the transfer, unless it was started already.
func (t *prefetchedTransfer) run(ctx context.Context, p *transportPool) bool {
	if t.started.Swap(true) {
		return false
	}
	t.res = p.do(ctx, t.r)
	close(t.done)

	return true
}

// maxPrefetches bounds the prefetched transfers running at once: over
// HTTP/2 (Packagist, GitHub) they share one connection; a server
// answering over HTTP/1 gets that many connections at most.
var maxPrefetches = 64 // a variable for the tests

// prefetches holds the transfers a pool started ahead of time. They start
// at most maxPrefetches at once: first the urgent ones (asked for by a
// reader of their response), then the others, each in the order they
// were asked for.
type prefetches struct {
	mu      sync.Mutex
	pending map[prefetchKey][]*prefetchedTransfer
	urgent  []*prefetchedTransfer
	later   []*prefetchedTransfer
	running int
}

// prefetch starts r in the background, unless an identical transfer is
// already waiting to be taken, and returns the transfer (nil when r may
// not be prefetched). An urgent transfer starts before the others; one
// asked for again as urgent becomes so. It produces no output.
func (p *transportPool) prefetch(r *transferRequest, urgent bool) *prefetchedTransfer {
	key, ok := prefetchKeyOf(r)
	if !ok {
		return nil
	}

	p.ahead.mu.Lock()
	defer p.ahead.mu.Unlock()

	if p.ahead.pending == nil {
		p.ahead.pending = map[prefetchKey][]*prefetchedTransfer{}
	}
	if list := p.ahead.pending[key]; len(list) > 0 {
		t := list[0]
		if i := slices.Index(p.ahead.later, t); urgent && i >= 0 {
			p.ahead.later = slices.Delete(p.ahead.later, i, i+1)
			p.ahead.urgent = append(p.ahead.urgent, t)
		}

		return t
	}
	t := &prefetchedTransfer{r: r, done: make(chan struct{})}
	p.ahead.pending[key] = append(p.ahead.pending[key], t)
	if urgent {
		p.ahead.urgent = append(p.ahead.urgent, t)
	} else {
		p.ahead.later = append(p.ahead.later, t)
	}
	if p.ahead.running < maxPrefetches {
		p.ahead.running++
		go p.runPrefetches()
	}

	return t
}

// runPrefetches runs queued transfers until there are none.
func (p *transportPool) runPrefetches() {
	for {
		p.ahead.mu.Lock()
		queue := &p.ahead.urgent
		if len(*queue) == 0 {
			queue = &p.ahead.later
		}
		if len(*queue) == 0 {
			p.ahead.running--
			p.ahead.mu.Unlock()

			return
		}
		t := (*queue)[0]
		(*queue)[0] = nil
		*queue = (*queue)[1:]
		p.ahead.mu.Unlock()

		t.run(context.Background(), p)
	}
}

// response waits for the transfer and returns its status and body, ok
// false when it failed.
func (t *prefetchedTransfer) response() (status int, body string, ok bool) {
	<-t.done
	if res := t.res; res != nil && res.errno == 0 && res.err == nil {
		return res.status, string(res.body), true
	}

	return 0, "", false
}

// take removes and returns a prefetched transfer identical to r, if any.
func (p *transportPool) take(r *transferRequest) *prefetchedTransfer {
	key, ok := prefetchKeyOf(r)
	if !ok {
		return nil
	}

	p.ahead.mu.Lock()
	defer p.ahead.mu.Unlock()

	list := p.ahead.pending[key]
	if len(list) == 0 {
		return nil
	}
	t := list[0]
	if len(list) == 1 {
		delete(p.ahead.pending, key)
	} else {
		p.ahead.pending[key] = list[1:]
	}

	return t
}

// doOrTake is do, answered by a prefetched transfer of the same request
// when there is one (made here when it is still queued). A cancellation
// before it finished falls back to do, which reports the cancellation as
// it would have.
func (p *transportPool) doOrTake(ctx context.Context, r *transferRequest) *transferResult {
	if t := p.take(r); t != nil {
		if t.run(ctx, p) {
			return t.res
		}
		select {
		case <-t.done:
			return t.res
		case <-ctx.Done():
		}
	}

	return p.do(ctx, r)
}
