// The response heads as they arrived. curl hands Composer every header
// line as received (CURLOPT_WRITEHEADER): proxy CONNECT responses, 1xx
// responses and the final one, in wire order and spelling; PHP's http
// stream wrapper fills $http_response_header likewise from the final
// head. net/http keeps only a map of the final head's fields, so HTTP/1
// connections record the bytes of the heads read from them (headConn),
// and the lines are rendered from those. curl also writes the trailer
// fields of a chunked body to the header handle, so for curl the chunks
// are followed to them.
//
// HTTP/2 connections record their heads per stream instead
// (h2capture.go).

package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
)

// maxRecordedHead bounds the bytes recorded for a response's heads; past
// it the lines are rebuilt from net/http's map (which rejects heads above
// its own limit of 1 MiB anyway).
const maxRecordedHead = 1 << 20

// headConn is an HTTP/1 connection that records response heads for the
// transfer currently using it (record).
type headConn struct {
	net.Conn

	mu  sync.Mutex
	rec *headRecorder
	// connect holds the proxy CONNECT response heads of the tunnel this
	// connection runs through, until the first transfer takes them.
	connect []byte
}

// tlsHeadConn is a headConn over TLS: net/http reads the connection state
// from it (connectionStater) as from a *tls.Conn.
type tlsHeadConn struct {
	*headConn

	tls *tls.Conn
}

// ConnectionState implements net/http's connectionStater.
func (c *tlsHeadConn) ConnectionState() tls.ConnectionState { return c.tls.ConnectionState() }

// HandshakeContext implements net/http's handshaker (the handshake is done).
func (c *tlsHeadConn) HandshakeContext(ctx context.Context) error { return c.tls.HandshakeContext(ctx) }

func (c *headConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)

	if n > 0 {
		c.mu.Lock()
		if c.rec != nil && c.rec.feed(b[:n]) {
			c.rec = nil
		}
		c.mu.Unlock()
	}

	return n, err
}

// record starts recording the response heads of the transfer that got
// the connection; the first transfer on it also gets its CONNECT heads.
// stream is set for PHP's http stream wrapper, noBody for a response
// without a body whatever its head says (a HEAD request).
func (c *headConn) record(stream, noBody bool) *headRecorder {
	c.mu.Lock()
	defer c.mu.Unlock()

	rec := &headRecorder{connect: c.connect, stream: stream, noBody: noBody}
	c.connect = nil
	c.rec = rec

	return rec
}

// asHeadConn returns the headConn a connection net/http hands out is, nil
// for others.
func asHeadConn(c net.Conn) *headConn {
	switch hc := c.(type) {
	case *headConn:
		return hc
	case *tlsHeadConn:
		return hc.headConn
	}

	return nil
}

// recordingTLSConn is the connection DialTLSContext hands net/http: an
// h2HeadConn when HTTP/2 was negotiated, else a tlsHeadConn. connect is
// the CONNECT heads of the tunnel it runs through, if any.
func recordingTLSConn(tc *tls.Conn, connect []byte) net.Conn {
	if tc.ConnectionState().NegotiatedProtocol == "h2" {
		return newH2HeadConn(tc, connect)
	}

	return &tlsHeadConn{headConn: &headConn{Conn: tc, connect: connect}, tls: tc}
}

// headRecorder collects the response heads of one transfer: the
// informational (1xx) ones, then the final one; it stops at the end of the
// final head, or for curl at the end of a chunked body's trailer.
type headRecorder struct {
	mu sync.Mutex
	// connect are the CONNECT heads of the tunnel, as received.
	connect []byte
	buf     []byte
	// start is where the head being read begins in buf.
	start    int
	complete bool
	overflow bool
	// stream applies the stream wrapper's reading of informational heads
	// (early); noBody tells a response without a body (no trailer).
	stream, noBody bool
	nheads         int
	// early is set when the stream wrapper takes an informational head
	// for the response; rest are the bytes read after it, its body.
	early bool
	rest  []byte
	// chunks follows a chunked body to its trailer (curl).
	chunks *chunkTracker
}

// maxEarlyBody bounds the bytes recorded as the body of an informational
// head the stream wrapper takes for the response.
const maxEarlyBody = 64 << 20

// feed adds bytes read from the connection; it reports whether recording
// is over.
func (r *headRecorder) feed(b []byte) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	switch {
	case r.early:
		if len(r.rest)+len(b) > maxEarlyBody {
			r.early, r.rest = false, nil

			return true
		}

		r.rest = append(r.rest, b...)

		return false
	case r.chunks != nil:
		return r.chunks.feed(b)
	case r.complete || r.overflow:
		return true
	}

	r.buf = append(r.buf, b...)

	for {
		end := headEnd(r.buf, r.start)
		if end < 0 {
			break
		}

		head := r.buf[r.start:end]
		code := statusCode(head)
		r.start = end
		r.nheads++

		// net/http, like curl, reads past informational responses (but
		// not 101, which ends the exchange)
		informational := code >= 100 && code < 200 && code != 101

		if informational && r.stream && r.nheads == 2 {
			// PHP's http wrapper skips one informational head, reading
			// lines up to the next "HTTP/1" status line, and takes the
			// head starting there for the response, informational or
			// not; the rest of the stream is its body
			// (ext/standard/http_fopen_wrapper.c, php_stream_url_wrap_http_ex)
			r.early = true
			r.rest = append([]byte(nil), r.buf[end:]...)
			r.buf = r.buf[:end]
			r.complete = true

			return false
		}

		if !informational {
			rest := r.buf[end:]
			r.buf = r.buf[:end]
			r.complete = true

			if !r.stream && !r.noBody && code >= 200 && code != 204 && code != 304 && isChunked(head) {
				r.chunks = &chunkTracker{}

				return r.chunks.feed(rest)
			}

			return true
		}
	}

	if len(r.buf) > maxRecordedHead {
		r.overflow = true

		return true
	}

	return false
}

// heads returns the CONNECT heads and the response heads recorded, or ok
// false when the final head was not seen whole.
func (r *headRecorder) heads() (connect, response []byte, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.connect, r.buf, r.complete
}

// trailer returns the trailer of a chunked body as received, once the
// body was read through.
func (r *headRecorder) trailer() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.chunks == nil || !r.chunks.done || r.chunks.failure != "" {
		return nil
	}

	return r.chunks.trailer
}

// chunkFailure is curl's error for the chunked body, "" when curl reads
// it (or it is not chunked).
func (r *headRecorder) chunkFailure() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.chunks == nil {
		return ""
	}

	return r.chunks.failure
}

// earlyResponse returns the status and body of an informational head
// the stream wrapper takes for the response (feed).
func (r *headRecorder) earlyResponse() (int, []byte, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.early {
		return 0, nil, false
	}

	heads := splitHeads(r.buf)

	return statusCode(bytes.Join(heads[len(heads)-1], nil)), r.rest, true
}

// isChunked reports whether a head's Transfer-Encoding names chunked.
func isChunked(head []byte) bool {
	for _, line := range bytes.Split(head, []byte("\n"))[1:] {
		name, value, ok := bytes.Cut(line, []byte(":"))
		if ok && php.Strcasecmp(string(bytes.TrimSpace(name)), "transfer-encoding") == 0 && bytes.Contains(bytes.ToLower(value), []byte("chunked")) {
			return true
		}
	}

	return false
}

// headEnd is the offset just past the empty line ending the head that
// starts at start, or -1; lines end in LF, CRLF accepted (as curl and
// net/http read them).
func headEnd(buf []byte, start int) int {
	for i, first := start, true; ; first = false {
		j := bytes.IndexByte(buf[i:], '\n')
		if j < 0 {
			return -1
		}

		if line := buf[i : i+j]; !first && (len(line) == 0 || len(line) == 1 && line[0] == '\r') {
			return i + j + 1
		}

		i += j + 1
	}
}

// statusCode reads the code of a head's status line ("HTTP/1.1 200 OK");
// 0 when there is none.
func statusCode(head []byte) int {
	line, _, _ := bytes.Cut(head, []byte("\n"))
	_, rest, ok := bytes.Cut(line, []byte(" "))

	if !ok || len(rest) < 3 {
		return 0
	}

	code, err := strconv.Atoi(string(rest[:3]))
	if err != nil {
		return 0
	}

	return code
}

// splitHeads splits recorded bytes into heads, each a list of lines with
// their terminators.
func splitHeads(b []byte) [][][]byte {
	var (
		heads [][][]byte
		cur   [][]byte
	)

	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')

		var line []byte
		if i < 0 {
			line, b = b, nil
		} else {
			line, b = b[:i+1], b[i+1:]
		}

		cur = append(cur, line)

		if t := bytes.TrimRight(line, "\r\n"); len(t) == 0 {
			heads = append(heads, cur)
			cur = nil
		}
	}

	if len(cur) > 0 {
		heads = append(heads, cur)
	}

	return heads
}

// curlHeaderText is what curl writes to CURLOPT_WRITEHEADER for recorded
// heads: each line as received, except that a folded field (obs-fold) is
// written as one line, the value's trailing white space and each
// continuation's leading white space replaced by one space
// (lib/http.c's unfolding).
func curlHeaderText(heads []byte) string {
	var out strings.Builder

	for _, head := range splitHeads(heads) {
		var pending []byte // a field line whose continuations may follow

		flush := func() {
			out.Write(pending)
			pending = nil
		}

		for _, line := range head {
			if (line[0] == ' ' || line[0] == '\t') && pending != nil {
				body, term := cutTerminator(line)
				prev, _ := cutTerminator(pending)
				joined := append(bytes.TrimRight(prev, " \t"), ' ')
				joined = append(joined, bytes.TrimLeft(body, " \t")...)
				pending = append(joined, term...)

				continue
			}

			flush()
			pending = append([]byte(nil), line...)
		}

		flush()
	}

	return out.String()
}

// cutTerminator splits a line from its CRLF or LF.
func cutTerminator(line []byte) (body, term []byte) {
	n := len(line)

	switch {
	case n >= 2 && line[n-2] == '\r' && line[n-1] == '\n':
		return line[:n-2], line[n-2:]
	case n >= 1 && line[n-1] == '\n':
		return line[:n-1], line[n-1:]
	}

	return line, nil
}

// curlHeaderLines is Composer's explode("\r\n", rtrim($headerText)) of
// the header text curl wrote.
func curlHeaderLines(text string) []string {
	return strings.Split(strings.TrimRight(text, " \t\n\r\x00\x0B"), "\r\n")
}

// streamHeaderLines is $http_response_header for a recorded final head as
// PHP's http wrapper fills it: one entry per line without its terminator,
// the status line as received, fields with trailing white space removed
// and folded fields joined with one space. Its CONNECT and 100 Continue
// heads are not included (the wrapper consumes them), nor is a
// Transfer-Encoding field whose value starts with "chunked": the wrapper
// decodes the body with its dechunk filter then and does not store the
// field (php_stream_http_response_header).
func streamHeaderLines(heads []byte) []string {
	all := splitHeads(heads)
	if len(all) == 0 {
		return nil
	}

	var lines []string

	for i, line := range all[len(all)-1] {
		body, _ := cutTerminator(line)
		body = bytes.TrimSuffix(body, []byte("\r"))

		switch {
		case len(body) == 0:
			// the empty line ending the head
		case i == 0:
			lines = append(lines, string(body))
		case (body[0] == ' ' || body[0] == '\t') && len(lines) > 1:
			joined := lines[len(lines)-1] + " " + strings.Trim(string(body), " \t")
			lines[len(lines)-1] = strings.TrimRight(joined, " \t")
		default:
			lines = append(lines, strings.TrimRight(string(body), " \t"))
		}
	}

	return slices.DeleteFunc(lines, func(line string) bool {
		const name = "Transfer-Encoding:"

		return php.Strncasecmp(line, name, len(name)) == 0 && php.Strncasecmp(strings.TrimLeft(line[len(name):], " \t"), "chunked", 7) == 0
	})
}
