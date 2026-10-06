// HTTP/2 response heads as they arrived. curl writes each response field
// to CURLOPT_WRITEHEADER as nghttp2 hands it over, in the order of the
// HPACK block (lib/http2.c, on_header: "HTTP/2 <status> \r\n" for
// :status, "name: value\r\n" for the others, the names lowercase as
// received; on_stream_frame ends every head, 1xx ones included, with
// "\r\n"). net/http keeps only a map of the final head, so the plaintext
// of HTTP/2 connections is watched (h2HeadConn): the frames the server
// sends are parsed as they are read and their header blocks decoded by a
// second HPACK decoder kept in step with net/http's (it sees every block,
// in order, with the same table size), and the frames the client writes
// tell which stream a request got.

package http

import (
	"crypto/tls"
	"encoding/binary"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/http2/hpack"
)

// HTTP/2 frame types and flags (RFC 9113, section 6) the capture reads.
const (
	h2FrameHeaders      = 0x1
	h2FrameSettings     = 0x4
	h2FramePushPromise  = 0x5
	h2FrameContinuation = 0x9

	h2FlagEndHeaders = 0x4
	h2FlagPadded     = 0x8
	h2FlagPriority   = 0x20
	h2FlagAck        = 0x1

	h2SettingHeaderTableSize = 0x1
	// h2InitialHeaderTableSize is HPACK's initial dynamic table size,
	// which net/http's HTTP/2 client keeps (it sends no
	// SETTINGS_HEADER_TABLE_SIZE unless configured otherwise).
	h2InitialHeaderTableSize = 4096
	// h2ClientPrefaceLen is the length of the client connection preface
	// ("PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n") written before the first frame.
	h2ClientPrefaceLen = 24
)

// h2HeadConn is a TLS connection speaking HTTP/2 whose response heads
// are recorded per stream. net/http (Go 1.27) runs HTTP/2 over any
// connection DialTLSContext returns that reports its TLS state.
type h2HeadConn struct {
	*tls.Conn

	mu sync.Mutex
	// connect holds the proxy CONNECT response heads of the tunnel this
	// connection runs through, until the first transfer takes them.
	connect []byte
	in, out h2FrameScanner
	dec     *hpack.Decoder
	// block is the header block being received (HEADERS or PUSH_PROMISE,
	// then CONTINUATION frames) for blockStream.
	block       []byte
	blockStream uint32
	blockPush   bool
	// lastSent is the stream of the last HEADERS frame written; maxSent
	// the highest one (streams only open upwards).
	lastSent, maxSent uint32
	// streams are the heads decoded for the streams transfers wait on.
	streams map[uint32][][]hpack.HeaderField
	// broken is set when a header block could not be decoded: the decoder
	// state is then unknown, and the heads are rebuilt from net/http's
	// map from then on.
	broken bool
}

func newH2HeadConn(tc *tls.Conn, connect []byte) *h2HeadConn {
	c := &h2HeadConn{Conn: tc, connect: connect, streams: map[uint32][][]hpack.HeaderField{}}
	c.out.skip = h2ClientPrefaceLen
	c.dec = hpack.NewDecoder(h2InitialHeaderTableSize, nil)

	return c
}

func (c *h2HeadConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)

	if n > 0 {
		c.mu.Lock()
		c.in.feed(b[:n], h2ServerFrameKept, c.serverFrame)
		c.mu.Unlock()
	}

	return n, err
}

// Write notes the frames before they leave, so that a stream is known
// before its response can arrive.
func (c *h2HeadConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	c.out.feed(b, h2ClientFrameKept, c.clientFrame)
	c.mu.Unlock()

	return c.Conn.Write(b)
}

func h2ServerFrameKept(typ byte) bool {
	return typ == h2FrameHeaders || typ == h2FramePushPromise || typ == h2FrameContinuation
}

func h2ClientFrameKept(typ byte) bool {
	return typ == h2FrameHeaders || typ == h2FrameSettings
}

// clientFrame notes a frame the client writes: the stream of a request's
// HEADERS, and the table size its SETTINGS give the server's encoder.
func (c *h2HeadConn) clientFrame(typ, flags byte, stream uint32, payload []byte) {
	switch typ {
	case h2FrameHeaders:
		c.lastSent = stream

		if stream > c.maxSent {
			c.maxSent = stream
			c.streams[stream] = nil
		}
	case h2FrameSettings:
		if flags&h2FlagAck != 0 {
			return
		}

		for ; len(payload) >= 6; payload = payload[6:] {
			if binary.BigEndian.Uint16(payload) == h2SettingHeaderTableSize {
				c.dec.SetAllowedMaxDynamicTableSize(binary.BigEndian.Uint32(payload[2:]))
			}
		}
	}
}

// serverFrame collects the header blocks the server sends.
func (c *h2HeadConn) serverFrame(typ, flags byte, stream uint32, payload []byte) {
	if c.broken {
		return
	}

	switch typ {
	case h2FrameHeaders, h2FramePushPromise:
		p, ok := h2Unpad(flags, payload)

		switch {
		case !ok:
		case typ == h2FrameHeaders && flags&h2FlagPriority != 0:
			ok = len(p) >= 5
			if ok {
				p = p[5:]
			}
		case typ == h2FramePushPromise:
			ok = len(p) >= 4
			if ok {
				p = p[4:]
			}
		}

		if !ok {
			c.broken = true

			return
		}

		c.block = append(c.block[:0], p...)
		c.blockStream = stream
		c.blockPush = typ == h2FramePushPromise
	case h2FrameContinuation:
		c.block = append(c.block, payload...)
	}

	if flags&h2FlagEndHeaders == 0 {
		return
	}

	fields, err := c.dec.DecodeFull(c.block)
	if err != nil {
		c.broken = true

		return
	}

	if heads, ok := c.streams[c.blockStream]; ok && !c.blockPush {
		c.streams[c.blockStream] = append(heads, fields)
	}
}

// h2Unpad strips the padding of a PADDED frame's payload.
func h2Unpad(flags byte, p []byte) ([]byte, bool) {
	if flags&h2FlagPadded == 0 {
		return p, true
	}

	if len(p) == 0 || int(p[0]) > len(p)-1 {
		return nil, false
	}

	return p[1 : len(p)-int(p[0])], true
}

// sentStream is the stream of the request whose headers were written
// last; read in httptrace's WroteHeaders, which net/http's HTTP/2 client
// calls right after writing them, still holding the connection's write
// lock.
func (c *h2HeadConn) sentStream() uint32 {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.lastSent
}

// streamHeads returns the heads received on a stream up to the final
// (non-1xx) one, and after it its trailer, if any; ok is false when the
// heads are not known.
func (c *h2HeadConn) streamHeads(stream uint32) (heads [][]hpack.HeaderField, trailer []hpack.HeaderField, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	all := c.streams[stream]

	if c.broken {
		return nil, nil, false
	}

	for i, head := range all {
		if status := h2Status(head); len(status) != 3 || status[0] != '1' {
			if i+1 < len(all) {
				trailer = all[i+1]
			}

			return all[:i+1], trailer, true
		}
	}

	return nil, nil, false
}

// forget stops recording a stream.
func (c *h2HeadConn) forget(stream uint32) {
	c.mu.Lock()
	delete(c.streams, stream)
	c.mu.Unlock()
}

// takeConnect returns the CONNECT heads of the tunnel, once.
func (c *h2HeadConn) takeConnect() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	b := c.connect
	c.connect = nil

	return b
}

func h2Status(head []hpack.HeaderField) string {
	for _, f := range head {
		if f.Name == ":status" {
			return f.Value
		}
	}

	return ""
}

// curlH2HeaderText is what curl writes to its header handle for HTTP/2
// heads (lib/http2.c, on_header and on_stream_frame).
func curlH2HeaderText(heads [][]hpack.HeaderField) string {
	var b strings.Builder

	for _, head := range heads {
		b.WriteString("HTTP/2 " + h2Status(head) + " \r\n")

		for _, f := range head {
			if !strings.HasPrefix(f.Name, ":") {
				b.WriteString(f.Name + ": " + f.Value + "\r\n")
			}
		}

		b.WriteString("\r\n")
	}

	return b.String()
}

// curlH2TrailerText is what curl writes to its header handle for the
// trailer of an HTTP/2 response: its fields, as for a head, with no line
// after them (lib/http2.c, on_header once the body started).
func curlH2TrailerText(trailer []hpack.HeaderField) string {
	var b strings.Builder

	for _, f := range trailer {
		b.WriteString(f.Name + ": " + f.Value + "\r\n")
	}

	return b.String()
}

// h2Capture follows the HTTP/2 stream of one transfer: the connection
// net/http gave it (GotConn) and the stream its headers went out on
// (WroteHeaders). net/http may retry a request on another connection;
// the last one counts.
type h2Capture struct {
	mu     sync.Mutex
	conn   *h2HeadConn
	stream uint32
	// wrote is closed when the stream is known.
	wrote chan struct{}
}

// h2WroteHeadersWait bounds the wait for WroteHeaders once the response
// is there; it comes right after the headers are written, so this is
// only a guard.
const h2WroteHeadersWait = 5 * time.Second

func (h *h2Capture) gotConn(c *h2HeadConn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.releaseLocked()
	h.conn = c
	h.wrote = make(chan struct{})
}

func (h *h2Capture) wroteHeaders() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.conn != nil && h.stream == 0 {
		h.stream = h.conn.sentStream()
		close(h.wrote)
	}
}

// heads returns the heads of the transfer's stream, ok false when they
// are not known. net/http's HTTP/2 client writes the headers on a
// goroutine of the stream, which may report them (WroteHeaders) only
// after the response was handed over: heads waits for that.
func (h *h2Capture) heads() ([][]hpack.HeaderField, bool) {
	h.mu.Lock()
	wrote := h.wrote
	h.mu.Unlock()

	if wrote == nil {
		return nil, false
	}

	select {
	case <-wrote:
	case <-time.After(h2WroteHeadersWait):
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.conn == nil || h.stream == 0 {
		return nil, false
	}

	heads, _, ok := h.conn.streamHeads(h.stream)

	return heads, ok
}

// trailer returns the trailer of the transfer's stream, once the body
// was read through.
func (h *h2Capture) trailer() []hpack.HeaderField {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.conn == nil || h.stream == 0 {
		return nil
	}

	_, trailer, _ := h.conn.streamHeads(h.stream)

	return trailer
}

// release stops recording the transfer's stream.
func (h *h2Capture) release() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.releaseLocked()
}

func (h *h2Capture) releaseLocked() {
	if h.conn != nil && h.stream != 0 {
		h.conn.forget(h.stream)
	}

	h.conn, h.stream, h.wrote = nil, 0, nil
}

// h2FrameScanner splits a byte stream into HTTP/2 frames, keeping the
// payloads of the frame types asked for.
type h2FrameScanner struct {
	// skip is the number of bytes still to pass over (the preface).
	skip    int
	hdr     [9]byte
	nhdr    int
	left    int
	typ     byte
	flags   byte
	stream  uint32
	keep    bool
	payload []byte
}

func (s *h2FrameScanner) feed(b []byte, kept func(typ byte) bool, frame func(typ, flags byte, stream uint32, payload []byte)) {
	for len(b) > 0 {
		if s.skip > 0 {
			k := min(s.skip, len(b))
			s.skip -= k
			b = b[k:]

			continue
		}

		if s.nhdr < len(s.hdr) {
			k := copy(s.hdr[s.nhdr:], b)
			s.nhdr += k
			b = b[k:]

			if s.nhdr < len(s.hdr) {
				return
			}

			s.left = int(s.hdr[0])<<16 | int(s.hdr[1])<<8 | int(s.hdr[2])
			s.typ, s.flags = s.hdr[3], s.hdr[4]
			s.stream = binary.BigEndian.Uint32(s.hdr[5:]) & 0x7fffffff
			s.keep = kept(s.typ)
			s.payload = s.payload[:0]
		}

		k := min(s.left, len(b))
		if s.keep {
			s.payload = append(s.payload, b[:k]...)
		}

		s.left -= k
		b = b[k:]

		if s.left == 0 {
			if s.keep {
				frame(s.typ, s.flags, s.stream, s.payload)
			}

			s.nhdr = 0
		}
	}
}
