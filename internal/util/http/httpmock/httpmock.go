// Package httpmock ports tests/Composer/Test/Mock/HttpDownloaderMock.php:
// an HttpDownloader stand-in answering requests from expectations, for the
// tests of every package that downloads.
package httpmock

import (
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Expectation is one expected request. A nil Options matches any options;
// Status defaults to 200 and Headers to [""] as in Composer.
type Expectation struct {
	URL     string
	Options *php.Array
	Status  int
	Body    string
	Headers []string
}

// AssertionError is PHPUnit's AssertionFailedError the mock raises.
type AssertionError struct{ Message string }

func (e *AssertionError) Error() string { return e.Message }

// Downloader is HttpDownloaderMock. It implements http.Getter and the
// Add of HttpDownloader.
type Downloader struct {
	expectations   []Expectation
	configured     bool
	strict         bool
	defaultHandler Expectation
	log            []string
}

// New returns a mock answering every request with an empty 200 response
// until Expects is called.
func New() *Downloader {
	return &Downloader{defaultHandler: Expectation{Status: 200}}
}

// Expects is expects($expectations, $strict, $defaultHandler): requests
// must arrive in this order; with strict, any other request fails, else it
// gets the default response (an empty 200 when defaultHandler is nil).
func (d *Downloader) Expects(expectations []Expectation, strict bool, defaultHandler *Expectation) {
	d.expectations = make([]Expectation, len(expectations))

	for i, e := range expectations {
		if e.Status == 0 {
			e.Status = 200
		}

		if e.Headers == nil {
			e.Headers = []string{""}
		}

		d.expectations[i] = e
	}

	d.configured = true
	d.strict = strict

	if defaultHandler != nil {
		h := *defaultHandler
		if h.Status == 0 {
			h.Status = 200
		}

		d.defaultHandler = h
	}
}

// AssertComplete is assertComplete(): an error when expected requests
// were not made.
func (d *Downloader) AssertComplete() error {
	// this was not configured to expect anything, so no need to react here
	if !d.configured || len(d.expectations) == 0 {
		return nil
	}

	urls := make([]string, len(d.expectations))
	for i, e := range d.expectations {
		urls[i] = e.URL
	}

	return &AssertionError{Message: "There are still " + strconv.Itoa(len(d.expectations)) + " expected HTTP requests which have not been consumed:\n" +
		strings.Join(urls, "\n") + "\n\nReceived calls:\n" + strings.Join(d.log, "\n")}
}

// Get is get($fileUrl, $options).
func (d *Downloader) Get(fileURL string, options *php.Array) (*http.Response, error) {
	if fileURL == "" {
		return nil, &util.LogicError{Message: "url cannot be an empty string"}
	}

	if options == nil {
		options = php.NewArray()
	}

	d.log = append(d.log, fileURL)

	if d.configured && len(d.expectations) > 0 && fileURL == d.expectations[0].URL &&
		(d.expectations[0].Options == nil || php.StrictEquals(options, d.expectations[0].Options)) {
		expect := d.expectations[0]
		d.expectations = d.expectations[1:]

		return respond(fileURL, expect.Status, expect.Headers, expect.Body)
	}

	if !d.strict {
		return respond(fileURL, d.defaultHandler.Status, d.defaultHandler.Headers, d.defaultHandler.Body)
	}

	encoded, _ := php.JSONEncode(options, 0)
	msg := `Received unexpected request for "` + fileURL + `" with options "` + encoded + `"` + "\n"

	if d.configured && len(d.expectations) > 0 {
		msg += `Expected "` + d.expectations[0].URL
		if d.expectations[0].Options != nil {
			expected, _ := php.JSONEncode(d.expectations[0].Options, 0)
			msg += `" with options "` + expected
		}

		msg += `" at this point.`
	} else {
		msg += "Expected no more calls at this point."
	}

	return nil, &AssertionError{Message: msg + "\nReceived calls:\n" + strings.Join(d.log[:len(d.log)-1], "\n")}
}

// Add is add($url, $options): a settled promise of Get.
func (d *Downloader) Add(url string, options *php.Array) (*util.Promise[*http.Response], error) {
	r, err := d.Get(url, options)
	if err != nil {
		return util.Rejected[*http.Response](err), nil
	}

	return util.Resolved(r), nil
}

func respond(url string, status int, headers []string, body string) (*http.Response, error) {
	if status < 400 {
		return http.NewResponse(url, status, headers, body), nil
	}

	e := util.NewTransportError(`The "`+url+`" file could not be downloaded`, status)
	e.Headers = headers
	e.SetResponse(body)

	return nil, e
}
