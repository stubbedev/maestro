// Ports nothing: requests started ahead of time (deliberate deviation 3,
// speed).

package http

import (
	"context"
	"strings"
	"sync"
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
	done chan struct{}
	res  *transferResult
}

// maxPrefetches bounds the prefetched transfers running at once: over
// HTTP/2 (Packagist, GitHub) they share one connection; a server
// answering over HTTP/1 gets that many connections at most.
const maxPrefetches = 64

// prefetches holds the transfers a pool started ahead of time.
type prefetches struct {
	mu      sync.Mutex
	pending map[prefetchKey][]*prefetchedTransfer
	slots   chan struct{}
}

// prefetch starts r in the background, unless an identical transfer is
// already waiting to be taken. It produces no output.
func (p *transportPool) prefetch(r *transferRequest) {
	key, ok := prefetchKeyOf(r)
	if !ok {
		return
	}

	p.ahead.mu.Lock()
	if p.ahead.pending == nil {
		p.ahead.pending = map[prefetchKey][]*prefetchedTransfer{}
		p.ahead.slots = make(chan struct{}, maxPrefetches)
	}
	if len(p.ahead.pending[key]) > 0 {
		p.ahead.mu.Unlock()

		return
	}
	t := &prefetchedTransfer{done: make(chan struct{})}
	p.ahead.pending[key] = append(p.ahead.pending[key], t)
	slots := p.ahead.slots
	p.ahead.mu.Unlock()

	go func() {
		slots <- struct{}{}
		defer func() { <-slots }()

		t.res = p.do(context.Background(), r)
		close(t.done)
	}()
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
// when there is one. A cancellation before it finished falls back to do,
// which reports the cancellation as it would have.
func (p *transportPool) doOrTake(ctx context.Context, r *transferRequest) *transferResult {
	if t := p.take(r); t != nil {
		select {
		case <-t.done:
			return t.res
		case <-ctx.Done():
		}
	}

	return p.do(ctx, r)
}
