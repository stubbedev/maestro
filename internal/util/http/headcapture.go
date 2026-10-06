// The response heads as they arrived. curl hands Composer every header
// line as received (CURLOPT_WRITEHEADER): proxy CONNECT responses, 1xx
// responses and the final one, in wire order and spelling; PHP's http
// stream wrapper fills $http_response_header likewise from the final
// head. net/http keeps only a map of the final head's fields, so HTTP/1
// connections record the bytes of the heads read from them (headConn),
// and the lines are rendered from those.
//
// HTTP/2 connections are net/http's own *tls.Conn (its HTTP/2 client
// requires one), and HPACK-decoded fields are not observable in order: for
// them the lines are rebuilt from the field map (responseHeaderLines).

package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"strconv"
	"strings"
	"sync"
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
func (c *headConn) record() *headRecorder {
	c.mu.Lock()
	defer c.mu.Unlock()

	rec := &headRecorder{connect: c.connect}
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

// recordingTLSConn is the connection DialTLSContext hands net/http: a
// tlsHeadConn for HTTP/1, the *tls.Conn itself for HTTP/2 (net/http's
// HTTP/2 client takes only that), whose CONNECT head, if any, is kept in
// tunnelHeads.
func recordingTLSConn(tc *tls.Conn, connect []byte) net.Conn {
	if tc.ConnectionState().NegotiatedProtocol == "h2" {
		if connect != nil {
			tunnelHeads.Store(net.Conn(tc), connect)
		}

		return tc
	}

	return &tlsHeadConn{headConn: &headConn{Conn: tc, connect: connect}, tls: tc}
}

// tunnelHeads are the CONNECT heads of tunnelled connections that are not
// headConns (HTTP/2 ones), until the first transfer on them takes them.
var tunnelHeads sync.Map // net.Conn -> []byte

// takeConnectHeads returns the CONNECT heads of a tunnelled HTTP/2
// connection, once.
func takeConnectHeads(c net.Conn) []byte {
	if v, ok := tunnelHeads.LoadAndDelete(c); ok {
		b, _ := v.([]byte)

		return b
	}

	return nil
}

// headRecorder collects the response heads of one transfer: the
// informational (1xx) ones, then the final one; it stops at the end of the
// final head.
type headRecorder struct {
	mu sync.Mutex
	// connect are the CONNECT heads of the tunnel, as received.
	connect []byte
	buf     []byte
	// start is where the head being read begins in buf.
	start    int
	complete bool
	overflow bool
}

// feed adds bytes read from the connection; it reports whether recording
// is over.
func (r *headRecorder) feed(b []byte) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.complete || r.overflow {
		return true
	}

	r.buf = append(r.buf, b...)

	for {
		end := headEnd(r.buf, r.start)
		if end < 0 {
			break
		}

		code := statusCode(r.buf[r.start:end])
		r.start = end

		// net/http, like curl, reads past informational responses (but
		// not 101, which ends the exchange)
		if code < 100 || code >= 200 || code == 101 {
			r.buf = r.buf[:end]
			r.complete = true

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
// heads are not included (the wrapper consumes them).
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

	return lines
}
