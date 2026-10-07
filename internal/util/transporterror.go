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
}

// NewTransportError is new TransportException($message, $code).
func NewTransportError(message string, code int) *TransportError {
	return &TransportError{Message: message, Code: code}
}

func (e *TransportError) Error() string { return e.Message }

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

// IrrecoverableDownloadError is
// Composer\Exception\IrrecoverableDownloadException, a RuntimeException
// that stops the download manager from trying other sources.
type IrrecoverableDownloadError struct {
	Message string
}

func (e *IrrecoverableDownloadError) Error() string { return e.Message }
