// Ports src/Composer/Util/RemoteFilesystem.php.
//
// Composer (with ext-curl, the reference) only routes requests through
// RemoteFilesystem that curl does not take: URLs that are not http(s)
// (file:// paths of local repositories) and requests whose options set
// ssl.allow_self_signed. Those go through PHP's stream wrappers, which
// this port reproduces: local files are read directly, http(s) uses the
// shared net/http transfer engine with the stream wrapper's behaviour
// (HTTP/1.1, Connection: close, gzip decoded by RemoteFilesystem itself,
// $http_response_header style status lines).

package http

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"math"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// PHP stream notification codes RemoteFilesystem handles.
const (
	StreamNotifyFailure    = 9
	StreamNotifyFileSizeIs = 5
	StreamNotifyProgress   = 7
)

// Authenticator is the part of AuthHelper RemoteFilesystem uses;
// *AuthHelper implements it.
type Authenticator interface {
	PromptAuthIfNeeded(url, origin string, statusCode int, reason string, headers []string, retryCount int, responseBody string) (AuthResult, error)
	StoreAuth(origin string, storeAuth StoreAuth) error
	AddAuthenticationOptions(options *php.Array, origin, url string) *php.Array
}

// remoteContents is what getRemoteContents() produces: the content
// (false when ok is false), the response headers and the warnings PHP
// would have raised.
type remoteContents struct {
	result   string
	ok       bool
	headers  []string
	warnings []string
}

// RemoteFilesystem ports Composer\Util\RemoteFilesystem. A
// RemoteFilesystem performs one request at a time.
type RemoteFilesystem struct {
	io         mio.IO
	config     Config
	rt         Runtime
	authHelper Authenticator
	pool       transportPool

	mu sync.Mutex

	scheme       string
	bytesMax     int64
	originURL    string
	fileURL      string
	fileName     string
	hasFileName  bool
	retry        bool
	progress     bool
	lastProgress int
	options      *php.Array
	disableTLS   bool
	lastHeaders  []string
	storeAuth    StoreAuth
	degradedMode bool
	redirects    int
	maxRedirects int

	// getRemoteContents is getRemoteContents(); tests replace it.
	getRemoteContents func(originURL, fileURL string, ctx *php.Array, maxFileSize int64) (remoteContents, error)
}

// NewRemoteFilesystem is new RemoteFilesystem($io, $config, $options,
// $disableTls, $authHelper); a nil authHelper is a new AuthHelper.
func NewRemoteFilesystem(ioi mio.IO, config Config, options *php.Array, disableTLS bool, authHelper Authenticator) (*RemoteFilesystem, error) {
	r := &RemoteFilesystem{io: ioi, config: config, rt: defaultRuntime, options: php.NewArray(), maxRedirects: 20, lastProgress: -1}

	// Setup TLS options. The cafile option can be set via config.json.
	if !disableTLS {
		defaults, err := GetTLSDefaults(options, ioi)
		if err != nil {
			return nil, err
		}

		r.options = defaults
	} else {
		r.disableTLS = true
	}

	// handle the other externally set options normally.
	if options != nil {
		r.options = php.ArrayReplaceRecursive(r.options, options).Clone()
	}

	if authHelper == nil {
		authHelper = NewAuthHelper(ioi, config)
	}

	r.authHelper = authHelper
	r.getRemoteContents = r.remoteContents

	return r, nil
}

// Copy is copy($originUrl, $fileUrl, $fileName, $progress, $options):
// download fileURL into fileName.
func (r *RemoteFilesystem) Copy(originURL, fileURL, fileName string, progress bool, options *php.Array) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, ok, err := r.get(originURL, fileURL, options, fileName, true, progress)

	return ok, err
}

// GetContents is getContents($originUrl, $fileUrl, $progress, $options).
func (r *RemoteFilesystem) GetContents(originURL, fileURL string, progress bool, options *php.Array) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	result, _, err := r.get(originURL, fileURL, options, "", false, progress)

	return result, err
}

// Options is getOptions().
func (r *RemoteFilesystem) Options() *php.Array {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.options.Clone()
}

// SetOptions is setOptions($options).
func (r *RemoteFilesystem) SetOptions(options *php.Array) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.options = php.ArrayReplaceRecursive(r.options, options).Clone()
}

// IsTLSDisabled is isTlsDisabled().
func (r *RemoteFilesystem) IsTLSDisabled() bool { return r.disableTLS }

// LastHeaders is getLastHeaders().
func (r *RemoteFilesystem) LastHeaders() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.lastHeaders
}

var statusCodeRegex = php.MustCompile(`{^HTTP/\S+ (\d+)}i`)

// FindStatusCode is RemoteFilesystem::findStatusCode($headers): the status
// of the last response among the headers; false for null.
func FindStatusCode(headers []string) (int, bool) {
	value, found := 0, false

	for _, header := range headers {
		if m, _ := statusCodeRegex.Match(header); m != nil {
			value, found = int(php.ToInt(m.Get(1))), true
		}
	}

	return value, found
}

// FindStatusMessage is findStatusMessage($headers); false for null.
func (r *RemoteFilesystem) FindStatusMessage(headers []string) (string, bool) {
	return findStatusMessage(headers)
}

// get is get(): the content (or for copies "1"), whether it is not false,
// and the error PHP throws.
//
//nolint:gocyclo,cyclop,funlen // a faithful port of Composer's get()
func (r *RemoteFilesystem) get(originURL, fileURL string, additionalOptions *php.Array, fileName string, hasFileName, progress bool) (string, bool, error) {
	r.scheme = util.URLScheme(strings.ReplaceAll(fileURL, `\`, "/"))
	r.bytesMax = 0
	r.originURL = originURL
	r.fileURL = fileURL
	r.fileName = fileName
	r.hasFileName = hasFileName
	r.progress = progress
	r.lastProgress = -1
	retryAuthFailure := true
	r.lastHeaders = []string{}
	r.redirects = 1 // The first request counts.

	tempAdditionalOptions := cloneOptions(additionalOptions)
	if v, ok := path(tempAdditionalOptions, "retry-auth-failure"); ok {
		retryAuthFailure = php.ToBool(v)
		tempAdditionalOptions.Delete("retry-auth-failure")
	}

	isRedirect := false
	if v, ok := path(tempAdditionalOptions, "redirects"); ok {
		r.redirects = int(php.ToInt(v))
		isRedirect = true
		tempAdditionalOptions.Delete("redirects")
	}

	options := r.optionsForURL(originURL, tempAdditionalOptions)
	origFileURL := fileURL

	if _, ok := path(options, "prevent_ip_access_callable"); ok {
		return "", false, &util.RuntimeError{Message: "RemoteFilesystem doesn't support the 'prevent_ip_access_callable' config."}
	}

	if _, ok := path(options, "prevent_url_access_callable"); ok {
		return "", false, &util.RuntimeError{Message: "RemoteFilesystem doesn't support the 'prevent_url_access_callable' config."}
	}

	if token, ok := optionString(options, "gitlab-token"); ok {
		sep := "?"
		if strings.Contains(fileURL, "?") {
			sep = "&"
		}

		fileURL += sep + "access_token=" + token
		options.Delete("gitlab-token")
	}

	if _, ok := path(options, "http"); ok {
		httpArray(options).Set("ignore_errors", true)
	}

	degradedPackagist := false

	if r.degradedMode && strings.HasPrefix(fileURL, "http://repo.packagist.org/") {
		// access packagist using the resolved IPv4 instead of the hostname
		// to force IPv4 protocol
		fileURL = "http://" + gethostbyname("repo.packagist.org") + fileURL[20:]
		degradedPackagist = true
	}

	var maxFileSize int64 = -1

	if v, ok := path(options, "max_file_size"); ok {
		maxFileSize = php.ToInt(v)
		options.Delete("max_file_size")
	}

	ctx, err := GetContext(fileURL, options, r.rt)
	if err != nil {
		return "", false, err
	}

	proxy, err := GetProxyManager().ProxyForRequest(fileURL)
	if err != nil {
		return "", false, err
	}

	usingProxy, _ := proxy.StatusFormat(" using proxy (%s)")

	verb := "Reading "
	if strings.HasPrefix(origFileURL, "http") {
		verb = "Downloading "
	}

	r.io.WriteError(verb+util.SanitizeURL(origFileURL)+usingProxy, true, mio.Debug)

	// Check for secure HTTP, but allow insecure Packagist calls to $hashed
	// providers as file integrity is verified with sha256
	if ok, _ := insecurePackagistRegex.IsMatch(fileURL); (!ok || (!strings.Contains(fileURL, "$") && !strings.Contains(fileURL, "%24"))) && !degradedPackagist {
		if err := r.config.ProhibitURLByConfig(fileURL, r.io, nil); err != nil {
			return "", false, err
		}
	}

	if r.progress && !isRedirect {
		r.io.WriteError("Downloading (<comment>connecting...</comment>)", false, mio.Normal)
	}

	var (
		errorMessage string
		caught       error
		headers      []string
		result       string
		resultOK     bool
	)

	contents, err := r.getRemoteContents(originURL, fileURL, ctx, maxFileSize)
	errorMessage = strings.Join(contents.warnings, "\n")

	if err == nil {
		headers = contents.headers
		result, resultOK = contents.result, contents.ok
	}

	if err == nil && len(headers) > 0 && headers[0] != "" {
		statusCode, _ := FindStatusCode(headers)

		if contentType, ok := FindHeaderValue(headers, "content-type"); statusCode >= 300 && ok && contentType == "application/json" {
			data, _ := php.JSONDecode(result, true)
			_, err = OutputWarnings(r.io, originURL, data)
		}

		if err == nil && (statusCode == 401 || statusCode == 403) && retryAuthFailure {
			statusMessage, _ := findStatusMessage(headers)
			err = r.promptAuthAndRetry(statusCode, statusMessage, headers)
		}
	}

	if err == nil && len(headers) > 0 && headers[0] != "" {
		contentLength, ok := FindHeaderValue(headers, "content-length")
		if ok && php.ToBool(contentLength) && php.Compare(int64(len(result)), contentLength) < 0 {
			// alas, this is not possible via the stream callback because
			// STREAM_NOTIFY_COMPLETED is documented, but not implemented
			// anywhere in PHP
			e := util.NewTransportError("Content-Length mismatch, received "+strconv.Itoa(len(result))+" bytes out of the expected "+contentLength+" for "+util.SanitizeURL(fileURL), 400)
			e.Headers = headers
			e.StatusCode, _ = FindStatusCode(headers)

			if decoded, decodedOK, derr := r.decodeResult(result, resultOK, headers); derr == nil {
				if decodedOK {
					e.SetResponse(decoded)
				}
			} else if resultOK {
				e.SetResponse(result)
			}

			r.io.WriteError("Content-Length mismatch, received "+strconv.Itoa(len(result))+" out of "+contentLength+" bytes: ("+base64.StdEncoding.EncodeToString([]byte(result))+")", true, mio.Debug)
			err = e
		}
	}

	if err != nil {
		if te, ok := errors.AsType[*util.TransportError](err); ok {
			if len(headers) > 0 && headers[0] != "" {
				te.Headers = headers
				te.StatusCode, _ = FindStatusCode(headers)
			}

			if resultOK {
				decoded, decodedOK, derr := r.decodeResult(result, resultOK, headers)
				if derr != nil {
					return "", false, derr
				}

				if decodedOK {
					te.SetResponse(decoded)
				}
			}
		}

		caught = err
		result, resultOK = "", false
	}

	if caught != nil && !r.retry {
		if !r.degradedMode && strings.Contains(caught.Error(), "Operation timed out") {
			return r.retryDegraded(caught.Error(), additionalOptions)
		}

		return "", false, caught
	}

	var (
		statusCode     int
		contentType    string
		locationHeader string
	)

	if len(headers) > 0 && headers[0] != "" {
		statusCode, _ = FindStatusCode(headers)
		contentType, _ = FindHeaderValue(headers, "content-type")
		locationHeader, _ = FindHeaderValue(headers, "location")
	}

	// check for bitbucket login page asking to authenticate
	if originURL == "bitbucket.org" &&
		!IsPublicBitBucketDownload(fileURL) &&
		strings.HasSuffix(fileURL, ".zip") &&
		(!php.ToBool(locationHeader) || !strings.HasSuffix(util.URLPath(locationHeader), ".zip")) &&
		php.ToBool(contentType) {
		if isHTML, _ := textHTMLRegex.IsMatch(contentType); isHTML {
			result, resultOK = "", false

			if retryAuthFailure {
				if err := r.promptAuthAndRetry(401, "", nil); err != nil {
					return "", false, err
				}
			}
		}
	}

	// check for gitlab 404 when downloading archives
	if statusCode == 404 && configHas(r.config, "gitlab-domains", originURL) && strings.Contains(fileURL, "archive.zip") {
		result, resultOK = "", false

		if retryAuthFailure {
			if err := r.promptAuthAndRetry(401, "", nil); err != nil {
				return "", false, err
			}
		}
	}

	// handle 3xx redirects, 304 Not Modified is excluded
	hasFollowedRedirect := false

	if statusCode >= 300 && statusCode <= 399 && statusCode != 304 && r.redirects < r.maxRedirects {
		hasFollowedRedirect = true

		if result, resultOK, err = r.handleRedirect(headers, additionalOptions, result, resultOK); err != nil {
			return "", false, err
		}
	}

	// fail 4xx and 5xx responses and capture the response
	if statusCode >= 400 && statusCode <= 599 {
		if !r.retry {
			if r.progress && !isRedirect {
				r.io.OverwriteError("Downloading (<error>failed</error>)", false, -1, mio.Normal)
			}

			e := util.NewTransportError(`The "`+r.fileURL+`" file could not be downloaded (`+headers[0]+`)`, statusCode)
			e.Headers = headers

			decoded, decodedOK, derr := r.decodeResult(result, resultOK, headers)
			if derr != nil {
				return "", false, derr
			}

			if decodedOK {
				e.SetResponse(decoded)
			}

			e.StatusCode = statusCode

			return "", false, e
		}

		result, resultOK = "", false
	}

	if r.progress && !r.retry && !isRedirect {
		state := "<comment>100%</comment>"
		if !resultOK {
			state = "<error>failed</error>"
		}

		r.io.OverwriteError("Downloading ("+state+")", false, -1, mio.Normal)
	}

	// decode gzip
	if resultOK && php.ToBool(result) && strings.HasPrefix(fileURL, "http") && !hasFollowedRedirect {
		decoded, decodedOK, err := r.decodeResult(result, resultOK, headers)
		if err != nil {
			if r.degradedMode {
				return "", false, err
			}

			r.degradedMode = true
			r.io.WriteErrorMessages([]string{
				"",
				"<error>Failed to decode response: " + err.Error() + "</error>",
				"<error>Retrying with degraded mode, check https://getcomposer.org/doc/articles/troubleshooting.md#degraded-mode for more info</error>",
			}, true, mio.Normal)

			return r.get(r.originURL, r.fileURL, additionalOptions, r.fileName, r.hasFileName, r.progress)
		}

		result, resultOK = decoded, decodedOK
	}

	// handle copy command if download was successful
	if resultOK && hasFileName && !isRedirect {
		if result == "" {
			return "", false, util.NewTransportError(`"`+r.fileURL+`" appears broken, and returned an empty 200 response`, 400)
		}

		if err := os.WriteFile(fileName, []byte(result), 0o666); err != nil {
			return "", false, util.NewTransportError(`The "`+r.fileURL+`" file could not be written to `+fileName+": Failed to open stream: "+util.Strerror(err), 400)
		}

		result = "1"
	}

	if r.retry {
		r.retry = false

		result, resultOK, err = r.get(r.originURL, r.fileURL, additionalOptions, r.fileName, r.hasFileName, r.progress)
		if err != nil {
			return "", false, err
		}

		if r.storeAuth != StoreAuthNo {
			if err := r.authHelper.StoreAuth(r.originURL, r.storeAuth); err != nil {
				return "", false, err
			}

			r.storeAuth = StoreAuthNo
		}

		return result, resultOK, nil
	}

	if !resultOK {
		e := util.NewTransportError(`The "`+r.fileURL+`" file could not be downloaded: `+errorMessage, 0)
		if len(headers) > 0 && headers[0] != "" {
			e.Headers = headers
		}

		if !r.degradedMode && strings.Contains(e.Message, "Operation timed out") {
			return r.retryDegraded(e.Message, additionalOptions)
		}

		return "", false, e
	}

	if len(headers) > 0 && headers[0] != "" {
		r.lastHeaders = headers
	}

	return result, true, nil
}

// retryDegraded switches to degraded mode after a timeout and tries again.
func (r *RemoteFilesystem) retryDegraded(message string, additionalOptions *php.Array) (string, bool, error) {
	r.degradedMode = true
	r.io.WriteError("", true, mio.Normal)
	r.io.WriteErrorMessages([]string{
		"<error>" + message + "</error>",
		"<error>Retrying with degraded mode, check https://getcomposer.org/doc/articles/troubleshooting.md#degraded-mode for more info</error>",
	}, true, mio.Normal)

	return r.get(r.originURL, r.fileURL, additionalOptions, r.fileName, r.hasFileName, r.progress)
}

// gethostbyname resolves a host to its first IPv4 address, or returns it
// unchanged.
func gethostbyname(host string) string {
	ips, err := net.DefaultResolver.LookupIP(context.Background(), "ip4", host)
	if err != nil || len(ips) == 0 {
		return host
	}

	return ips[0].String()
}

// CallbackGet is callbackGet(): the stream notification handler.
func (r *RemoteFilesystem) CallbackGet(notificationCode, _ int, message string, messageCode int, bytesTransferred, bytesMax int64) error {
	switch notificationCode {
	case StreamNotifyFailure:
		if messageCode == 400 {
			// This might happen if your host is secured by ssl client
			// certificate authentication but you do not send an
			// appropriate certificate
			return util.NewTransportError("The '"+r.fileURL+"' URL could not be accessed: "+message, messageCode)
		}
	case StreamNotifyFileSizeIs:
		r.bytesMax = bytesMax
	case StreamNotifyProgress:
		if r.bytesMax > 0 && r.progress {
			progression := min(100, int(math.Round(float64(bytesTransferred)/float64(r.bytesMax)*100)))

			if progression%5 == 0 && progression != 100 && progression != r.lastProgress {
				r.lastProgress = progression
				r.io.OverwriteError("Downloading (<comment>"+strconv.Itoa(progression)+"%</comment>)", false, -1, mio.Normal)
			}
		}
	}

	return nil
}

// promptAuthAndRetry is promptAuthAndRetry(): an error "RETRY" when the
// request is to be made again with new credentials.
func (r *RemoteFilesystem) promptAuthAndRetry(httpStatus int, reason string, headers []string) error {
	// always pass 1 as RemoteFilesystem is single threaded there is no race
	// condition possible
	result, err := r.authHelper.PromptAuthIfNeeded(r.fileURL, r.originURL, httpStatus, reason, headers, 1, "")
	if err != nil {
		return err
	}

	r.storeAuth = result.StoreAuth
	r.retry = result.Retry

	if r.retry {
		return util.NewTransportError("RETRY", 400)
	}

	return nil
}

// optionsForURL is getOptionsForUrl().
func (r *RemoteFilesystem) optionsForURL(originURL string, additionalOptions *php.Array) *php.Array {
	headers := []string{"Accept-Encoding: gzip"}

	options := php.ArrayReplaceRecursive(r.options, cloneOptions(additionalOptions)).Clone()

	if !r.degradedMode {
		// degraded mode disables HTTP/1.1 which causes issues with some bad
		// proxies/software due to the use of chunked encoding
		httpArray(options).Set("protocol_version", 1.1)
		headers = append(headers, "Connection: close")
	}

	if h, ok := path(options, "http", "header"); ok {
		if s, isString := h.(string); isString {
			httpArray(options).Set("header", php.StringList(splitCRLF(php.TrimSet(s, "\r\n"))))
		}
	}

	options = r.authHelper.AddAuthenticationOptions(options, originURL, r.fileURL)

	httpArray(options).Set("follow_location", 0)

	for _, header := range headers {
		appendHeader(options, header)
	}

	return options
}

// handleRedirect is handleRedirect(): the result of following the Location
// header.
func (r *RemoteFilesystem) handleRedirect(responseHeaders []string, additionalOptions *php.Array, result string, resultOK bool) (string, bool, error) {
	var targetURL string

	if locationHeader, ok := FindHeaderValue(responseHeaders, "location"); ok && php.ToBool(locationHeader) {
		switch {
		case util.URLScheme(locationHeader) != "":
			// Absolute URL; e.g. https://example.com/composer
			targetURL = locationHeader
		case util.URLHost(locationHeader) != "":
			// Scheme relative; e.g. //example.com/foo
			targetURL = r.scheme + ":" + locationHeader
		case locationHeader[0] == '/':
			// Absolute path; e.g. /foo
			re, err := php.Compile(`{^(.+(?://|@)` + php.PregQuote(util.URLHost(r.fileURL), "") + `(?::\d+)?)(?:[/\?].*)?$}`)
			if err != nil {
				return "", false, err
			}

			if targetURL, _, err = re.Replace(r.fileURL, `\1`+locationHeader, -1); err != nil {
				return "", false, err
			}
		default:
			// Relative path; e.g. foo
			var err error
			if targetURL, _, err = relativePathRegex.Replace(r.fileURL, `\1`+locationHeader, -1); err != nil {
				return "", false, err
			}
		}
	}

	if php.ToBool(targetURL) {
		if !util.IsAllowedRedirect(targetURL) {
			return "", false, util.NewTransportError(`Could not follow the redirect to "`+util.SanitizeURL(targetURL)+`" because only http and https redirects are supported.`, 400)
		}

		r.redirects++
		r.io.WriteError("", true, mio.Debug)
		r.io.WriteError("Following redirect ("+strconv.Itoa(r.redirects)+") "+util.SanitizeURL(targetURL), true, mio.Debug)

		options := cloneOptions(additionalOptions)
		options.Set("redirects", r.redirects)

		return r.get(util.URLHost(targetURL), targetURL, options, r.fileName, r.hasFileName, r.progress)
	}

	if !r.retry {
		e := util.NewTransportError(`The "`+r.fileURL+`" file could not be downloaded, got redirect without Location (`+responseHeaders[0]+`)`, 400)
		e.Headers = responseHeaders

		decoded, decodedOK, err := r.decodeResult(result, resultOK, responseHeaders)
		if err != nil {
			return "", false, err
		}

		if decodedOK {
			e.SetResponse(decoded)
		}

		return "", false, e
	}

	return "", false, nil
}

// decodeResult is decodeResult(): the gzip content encoding undone; ok is
// false for null.
func (r *RemoteFilesystem) decodeResult(result string, ok bool, responseHeaders []string) (string, bool, error) {
	if ok && php.ToBool(result) {
		contentEncoding, found := FindHeaderValue(responseHeaders, "content-encoding")
		if found && php.ToBool(contentEncoding) && strings.ToLower(contentEncoding) == "gzip" {
			decoded, err := zlibDecode([]byte(result))
			if err != nil {
				return "", false, util.NewTransportError("Failed to decode zlib stream", 400)
			}

			return string(decoded), true, nil
		}
	}

	return result, ok, nil
}

// zlibDecode is zlib_decode(): gzip, zlib or raw deflate data inflated.
func zlibDecode(data []byte) ([]byte, error) {
	var (
		rd  io.Reader
		err error
	)

	switch {
	case len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b:
		rd, err = gzip.NewReader(bytes.NewReader(data))
	case len(data) >= 2 && data[0]&0x0f == 8 && (uint16(data[0])<<8|uint16(data[1]))%31 == 0:
		rd, err = zlib.NewReader(bytes.NewReader(data))
	default:
		rd = flate.NewReader(bytes.NewReader(data))
	}

	if err != nil {
		return nil, err
	}

	return io.ReadAll(rd)
}

// remoteContents is getRemoteContents(): file_get_contents($fileUrl) with
// the stream context.
func (r *RemoteFilesystem) remoteContents(_, fileURL string, ctx *php.Array, maxFileSize int64) (remoteContents, error) {
	var out remoteContents

	scheme, rest, hasScheme := strings.Cut(fileURL, "://")
	if !hasScheme || len(scheme) == 1 {
		scheme, rest = "file", fileURL
	}

	switch strings.ToLower(scheme) {
	case "http", "https":
		return r.streamHTTP(fileURL, ctx, maxFileSize)
	case "file":
	default:
		out.warnings = append(out.warnings, `Unable to find the wrapper "`+scheme+`" - did you forget to enable it when you configured PHP?`)
		rest = fileURL
	}

	data, err := readFileLimit(rest, maxFileSize)
	if err != nil {
		out.warnings = append(out.warnings, "Failed to open stream: "+util.Strerror(err))

		return out, nil
	}

	out.result, out.ok = string(data), true

	if maxFileSize >= 0 && int64(len(data)) >= maxFileSize {
		return out, util.NewMaxFileSizeExceededError("Maximum allowed download size reached. Downloaded " + strconv.Itoa(len(data)) + " of allowed " + strconv.FormatInt(maxFileSize, 10) + " bytes for " + util.SanitizeURL(fileURL))
	}

	return out, nil
}

// readFileLimit reads a file, at most limit bytes when limit >= 0.
func readFileLimit(path string, limit int64) ([]byte, error) {
	if limit < 0 {
		return os.ReadFile(path)
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(io.LimitReader(f, limit))
}

// streamHTTP performs an http(s) request as PHP's http stream wrapper
// does with Composer's context options.
func (r *RemoteFilesystem) streamHTTP(fileURL string, ctx *php.Array, maxFileSize int64) (remoteContents, error) {
	var out remoteContents

	ssl, _ := arrayValue(ctx, "ssl").(*php.Array)
	req := &transferRequest{
		url:            fileURL,
		connectTimeout: 60 * time.Second,
		readTimeout:    60 * time.Second, // default_socket_timeout
		safeURL:        util.SanitizeURL(fileURL),
		key:            transportKey{tls: tlsFromOptions(ssl), http1: true},
	}

	if maxFileSize >= 0 {
		req.limit = max(maxFileSize, 1)
	}

	if v, ok := optionString(ctx, "http", "method"); ok {
		req.method = v
	}

	if v, ok := path(ctx, "http", "content"); ok {
		content := php.ToString(v)
		req.content = &content
	}

	if v, ok := path(ctx, "http", "timeout"); ok {
		req.readTimeout = time.Duration(php.ToFloat(v) * float64(time.Second))
	}

	isHTTPS := strings.HasPrefix(strings.ToLower(fileURL), "https://")

	for _, line := range headerList(ctx) {
		if hasPrefixFold(line, "proxy-authorization:") && isHTTPS {
			// sent with the CONNECT request, as the wrapper does
			req.key.proxyHeader = strings.TrimSpace(line[len("proxy-authorization:"):])

			continue
		}

		req.headers = append(req.headers, line)
	}

	if proxy, ok := optionString(ctx, "http", "proxy"); ok {
		switch {
		case strings.HasPrefix(proxy, "tcp://"):
			req.key.proxy = "http://" + proxy[len("tcp://"):]
		case strings.HasPrefix(proxy, "ssl://"):
			req.key.proxy = "https://" + proxy[len("ssl://"):]
		default:
			req.key.proxy = proxy
		}
	}

	res := r.pool.do(context.Background(), req)

	if res.err != nil {
		return out, res.err
	}

	if res.errno != 0 {
		out.warnings = append(out.warnings, "Failed to open stream: "+res.errMsg)

		return out, nil
	}

	if maxFileSize >= 0 && int64(len(res.body)) > maxFileSize {
		res.body = res.body[:maxFileSize]
	}

	out.headers = res.headers
	out.result, out.ok = string(res.body), true

	if res.status > 0 {
		r.bytesMax = res.info.DownloadContentLength
	}

	if maxFileSize >= 0 && int64(len(res.body)) >= maxFileSize {
		return out, util.NewMaxFileSizeExceededError("Maximum allowed download size reached. Downloaded " + strconv.Itoa(len(res.body)) + " of allowed " + strconv.FormatInt(maxFileSize, 10) + " bytes for " + util.SanitizeURL(fileURL))
	}

	return out, nil
}
