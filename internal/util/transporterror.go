// Ports src/Composer/Downloader/TransportException.php,
// src/Composer/Downloader/MaxFileSizeExceededException.php and
// src/Composer/Exception/IrrecoverableDownloadException.php.
//
// They live here rather than with the downloaders because Composer\Util's
// HTTP layer throws them and everything above catches them.

package util

// TransportError is Composer\Downloader\TransportException, a
// RuntimeException carrying what is known about the failed HTTP response.
type TransportError struct {
	Message string
	// Code is the exception code (400 unless given), which
	// Application::doRun turns into the exit code.
	Code int
	// Previous is getPrevious() (phperr.Chained). Unwrap must not return
	// it: `catch (TransportException $e)` does not look at it.
	Previous error

	// Headers are the raw response header lines; nil is PHP's null.
	Headers []string
	// Response is the response body; nil is PHP's null.
	Response *string
	// StatusCode is the HTTP status; 0 is PHP's null.
	StatusCode int
	// ResponseInfo describes the transfer (curl_getinfo); nil when unknown.
	ResponseInfo *TransferInfo
	// Curl is the failure of a transfer that ended before a response, as
	// curl numbers and words it; nil otherwise. What failed is read from
	// it (IsTimeout, IsResolveFailure), never from Message, which is
	// maestro's own wording.
	Curl *CurlFailure
}

// The curl error numbers maestro's hints and retries tell apart.
const (
	CurleCouldntResolveHost = 6
	CurleOperationTimedout  = 28
)

// CurlFailure is a transfer that failed before a response, as curl
// reports it.
type CurlFailure struct {
	// Errno is the CURLE_* number.
	Errno int
	// Message is curl's own text, without maestro's framing ("curl error
	// N while downloading URL: ").
	Message string
	// Timeout says, for a CURLE_OPERATION_TIMEDOUT, what timed out (curl's
	// "Resolving", "Connection" and "Operation timed out").
	Timeout CurlTimeout
	// Reset is set when the peer reset the connection ("Connection reset
	// by peer", under CURLE_RECV_ERROR or CURLE_SSL_CONNECT_ERROR).
	Reset bool
	// VerifyResult is OpenSSL's verify result (X509_V_ERR_*) for a peer
	// certificate it rejected (CURLE_PEER_FAILED_VERIFICATION), 0
	// otherwise.
	VerifyResult int
}

// X509VErrUnableToGetIssuerCertLocally is OpenSSL's verify result for a
// certificate whose issuer is not in the CA bundle.
const X509VErrUnableToGetIssuerCertLocally = 20

// CurlTimeout is what a CURLE_OPERATION_TIMEDOUT timed out on.
type CurlTimeout int

// What timed out.
const (
	// TimeoutNone is any other failure.
	TimeoutNone CurlTimeout = iota
	// TimeoutResolving is resolving the host.
	TimeoutResolving
	// TimeoutConnecting is connecting to it.
	TimeoutConnecting
	// TimeoutTransfer is the transfer once connected.
	TimeoutTransfer
)

// IsTimeout reports whether the transfer timed out, in any phase
// (CURLE_OPERATION_TIMEDOUT).
func (e *TransportError) IsTimeout() bool {
	return e.Curl != nil && e.Curl.Errno == CurleOperationTimedout
}

// IsTransferTimeout reports whether the transfer timed out once connected
// (curl's "Operation timed out").
func (e *TransportError) IsTransferTimeout() bool {
	return e.Curl != nil && e.Curl.Timeout == TimeoutTransfer
}

// IsResolveFailure reports whether the host could not be resolved, or
// resolving it timed out.
func (e *TransportError) IsResolveFailure() bool {
	return e.Curl != nil && (e.Curl.Errno == CurleCouldntResolveHost || e.Curl.Timeout == TimeoutResolving)
}

// NewTransportError is new TransportException($message, $code).
func NewTransportError(message string, code int) *TransportError {
	return &TransportError{Message: message, Code: code}
}

func (e *TransportError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (*TransportError) PHPClass() string { return `Composer\Downloader\TransportException` }

// PHPCode implements phperr.Coded.
func (e *TransportError) PHPCode() int { return e.Code }

// PHPPrevious implements phperr.Chained.
func (e *TransportError) PHPPrevious() error { return e.Previous }

// SetResponse is setResponse(?string).
func (e *TransportError) SetResponse(body string) { e.Response = &body }

// TransferInfo is the subset of curl_getinfo() Composer and maestro read
// about a transfer.
type TransferInfo struct {
	URL      string
	HTTPCode int
	// PrimaryIP is the IP the connection went to, "" before connecting.
	PrimaryIP string
	// ErrorCode is the curl error number of a failed transfer.
	ErrorCode int
	// SizeDownload counts the bytes received on the wire (before content
	// decoding), DownloadContentLength the announced Content-Length, -1
	// when there was none.
	SizeDownload          int64
	DownloadContentLength int64
	// TotalTime is the transfer's duration in seconds.
	TotalTime float64
}

// MaxFileSizeExceededError is Composer\Downloader\MaxFileSizeExceededException,
// a TransportException: errors.As finds both it and its *TransportError.
type MaxFileSizeExceededError struct {
	*TransportError
}

// NewMaxFileSizeExceededError is new MaxFileSizeExceededException($message).
func NewMaxFileSizeExceededError(message string) *MaxFileSizeExceededError {
	return &MaxFileSizeExceededError{TransportError: NewTransportError(message, 400)}
}

// Unwrap exposes the TransportException part.
func (e *MaxFileSizeExceededError) Unwrap() error { return e.TransportError }

// PHPClass implements phperr.Exception (not the TransportException's).
func (*MaxFileSizeExceededError) PHPClass() string {
	return `Composer\Downloader\MaxFileSizeExceededException`
}

// IrrecoverableDownloadError is
// Composer\Exception\IrrecoverableDownloadException, a RuntimeException
// that stops the download manager from trying other sources.
type IrrecoverableDownloadError struct {
	Message string
}

func (e *IrrecoverableDownloadError) Error() string { return e.Message }

// PHPClass implements phperr.Exception.
func (*IrrecoverableDownloadError) PHPClass() string {
	return `Composer\Exception\IrrecoverableDownloadException`
}
