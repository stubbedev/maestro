// How the Application presents the errors it knows more about than the
// console does. Their wording is maestro's own (docs/PORTING.md "The
// contract", #13); the errors themselves, which plugins see, keep
// Composer's messages.

package command

import (
	"errors"
	"strconv"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/ui"
	"github.com/stubbedev/maestro/internal/util"
)

// curlCauses say what the curl errors Composer reports mean, by curl
// error number.
var curlCauses = map[int]string{
	5:  "could not resolve the proxy host",
	6:  "could not resolve the host",
	7:  "could not connect to the server",
	16: "the HTTP/2 connection failed",
	28: "the connection timed out",
	35: "the TLS/SSL handshake failed",
	47: "there were too many redirects",
	52: "the server sent an empty response",
	55: "sending data to the server failed",
	56: "receiving data from the server failed",
	60: "the server's TLS/SSL certificate could not be verified",
	92: "the HTTP/2 stream failed",
}

// curlCausesWithDetail are the curl errors whose own text says what went
// wrong (which certificate problem, which TLS alert): it follows the cause.
var curlCausesWithDetail = map[int]bool{35: true, 60: true}

// transportDiagnostic presents a failed transfer (a TransportException
// from a curl error, "curl error 7 while downloading URL: Failed to
// connect to host port 1 after 0 ms: Could not connect to server") as
// what failed and why: "Could not download URL: could not connect to the
// server". curl's own text moves to the debugging details. Other errors,
// and transport errors carrying an HTTP status, which Composer already
// words as "The "URL" file could not be downloaded (HTTP/1.1 404 Not
// Found)", are left as they are.
func transportDiagnostic(err error, d *ui.Diagnostic) {
	te, ok := errors.AsType[*util.TransportError](err)
	if !ok || te.Curl == nil || te.ResponseInfo == nil || php.Trim(te.Message) != d.Message {
		return
	}

	errno, curlText := te.Curl.Errno, te.Curl.Message
	url := util.SanitizeURL(te.ResponseInfo.URL)

	cause, known := curlCauses[errno]
	switch {
	case !known:
		cause = curlText
	case curlCausesWithDetail[errno]:
		cause += " (" + curlText + ")"
	}

	d.Message = "Could not download " + url + ": " + cause
	d.Details = append(d.Details, "curl error "+strconv.Itoa(errno)+": "+curlText)
}
