// Ports tests/Composer/Test/Util/Http/CurlDownloaderTest.php (the
// Retry-After handling and the delayed retries it schedules).

package http

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// newTestCurlDownloader returns a CurlDownloader whose output goes to the
// returned buffer; ioi nil is such a buffer.
func newTestCurlDownloader(t *testing.T, ioi io.IO) (*CurlDownloader, *io.BufferIO) {
	t.Helper()

	b, err := io.NewBufferIO("", console.VerbosityNormal, nil)
	if err != nil {
		t.Fatal(err)
	}

	if ioi == nil {
		ioi = b
	}

	config := newFakeConfig(map[string]any{"github-domains": list(), "gitlab-domains": list()})

	return newCurlDownloader(ioi, config, NewStaticRuntime("8.3.0", ""), &sync.Mutex{}), b
}

func retryResponse(status int, headers ...string) *Response {
	all := append([]string{"HTTP/1.1 " + strconv.Itoa(status) + " Status"}, headers...)

	return NewResponse("https://example.org/packages.json", status, all, "")
}

func retryJob(retries int, origin string) *curlJob {
	return &curlJob{
		id:         1,
		url:        "https://" + origin + "/packages.json",
		origin:     origin,
		attributes: curlAttributes{retryAuthFailure: true, retries: retries},
		options:    php.NewArray(),
	}
}

func TestCurlDownloader_RetryAfterIsReadAsDelaySeconds(t *testing.T) {
	if delay, ok := retryAfterDelay(retryResponse(429, "Retry-After: 42")); !ok || delay != 42 {
		t.Fatalf("got %d %v", delay, ok)
	}
}

func TestCurlDownloader_RetryAfterIsReadAsHttpDate(t *testing.T) {
	delay, ok := retryAfterDelay(retryResponse(429, "Retry-After: "+time.Now().UTC().Add(120*time.Second).Format("Mon, 02 Jan 2006 15:04:05 GMT")))
	if !ok || delay <= 115 || delay > 120 {
		t.Fatalf("got %d %v", delay, ok)
	}

	// the obsolete RFC 850 format must be accepted too
	if delay, ok := retryAfterDelay(retryResponse(429, "Retry-After: Sunday, 06-Nov-94 08:49:37 GMT")); !ok || delay != 0 {
		t.Fatalf("got %d %v", delay, ok)
	}
}

func TestCurlDownloader_RetryAfterInThePastMeansNoWait(t *testing.T) {
	delay, ok := retryAfterDelay(retryResponse(429, "Retry-After: "+time.Now().UTC().Add(-120*time.Second).Format("Mon, 02 Jan 2006 15:04:05 GMT")))
	if !ok || delay != 0 {
		t.Fatalf("got %d %v", delay, ok)
	}
}

func TestCurlDownloader_MalformedRetryAfterIsIgnored(t *testing.T) {
	for _, response := range []*Response{
		retryResponse(429, "Retry-After: not a date"),
		retryResponse(429, "Retry-After: 99999999999999999999"),
		retryResponse(429, "Retry-After: "),
		retryResponse(429),
	} {
		if _, ok := retryAfterDelay(response); ok {
			t.Fatalf("%q was read", response.Headers())
		}
	}
}

func decideRetry(t *testing.T, job *curlJob, response *Response) (bool, *time.Duration) {
	t.Helper()

	c, _ := newTestCurlDownloader(t, nil)

	return c.isStatusCodeRetryNeeded(job, response)
}

func TestCurlDownloader_RateLimitedResponseIsRetriedAfterTheInterval(t *testing.T) {
	retry, delay := decideRetry(t, retryJob(0, "example.org"), retryResponse(429, "Retry-After: 5"))

	if !retry || delay == nil || *delay != 5*time.Second {
		t.Fatalf("got %v %v", retry, delay)
	}
}

func TestCurlDownloader_ZeroRetryAfterUsesTheDefaultBackoff(t *testing.T) {
	if retry, delay := decideRetry(t, retryJob(0, "example.org"), retryResponse(429, "Retry-After: 0")); !retry || delay != nil {
		t.Fatalf("got %v %v", retry, delay)
	}
}

func TestCurlDownloader_RateLimitedResponseWithoutRetryAfterIsNotRetried(t *testing.T) {
	if retry, delay := decideRetry(t, retryJob(0, "example.org"), retryResponse(429)); retry || delay != nil {
		t.Fatalf("got %v %v", retry, delay)
	}
}

func TestCurlDownloader_RetryableStatusCodeHonorsRetryAfter(t *testing.T) {
	retry, delay := decideRetry(t, retryJob(0, "example.org"), retryResponse(503, "Retry-After: 30"))

	if !retry || delay == nil || *delay != 30*time.Second {
		t.Fatalf("got %v %v", retry, delay)
	}
}

func TestCurlDownloader_RetryableStatusCodeWithoutRetryAfterUsesTheDefaultBackoff(t *testing.T) {
	for _, response := range []*Response{retryResponse(503), retryResponse(503, "Retry-After: whenever")} {
		if retry, delay := decideRetry(t, retryJob(0, "example.org"), response); !retry || delay != nil {
			t.Fatalf("got %v %v", retry, delay)
		}
	}
}

func TestCurlDownloader_RetryAfterBeyondTheCapIsNotWaitedOut(t *testing.T) {
	// 429 is only retryable because the header says when to come back, so
	// an interval we will not wait for leaves nothing to act on
	if retry, delay := decideRetry(t, retryJob(0, "example.org"), retryResponse(429, "Retry-After: 3600")); retry || delay != nil {
		t.Fatalf("got %v %v", retry, delay)
	}

	// a status code which was retryable before this header was read must
	// stay retryable, on the default backoff
	if retry, delay := decideRetry(t, retryJob(0, "example.org"), retryResponse(503, "Retry-After: 3600")); !retry || delay != nil {
		t.Fatalf("got %v %v", retry, delay)
	}
}

func TestCurlDownloader_RetryAfterDoesNotDefeatMaxRetries(t *testing.T) {
	if retry, delay := decideRetry(t, retryJob(3, "example.org"), retryResponse(429, "Retry-After: 5")); retry || delay != nil {
		t.Fatalf("got %v %v", retry, delay)
	}
}

func TestCurlDownloader_NonGetRequestsAreNotRetried(t *testing.T) {
	job := retryJob(0, "example.org")
	job.options = php.ArrayOf("http", php.ArrayOf("method", "POST"))

	if retry, delay := decideRetry(t, job, retryResponse(429, "Retry-After: 5")); retry || delay != nil {
		t.Fatalf("got %v %v", retry, delay)
	}
}

// parkRetry is restartJobWithDelay for job, with c.mu held.
func parkRetry(t *testing.T, c *CurlDownloader, job *curlJob, retryAfter *time.Duration, statusCode int) {
	t.Helper()

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.restartJobWithDelay(job, job.url, job.attributes, retryAfter, statusCode); err != nil {
		t.Fatal(err)
	}
}

func TestCurlDownloader_DelayedRetryDoesNotBlockTheDownloadLoop(t *testing.T) {
	c, _ := newTestCurlDownloader(t, nil)
	job := retryJob(0, "example.org")

	// a delay which is waited out rather than scheduled stalls every
	// transfer, not just the request which is being retried
	delay := 30 * time.Second
	start := time.Now()
	parkRetry(t, c, job, &delay, 429)
	elapsed := time.Since(start)

	if elapsed >= time.Second {
		t.Fatalf("parking took %v", elapsed)
	}

	c.mu.Lock()
	if len(c.delayed) != 1 {
		t.Fatalf("delayed %d", len(c.delayed))
	}
	c.mu.Unlock()

	c.AbortRequest(job.id)
}

func TestCurlDownloader_DelayedRetryStartsWhenDue(t *testing.T) {
	c, _ := newTestCurlDownloader(t, nil)
	sched := util.NewScheduler()

	var delivered int

	job := retryJob(0, "example.org")
	job.async = &asyncTransfers{sched: sched, deliver: func(curlEvent) { delivered++ }}

	delay := 20 * time.Millisecond
	parkRetry(t, c, job, &delay, 429)

	// not before its delay
	sched.RunReady()
	if delivered != 0 {
		t.Fatal("the retry was started straight away")
	}

	deadline := time.Now().Add(2 * time.Second)
	for delivered == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the retry was not started once it was due")
		}

		sched.RunReady()
		time.Sleep(5 * time.Millisecond)
	}
}

func TestCurlDownloader_DueRetryIsStartedWhileALaterOneKeepsWaiting(t *testing.T) {
	srv := packagesServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	c, _ := newTestCurlDownloader(t, nil)

	due := retryJob(0, "example.org")
	due.url = srv + "/due"

	later := retryJob(1, "other.org")
	later.id, later.url = 2, "https://other.org/later"

	parkRetry(t, c, later, duration(30*time.Second), 429)
	parkRetry(t, c, due, duration(time.Millisecond), 429)

	time.Sleep(5 * time.Millisecond)

	c.mu.Lock()
	c.restartDue()
	delayed := len(c.delayed)
	started := len(c.jobs)
	var startedID int
	for id := range c.jobs {
		startedID = id
	}
	c.mu.Unlock()

	if delayed != 1 {
		t.Fatalf("delayed %d, want the retry which is not due yet", delayed)
	}

	if started != 1 {
		t.Fatalf("started %d, want the due retry", started)
	}

	c.AbortRequest(startedID)
}

// duration is a pointer to d, for the retryAfter arguments.
func duration(d time.Duration) *time.Duration { return new(d) }

// retryWaitLine reports whether out holds a "waiting Ns before retrying"
// line whose origin and reason are pattern.
func retryWaitLine(out, pattern string) bool {
	return regexp.MustCompile(`<warning>` + pattern + `, waiting \d+s before retrying</warning>`).MatchString(out)
}

func TestCurlDownloader_AbortedRequestIsNotRetried(t *testing.T) {
	c, _ := newTestCurlDownloader(t, nil)
	job := retryJob(0, "example.org")

	parkRetry(t, c, job, duration(30*time.Second), 429)

	// a request is cancelled by the id of the job it was first sent on
	c.AbortRequest(job.id)

	c.mu.Lock()
	delayed := len(c.delayed)
	c.mu.Unlock()

	if delayed != 0 {
		t.Fatalf("delayed %d", delayed)
	}

	c.mu.Lock()
	c.restartDue()
	jobs := len(c.jobs)
	c.mu.Unlock()

	if jobs != 0 {
		t.Fatal("a cancelled request must not be sent again")
	}
}

func TestCurlDownloader_UnauthorizedRequestWhichWasOnlyRetriedForNetworkErrorsGetsCredentialsFromAnotherRequest(t *testing.T) {
	// e.g. it timed out while another request to the same origin prompted
	// for credentials
	fio := newFakeIO()
	fio.interactive = true
	fio.hasAuth = func(string) bool { return true }

	c, _ := newTestCurlDownloader(t, fio)
	job := retryJob(1, "example.org") // a network retry, no auth one

	c.mu.Lock()
	result, err := c.isAuthenticatedRetryNeeded(job, retryResponse(401))
	c.mu.Unlock()

	if err != nil || !result.Retry || result.StoreAuth != StoreAuthNo {
		t.Fatalf("got %v %v", result, err)
	}
}

func TestCurlDownloader_UnauthorizedRequestWhichWasAlreadyRetriedWithCredentialsFails(t *testing.T) {
	fio := newFakeIO()
	fio.interactive = true
	fio.hasAuth = func(string) bool { return true }

	c, _ := newTestCurlDownloader(t, fio)
	job := retryJob(0, "example.org")
	job.attributes.authRetries = 1

	c.mu.Lock()
	_, err := c.isAuthenticatedRetryNeeded(job, retryResponse(401))
	c.mu.Unlock()

	te, ok := errors.AsType[*util.TransportError](err)
	if !ok || !strings.Contains(te.Message, "Invalid credentials (HTTP 401)") {
		t.Fatalf("got %v", err)
	}
}

func TestCurlDownloader_RetryWaitsForTheLatestRetryAfterOfItsOrigin(t *testing.T) {
	srv := packagesServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	c, b := newTestCurlDownloader(t, nil)

	// another request to example.org was told to come back in 30s, which
	// holds for the next ones too
	first := retryJob(0, "example.org")
	first.url = srv + "/first"

	small := retryJob(1, "example.org")
	small.id, small.url = 2, srv+"/small"

	backoff := retryJob(3, "example.org")
	backoff.id, backoff.url = 3, srv+"/backoff"

	other := retryJob(0, "other.org")
	other.id = 4

	parkRetry(t, c, first, duration(30*time.Second), 429)
	parkRetry(t, c, small, duration(time.Millisecond), 429)
	parkRetry(t, c, backoff, nil, 0)
	parkRetry(t, c, other, duration(time.Millisecond), 429)

	time.Sleep(5 * time.Millisecond)

	c.mu.Lock()
	c.restartDue()
	delayed := slices_CloneDelayed(c.delayed)
	started := len(c.jobs)
	var startedID int
	for id := range c.jobs {
		startedID = id
	}
	c.announceRetryAfterWaits()
	c.mu.Unlock()

	// retries to example.org must wait for its deadline, whether or not
	// they got a Retry-After
	if len(delayed) != 3 {
		t.Fatalf("delayed %d, want 3", len(delayed))
	}

	for _, d := range delayed {
		if d.job.origin != "example.org" {
			t.Fatalf("delayed job for %s", d.job.origin)
		}
	}

	if started != 1 {
		t.Fatalf("started %d, want the retry to another origin", started)
	}

	c.AbortRequest(startedID)

	if out := php.NormalizeEOL(b.Output()); !retryWaitLine(out, `example\.org is rate limiting requests \(3 pending\)`) {
		t.Fatalf("output %q", out)
	}
}

func slices_CloneDelayed(delayed []*delayedCurlJob) []*delayedCurlJob {
	out := make([]*delayedCurlJob, len(delayed))
	copy(out, delayed)

	return out
}

func TestCurlDownloader_FirstRetryToABlockedOriginIsNotStartedStraightAway(t *testing.T) {
	c, _ := newTestCurlDownloader(t, nil)

	first := retryJob(0, "example.org")
	parkRetry(t, c, first, duration(30*time.Second), 429)

	// the first retry has no backoff of its own
	second := retryJob(0, "example.org")
	second.id = 2
	parkRetry(t, c, second, nil, 0)

	c.mu.Lock()
	delayed, jobs := len(c.delayed), len(c.jobs)
	c.mu.Unlock()

	if delayed != 2 || jobs != 0 {
		t.Fatalf("delayed %d, started %d", delayed, jobs)
	}
}

func TestCurlDownloader_RetryAfterWaitIsAnnouncedOncePerOrigin(t *testing.T) {
	c, b := newTestCurlDownloader(t, nil)

	parkRetry(t, c, retryJob(0, "example.org"), duration(10*time.Second), 429)

	second := retryJob(0, "example.org")
	second.id = 2
	parkRetry(t, c, second, duration(30*time.Second), 429)

	third := retryJob(0, "other.org")
	third.id = 3
	parkRetry(t, c, third, duration(30*time.Second), 429)

	c.mu.Lock()
	c.announceRetryAfterWaits()
	c.announceRetryAfterWaits()
	c.mu.Unlock()

	want := `<warning>example.org is rate limiting requests \(2 pending\), waiting \d+s before retrying</warning>
` +
		`<warning>other.org is rate limiting requests \(1 pending\), waiting \d+s before retrying</warning>
`

	if got := php.NormalizeEOL(b.Output()); !regexp.MustCompile(`^` + want + `$`).MatchString(got) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCurlDownloader_RetryAfterWaitIsAnnouncedWhileRequestsToTheOriginAreStillInFlight(t *testing.T) {
	c, b := newTestCurlDownloader(t, nil)

	// a long transfer to the origin can outlast the whole wait, so the
	// notice must not wait for it
	parkRetry(t, c, retryJob(0, "example.org"), duration(30*time.Second), 429)

	c.mu.Lock()
	c.jobs[99] = &curlJob{id: 99, origin: "example.org"}
	c.announceRetryAfterWaits()
	c.mu.Unlock()

	c.mu.Lock()
	delete(c.jobs, 99)
	c.mu.Unlock()

	if got := php.NormalizeEOL(b.Output()); got != "<warning>example.org is rate limiting requests (1 pending), waiting 30s before retrying</warning>\n" {
		t.Fatalf("got %q", got)
	}
}

func TestCurlDownloader_RetryAfterWaitIsAnnouncedAgainOnceTheOriginTurnsRequestsAwayAnew(t *testing.T) {
	c, b := newTestCurlDownloader(t, nil)

	parkRetry(t, c, retryJob(0, "example.org"), duration(time.Second), 503)

	c.mu.Lock()
	c.announceRetryAfterWaits()

	// the wait runs out and the retry is started, which ends the episode
	for _, d := range c.delayed {
		d.at = time.Now().Add(-time.Second)
	}
	for _, state := range c.retryAfterOrigins {
		state.until = time.Now().Add(-time.Second)
	}
	c.restartDue()
	c.announceRetryAfterWaits()
	c.mu.Unlock()

	for id := range c.jobs {
		c.AbortRequest(id)
	}

	parkRetry(t, c, retryJob(1, "example.org"), duration(5*time.Second), 429)

	c.mu.Lock()
	c.announceRetryAfterWaits()
	c.mu.Unlock()

	want := `<warning>example.org responded with status code 503 \(1 pending\), waiting \d+s before retrying</warning>
` +
		`<warning>example.org is rate limiting requests \(1 pending\), waiting \d+s before retrying</warning>
`

	if got := php.NormalizeEOL(b.Output()); !regexp.MustCompile(`^` + want + `$`).MatchString(got) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestCurlDownloader_DefaultBackoffIsNotAnnounced(t *testing.T) {
	c, b := newTestCurlDownloader(t, nil)

	parkRetry(t, c, retryJob(3, "example.org"), nil, 0)

	c.mu.Lock()
	delayed := len(c.delayed)
	c.announceRetryAfterWaits()
	c.mu.Unlock()

	if delayed != 1 || php.NormalizeEOL(b.Output()) != "" {
		t.Fatalf("delayed %d, output %q", delayed, php.NormalizeEOL(b.Output()))
	}
}

func TestCurlDownloader_JsonResponseIsRecognisedWithCharset(t *testing.T) {
	for _, tc := range []struct {
		headers []string
		want    bool
	}{
		{[]string{"Content-Type: application/json"}, true},
		{[]string{"Content-Type: application/json; charset=utf-8"}, true},
		{[]string{"Content-Type: Application/JSON; charset=UTF-8"}, true},
		{[]string{"Content-Type: text/html"}, false},
		{nil, false},
	} {
		got, err := isJSONResponse(retryResponse(429, tc.headers...))
		if err != nil || got != tc.want {
			t.Errorf("%q: got %v %v", tc.headers, got, err)
		}
	}
}

func TestCurlDownloader_FailureReportsARetryAfterWhichWasNotWaitedOut(t *testing.T) {
	if got := statusFailureMessage(retryResponse(429, "Retry-After: 3600")); got != "HTTP/1.1 429 Status, the server asked to retry in 3600s which is longer than the 60s Composer is willing to wait, try again later" {
		t.Fatalf("got %q", got)
	}

	// an interval which was waited out, or no interval at all, has nothing
	// to add
	for _, response := range []*Response{retryResponse(429, "Retry-After: 5"), retryResponse(429)} {
		if got := statusFailureMessage(response); got != "HTTP/1.1 429 Status" {
			t.Fatalf("got %q", got)
		}
	}
}

// packagesServer serves handler at every path and returns its URL.
func packagesServer(t *testing.T, handler http.HandlerFunc) string {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return srv.URL
}
