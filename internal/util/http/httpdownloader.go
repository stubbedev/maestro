// Ports src/Composer/Util/HttpDownloader.php.

package http

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// Job states.
const (
	statusQueued = iota + 1
	statusStarted
	statusCompleted
	statusFailed
	statusAborted
)

type httpJob struct {
	id      int
	status  int
	url     string
	options *php.Array
	copyTo  string
	sync    bool
	origin  string
	resolve func(*Response)
	reject  func(error)
	// rejectRaw rejects the promise without the job bookkeeping.
	rejectRaw func(error)
	curlID    int
	response  *Response
	err       error
	// counted: the job holds one of the maxJobs slots.
	counted bool
}

// HttpDownloader ports Composer\Util\HttpDownloader: parallel HTTP requests
// with Composer's concurrency limit, retries and authentication.
//
// Asynchronous requests (Add, AddCopy) transfer on their own goroutines;
// what Composer does when one finishes (retries, redirects, authentication
// prompts, settling the promise and running its callbacks) runs on the
// goroutine driving the downloader's Scheduler (Loop.Wait, Wait,
// Promise.Await), in the order the transfers started, as it runs inside
// wait() in PHP; queued requests start there too. See util.Scheduler.
//
// Synchronous requests (Get, Copy) are finished by the goroutine waiting
// for them, which may be any; it does not run other requests' callbacks
// (Composer's wait($id) ticks every job), and they do not count against
// the maximum number of parallel requests, so that they never wait for a
// slot only a driving goroutine would free. Apart from that the downloader
// is safe for concurrent use.
type HttpDownloader struct {
	io     io.IO
	config Config
	rt     Runtime

	mu          sync.Mutex
	jobs        []*httpJob
	byID        map[int]*httpJob
	options     *php.Array
	runningJobs int
	maxJobs     int
	// burstJobs (0: none) is how many https requests may run at once when
	// nothing shows when requests start (see canStart).
	burstJobs  int
	curl       *CurlDownloader
	rfs        *RemoteFilesystem
	idGen      int
	disabled   bool
	allowAsync bool
	// sched runs the completions of asynchronous requests.
	sched *util.Scheduler
	// settles are the promise settlements decided with mu held, run once
	// it is released: their callbacks may call the downloader again.
	settles []func()
}

// NewHttpDownloader is new HttpDownloader($io, $config, $options,
// $disableTls). rt supplies the User-Agent facts; nil uses defaults.
func NewHttpDownloader(ioi io.IO, config Config, options *php.Array, disableTLS bool, rt Runtime) (*HttpDownloader, error) {
	if rt == nil {
		rt = defaultRuntime
	}

	h := &HttpDownloader{
		io:      ioi,
		config:  config,
		rt:      rt,
		byID:    map[int]*httpJob{},
		maxJobs: 12,
		sched:   util.NewScheduler(),
	}

	disabled, _ := util.GetEnv("COMPOSER_DISABLE_NETWORK")
	h.disabled = php.ToBool(disabled)

	h.options = php.NewArray()

	// Setup TLS options. The cafile option can be set via config.json.
	if !disableTLS {
		defaults, err := GetTLSDefaults(options, ioi)
		if err != nil {
			return nil, err
		}

		h.options = defaults
	}

	// handle the other externally set options normally.
	if options != nil {
		h.options = php.ArrayReplaceRecursive(h.options, options).Clone()
	}

	h.curl = newCurlDownloader(ioi, config, rt, &h.mu)

	rfs, err := NewRemoteFilesystem(ioi, config, options, disableTLS, nil)
	if err != nil {
		return nil, err
	}

	rfs.rt = rt
	h.rfs = rfs

	if v, ok := util.GetEnv("COMPOSER_MAX_PARALLEL_HTTP"); ok && php.IsNumeric(v) {
		h.maxJobs = int(max(1, min(50, php.ToInt(v))))
	} else if !ioi.IsDebug() {
		h.burstJobs = defaultBurstJobs
	}

	return h, nil
}

// Get is get($url, $options): a request completed synchronously.
func (h *HttpDownloader) Get(url string, options *php.Array) (*Response, error) {
	return h.syncRequest(url, options, "")
}

// Add is add($url, $options): an asynchronous request. It needs a Loop.
func (h *HttpDownloader) Add(url string, options *php.Array) (*util.Promise[*Response], error) {
	return h.asyncRequest(url, options, "")
}

// Copy is copy($url, $to, $options): a download into the file to,
// completed synchronously.
func (h *HttpDownloader) Copy(url, to string, options *php.Array) (*Response, error) {
	return h.syncRequest(url, options, to)
}

// AddCopy is addCopy($url, $to, $options): an asynchronous download into
// the file to.
func (h *HttpDownloader) AddCopy(url, to string, options *php.Array) (*util.Promise[*Response], error) {
	return h.asyncRequest(url, options, to)
}

// emptyURLError is the InvalidArgumentException get/copy (line, else
// copyLine when copying to a file) throw for an empty URL.
func emptyURLError(copyTo string, line, copyLine int) error {
	if copyTo != "" {
		line = copyLine
	}

	return &util.InvalidArgumentError{Message: "$url must not be an empty string", Site: phperr.At("HttpDownloader.php", line)}
}

func (h *HttpDownloader) syncRequest(url string, options *php.Array, copyTo string) (*Response, error) {
	if url == "" {
		return nil, emptyURLError(copyTo, 108, 154)
	}

	h.mu.Lock()
	job, _, err := h.addJob(url, options, copyTo, true)
	h.unlock()

	if err != nil {
		return nil, err
	}

	h.waitJob(job.id)

	return h.response(job.id)
}

func (h *HttpDownloader) asyncRequest(url string, options *php.Array, copyTo string) (*util.Promise[*Response], error) {
	if url == "" {
		return nil, emptyURLError(copyTo, 134, 179)
	}

	h.mu.Lock()
	defer h.unlock()

	_, promise, err := h.addJob(url, options, copyTo, false)

	return promise, err
}

// unlock releases h.mu and runs the settlements decided meanwhile.
func (h *HttpDownloader) unlock() {
	settles := h.settles
	h.settles = nil
	h.mu.Unlock()

	for _, settle := range settles {
		settle()
	}
}

// Scheduler returns the scheduler running the completions of
// asynchronous requests (the Loop's).
func (h *HttpDownloader) Scheduler() *util.Scheduler { return h.sched }

// Options is getOptions().
func (h *HttpDownloader) Options() *php.Array {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.options.Clone()
}

// SetOptions is setOptions($options): merged into the current options.
func (h *HttpDownloader) SetOptions(options *php.Array) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.options = php.ArrayReplaceRecursive(h.options, options).Clone()
}

// Infallible: the pattern does bounded work per start position, so Preg
// cannot throw on it and its call sites ignore the error.
var urlCredentialsRegex = php.MustCompile(`{^https?://([^:/]+):([^@/]+)@([^/]+)}i`)

// addJob is addJob(); h.mu is held.
func (h *HttpDownloader) addJob(url string, options *php.Array, copyTo string, sync bool) (*httpJob, *util.Promise[*Response], error) {
	// one deep copy: the job's options must not change with h.options or
	// the caller's array
	merged := h.options
	if options != nil {
		merged = php.ArrayReplaceRecursive(merged, options)
	}

	merged = merged.Clone()

	job := &httpJob{
		id:      h.idGen,
		status:  statusQueued,
		url:     url,
		options: merged,
		copyTo:  copyTo,
		sync:    sync,
		origin:  util.GetOrigin(url, configList(h.config, "gitlab-domains")),
	}
	h.idGen++

	if !sync && !h.allowAsync {
		return nil, nil, &util.LogicError{Message: `You must use the HttpDownloader instance which is part of a Composer\Loop instance to be able to run async http requests`, Site: phperr.At("HttpDownloader.php", 226)}
	}

	// capture username/password from URL if there is one
	if m, _ := urlCredentialsRegex.MatchStrictGroups(url); m != nil {
		password := php.Rawurldecode(m.Get(2))
		h.io.SetAuthentication(job.origin, php.Rawurldecode(m.Get(1)), &password)
	}

	promise, resolve, reject := util.NewDeferredOn[*Response](h.sched, func() { h.cancelJob(job) })

	job.resolve = func(r *Response) {
		job.status = statusCompleted
		job.response = r
		h.markJobDone(job)
		h.settles = append(h.settles, func() { resolve(r) })
	}
	job.reject = func(err error) {
		job.status = statusFailed
		job.err = err
		h.markJobDone(job)
		h.settles = append(h.settles, func() { reject(err) })
	}
	job.rejectRaw = func(err error) {
		h.settles = append(h.settles, func() { reject(err) })
	}

	if !h.canUseCurl(job) {
		// RemoteFilesystem requests run right away, as Composer runs them in
		// the promise's resolver.
		job.status = statusStarted
		h.runRemoteFilesystem(job)
	}

	h.jobs = append(h.jobs, job)
	h.byID[job.id] = job

	if sync || h.canStart(job) {
		h.startJob(job)
	}

	return job, promise, nil
}

// runRemoteFilesystem performs a job through RemoteFilesystem.
func (h *HttpDownloader) runRemoteFilesystem(job *httpJob) {
	if job.copyTo != "" {
		if _, err := h.rfs.Copy(job.origin, job.url, job.copyTo, false, job.options); err != nil {
			job.reject(err)

			return
		}

		headers := h.rfs.LastHeaders()
		code, _ := FindStatusCode(headers)
		job.resolve(NewResponse(job.url, code, headers, job.copyTo+"~"))

		return
	}

	body, err := h.rfs.GetContents(job.origin, job.url, false, job.options)
	if err != nil {
		job.reject(err)

		return
	}

	headers := h.rfs.LastHeaders()
	code, _ := FindStatusCode(headers)
	job.resolve(NewResponse(job.url, code, headers, body))
}

// cancelJob is the promise canceller. A queued job's promise, which React
// leaves pending, is rejected so that waiters do not hang.
func (h *HttpDownloader) cancelJob(job *httpJob) {
	h.mu.Lock()
	defer h.unlock()

	err := &util.IrrecoverableDownloadError{Message: "Download of " + util.SanitizeURL(job.url) + " canceled", Site: phperr.At("HttpDownloader.php", 280)}

	switch job.status {
	case statusQueued:
		job.status = statusAborted
		job.err = err
		job.rejectRaw(err)

		return
	case statusStarted:
	default:
		return
	}

	job.status = statusAborted
	h.curl.abortRequest(job.curlID)
	job.reject(err)
}

// startJob is startJob(); h.mu is held. An asynchronous transfer's end
// is handed to the scheduler (onTransfer).
func (h *HttpDownloader) startJob(job *httpJob) {
	if job.status != statusQueued {
		return
	}

	job.status = statusStarted

	if !job.sync {
		h.runningJobs++
		job.counted = true
	}

	if h.disabled {
		if _, ok := path(job.options, "http", "header"); ok && php.Stripos(strings.Join(headerList(job.options), ""), "if-modified-since") >= 0 {
			job.resolve(NewResponse(job.url, 304, []string{}, ""))
		} else {
			e := transportError(phperr.At("HttpDownloader.php", 332), "Network disabled, request canceled: "+util.SanitizeURL(job.url), 499)
			e.StatusCode = 499
			job.reject(e)
		}

		return
	}

	var async *asyncTransfers
	if !job.sync {
		async = &asyncTransfers{sched: h.sched, deliver: h.onTransfer}
	}

	id, err := h.curl.download(job.resolve, job.reject, job.origin, job.url, job.options, job.copyTo, async)
	if err != nil {
		job.reject(err)

		return
	}

	job.curlID = id
}

// onTransfer is the tick of an asynchronous transfer that ended, run by
// the scheduler: CurlDownloader's processing of it (which settles the
// job's promise, running its callbacks, or restarts it), then, as the next
// countActiveJobs() does, the start of queued jobs.
func (h *HttpDownloader) onTransfer(ev curlEvent) {
	h.mu.Lock()
	h.curl.process(ev)
	h.unlock()

	h.mu.Lock()
	h.startQueued()
	h.unlock()
}

// countQueued is the number of asynchronous jobs waiting for a slot.
func (h *HttpDownloader) countQueued() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	n := 0
	for _, job := range h.jobs {
		if job.status == statusQueued && !job.sync {
			n++
		}
	}

	return n
}

// startQueued starts queued jobs while slots are free; h.mu is held.
func (h *HttpDownloader) startQueued() {
	for _, job := range h.jobs {
		if h.runningJobs >= max(h.maxJobs, h.burstJobs) {
			return
		}

		if job.status == statusQueued && h.canStart(job) {
			h.startJob(job)
		}
	}
}

// defaultBurstJobs is how many https requests maestro runs at once by
// default where nothing shows the difference.
const defaultBurstJobs = 48

// canStart reports whether a queued job may start now. Composer runs at
// most COMPOSER_MAX_PARALLEL_HTTP (12) requests at once, a limit of its
// curl set-up rather than of anything it shows (deliberate deviation 3,
// speed): unless that variable is set, maestro lets up to 48 https
// requests run at once, the queue only mattering for what is printed when
// a request starts: the -vvv "Downloading" lines (so not at -vvv) and the
// insecure-protocol warning of plain http URLs, which keep Composer's
// limit. h.mu is held.
func (h *HttpDownloader) canStart(job *httpJob) bool {
	if h.runningJobs < h.maxJobs {
		return true
	}

	return h.runningJobs < h.burstJobs && strings.HasPrefix(job.url, "https://")
}

// markJobDone is markJobDone(): the job frees its slot; h.mu is held.
func (h *HttpDownloader) markJobDone(job *httpJob) {
	if job.counted {
		job.counted = false
		h.runningJobs--
	}
}

// Wait is wait(): it drives the scheduler until no asynchronous request
// is queued or running. Only one goroutine may drive it at a time.
func (h *HttpDownloader) Wait() {
	h.sched.Run(func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()

		return h.countActive(false) == 0
	})
}

// waitJob finishes a synchronous request: it processes the ends of
// synchronous transfers until the job completed.
func (h *HttpDownloader) waitJob(id int) {
	for {
		h.mu.Lock()

		if job, ok := h.byID[id]; !ok || job.status >= statusCompleted {
			h.unlock()

			return
		}

		h.curl.tick(h.curl.selectTimeout)
		h.unlock()
	}
}

// EnableAsync is enableAsync().
func (h *HttpDownloader) EnableAsync() {
	h.mu.Lock()
	h.allowAsync = true
	h.mu.Unlock()
}

// CountActiveJobs is countActiveJobs(): the number of queued or running
// requests. It does not tick: the scheduler's driver does.
func (h *HttpDownloader) CountActiveJobs() int {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.countActive(true)
}

// countActive counts the queued or running jobs (synchronous ones too when
// withSync) and forgets the finished asynchronous ones; h.mu is held.
func (h *HttpDownloader) countActive(withSync bool) int {
	active := 0
	h.jobs = slices.DeleteFunc(h.jobs, func(job *httpJob) bool {
		if job.status < statusCompleted {
			if withSync || !job.sync {
				active++
			}

			return false
		}

		if !job.sync {
			delete(h.byID, job.id)

			return true
		}

		return false
	})

	return active
}

// response is getResponse($index).
func (h *HttpDownloader) response(id int) (*Response, error) {
	h.mu.Lock()
	defer h.unlock()

	job, ok := h.byID[id]
	if !ok {
		return nil, &util.LogicError{Message: "Invalid request id", Site: phperr.At("HttpDownloader.php", 420)}
	}

	if job.status == statusFailed {
		return nil, job.err
	}

	if job.response == nil {
		return nil, &util.LogicError{Message: "Response not available yet, call wait() first", Site: phperr.At("HttpDownloader.php", 429)}
	}

	delete(h.byID, id)
	h.jobs = slices.DeleteFunc(h.jobs, func(j *httpJob) bool { return j == job })

	return job.response, nil
}

// Infallible: the pattern does bounded work per start position, so Preg
// cannot throw on it and its call sites ignore the error.
var httpURLRegex = php.MustCompile(`{^https?://}i`)

// canUseCurl is canUseCurl(): http(s) requests not asking for
// allow_self_signed go through the curl port.
func (h *HttpDownloader) canUseCurl(job *httpJob) bool {
	if ok, _ := httpURLRegex.IsMatch(job.url); !ok {
		return false
	}

	v, _ := path(job.options, "ssl", "allow_self_signed")

	return !php.ToBool(v)
}

var ansiColorRegex = php.MustCompile("{\x1b\\[[;\\d]*m}u")

// OutputWarnings is HttpDownloader::outputWarnings($io, $url, $data): it
// shows the warnings and infos a repository sends (the legacy
// warning/info keys and the warnings/infos lists) that apply to this
// Composer version, and reports whether it wrote any.
func OutputWarnings(ioi io.IO, url string, data any) (bool, error) {
	a, _ := data.(*php.Array)
	wrote := false

	cleanMessage := func(msg string) (string, error) {
		if !ioi.IsDecorated() {
			out, _, err := ansiColorRegex.Replace(msg, "", -1)

			return out, err
		}

		return msg, nil
	}

	var parser semver.VersionParser

	composerVersion, err := parser.Normalize(ComposerVersion)
	if err != nil {
		return false, err
	}

	composer := semver.NewConstraintOp(semver.OpEQ, composerVersion)

	matches := func(versions any) (bool, error) {
		constraint, err := parser.ParseConstraints(php.ToString(versions))
		if err != nil {
			return false, err
		}

		return constraint.Matches(composer), nil
	}

	write := func(typ string, message any) error {
		msg, err := cleanMessage(php.ToString(message))
		if err != nil {
			return err
		}

		ioi.WriteError("<"+typ+">"+php.Ucfirst(typ)+" from "+util.SanitizeURL(url)+": "+msg+"</"+typ+">", true, io.Normal)
		wrote = true

		return nil
	}

	// legacy warning/info keys
	for _, typ := range [2]string{"warning", "info"} {
		if !php.ToBool(arrayValue(a, typ)) {
			continue
		}

		if versions := arrayValue(a, typ+"-versions"); php.ToBool(versions) {
			ok, err := matches(versions)
			if err != nil {
				return wrote, err
			}

			if !ok {
				continue
			}
		}

		if err := write(typ, arrayValue(a, typ)); err != nil {
			return wrote, err
		}
	}

	// modern Composer 2.2+ format with support for multiple warning/info
	// messages
	for _, key := range [2]string{"warnings", "infos"} {
		specs, _ := arrayValue(a, key).(*php.Array)
		if specs == nil || specs.Len() == 0 {
			continue
		}

		typ := key[:len(key)-1]

		for _, spec := range specs.All() {
			s, _ := spec.(*php.Array)

			ok, err := matches(arrayValue(s, "versions"))
			if err != nil {
				return wrote, err
			}

			if !ok {
				continue
			}

			if err := write(typ, arrayValue(s, "message")); err != nil {
				return wrote, err
			}
		}
	}

	return wrote, nil
}

// connectivityCheck is the request getExceptionHints makes to tell a DNS
// problem from being offline; tests replace it.
var connectivityCheck = func() bool {
	client := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // as Composer's verify_peer false
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://8.8.8.8", nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}

	_ = resp.Body.Close()

	return true
}

// GetExceptionHints is HttpDownloader::getExceptionHints($e): hints shown
// before a TransportException about name resolution; nil when there are
// none.
func GetExceptionHints(e error) []string {
	te, ok := errors.AsType[*util.TransportError](e)
	if !ok {
		return nil
	}

	if !strings.Contains(te.Message, "Resolving timed out") && !strings.Contains(te.Message, "Could not resolve host") {
		return nil
	}

	if connectivityCheck() {
		return []string{"<error>The following exception probably indicates you have misconfigured DNS resolver(s)</error>"}
	}

	return []string{"<error>The following exception probably indicates you are offline or have misconfigured DNS resolver(s)</error>"}
}

// IsCurlEnabled is isCurlEnabled(): the curl port is always available.
func IsCurlEnabled() bool { return true }
