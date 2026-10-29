// Ports src/Composer/Util/Http/CurlDownloader.php on net/http: curl_multi's
// parallel transfers are goroutines running transport.go's transfers; their
// results are processed on one goroutine, as curl_multi_info_read results
// are in PHP, so retries, redirects, authentication prompts and promise
// resolution happen there: the goroutine that ticks, or, for HttpDownloader's
// asynchronous requests, the one driving its scheduler, in the order the
// transfers started.

package http

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// curlAttributes are a job's retry state (initDownload's $attributes).
type curlAttributes struct {
	retryAuthFailure bool
	redirects        int
	retries          int
	// authRetries counts the authentication retries only: network ones
	// do not count, so another request's credentials are still picked up
	// by a request which was only retried for a network error (see
	// AuthHelper.PromptAuthIfNeeded).
	authRetries int
	storeAuth   StoreAuth
	// ipResolve is 4 or 6 to force the IP version, 0 for null.
	ipResolve int
}

type curlJob struct {
	id         int
	url        string
	origin     string
	attributes curlAttributes
	// options are the request options as given (restarts start over from
	// them).
	options  *php.Array
	filename string
	hasFile  bool
	bodyFile *os.File
	resolve  func(*Response)
	reject   func(error)
	cancel   context.CancelFunc
	// async, when set, routes the ends of the job's transfers through a
	// scheduler instead of the tick queue.
	async *asyncTransfers
}

// asyncTransfers routes the end of each transfer of a job to deliver, run
// by sched in the order the transfers started (each takes a ticket).
type asyncTransfers struct {
	sched   *util.Scheduler
	deliver func(curlEvent)
}

type curlEvent struct {
	id     int
	job    *curlJob
	result *transferResult
	// due asks for the delayed retries which came due to be started, see
	// restartJobWithDelay.
	due bool
}

// delayedCurlJob is a retry waiting for its delay: it holds no transfer
// open, so its slot is free for another request meanwhile (see
// HttpDownloader.canStart).
type delayedCurlJob struct {
	job        *curlJob
	url        string
	attributes curlAttributes
	at         time.Time
}

// retryAfterOrigin is an origin which asked for requests to be retried
// later: until is the latest point in time it asked to be left alone for,
// see restartJobWithDelay.
type retryAfterOrigin struct {
	origin     string
	statusCode int
	announced  bool
	until      time.Time
}

// processTransports are the transports, and their connections, of the
// CurlDownloaders of the process: a Composer instance created after
// another (require's and create-project's second one) finds the first's
// connections open instead of opening its own (deliberate deviation 3;
// curl keeps a connection cache per multi handle). What goes over them
// is unchanged: requests are made, and answered, by each downloader.
var processTransports = &transportPool{}

// CurlDownloader ports Composer\Util\Http\CurlDownloader.
type CurlDownloader struct {
	io         io.IO
	config     Config
	authHelper *AuthHelper
	rt         Runtime
	pool       *transportPool
	// ahead are the transfers started ahead (prefetch.go).
	ahead prefetches

	// mu guards jobs; HttpDownloader shares its own lock.
	mu     *sync.Mutex
	jobs   map[int]*curlJob
	nextID int

	// delayed are the retries waiting for their delay, in the order they
	// were parked, see restartJobWithDelay.
	delayed []*delayedCurlJob
	// retryAfterOrigins are the origins which asked for requests to be
	// retried later, in the order they first did.
	retryAfterOrigins []*retryAfterOrigin
	// distrustedHosts are the hosts which presented a distrusted TLS
	// chain, keyed by host:port (TlsChainHostKey).
	distrustedHosts map[string]string

	queueMu sync.Mutex
	queue   []curlEvent
	ready   chan struct{}

	selectTimeout time.Duration
	maxRedirects  int
	maxRetries    int
}

func newCurlDownloader(ioi io.IO, config Config, rt Runtime, mu *sync.Mutex) *CurlDownloader {
	if rt == nil {
		rt = defaultRuntime
	}

	authHelper := NewAuthHelper(ioi, config)
	authHelper.rt = rt

	return &CurlDownloader{
		io:            ioi,
		config:        config,
		authHelper:    authHelper,
		rt:            rt,
		pool:          processTransports,
		mu:            mu,
		jobs:          map[int]*curlJob{},
		ready:         make(chan struct{}, 1),
		selectTimeout: 5 * time.Second,
		maxRedirects:  20,
		maxRetries:    3,
	}
}

// Download is download($resolve, $reject, $origin, $url, $options,
// $copyTo): it starts the transfer and returns its id. copyTo "" is null.
// resolve and reject are called from Tick.
func (c *CurlDownloader) Download(resolve func(*Response), reject func(error), origin, url string, options *php.Array, copyTo string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.download(resolve, reject, origin, url, options, copyTo, nil)
}

// download is Download with c.mu held; async (nil for the tick queue)
// routes the ends of the transfers.
func (c *CurlDownloader) download(resolve func(*Response), reject func(error), origin, url string, options *php.Array, copyTo string, async *asyncTransfers) (int, error) {
	attributes := curlAttributes{retryAuthFailure: true}

	if options == nil {
		options = php.NewArray()
	} else if v, ok := options.Get("retry-auth-failure"); ok {
		attributes.retryAuthFailure = php.ToBool(v)
		options = options.Clone()
		options.Delete("retry-auth-failure")
	}

	c.nextID++
	job := &curlJob{id: c.nextID, filename: copyTo, hasFile: copyTo != "", resolve: resolve, reject: reject, async: async}

	return job.id, c.initDownload(job, origin, url, options, attributes)
}

// Infallible: the pattern does bounded work per start position, so Preg
// cannot throw on it and its call sites ignore the error.
var insecurePackagistRegex = php.MustCompile(`{^http://(repo\.)?packagist\.org/p/}`)

// initDownload is initDownload: it checks the URL may be accessed, builds
// the request and starts it. c.mu is held.
func (c *CurlDownloader) initDownload(job *curlJob, origin, url string, options *php.Array, attributes curlAttributes) error {
	if attributes.ipResolve == 0 {
		switch v, _ := util.GetEnv("COMPOSER_IPRESOLVE"); v {
		case "4":
			attributes.ipResolve = 4
		case "6":
			attributes.ipResolve = 6
		}
	}

	originalOptions := options

	// check URL can be accessed (i.e. is not insecure), but allow insecure
	// Packagist calls to $hashed providers as file integrity is verified
	// with sha256
	if ok, _ := insecurePackagistRegex.IsMatch(url); !ok || (!strings.Contains(url, "$") && !strings.Contains(url, "%24")) {
		if err := c.config.ProhibitURLByConfig(url, c.io, options); err != nil {
			return err
		}
	}

	if v, ok := path(options, "prevent_url_access_callable"); ok {
		if prevent, ok := callableOption(v); ok && prevent(url) {
			return util.NewTransportError(`Access to "`+util.SanitizeURL(url)+`" is blocked.`, 400)
		}
	}

	if hostKey, ok := TlsChainHostKey(url); ok {
		if certName, distrusted := c.distrustedHosts[hostKey]; distrusted {
			return NewTlsChainError(url, certName)
		}
	}

	var bodyFile *os.File

	if job.hasFile {
		f, err := os.OpenFile(job.filename+"~", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666) //nolint:gosec // fopen(w+b) mode, umask applies
		if err != nil {
			return util.NewTransportError(`The "`+util.SanitizeURL(url)+`" file could not be written to `+job.filename+": Failed to open stream: "+php.Strerror(err), 400)
		}

		bodyFile = f
	}

	req, proxy, options, err := c.buildTransfer(origin, url, options, attributes, bodyFile)
	if err != nil {
		closeFile(bodyFile)

		return err
	}

	job.url, job.origin, job.attributes, job.options, job.bodyFile = url, origin, attributes, originalOptions, bodyFile

	ctx, cancel := context.WithCancel(context.Background())
	job.cancel = cancel
	c.jobs[job.id] = job

	usingProxy, _ := proxy.StatusFormat(" using proxy (%s)")

	ifModified := ""
	if php.Stripos(strings.Join(headerList(options), ","), "if-modified-since:") >= 0 {
		ifModified = " if modified"
	}

	if attributes.redirects == 0 && attributes.retries == 0 {
		c.io.WriteError("Downloading "+util.SanitizeURL(url)+usingProxy+ifModified, true, io.Debug)
	}

	post := c.post
	if a := job.async; a != nil {
		ticket := a.sched.Ticket()
		post = func(ev curlEvent) { ticket.Complete(func() { a.deliver(ev) }) }
	}

	go func() {
		result := c.ahead.doOrTake(ctx, c.pool, req)
		post(curlEvent{id: job.id, job: job, result: result})
	}()

	return nil
}

// buildTransfer is initDownload's preparation of the request from the
// options (Connection header, proxy, authentication, stream context): the
// transfer, the proxy and the final options.
func (c *CurlDownloader) buildTransfer(origin, url string, options *php.Array, attributes curlAttributes, bodyFile *os.File) (*transferRequest, *RequestProxy, *php.Array, error) {
	options = options.Clone()
	httpOptions := HTTPOptions(options)

	headers := php.NewArray()

	if h, ok := httpOptions.Get("header"); ok && h != nil {
		if a, ok := h.(*php.Array); ok {
			headers = php.ArrayDiff(a, php.ListOf("Connection: close"))
		} else {
			headers.Append(h)
		}
	}

	headers.Append("Connection: keep-alive")
	httpOptions.Set("header", headers)

	proxy, err := GetProxyManager().ProxyForRequest(url)
	if err != nil {
		return nil, nil, nil, err
	}

	options = c.authHelper.addAuthenticationOptions(options, origin, url)

	if options, err = initOptions(url, options, true, c.rt); err != nil {
		return nil, nil, nil, err
	}

	ssl, _ := options.At("ssl").(*php.Array)

	return c.buildRequest(url, options, ssl, proxy, attributes, bodyFile), proxy, options, nil
}

// prefetch starts, without output, the transfer initDownload would make
// for url and options (deliberate deviation 3): a later download of the
// same request takes its result instead of waiting for the network
// (transportPool.doOrTake). Requests whose preparation could print or
// prompt (stored credentials, an insecure URL) or that a callback vets are
// left alone. spool writes the body to an unlinked temporary file, for a
// download into a file (prefetchcopy.go). c.mu is held.
func (c *CurlDownloader) prefetch(origin, url string, options *php.Array, urgent, spool bool) *prefetchedTransfer {
	if _, found := FindAuthOrigin(c.io, origin); found {
		return nil
	}
	if _, ok := path(options, "prevent_url_access_callable"); ok {
		return nil
	}
	if ssl, _ := options.At("ssl").(*php.Array); !caChecked(ssl) {
		return nil
	}
	if ok, _ := insecurePackagistRegex.IsMatch(url); !ok || (!strings.Contains(url, "$") && !strings.Contains(url, "%24")) {
		if c.config.ProhibitURLByConfig(url, io.NewNullIO(), options) != nil {
			return nil
		}
	}

	attributes := curlAttributes{retryAuthFailure: true}
	switch v, _ := util.GetEnv("COMPOSER_IPRESOLVE"); v {
	case "4":
		attributes.ipResolve = 4
	case "6":
		attributes.ipResolve = 6
	}
	if options.Has("retry-auth-failure") {
		options = options.Clone()
		options.Delete("retry-auth-failure")
	}

	var spoolFile *os.File
	if spool {
		if spoolFile = newSpool(); spoolFile == nil {
			return nil
		}
	}

	req, _, _, err := c.buildTransfer(origin, url, options, attributes, spoolFile)
	if err != nil {
		closeFile(spoolFile)

		return nil
	}

	t := c.ahead.prefetch(c.pool, req, urgent)
	if t == nil || t.r != req {
		// an identical transfer was waiting already
		closeFile(spoolFile)
	}

	return t
}

// buildRequest maps the request options onto the transfer the way
// CurlDownloader maps them onto curl options.
func (c *CurlDownloader) buildRequest(url string, options, ssl *php.Array, proxy *RequestProxy, attributes curlAttributes, bodyFile *os.File) *transferRequest {
	req := &transferRequest{
		url:             url,
		headers:         headerList(options),
		connectTimeout:  10 * time.Second,
		timeout:         300 * time.Second, // max(default_socket_timeout, 300)
		decode:          true,
		curlStatusLines: true,
		body:            bodyFile,
		safeURL:         util.SanitizeURL(url),
	}

	if v, ok := optionString(options, "http", "method"); ok {
		req.method = v
	}

	if v, ok := path(options, "http", "content"); ok {
		content := php.ToString(v)
		req.content = &content
	}

	if v, ok := path(options, "http", "timeout"); ok {
		req.timeout = time.Duration(php.ToInt(v)) * time.Second
	}

	if v, ok := path(options, "max_file_size"); ok {
		req.maxFileSize = php.ToInt(v)
	}

	if v, ok := path(options, "prevent_ip_access_callable"); ok {
		if prevent, ok := callableOption(v); ok {
			req.preventIP = prevent
		}
	}

	req.key = transportKey{tls: tlsFromOptions(ssl, false), ipResolve: attributes.ipResolve, fresh: attributes.retries > 0}

	proxyOptions := proxy.CurlOptions(ssl)
	req.key.proxy, req.key.proxyAuth = proxyOptions.Proxy, proxyOptions.UserPwd

	return req
}

// tlsFromOptions reads the ssl options curl is given (CurlDownloader's
// $options['ssl'] map: cafile, capath, verify_peer, verify_peer_name,
// local_cert, local_pk, passphrase), and for PHP's stream wrapper (stream)
// also ciphers and verify_depth, which CurlDownloader does not pass on.
func tlsFromOptions(ssl *php.Array, stream bool) tlsSettings {
	s := tlsSettings{verifyPeer: true, verifyPeerName: true, stream: stream}

	if ssl == nil {
		return s
	}

	s.cafile, _ = optionString(ssl, "cafile")
	s.capath, _ = optionString(ssl, "capath")
	s.localCert, _ = optionString(ssl, "local_cert")
	s.localPK, _ = optionString(ssl, "local_pk")
	s.passphrase, _ = optionString(ssl, "passphrase")

	if v, ok := path(ssl, "verify_peer"); ok {
		s.verifyPeer = php.ToBool(v)
	}

	if v, ok := path(ssl, "verify_peer_name"); ok {
		s.verifyPeerName = php.ToBool(v)
	}

	if v, ok := path(ssl, "allow_self_signed"); ok {
		s.allowSelfSigned = php.ToBool(v)
	}

	if stream {
		if v, ok := path(ssl, "ciphers"); ok {
			s.ciphers, s.hasCiphers = php.ToString(v), true
		}

		if v, ok := path(ssl, "verify_depth"); ok {
			s.verifyDepth, s.hasVerifyDepth = php.ToInt(v), true
		}
	}

	return s
}

func closeFile(f *os.File) {
	if f != nil {
		_ = f.Close()
	}
}

// post queues a finished transfer for the next tick.
func (c *CurlDownloader) post(ev curlEvent) {
	c.queueMu.Lock()
	c.queue = append(c.queue, ev)
	c.queueMu.Unlock()

	select {
	case c.ready <- struct{}{}:
	default:
	}
}

func (c *CurlDownloader) takeEvents() []curlEvent {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()

	events := c.queue
	c.queue = nil

	return events
}

// AbortRequest is abortRequest($id).
func (c *CurlDownloader) AbortRequest(id int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.abortRequest(id)
}

func (c *CurlDownloader) abortRequest(id int) {
	// a retry waiting for its delay is not in flight but must not be
	// started either
	for _, d := range c.delayed {
		if d.job.id == id && d.job.hasFile {
			_ = os.Remove(d.job.filename + "~")
		}
	}

	c.delayed = slices.DeleteFunc(c.delayed, func(d *delayedCurlJob) bool { return d.job.id == id })

	job, ok := c.jobs[id]
	if !ok {
		return
	}

	job.cancel()
	closeFile(job.bodyFile)

	if job.hasFile {
		_ = os.Remove(job.filename + "~")
	}

	delete(c.jobs, id)
}

// countDelayedJobs is countDelayedJobs(): the number of retries waiting
// for their delay, which hold no transfer open. c.mu is held.
func (c *CurlDownloader) countDelayedJobs() int {
	return len(c.delayed)
}

// originOnHold is isOriginOnHold($origin): whether the origin asked for
// requests to be held back until later. c.mu is held.
func (c *CurlDownloader) originOnHold(origin string) bool {
	state := c.retryAfterOrigin(origin)

	return state != nil && state.until.After(time.Now())
}

// retryAfterOrigin is the hold of origin, nil for none. c.mu is held.
func (c *CurlDownloader) retryAfterOrigin(origin string) *retryAfterOrigin {
	for _, state := range c.retryAfterOrigins {
		if state.origin == origin {
			return state
		}
	}

	return nil
}

// dueAt is when the delayed retry may be started: A Retry-After speaks for
// the whole origin, so a retry also waits for a later hold the origin asked
// for. c.mu is held.
func (c *CurlDownloader) dueAt(d *delayedCurlJob) time.Time {
	due := d.at

	if state := c.retryAfterOrigin(d.job.origin); state != nil && state.until.After(due) {
		due = state.until
	}

	return due
}

// Tick is tick(): it waits up to the select timeout for transfers to
// finish and processes those that did.
func (c *CurlDownloader) Tick() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tick(c.selectTimeout)
}

// tick runs with c.mu held; the lock is released while waiting up to wait
// for a transfer of the tick queue, or for a delayed retry, to come due.
func (c *CurlDownloader) tick(wait time.Duration) {
	if len(c.jobs) == 0 && len(c.delayed) == 0 {
		return
	}

	events := c.takeEvents()

	if len(events) == 0 && wait > 0 {
		timer := time.NewTimer(wait)

		c.mu.Unlock()
		select {
		case <-c.ready:
		case <-timer.C:
		}
		c.mu.Lock()
		timer.Stop()

		events = c.takeEvents()
	}

	for _, ev := range events {
		c.process(ev)
	}
}

// process handles the end of a transfer, unless the job was aborted or
// restarted since, and the wake-up of the delayed retries which came due;
// c.mu is held.
func (c *CurlDownloader) process(ev curlEvent) {
	if ev.due {
		c.restartDue()
	} else if job, ok := c.jobs[ev.id]; ok && job == ev.job {
		delete(c.jobs, ev.id)
		c.complete(job, ev.result)
	}

	c.announceRetryAfterWaits()
}

// retryCurlErrors are the curl errors a GET is retried on.
var retryCurlErrors = []int{curleCouldntConnect, curleHTTP2, curleHTTP2Stream, curleCouldntResolveHost, curleOperationTimedout}

// complete is the body of tick()'s curl_multi_info_read loop for one
// finished transfer.
func (c *CurlDownloader) complete(job *curlJob, result *transferResult) {
	info := result.info

	if result.distrustedCert != "" {
		// certinfo is only filled for the request which opened the
		// connection, so the verdict is kept for the host
		if hostKey, ok := TlsChainHostKey(job.url); ok {
			if c.distrustedHosts == nil {
				c.distrustedHosts = map[string]string{}
			}

			c.distrustedHosts[hostKey] = result.distrustedCert
		}
	}

	if result.err != nil {
		c.rejectJob(job, result.err)

		return
	}

	var (
		headers  []string
		status   int
		response *Response
	)

	err := func() error {
		if result.fail.Errno != 0 {
			errno := result.fail.Errno
			isGet := isGetRequest(job.options)

			if isGet && (slices.Contains(retryCurlErrors, errno) ||
				((errno == curleRecvError || errno == curleSSLConnectError) && result.fail.Reset)) &&
				job.attributes.retries < c.maxRetries {
				attributes := job.attributes
				attributes.retries++

				if errno == curleCouldntConnect && job.attributes.ipResolve == 0 {
					// retry forcing IPv4 if no IP stack was selected
					attributes.ipResolve = 4
				}

				c.io.WriteError("Retrying ("+strconv.Itoa(attributes.retries)+") "+util.SanitizeURL(job.url)+" due to curl error "+strconv.Itoa(errno), true, io.Debug)

				return c.restartJobWithDelay(job, job.url, attributes, nil, 0)
			}

			if errno == curleSendError {
				attributes := job.attributes
				attributes.retries++
				c.io.WriteError("Retrying ("+strconv.Itoa(attributes.retries)+") "+util.SanitizeURL(job.url)+" due to curl error "+strconv.Itoa(errno), true, io.Debug)

				return c.restartJobWithDelay(job, job.url, attributes, nil, 0)
			}

			e := util.NewTransportError("curl error "+strconv.Itoa(errno)+" while downloading "+util.SanitizeURL(info.URL)+": "+result.fail.Message, 400)
			e.Curl = &result.fail

			return e
		}

		status = result.status
		headers = result.headers

		if job.hasFile {
			contents := job.filename + "~"

			if status >= 300 {
				data, _ := os.ReadFile(job.filename + "~")
				contents = string(data)
			}

			response = &Response{url: job.url, code: status, headers: headers, body: contents, Info: &info}
			c.io.WriteError("["+strconv.Itoa(status)+"] "+util.SanitizeURL(job.url), true, io.Debug)
		} else {
			contents := result.body

			if v, ok := path(job.options, "max_file_size"); ok {
				maxFileSize := php.ToInt(v)
				if int64(len(contents)) > maxFileSize {
					contents = contents[:max(0, maxFileSize)]
				}

				// Gzipped responses with missing Content-Length header cannot
				// be detected during the file download because the downloaded
				// size counts the gzipped bytes, not the actual file size.
				if int64(len(contents)) >= maxFileSize {
					return util.NewMaxFileSizeExceededError("Maximum allowed download size reached. Downloaded " + strconv.Itoa(len(contents)) + " of allowed " + strconv.FormatInt(maxFileSize, 10) + " bytes for " + util.SanitizeURL(job.url))
				}
			}

			response = &Response{url: job.url, code: status, headers: headers, body: string(contents), Info: &info}
			c.io.WriteError("["+strconv.Itoa(status)+"] "+util.SanitizeURL(job.url), true, io.Debug)
		}

		closeFile(job.bodyFile)

		warningsOutput := false

		if status >= 300 {
			isJSON, err := isJSONResponse(response)
			if err != nil {
				return err
			}

			if isJSON {
				data, _ := php.JSONDecode(response.Body(), true)

				if warningsOutput, err = OutputResponseWarnings(c.io, job.origin, data); err != nil {
					return err
				}
			}
		}

		retry, err := c.isAuthenticatedRetryNeeded(job, response)
		if err != nil {
			return err
		}

		if retry.Retry {
			attributes := job.attributes
			attributes.storeAuth = retry.StoreAuth
			attributes.retries++
			attributes.authRetries++

			return c.restartJob(job, job.url, attributes)
		}

		// handle 3xx redirects, 304 Not Modified is excluded
		if status >= 300 && status <= 399 && status != 304 && job.attributes.redirects < c.maxRedirects {
			location, err := c.handleRedirect(job, response)
			if err != nil {
				return err
			}

			if location != "" {
				attributes := job.attributes
				attributes.redirects++

				return c.restartJob(job, location, attributes)
			}
		}

		// fail 4xx and 5xx responses and capture the response
		if status >= 400 && status <= 599 {
			retry, delay := c.isStatusCodeRetryNeeded(job, response)
			if retry {
				c.io.WriteError("Retrying ("+strconv.Itoa(job.attributes.retries+1)+") "+util.SanitizeURL(job.url)+" due to status code "+strconv.Itoa(status)+retryAfterSuffix(delay), true, io.Debug)

				attributes := job.attributes
				attributes.retries++

				return c.restartJobWithDelay(job, job.url, attributes, delay, status)
			}

			statusMessage := statusFailureMessage(response)

			return c.failResponse(job, response, statusMessage, warningsOutput)
		}

		if job.attributes.storeAuth != StoreAuthNo {
			if err := c.authHelper.StoreAuth(job.origin, job.attributes.storeAuth); err != nil {
				return err
			}
		}

		// resolve promise
		if job.hasFile {
			if err := os.Rename(job.filename+"~", job.filename); err != nil {
				return &util.ErrorException{Message: "rename(" + job.filename + "~," + job.filename + "): " + php.Strerror(err)}
			}
		}

		job.resolve(response)

		return nil
	}()
	if err != nil {
		if te, ok := errors.AsType[*util.TransportError](err); ok {
			if headers != nil {
				te.Headers = headers
				te.StatusCode = status
			}

			if response != nil {
				te.SetResponse(response.Body())
			}

			te.ResponseInfo = &info
		}

		c.rejectJob(job, err)
	}
}

// isGetRequest is !isset($options['http']['method']) || method === 'GET'.
func isGetRequest(options *php.Array) bool {
	method, ok := path(options, "http", "method")

	return !ok || method == "GET"
}

var (
	// Infallible (anchored, fixed-length): call sites ignore the error.
	textHTMLRegex     = php.MustCompile(`{^text/html\b}i`)
	relativePathRegex = php.MustCompile(`{^(.+/)[^/?]*(?:\?.*)?$}`)
)

// handleRedirect is handleRedirect(): the absolute URL a 3xx response
// points to.
func (c *CurlDownloader) handleRedirect(job *curlJob, response *Response) (string, error) {
	targetURL, err := redirectTarget(job.url, response)
	if err != nil {
		return "", err
	}

	if php.Truthy(targetURL) {
		if !util.IsAllowedRedirect(targetURL) {
			return "", util.NewTransportError(`Could not follow the redirect to "`+util.SanitizeURL(targetURL)+`" because only http and https redirects are supported.`, 400)
		}

		c.io.WriteError("Following redirect ("+strconv.Itoa(job.attributes.redirects+1)+") "+util.SanitizeURL(targetURL), true, io.Debug)

		return targetURL, nil
	}

	statusMessage, _ := response.StatusMessage()

	return "", util.NewTransportError(`The "`+util.SanitizeURL(job.url)+`" file could not be downloaded, got redirect without Location (`+statusMessage+`)`, 400)
}

// redirectTarget resolves the Location header of response against url as
// Composer does; "" when there is none.
func redirectTarget(url string, response *Response) (string, error) {
	locationHeader, ok, err := response.HeaderChecked("location")
	if err != nil || !ok || !php.Truthy(locationHeader) {
		return "", err
	}

	switch {
	case util.URLScheme(locationHeader) != "":
		// Absolute URL; e.g. https://example.com/composer
		return locationHeader, nil
	case util.URLHost(locationHeader) != "":
		// Scheme relative; e.g. //example.com/foo
		return util.URLScheme(url) + ":" + locationHeader, nil
	case locationHeader[0] == '/':
		// Absolute path; e.g. /foo
		urlHost := util.URLHost(url)

		// Replace path using hostname as an anchor.
		re, err := php.Compile(`{^(.+(?://|@)` + php.PregQuote(urlHost, "") + `(?::\d+)?)(?:[/\?].*)?$}`)
		if err != nil {
			return "", err
		}

		target, _, err := re.Replace(url, `\1`+locationHeader, -1)

		return target, err
	}

	// Relative path; e.g. foo
	// This actually differs from PHP which seems to add duplicate slashes.
	target, _, err := relativePathRegex.Replace(url, `\1`+locationHeader, -1)

	return target, err
}

// isAuthenticatedRetryNeeded is isAuthenticatedRetryNeeded().
func (c *CurlDownloader) isAuthenticatedRetryNeeded(job *curlJob, response *Response) (AuthResult, error) {
	status := response.StatusCode()

	if (status == 401 || status == 403) && job.attributes.retryAuthFailure {
		statusMessage, _ := response.StatusMessage()

		result, err := c.authHelper.PromptAuthIfNeeded(job.url, job.origin, status, statusMessage, response.Headers(), job.attributes.authRetries, response.Body())
		if err != nil || result.Retry {
			return result, err
		}
	}

	locationHeader, _, err := response.HeaderChecked("location")
	if err != nil {
		return AuthResult{}, err
	}
	needsAuthRetry := ""

	// check for bitbucket login page asking to authenticate
	if job.origin == "bitbucket.org" &&
		!IsPublicBitBucketDownload(job.url) &&
		strings.HasSuffix(job.url, ".zip") &&
		(!php.Truthy(locationHeader) || !strings.HasSuffix(locationHeader, ".zip")) {
		contentType, ok, err := response.HeaderChecked("content-type")
		if err != nil {
			return AuthResult{}, err
		}
		if ok {
			if isHTML, _ := textHTMLRegex.IsMatch(contentType); isHTML {
				needsAuthRetry = "Bitbucket requires authentication and it was not provided"
			}
		}
	}

	// check for gitlab 404 when downloading archives
	if status == 404 && configHas(c.config, "gitlab-domains", job.origin) && strings.Contains(job.url, "archive.zip") {
		needsAuthRetry = "GitLab requires authentication and it was not provided"
	}

	if needsAuthRetry != "" {
		if job.attributes.retryAuthFailure {
			result, err := c.authHelper.PromptAuthIfNeeded(job.url, job.origin, 401, "", nil, job.attributes.authRetries, "")
			if err != nil || result.Retry {
				return result, err
			}
		}

		return AuthResult{}, c.failResponse(job, response, needsAuthRetry, false)
	}

	return AuthResult{}, nil
}

// restartJob is restartJob(): the same request again (or to url) with
// updated attributes.
func (c *CurlDownloader) restartJob(job *curlJob, url string, attributes curlAttributes) error {
	if job.hasFile {
		_ = os.Remove(job.filename + "~")
	}

	closeFile(job.bodyFile)

	origin := util.GetOrigin(url, php.ToStrings(c.config.Get("gitlab-domains")))

	return c.initDownload(job, origin, url, job.options, attributes)
}

// maxRetryAfter is MAX_RETRY_AFTER: the longest Retry-After interval which
// is waited out, in seconds. Without a ceiling a server could park an
// install for as long as it likes (isStatusCodeRetryNeeded).
const maxRetryAfter = 60

// Infallible (anchored, fixed-length): call sites ignore the error.
var retryAfterSecondsRegex = php.MustCompile(`{^\d{1,9}$}`)

// isStatusCodeRetryNeeded decides whether a 4xx/5xx response should be
// retried, and how long to wait beforehand: delay is nil when the default
// backoff applies.
func (c *CurlDownloader) isStatusCodeRetryNeeded(job *curlJob, response *Response) (bool, *time.Duration) {
	if !isGetRequest(job.options) || job.attributes.retries >= c.maxRetries {
		return false, nil
	}

	status := response.StatusCode()
	retryAfter, hasRetryAfter := retryAfterDelay(response)

	retryableByStatus := slices.Contains([]int{423, 425, 500, 502, 503, 504, 507, 510}, status) ||
		// codeload.github.com intermittently returns 400 on reused
		// connections, retry those specifically, see #12958
		(status == 400 && util.URLHost(job.url) == "codeload.github.com")

	// a rate limit is only retried when the server said when to come back
	retryableByHeader := status == 429 && hasRetryAfter

	if !retryableByStatus && !retryableByHeader {
		return false, nil
	}

	// an interval beyond the cap is not waited for: a status which is
	// retryable anyway falls back to the usual backoff, a rate limit has
	// nothing else to go on and fails
	if hasRetryAfter && retryAfter > maxRetryAfter {
		if !retryableByStatus {
			return false, nil
		}

		hasRetryAfter = false
	}

	// a wait of 0s is no wait, so the usual backoff spaces such retries out
	if hasRetryAfter && retryAfter > 0 {
		delay := time.Duration(retryAfter) * time.Second

		return true, &delay
	}

	return true, nil
}

// retryAfterDelay reads the Retry-After header, in either of the forms RFC
// 9110 allows, as a number of seconds to wait.
func retryAfterDelay(response *Response) (int, bool) {
	retryAfter, ok, err := response.HeaderChecked("retry-after")
	if err != nil || !ok {
		return 0, false
	}

	// more digits would overflow an int, and are no interval anyone can mean
	if ok, _ := retryAfterSecondsRegex.IsMatch(retryAfter); ok {
		return php.ToNativeInt(retryAfter), true
	}

	// the three date formats RFC 9110 requires recipients to accept, with
	// the leading day name skipped as it is redundant
	if at, err := http.ParseTime(retryAfter); err == nil {
		// a date which has already passed means the wait is over and we
		// can retry straight away
		return max(0, int(at.Unix()-time.Now().Unix())), true
	}

	return 0, false
}

// retryAfterSuffix is the " in Ns as requested by Retry-After" the retry
// message adds for a wait the server asked for.
func retryAfterSuffix(delay *time.Duration) string {
	if delay == nil {
		return ""
	}

	return " in " + strconv.FormatFloat(delay.Seconds(), 'f', -1, 64) + "s as requested by Retry-After"
}

// startDelayed arranges for the delayed retry to start once it is due: on
// the goroutine driving the loop for an asynchronous request, through the
// tick queue for a synchronous one. c.mu is held.
func (c *CurlDownloader) startDelayed(d *delayedCurlJob) {
	post := c.post
	if a := d.job.async; a != nil {
		sched := a.sched
		deliver := a.deliver
		post = func(ev curlEvent) { sched.Interrupt(func() { deliver(ev) }) }
	}

	go func() {
		c.waitDue(d)
		post(curlEvent{due: true})
	}()
}

// waitDue blocks until the delayed retry may start: its own delay, or a
// later Retry-After hold the origin asked for.
func (c *CurlDownloader) waitDue(d *delayedCurlJob) {
	for {
		c.mu.Lock()
		due := c.dueAt(d)
		c.mu.Unlock()

		wait := time.Until(due)
		if wait <= 0 {
			return
		}

		time.Sleep(wait)
	}
}

// restartDue is restartDueJobs(): the delayed retries whose delay elapsed
// are started, in the order they were parked. c.mu is held.
func (c *CurlDownloader) restartDue() {
	now := time.Now()

	for _, d := range slices.Clone(c.delayed) {
		if c.dueAt(d).After(now) {
			continue
		}

		c.delayed = slices.DeleteFunc(c.delayed, func(delayed *delayedCurlJob) bool { return delayed == d })

		if err := c.restartJob(d.job, d.url, d.attributes); err != nil {
			// an immediate restart is covered by complete()'s error handling,
			// a deferred one is not
			c.rejectJob(d.job, err)
		}
	}
}

// announceRetryAfterWaits tells the user when an origin asked for requests
// to be retried later, once per origin and episode: the wait can last up to
// maxRetryAfter seconds, which without a word would look like a hang. c.mu
// is held.
func (c *CurlDownloader) announceRetryAfterWaits() {
	now := time.Now()

	origins := slices.Clone(c.retryAfterOrigins)

	for _, state := range origins {
		pending := 0
		lastDue := now

		for _, d := range c.delayed {
			if d.job.origin == state.origin {
				pending++
				lastDue = maxTime(lastDue, c.dueAt(d))
			}
		}

		if pending == 0 {
			c.retryAfterOrigins = slices.DeleteFunc(c.retryAfterOrigins, func(s *retryAfterOrigin) bool { return s == state })

			continue
		}

		if state.announced {
			continue
		}

		reason := "is rate limiting requests"
		if state.statusCode != 429 {
			reason = "responded with status code " + strconv.Itoa(state.statusCode)
		}

		wait := int(math.Ceil(lastDue.Sub(now).Seconds()))
		c.io.WriteError("<warning>"+state.origin+" "+reason+" ("+strconv.Itoa(pending)+" pending), waiting "+strconv.Itoa(wait)+"s before retrying</warning>", true, io.Normal)
		state.announced = true
	}
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}

	return a
}

// restartJobWithDelay is restartJobWithDelay(): the same request again
// with updated attributes, once its delay elapsed. Sleeping here would
// stall every other transfer the loop drives, so the retry is scheduled
// instead (delayedCurlJob).
func (c *CurlDownloader) restartJobWithDelay(job *curlJob, url string, attributes curlAttributes, retryAfter *time.Duration, statusCode int) error {
	now := time.Now()

	var delay time.Duration

	if retryAfter != nil {
		delay = *retryAfter
		until := now.Add(delay)

		// a hold which has already run out is over, so the origin is
		// announced anew rather than extended
		if state := c.retryAfterOrigin(job.origin); state == nil {
			c.retryAfterOrigins = append(c.retryAfterOrigins, &retryAfterOrigin{origin: job.origin, statusCode: statusCode, until: until})
		} else if !state.until.After(now) {
			state.statusCode, state.announced, state.until = statusCode, false, until
		} else if until.After(state.until) {
			state.until = until
		}
	} else {
		switch {
		case attributes.retries >= 3:
			delay = 500 * time.Millisecond // half a second delay for 3rd retry and beyond
		case attributes.retries >= 2:
			delay = 100 * time.Millisecond // 100ms delay for 2nd retry
		}
	}

	if delay <= 0 && !c.originOnHold(job.origin) {
		return c.restartJob(job, url, attributes)
	}

	d := &delayedCurlJob{job: job, url: url, attributes: attributes, at: now.Add(delay)}
	c.delayed = append(c.delayed, d)
	c.startDelayed(d)

	return nil
}

// failResponse is failResponse(): the TransportException of a 4xx/5xx
// response, with the start of a JSON body unless warnings showed it.
// The error is that TransportException, or the PcreException reading the
// content-type header throws.
func (c *CurlDownloader) failResponse(job *curlJob, response *Response, errorMessage string, warningsOutput bool) error {
	if job.hasFile {
		_ = os.Remove(job.filename + "~")
	}

	details := ""

	isJSON, err := isJSONResponse(response)
	if err != nil {
		return err
	}

	if !warningsOutput && isJSON {
		body := response.Body()
		details = ":" + php.EOL + body[:min(200, len(body))]

		if len(body) > 200 {
			details += "..."
		}
	}

	return util.NewTransportError(`The "`+util.SanitizeURL(job.url)+`" file could not be downloaded (`+errorMessage+`)`+details, response.StatusCode())
}

// isJSONResponse reports whether the response's body is a JSON document,
// as its content-type says. A PcreException reading the header is the
// error.
func isJSONResponse(response *Response) (bool, error) {
	contentType, _, err := response.HeaderChecked("content-type")
	if err != nil {
		return false, err
	}

	ct := php.Strtolower(contentType)

	return ct == "application/json" || ct == "application/json; charset=utf-8", nil
}

// statusFailureMessage describes why a 4xx/5xx response failed the
// request: a Retry-After which is longer than we wait for is spelled out,
// as it tells the user whether trying again straight away can help or
// whether to come back later.
func statusFailureMessage(response *Response) string {
	message, _ := response.StatusMessage()

	if retryAfter, ok := retryAfterDelay(response); ok && retryAfter > maxRetryAfter {
		message += ", the server asked to retry in " + strconv.Itoa(retryAfter) + "s which is longer than the " + strconv.Itoa(maxRetryAfter) + "s Composer is willing to wait, try again later"
	}

	return message
}

// rejectJob is rejectJob().
func (c *CurlDownloader) rejectJob(job *curlJob, err error) {
	closeFile(job.bodyFile)

	if job.hasFile {
		_ = os.Remove(job.filename + "~")
	}

	job.reject(err)
}
