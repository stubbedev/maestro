// Ports src/Composer/Repository/ComposerRepository.php: fetchFile,
// fetchFileIfLastModified, asyncFetchFile and startCachedAsyncDownload,
// with the PRE_FILE_DOWNLOAD/POST_FILE_DOWNLOAD events and the metadata
// cache (Last-Modified / If-Modified-Since, the degraded mode).

package composerrepo

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// MetadataContext is the context of the PRE_FILE_DOWNLOAD and
// POST_FILE_DOWNLOAD events of metadata downloads: PHP's
// ['repository' => $this] and ['response' => $response, 'repository' =>
// $this] (Response nil in the former).
type MetadataContext struct {
	Response   *http.Response
	Repository repository.RepositoryInterface
}

var httpURLRegex = php.MustCompile(`{^https?://}i`)

// preFileDownload dispatches PRE_FILE_DOWNLOAD for a metadata URL: the
// URL and transport options to use. The options are the repository's
// own (or a listener's): callers clone them before modifying. eventLine is
// the line of the caller's setTransportOptions($this->options) call,
// getLine that of its HttpDownloader call: where options that are not an
// array fail (strict_types).
func (r *ComposerRepository) preFileDownload(filename string, eventLine, getLine int) (string, *php.Array, error) {
	if r.eventDispatcher == nil {
		if r.options == nil {
			fn, decl := `Composer\Util\HttpDownloader::get`, phperr.At("HttpDownloader.php", 105)
			if getLine == 2012 {
				fn, decl = `Composer\Util\HttpDownloader::add`, phperr.At("HttpDownloader.php", 131)
			}

			return "", nil, pkg.ArgumentTypeError(fn, 2, "options", "array", r.rawOptions).
				Called(strings.Replace(fn, "::", "->", 1), decl, "ComposerRepository.php", getLine)
		}

		return filename, r.options, nil
	}

	event := eventdispatcher.NewPreFileDownloadEvent(eventdispatcher.PreFileDownload, r.httpDownloader, filename, "metadata", &MetadataContext{Repository: r})
	if r.options == nil {
		return "", nil, pkg.ArgumentTypeError(`Composer\Plugin\PreFileDownloadEvent::setTransportOptions`, 1, "options", "array", r.rawOptions).
			Called(`Composer\Plugin\PreFileDownloadEvent->setTransportOptions`, phperr.At("PreFileDownloadEvent.php", 154), "ComposerRepository.php", eventLine)
	}
	event.SetTransportOptions(r.options)
	if _, err := r.eventDispatcher.Dispatch(event.Name(), event); err != nil {
		return "", nil, err
	}

	return event.ProcessedURL(), event.TransportOptions(), nil
}

// postFileDownload dispatches POST_FILE_DOWNLOAD for a metadata response.
func (r *ComposerRepository) postFileDownload(checksum pkg.NullString, filename string, response *http.Response) error {
	if r.eventDispatcher == nil {
		return nil
	}

	event := eventdispatcher.NewPostFileDownloadEvent(eventdispatcher.PostFileDownload, pkg.NullString{}, checksum, filename, "metadata", &MetadataContext{Response: response, Repository: r})
	_, err := r.eventDispatcher.Dispatch(event.Name(), event)

	return err
}

// withIfModifiedSince returns a copy of options with the
// If-Modified-Since header.
func withIfModifiedSince(options *php.Array, lastModifiedTime string) *php.Array {
	options = cloneOptions(options)
	http.AppendHeader(http.HTTPOptions(options), "If-Modified-Since: "+lastModifiedTime)

	return options
}

func cloneOptions(options *php.Array) *php.Array {
	if options == nil {
		return php.NewArray()
	}

	return options.Clone()
}

// fetchFileRelative ports fetchFile($filename) without a cache key: the
// file is relative to the repository's base URL and cached under its
// name.
func (r *ComposerRepository) fetchFileRelative(filename string) (*php.Array, error) {
	if filename == "" {
		return nil, &util.InvalidArgumentError{Site: phperr.At("ComposerRepository.php", 1736), Message: "$filename should not be an empty string"}
	}

	return r.fetchFile(r.baseURL+"/"+filename, filename, "", false)
}

// fetchFile ports fetchFile($filename, $cacheKey, $sha256,
// $storeLastModifiedTime): the decoded file (nil when it is not an
// array), checked against sha256 ("" for null), cached, or read from the
// cache when the repository cannot be reached.
func (r *ComposerRepository) fetchFile(filename, cacheKey, sha256 string, storeLastModifiedTime bool) (*php.Array, error) {
	if filename == "" {
		return nil, &util.InvalidArgumentError{Site: phperr.At("ComposerRepository.php", 1736), Message: "$filename should not be an empty string"}
	}

	// url-encode $ signs in URLs as bad proxies choke on them
	if pos := strings.IndexByte(filename, '$'); pos > 0 {
		if m, err := httpURLRegex.IsMatch(filename); err != nil {
			return nil, err
		} else if m {
			filename = filename[:pos] + "%24" + filename[pos+1:]
		}
	}

	for retries := 3; retries > 0; {
		retries--
		data, retry, err := r.fetchFileAttempt(&filename, cacheKey, sha256, storeLastModifiedTime, retries)
		if retry {
			continue
		}
		if err == nil {
			return data, nil
		}

		if isPHPError(err) || isLogic(err) || statusCode(err) == 404 {
			return nil, err
		}
		if _, ok := errors.AsType[*repository.SecurityError](err); ok {
			return nil, err
		}

		if php.ToBool(cacheKey) {
			contents, ok, rerr := r.cache.Read(cacheKey)
			if rerr != nil {
				return nil, rerr
			}
			if ok && php.ToBool(contents) {
				r.degradedWarning(err)
				parsed, perr := json.ParseJSON(contents, r.cache.Root()+cacheKey)
				if perr != nil {
					return nil, perr
				}
				data, _ := parsed.(*php.Array)

				return data, nil
			}
		}

		return nil, err
	}

	return nil, &util.LogicError{Site: phperr.At("ComposerRepository.php", 1831), Message: "ComposerRepository: Undefined $data. Please report at https://github.com/composer/composer/issues/new."}
}

// fetchFileAttempt is one try of fetchFile's loop; retry tells it to
// continue with the next try.
func (r *ComposerRepository) fetchFileAttempt(filename *string, cacheKey, sha256 string, storeLastModifiedTime bool, retries int) (*php.Array, bool, error) {
	processed, options, err := r.preFileDownload(*filename, 1755, 1761)
	if err != nil {
		return nil, false, err
	}
	*filename = processed

	response, err := r.httpDownloader.Get(*filename, options)
	if err != nil {
		return nil, false, phperr.Call(err, `Composer\Util\HttpDownloader->get`, "ComposerRepository.php", 1761)
	}
	body := response.Body()
	if sha256 != "" && sha256 != sha256Hex(body) {
		// undo downgrade before trying again if http seems to be hijacked or modifying content somehow
		if r.allowSslDowngrade {
			r.url = strings.ReplaceAll(r.url, "http://", "https://")
			r.baseURL = strings.ReplaceAll(r.baseURL, "http://", "https://")
			*filename = strings.ReplaceAll(*filename, "http://", "https://")
		}

		if retries > 0 {
			time.Sleep(100 * time.Millisecond)

			return nil, true, nil
		}

		// TODO use scarier wording once we know for sure it doesn't do false positives anymore
		return nil, false, &repository.SecurityError{Site: phperr.At("ComposerRepository.php", 1778), Message: "The contents of " + util.SanitizeURL(*filename) + " do not match its signature. This could indicate a man-in-the-middle attack or e.g. antivirus software corrupting files. Try running composer again and report this if you think it is a mistake."}
	}

	if err := r.postFileDownload(pkg.NonEmpty(sha256), *filename, response); err != nil {
		return nil, false, err
	}

	decoded, err := response.DecodeJSON()
	if err != nil {
		return nil, false, err
	}
	if _, err := http.OutputWarnings(r.io, r.url, decoded); err != nil {
		return nil, false, err
	}
	data, _ := decoded.(*php.Array)

	if php.ToBool(cacheKey) && !r.cache.IsReadOnly() {
		if storeLastModifiedTime {
			if lastModifiedDate, _ := response.Header("last-modified"); php.ToBool(lastModifiedDate) {
				if data, body, err = withLastModified(data, lastModifiedDate, 0); err != nil {
					return nil, false, err
				}
			}
		}
		if _, err := r.cache.Write(cacheKey, body); err != nil {
			return nil, false, err
		}
	}

	response.Collect()

	return data, false, nil
}

// withLastModified is $data['last-modified'] = $lastModifiedDate followed
// by JsonFile::encode($data, $flags).
func withLastModified(data *php.Array, lastModifiedDate string, flags php.JSONFlag) (*php.Array, string, error) {
	if data == nil {
		data = php.NewArray()
	}
	data.Set("last-modified", lastModifiedDate)
	encoded, err := json.Encode(data, flags, json.IndentDefault)

	return data, encoded, err
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

// degradedWarning shows, once, that the repository could not be reached
// and the cache is used instead.
func (r *ComposerRepository) degradedWarning(err error) {
	if !r.degradedMode {
		r.io.WriteError("<warning>"+util.SanitizeURL(r.url)+" could not be fully loaded ("+err.Error()+"), package information was loaded from the local cache and may be out of date</warning>", true, io.Normal)
	}
	r.degradedMode = true
}

// fetchFileIfLastModified ports fetchFileIfLastModified: the file when it
// changed since lastModifiedTime, else fresh (PHP's true), which is also
// the result when the repository cannot be reached.
func (r *ComposerRepository) fetchFileIfLastModified(filename, cacheKey, lastModifiedTime string) (*php.Array, bool, error) {
	if filename == "" {
		return nil, false, &util.InvalidArgumentError{Site: phperr.At("ComposerRepository.php", 1843), Message: "$filename should not be an empty string"}
	}

	data, fresh, err := r.fetchFileIfLastModifiedAttempt(filename, cacheKey, lastModifiedTime)
	if err == nil {
		return data, fresh, nil
	}
	if isPHPError(err) || isLogic(err) || statusCode(err) == 404 {
		return nil, false, err
	}

	r.degradedWarning(err)

	return nil, true, nil
}

func (r *ComposerRepository) fetchFileIfLastModifiedAttempt(filename, cacheKey, lastModifiedTime string) (*php.Array, bool, error) {
	filename, options, err := r.preFileDownload(filename, 1850, 1860)
	if err != nil {
		return nil, false, err
	}

	response, err := r.httpDownloader.Get(filename, withIfModifiedSince(options, lastModifiedTime))
	if err != nil {
		return nil, false, err
	}
	body := response.Body()
	if body == "" && response.StatusCode() == 304 {
		return nil, true, nil
	}

	if err := r.postFileDownload(pkg.NullString{}, filename, response); err != nil {
		return nil, false, err
	}

	decoded, err := response.DecodeJSON()
	if err != nil {
		return nil, false, err
	}
	if _, err := http.OutputWarnings(r.io, r.url, decoded); err != nil {
		return nil, false, err
	}
	data, _ := decoded.(*php.Array)

	lastModifiedDate, _ := response.Header("last-modified")
	response.Collect()
	if php.ToBool(lastModifiedDate) {
		if data, body, err = withLastModified(data, lastModifiedDate, 0); err != nil {
			return nil, false, err
		}
	}
	if !r.cache.IsReadOnly() {
		if _, err := r.cache.Write(cacheKey, body); err != nil {
			return nil, false, err
		}
	}

	return data, false, nil
}

// fetchResult is asyncFetchFile's resolution: the decoded file (nil for
// PHP's false or a non-array), or fresh (PHP's true: the cached copy is
// fresh).
type fetchResult struct {
	data  *php.Array
	fresh bool
}

// notFoundResult is ['packages' => []].
func notFoundResult() fetchResult {
	return fetchResult{data: php.ArrayOf("packages", php.NewArray())}
}

// asyncFetch is an asyncFetchFile request in flight. The response is
// decoded (and re-encoded with its Last-Modified date for the cache) on
// its own goroutine as soon as it arrives; what Composer's callbacks do
// beyond that (events, warnings, cache writes, the repository's state)
// happens in finishFetch, on the caller's goroutine, in request order.
type asyncFetch struct {
	filename     string // the URL, after PRE_FILE_DOWNLOAD
	cacheKey     string
	lastModified string

	// settled is the result when asyncFetchFile resolved at once.
	settled *fetchResult

	promise *util.Promise[*http.Response]
	done    chan struct{}
	// set by the decoding goroutine
	response  *http.Response
	err       error
	data      *php.Array
	json      string
	decodeErr error
}

// asyncFetchFile ports asyncFetchFile: it starts the request (the
// If-Modified-Since request when lastModifiedTime is not "", null).
func (r *ComposerRepository) asyncFetchFile(filename, cacheKey, lastModifiedTime string) (*asyncFetch, error) {
	if filename == "" {
		return nil, &util.InvalidArgumentError{Site: phperr.At("ComposerRepository.php", 1909), Message: "$filename should not be an empty string"}
	}

	f := &asyncFetch{filename: filename, cacheKey: cacheKey, lastModified: lastModifiedTime}
	if _, ok := r.packagesNotFoundCache[filename]; ok {
		res := notFoundResult()
		f.settled = &res

		return f, nil
	}

	if _, ok := r.freshMetadataUrls[filename]; ok && php.ToBool(lastModifiedTime) {
		// make it look like we got a 304 response
		f.settled = &fetchResult{fresh: true}

		return f, nil
	}

	filename, options, err := r.preFileDownload(filename, 1928, 2012)
	if err != nil {
		return nil, err
	}
	f.filename = filename

	if php.ToBool(lastModifiedTime) {
		options = withIfModifiedSince(options, lastModifiedTime)
	}

	f.promise, err = r.httpDownloader.Add(filename, options)
	if err != nil {
		return nil, err
	}
	f.done = make(chan struct{})
	go f.decode()

	return f, nil
}

// decode waits for the response and decodes it.
func (f *asyncFetch) decode() {
	defer close(f.done)

	f.response, f.err = f.promise.Wait()
	if f.err != nil {
		return
	}
	code := f.response.StatusCode()
	if code == 404 || (code == 304 && f.response.Body() == "") {
		return
	}

	decoded, err := f.response.DecodeJSON()
	if err != nil {
		f.decodeErr = err

		return
	}
	f.data, _ = decoded.(*php.Array)
	f.json = f.response.Body()
	if lastModifiedDate, _ := f.response.Header("last-modified"); php.ToBool(lastModifiedDate) {
		f.data, f.json, f.decodeErr = withLastModified(f.data, lastModifiedDate, php.JSONUnescapedSlashes|php.JSONUnescapedUnicode)
	}
}

// waitFetches is Loop::wait for requests of asyncFetchFile: it drives the
// downloader until they all completed.
func (r *ComposerRepository) waitFetches(fetches []*asyncFetch) {
	for _, f := range fetches {
		if f != nil && f.promise != nil {
			if w, ok := r.httpDownloader.(interface{ Wait() }); ok {
				w.Wait()
			}

			return
		}
	}
}

// finishFetch runs asyncFetchFile's accept or reject callback for a
// completed request.
func (r *ComposerRepository) finishFetch(f *asyncFetch) (fetchResult, error) {
	if f.settled != nil {
		return *f.settled, nil
	}
	<-f.done
	if f.err != nil {
		return r.rejectFetch(f, f.err)
	}

	return r.acceptFetch(f)
}

// acceptFetch is asyncFetchFile's $accept.
func (r *ComposerRepository) acceptFetch(f *asyncFetch) (fetchResult, error) {
	response := f.response
	// package not found is acceptable for a v2 protocol repository
	if response.StatusCode() == 404 {
		r.packagesNotFoundCache[f.filename] = struct{}{}

		return notFoundResult(), nil
	}

	if response.StatusCode() == 304 && response.Body() == "" {
		r.freshMetadataUrls[f.filename] = struct{}{}

		return fetchResult{fresh: true}, nil
	}

	if err := r.postFileDownload(pkg.NullString{}, f.filename, response); err != nil {
		return fetchResult{}, err
	}

	if f.decodeErr != nil {
		return fetchResult{}, f.decodeErr
	}
	if _, err := http.OutputWarnings(r.io, r.url, f.data); err != nil {
		return fetchResult{}, err
	}

	response.Collect()
	if !r.cache.IsReadOnly() {
		if _, err := r.cache.Write(f.cacheKey, f.json); err != nil {
			return fetchResult{}, err
		}
	}
	r.freshMetadataUrls[f.filename] = struct{}{}

	return fetchResult{data: f.data}, nil
}

// rejectFetch is asyncFetchFile's $reject.
func (r *ComposerRepository) rejectFetch(f *asyncFetch, err error) (fetchResult, error) {
	if statusCode(err) == 404 {
		r.packagesNotFoundCache[f.filename] = struct{}{}

		return fetchResult{}, nil
	}

	r.degradedWarning(err)

	// if the file is in the cache, we fake a 304 Not Modified to allow the process to continue
	if php.ToBool(f.lastModified) {
		r.freshMetadataUrls[f.filename] = struct{}{}

		return fetchResult{fresh: true}, nil
	}

	// special error code returned when network is being artificially disabled
	if statusCode(err) == 499 {
		r.packagesNotFoundCache[f.filename] = struct{}{}

		return notFoundResult(), nil
	}

	return fetchResult{}, err
}

// cachedDownload is a startCachedAsyncDownload request.
type cachedDownload struct {
	url         string
	cacheKey    string
	packageName string
	// contents is the cached file, decoded (nil when missing or not an
	// array).
	contents *php.Array
	fetch    *asyncFetch
}

// startCachedAsyncDownloads ports startCachedAsyncDownload for several
// files at once (each fileName with its packageName, "" for the file's
// name): it reads and decodes their cached copies (decoding in parallel)
// and starts the requests, in order. Pass the result to waitFetches, then
// to finishCachedDownload in order.
func (r *ComposerRepository) startCachedAsyncDownloads(fileNames, packageNames []string) ([]*cachedDownload, error) {
	if r.lazyProvidersURL == "" {
		return nil, &util.LogicError{Site: phperr.At("ComposerRepository.php", 1369), Message: "startCachedAsyncDownload only supports v2 protocol composer repos with a metadata-url"}
	}

	downloads := make([]*cachedDownload, len(fileNames))
	cached := make([]string, len(fileNames))
	for i, fileName := range fileNames {
		name := php.Strtolower(fileName)
		packageName := packageNames[i]
		if packageName == "" {
			packageName = name
		}
		d := &cachedDownload{
			url:         strings.ReplaceAll(r.lazyProvidersURL, "%package%", name),
			cacheKey:    "provider-" + php.Strtr(name, "/", "~") + ".json",
			packageName: packageName,
		}
		downloads[i] = d

		contents, ok, err := r.cache.Read(d.cacheKey)
		if err != nil {
			return nil, err
		}
		if ok && php.ToBool(contents) {
			cached[i] = contents
		}
	}

	for i, contents := range parallelDecode(cached) {
		downloads[i].contents = contents
	}

	for _, d := range downloads {
		var err error
		if d.fetch, err = r.asyncFetchFile(d.url, d.cacheKey, php.ToString(get(d.contents, "last-modified"))); err != nil {
			return nil, err
		}
	}

	return downloads, nil
}

// finishCachedDownload ports startCachedAsyncDownload's callback: the
// file's data (nil for null when it has neither the package, nor
// security advisories, nor filter entries) and where it came from.
func (r *ComposerRepository) finishCachedDownload(d *cachedDownload) (*php.Array, string, error) {
	res, err := r.finishFetch(d.fetch)
	if err != nil {
		return nil, "", err
	}

	packagesSource := "downloaded file (" + util.SanitizeURL(d.url) + ")"
	response := res.data
	if res.fresh {
		packagesSource = "cached file (" + d.cacheKey + " originating from " + util.SanitizeURL(d.url) + ")"
		response = d.contents
	}

	if packages, _ := get(response, "packages").(*php.Array); get(packages, d.packageName) == nil && get(response, "security-advisories") == nil && get(response, "filter") == nil {
		return nil, packagesSource, nil
	}

	return response, packagesSource, nil
}

// parallelDecode is json_decode($s, true) of each non-empty string, run
// in parallel: nil for "" and for what does not decode to an array.
func parallelDecode(inputs []string) []*php.Array {
	out := make([]*php.Array, len(inputs))
	work := make(chan int)
	var wg sync.WaitGroup
	for range min(runtime.GOMAXPROCS(0), len(inputs)) {
		wg.Go(func() {
			for i := range work {
				out[i] = decodeArray(inputs[i])
			}
		})
	}
	for i, s := range inputs {
		if s != "" {
			work <- i
		}
	}
	close(work)
	wg.Wait()

	return out
}

// statusCode is $e->getStatusCode() of a TransportException, 0 (null)
// for other errors.
func statusCode(err error) int {
	if transport, ok := errors.AsType[*util.TransportError](err); ok {
		return transport.StatusCode
	}

	return 0
}

// isLogic is $e instanceof \LogicException.
func isLogic(err error) bool {
	var logic *util.LogicError

	return errors.As(err, &logic)
}

// isPHPError reports whether err is a PHP \Error (TypeError, ValueError),
// which `catch (\Exception $e)` does not catch.
func isPHPError(err error) bool {
	var typeErr *pkg.TypeError
	var valueErr *semver.ValueError

	return errors.As(err, &typeErr) || errors.As(err, &valueErr)
}

// exceptionClass is get_class($e) for the exceptions package loading
// raises.
func exceptionClass(err error) string {
	class, _ := util.PHPClassOf(err)

	return class
}

// RuntimeError is the \RuntimeException ComposerRepository raises with a
// previous exception (package loading failures). errors.As finds a
// *util.RuntimeError in it, not the previous exception, as PHP's catch
// blocks do not see it either.
type RuntimeError struct {
	Message  string
	Previous error
	phperr.Site
}

func newRuntimeError(site phperr.Site, message string, previous error) *RuntimeError {
	return &RuntimeError{Message: message, Previous: previous, Site: site}
}

// PHPPrevious implements phperr.Chained.
func (e *RuntimeError) PHPPrevious() error { return e.Previous }

// transportErrorAt is new TransportException($message, $code) at site.
func transportErrorAt(site phperr.Site, message string, code int) *util.TransportError {
	e := util.NewTransportError(message, code)
	e.Site = site

	return e
}

func (e *RuntimeError) Error() string { return e.Message }

// Unwrap returns the exception as a *util.RuntimeError.
func (e *RuntimeError) Unwrap() error { return &util.RuntimeError{Message: e.Message} }
