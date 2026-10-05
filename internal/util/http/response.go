// Ports src/Composer/Util/Http/Response.php and
// src/Composer/Util/Http/CurlResponse.php.

package http

import (
	"github.com/stubbedev/maestro/internal/json/jsonlint"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// Response is Composer\Util\Http\Response, and CurlResponse when Info is
// set.
type Response struct {
	url     string
	code    int
	headers []string
	body    string
	// Info is CurlResponse::getCurlInfo(): the transfer's details, nil for
	// responses that did not come from a transfer.
	Info *util.TransferInfo
}

// NewResponse is new Response(['url' => $url], $code, $headers, $body).
func NewResponse(url string, code int, headers []string, body string) *Response {
	return &Response{url: url, code: code, headers: headers, body: body}
}

// URL is the request's url.
func (r *Response) URL() string { return r.url }

// StatusCode is getStatusCode().
func (r *Response) StatusCode() int { return r.code }

var statusLineRegex = php.MustCompile(`{^HTTP/\S+ \d+}i`)

// StatusMessage is getStatusMessage(): the last status line among the
// headers (they hold every response's headers after redirects); false
// for null.
func (r *Response) StatusMessage() (string, bool) {
	return findStatusMessage(r.headers)
}

func findStatusMessage(headers []string) (string, bool) {
	value, found := "", false
	for _, header := range headers {
		// \S+ is possessive before the space: Preg::isMatch cannot throw.
		if ok, _ := statusLineRegex.IsMatch(header); ok {
			value, found = header, true
		}
	}

	return value, found
}

// Headers is getHeaders().
func (r *Response) Headers() []string { return r.headers }

// Header is getHeader($name); false for null. A PcreException reads as
// null here; HeaderChecked returns it as getHeader throws it.
func (r *Response) Header(name string) (string, bool) {
	return FindHeaderValue(r.headers, name)
}

// HeaderChecked is getHeader($name) with the PcreException (*php.PcreError)
// Preg::isMatch throws, e.g. on a header holding a long run of whitespace.
func (r *Response) HeaderChecked(name string) (string, bool, error) {
	return FindHeaderValueChecked(r.headers, name)
}

// Body is getBody().
func (r *Response) Body() string { return r.body }

// DecodeJSON is decodeJson(): the body decoded as JsonFile::parseJson
// does (objects become *php.Array). Invalid JSON is a
// *jsonlint.ParsingError naming only the URL, because the body may hold
// secrets.
func (r *Response) DecodeJSON() (any, error) {
	v, err := php.JSONDecode(r.body, true)
	if err != nil {
		return nil, &jsonlint.ParsingError{Message: `"` + util.SanitizeURL(r.url) + `" does not contain valid JSON`}
	}

	return v, nil
}

// DecodeJSONArray is DecodeJSON for callers expecting an array; a JSON
// scalar or null gives nil.
func (r *Response) DecodeJSONArray() (*php.Array, error) {
	v, err := r.DecodeJSON()
	if err != nil {
		return nil, err
	}

	a, _ := v.(*php.Array)

	return a, nil
}

// Collect is collect(): it frees the response's data.
func (r *Response) Collect() {
	r.url, r.code, r.headers, r.body, r.Info = "", 0, nil, "", nil
}

// FindHeaderValue is Response::findHeaderValue: the value of the last
// header line named name (case-insensitively), trimmed; false for null.
// A PcreException reads as null here; FindHeaderValueChecked returns it.
func FindHeaderValue(headers []string, name string) (string, bool) {
	value, found, _ := FindHeaderValueChecked(headers, name)

	return value, found
}

// FindHeaderValueChecked is FindHeaderValue with the PcreException
// (*php.PcreError) Preg::isMatch throws: `(.+?)\s*$` exhausts the
// backtrack limit on a header line holding some 10 KB of whitespace.
func FindHeaderValueChecked(headers []string, name string) (string, bool, error) {
	re, err := php.Compile(`{^` + php.PregQuote(name, "") + `:\s*(.+?)\s*$}i`)
	if err != nil {
		return "", false, err
	}

	value, found := "", false
	for _, header := range headers {
		m, err := re.Match(header)
		if err != nil {
			return "", false, err
		}
		if m != nil {
			value, found = m.Get(1), true
		}
	}

	return value, found, nil
}
