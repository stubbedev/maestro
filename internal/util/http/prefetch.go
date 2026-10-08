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
	// file: the body goes to a file (prefetchcopy.go)
	file bool
}

// prefetchKeyOf is r's key, if r may be prefetched: a plain GET whose
// connections no callback vets.
func prefetchKeyOf(r *transferRequest) (prefetchKey, bool) {
	if r.content != nil || r.preventIP != nil || (r.method != "" && r.method != "GET") {
		return prefetchKey{}, false
	}

	return prefetchKey{
		url: r.url, method: r.method, headers: strings.Join(r.headers, "\n"),
		timeout: r.timeout, readTimeout: r.readTimeout, connectTimeout: r.connectTimeout,
		key: r.key, decode: r.decode, curlStatusLines: r.curlStatusLines,
		limit: r.limit, maxFileSize: r.maxFileSize, file: r.body != nil,
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
	return t.runOn(ctx, p, 0)
}

// runOn is run, over the connections of a lane (see prefetchLanes) when
// the transfer may open an HTTP/2 connection of its own.
func (t *prefetchedTransfer) runOn(ctx context.Context, p *transportPool, lane int) bool {
	if t.started.Swap(true) {
		return false
	}
	r := t.r
	if lane > 0 && p.mayOpenLane(r) {
		laned := *r
		laned.key.lane = lane
		r = &laned
	}
	t.res = p.do(ctx, r)
	close(t.done)

	return true
}

// maxPrefetches bounds the prefetched transfers running at once on one
// connection. Over HTTP/2 they share it, as many at once as net/http lets
// a connection take before the server's settings arrive (100, GitHub's
// limit; Packagist allows 128): one more would open a connection of its
// own. Over HTTP/1 they share MaxConnsPerHost connections. A transfer
// waiting here starts only when a running one ends, a round trip later.
const maxPrefetches = 100

// prefetchLanes is how many connections to a host the prefetched
// transfers may use at once (curl's CURLMOPT_MAX_HOST_CONNECTIONS, which
// Composer sets to 8, allows them too). A lane is a transport of its own
// (transportKey.lane): the transfers over maxPrefetches open a second
// HTTP/2 connection as soon as they are asked for, in parallel with the
// first, rather than wait a round trip for streams of the first to free.
const prefetchLanes = 2

// prefetches holds the transfers a pool started ahead of time. They start
// at most limit at once per lane: first the urgent ones (asked for by a
// reader of their response), then the others, each in the order they
// were asked for.
type prefetches struct {
	mu      sync.Mutex
	pending map[prefetchKey][]*prefetchedTransfer
	urgent  []*prefetchedTransfer
	later   []*prefetchedTransfer
	// running counts the workers running transfers, per lane.
	running [prefetchLanes]int
	// limit is how many run at once per lane; zero is maxPrefetches.
	limit int
	// oneLane keeps the transfers to the first lane.
	oneLane bool
}

// max is how many prefetched transfers run at once per lane.
func (p *prefetches) max() int {
	if p.limit == 0 {
		return maxPrefetches
	}

	return p.limit
}

// freeLane is the first lane with room for one more worker, -1 for none.
func (p *prefetches) freeLane() int {
	for lane, n := range p.running {
		if lane > 0 && p.oneLane {
			break
		}
		if n < p.max() {
			return lane
		}
	}

	return -1
}

// prefetch starts r in the background, unless an identical transfer is
// already waiting to be taken, and returns the transfer (nil when r may
// not be prefetched). An urgent transfer starts before the others; one
// asked for again as urgent becomes so. It produces no output.
func (a *prefetches) prefetch(p *transportPool, r *transferRequest, urgent bool) *prefetchedTransfer {
	key, ok := prefetchKeyOf(r)
	if !ok {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if a.pending == nil {
		a.pending = map[prefetchKey][]*prefetchedTransfer{}
	}
	if list := a.pending[key]; len(list) > 0 {
		t := list[0]
		if i := slices.Index(a.later, t); urgent && i >= 0 {
			a.later = slices.Delete(a.later, i, i+1)
			a.urgent = append(a.urgent, t)
		}

		return t
	}
	t := &prefetchedTransfer{r: r, done: make(chan struct{})}
	a.pending[key] = append(a.pending[key], t)
	if urgent {
		a.urgent = append(a.urgent, t)
	} else {
		a.later = append(a.later, t)
	}
	if lane := a.freeLane(); lane >= 0 {
		a.running[lane]++
		go a.runPrefetches(p, lane)
	}

	return t
}

// runPrefetches runs queued transfers on a lane of p until there are
// none.
func (a *prefetches) runPrefetches(p *transportPool, lane int) {
	for {
		a.mu.Lock()
		queue := &a.urgent
		if len(*queue) == 0 {
			queue = &a.later
		}
		if len(*queue) == 0 {
			a.running[lane]--
			a.mu.Unlock()

			return
		}
		t := (*queue)[0]
		(*queue)[0] = nil
		*queue = (*queue)[1:]
		a.mu.Unlock()

		t.runOn(context.Background(), p, lane)
	}
}

// response waits for the transfer and returns its status and body, ok
// false when it failed.
func (t *prefetchedTransfer) response() (status int, body string, ok bool) {
	<-t.done
	if res := t.res; res != nil && res.fail.Errno == 0 && res.err == nil {
		return res.status, string(res.body), true
	}

	return 0, "", false
}

// take removes and returns a prefetched transfer identical to r, if any.
func (a *prefetches) take(r *transferRequest) *prefetchedTransfer {
	key, ok := prefetchKeyOf(r)
	if !ok {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	list := a.pending[key]
	if len(list) == 0 {
		return nil
	}
	t := list[0]
	if len(list) == 1 {
		delete(a.pending, key)
	} else {
		a.pending[key] = list[1:]
	}

	return t
}

// doOrTake is do, answered by a prefetched transfer of the same request
// when there is one (made here when it is still queued). A cancellation
// before it finished falls back to do, which reports the cancellation as
// it would have.
func (a *prefetches) doOrTake(ctx context.Context, p *transportPool, r *transferRequest) *transferResult {
	if t := a.take(r); t != nil {
		if t.run(ctx, p) {
			return t.handTo(ctx, p, r)
		}
		select {
		case <-t.done:
			return t.handTo(ctx, p, r)
		case <-ctx.Done():
		}
	}

	return p.do(ctx, r)
}
