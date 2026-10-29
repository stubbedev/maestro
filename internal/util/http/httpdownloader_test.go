// Ports tests/Composer/Test/Util/HttpDownloaderTest.php (the requests a
// parked retry frees its slot for and the origin holds).

package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

func TestHttpDownloader_ParkedRetriesDoNotHoldUpQueuedRequests(t *testing.T) {
	h, _ := newTestDownloader(t, nil, "")
	h.maxJobs = 1

	other := &httpJob{url: "https://other.org/b", origin: "other.org"}

	h.mu.Lock()
	h.runningJobs = 1
	if h.canStart(other) {
		h.mu.Unlock()
		t.Fatal("a request was started over the slot limit")
	}

	// a retry waiting for its Retry-After holds no transfer open, so its
	// slot is free for another request meanwhile
	h.curl.delayed = append(h.curl.delayed, &delayedCurlJob{job: &curlJob{id: 1, origin: "example.org"}, at: time.Now().Add(time.Minute)})
	started := h.canStart(other)
	h.mu.Unlock()

	if !started {
		t.Fatal("the parked retry kept its slot")
	}
}

func TestHttpDownloader_NoRequestIsStartedToAnOriginWhichAskedToBeLeftAlone(t *testing.T) {
	h, _ := newTestDownloader(t, nil, "")

	example := &httpJob{url: "https://example.org/a", origin: "example.org"}
	other := &httpJob{url: "https://other.org/b", origin: "other.org"}

	h.mu.Lock()
	// the origin asked to be left alone, so only the request to the other
	// one is sent
	h.curl.retryAfterOrigins = append(h.curl.retryAfterOrigins, &retryAfterOrigin{origin: "example.org", statusCode: 429, until: time.Now().Add(time.Minute)})

	if h.canStart(example) {
		h.mu.Unlock()
		t.Fatal("a request was started to an origin which asked to be left alone")
	}

	if !h.canStart(other) {
		h.mu.Unlock()
		t.Fatal("a request to another origin was held back")
	}

	// once the hold has run out the queued request is sent
	h.curl.retryAfterOrigins[0].until = time.Now().Add(-time.Second)
	started := h.canStart(example)
	h.mu.Unlock()

	if !started {
		t.Fatal("the queued request was not sent once the hold ran out")
	}
}

// retryAfterServer returns a server which answers the first request with
// status (and Retry-After: interval) and every later one with body.
func retryAfterServer(t *testing.T, status int, interval, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", interval)
			w.WriteHeader(status)

			return
		}

		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return srv, &calls
}

func TestHttpDownloader_RateLimitedResponseIsRetriedAfterTheInterval(t *testing.T) {
	srv, calls := retryAfterServer(t, http.StatusTooManyRequests, "1", "ok")
	h, b := newTestDownloader(t, nil, "")

	start := time.Now()

	r, err := h.Get(srv.URL, nil)
	if err != nil || r.Body() != "ok" {
		t.Fatalf("got %v %v", r, err)
	}

	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("the retry was not waited out: %v", elapsed)
	}

	if calls.Load() != 2 {
		t.Fatalf("%d requests", calls.Load())
	}

	out := php.NormalizeEOL(b.Output())
	if !strings.Contains(out, "Retrying (1) "+srv.URL+" due to status code 429 in 1s as requested by Retry-After\n") {
		t.Fatalf("output %q", out)
	}

	if !retryWaitLine(out, `127\.0\.0\.1:\d+ is rate limiting requests \(1 pending\)`) {
		t.Fatalf("output %q", out)
	}
}

func TestHttpDownloader_AsyncRateLimitedResponseIsRetriedAfterTheInterval(t *testing.T) {
	srv, calls := retryAfterServer(t, http.StatusTooManyRequests, "1", "ok")
	h, _ := newTestDownloader(t, nil, "")
	loop := NewLoop(h, nil)

	promise, err := h.Add(srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()

	if err := loop.WaitProgress([]Waitable{promise}, nil, nil); err != nil {
		t.Fatal(err)
	}

	if elapsed := time.Since(start); elapsed < time.Second {
		t.Fatalf("the retry was not waited out: %v", elapsed)
	}

	r, err := promise.Wait()
	if err != nil || r.Body() != "ok" || calls.Load() != 2 {
		t.Fatalf("got %v %v after %d requests", r, err, calls.Load())
	}
}

func TestHttpDownloader_RetryAfterBeyondTheCapFailsWithTheInterval(t *testing.T) {
	srv, calls := retryAfterServer(t, http.StatusTooManyRequests, "3600", "ok")
	h, b := newTestDownloader(t, nil, "")

	_, err := h.Get(srv.URL, nil)

	te, ok := err.(*util.TransportError) //nolint:errorlint // thrown directly
	if !ok || !strings.Contains(te.Message, ", the server asked to retry in 3600s which is longer than the 60s Composer is willing to wait, try again later") {
		t.Fatalf("got %v", err)
	}

	if calls.Load() != 1 {
		t.Fatalf("%d requests", calls.Load())
	}

	if strings.Contains(php.NormalizeEOL(b.Output()), "Retrying") {
		t.Fatalf("output %q", php.NormalizeEOL(b.Output()))
	}
}
