package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

// h2Server is a bare HTTP/2 server writing its frames itself, so that
// header field order, 1xx heads, padding and CONTINUATION frames are as
// a test wants them (net/http's server sorts fields). Responses go out
// from goroutines, so concurrent streams interleave.
func h2Server(t *testing.T) string {
	t.Helper()

	cert := httptest.NewTLSServer(nil)
	cfg := cert.TLS.Clone()
	cert.Close()

	cfg.NextProtos = []string{"h2"}

	l, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = l.Close() })

	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}

			go serveH2(c)
		}
	}()

	return l.Addr().String()
}

func serveH2(c net.Conn) {
	defer c.Close()

	preface := make([]byte, len(http2.ClientPreface))
	if _, err := io.ReadFull(c, preface); err != nil {
		return
	}

	fr := http2.NewFramer(c, c)
	fr.ReadMetaHeaders = hpack.NewDecoder(4096, nil)

	var (
		wmu  sync.Mutex
		hbuf bytes.Buffer
	)

	enc := hpack.NewEncoder(&hbuf)

	// block encodes fields; the caller holds wmu (the encoder's table
	// follows the order blocks are written in)
	block := func(fields ...string) []byte {
		hbuf.Reset()

		for i := 0; i < len(fields); i += 2 {
			_ = enc.WriteField(hpack.HeaderField{Name: fields[i], Value: fields[i+1], Sensitive: fields[i] == "x-secret"})
		}

		return slices.Clone(hbuf.Bytes())
	}

	wmu.Lock()
	_ = fr.WriteSettings()
	wmu.Unlock()

	for {
		f, err := fr.ReadFrame()
		if err != nil {
			return
		}

		switch f := f.(type) {
		case *http2.SettingsFrame:
			if !f.IsAck() {
				wmu.Lock()
				_ = fr.WriteSettingsAck()
				wmu.Unlock()
			}
		case *http2.MetaHeadersFrame:
			go respondH2(fr, &wmu, block, f.StreamID, f.PseudoValue("path"))
		}
	}
}

func respondH2(fr *http2.Framer, wmu *sync.Mutex, block func(...string) []byte, id uint32, path string) {
	write := func(fn func()) {
		wmu.Lock()
		defer wmu.Unlock()

		fn()
	}

	switch {
	case path == "/hints":
		write(func() {
			_ = fr.WriteHeaders(http2.HeadersFrameParam{StreamID: id, BlockFragment: block(":status", "103", "link", "</style.css>; rel=preload"), EndHeaders: true})
		})
	case strings.HasPrefix(path, "/slow"):
		time.Sleep(20 * time.Millisecond)
	}

	if path == "/split" {
		// padded HEADERS with a priority, then two CONTINUATION frames
		write(func() {
			b := block(":status", "200", "x-zeta", "1", "x-alpha", "2", "content-length", "2")
			n := len(b)
			_ = fr.WriteHeaders(http2.HeadersFrameParam{StreamID: id, BlockFragment: b[:n/3], PadLength: 7, Priority: http2.PriorityParam{Weight: 15}})
			_ = fr.WriteContinuation(id, false, b[n/3:2*n/3])
			_ = fr.WriteContinuation(id, true, b[2*n/3:])
			_ = fr.WriteData(id, true, []byte("ok"))
		})

		return
	}

	write(func() {
		_ = fr.WriteHeaders(http2.HeadersFrameParam{StreamID: id, BlockFragment: block(":status", "200", "x-zeta", "1", "content-type", "text/plain", "x-path", path, "x-alpha", "2", "x-zeta", "3", "x-secret", "s", "content-length", "2"), EndHeaders: true})
	})

	if path == "/trailers" {
		write(func() {
			_ = fr.WriteData(id, false, []byte("ok"))
			_ = fr.WriteHeaders(http2.HeadersFrameParam{StreamID: id, BlockFragment: block("x-trailer", "t"), EndHeaders: true, EndStream: true})
		})

		return
	}

	write(func() { _ = fr.WriteData(id, true, []byte("ok")) })
}

// TestHeaderLines_HTTP2 checks the header lines of HTTP/2 responses
// against what php-curl (libcurl 8.22, nghttp2) wrote to its header handle
// for the same frames: HPACK order, lowercase names, "HTTP/2 <status> "
// status lines, 1xx heads, and the trailer after the head.
func TestHeaderLines_HTTP2(t *testing.T) {
	addr := h2Server(t)

	order := func(path string) []string {
		return []string{"HTTP/2 200 ", "x-zeta: 1", "content-type: text/plain", "x-path: " + path, "x-alpha: 2", "x-zeta: 3", "x-secret: s", "content-length: 2"}
	}

	var pool transportPool

	key := transportKey{tls: tlsSettings{verifyPeer: false}}

	for _, tc := range []struct {
		path string
		want []string
	}{
		{"/order", order("/order")},
		// the second request reuses the connection and the HPACK table
		{"/order", order("/order")},
		{"/hints", append([]string{"HTTP/2 103 ", "link: </style.css>; rel=preload", ""}, order("/hints")...)},
		{"/split", []string{"HTTP/2 200 ", "x-zeta: 1", "x-alpha: 2", "content-length: 2"}},
		// curl writes the trailer fields after the head
		{"/trailers", append(order("/trailers"), "", "x-trailer: t")},
	} {
		res := pool.do(context.Background(), &transferRequest{url: "https://" + addr + tc.path, decode: true, curlStatusLines: true, key: key})
		if res.fail.Errno != 0 || !slices.Equal(res.headers, tc.want) || string(res.body) != "ok" {
			t.Errorf("%s: got %d %q %q %q, want %q", tc.path, res.fail.Errno, res.fail.Message, res.headers, res.body, tc.want)
		}
	}

	// concurrent streams on one connection, answered out of order
	var wg sync.WaitGroup

	for i := range 20 {
		wg.Go(func() {
			path := fmt.Sprintf("/order?%d", i)
			if i%2 == 0 {
				path = fmt.Sprintf("/slow?%d", i)
			}

			res := pool.do(context.Background(), &transferRequest{url: "https://" + addr + path, decode: true, curlStatusLines: true, key: key})
			if want := order(path); res.fail.Errno != 0 || !slices.Equal(res.headers, want) {
				t.Errorf("%s: got %d %q %q, want %q", path, res.fail.Errno, res.fail.Message, res.headers, want)
			}
		})
	}

	wg.Wait()
}

// TestH2FrameScanner feeds frames byte by byte and all at once.
func TestH2FrameScanner(t *testing.T) {
	var buf bytes.Buffer

	fr := http2.NewFramer(&buf, nil)
	_ = fr.WriteSettings(http2.Setting{ID: http2.SettingHeaderTableSize, Val: 100})
	_ = fr.WriteData(1, false, []byte("data"))
	_ = fr.WriteHeaders(http2.HeadersFrameParam{StreamID: 3, BlockFragment: []byte("abc"), EndHeaders: true})
	_ = fr.WriteSettingsAck()
	wire := append([]byte(http2.ClientPreface), buf.Bytes()...)

	type frame struct {
		typ, flags byte
		stream     uint32
		payload    string
	}

	want := []frame{{h2FrameSettings, 0, 0, "\x00\x01\x00\x00\x00\x64"}, {h2FrameHeaders, h2FlagEndHeaders, 3, "abc"}, {h2FrameSettings, h2FlagAck, 0, ""}}

	for _, step := range []int{1, len(wire)} {
		var (
			got []frame
			s   = h2FrameScanner{skip: h2ClientPrefaceLen}
		)

		keep := func(typ byte) bool { return typ != 0 }
		for b := wire; len(b) > 0; {
			n := min(step, len(b))
			s.feed(b[:n], keep, func(typ, flags byte, stream uint32, payload []byte) {
				got = append(got, frame{typ, flags, stream, string(payload)})
			})
			b = b[n:]
		}

		if !slices.Equal(got, want) {
			t.Errorf("step %d: got %+v", step, got)
		}
	}
}
