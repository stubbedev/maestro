// A chunked body as curl reads it (lib/http_chunks.c, libcurl 8.22),
// followed alongside net/http's chunked reader on the bytes of the
// connection: curl writes the trailer to the header handle, and fails
// malformed chunks with its own messages.

package http

import (
	"fmt"
	"strconv"
)

// chunkTracker is curl's chunk parser (httpchunk_readwrite) over the body
// bytes net/http reads.
type chunkTracker struct {
	state int
	hex   []byte
	left  int64
	// line is the trailer line being read; trailer the trailer as curl
	// writes it, each line ending in CRLF.
	line, trailer []byte
	done          bool
	// failure is curl's error for a malformed body (CURLE_RECV_ERROR).
	failure string
}

// curl's chunk parser states.
const (
	chunkHex = iota
	chunkLF
	chunkData
	chunkPostLF
	chunkTrailer
	chunkTrailerCR
	chunkTrailerPostCR
	chunkStop
)

// chunkMaxHex is CHUNK_MAXNUM_LEN: twice the size of curl_off_t.
const chunkMaxHex = 16

// feed follows bytes of the body; it reports whether it is done (the
// body ended or failed).
func (t *chunkTracker) feed(b []byte) bool {
	for len(b) > 0 && !t.done {
		c := b[0]

		switch t.state {
		case chunkHex:
			if isHexDigit(c) {
				if len(t.hex) >= chunkMaxHex {
					return t.fail(fmt.Sprintf("chunk hex-length longer than %d", chunkMaxHex))
				}

				t.hex = append(t.hex, c)
				b = b[1:]

				continue
			}

			if len(t.hex) == 0 {
				return t.fail(fmt.Sprintf("chunk hex-length char not a hex digit: 0x%x", c))
			}

			n, err := strconv.ParseInt(string(t.hex), 16, 64)
			if err != nil {
				return t.fail("invalid chunk size: '" + string(t.hex) + "'")
			}

			// the rest of the line (extensions, or anything) is skipped
			t.left, t.hex, t.state = n, t.hex[:0], chunkLF
		case chunkLF:
			if c == '\n' {
				t.state = chunkData
				if t.left == 0 {
					t.state = chunkTrailer
				}
			}

			b = b[1:]
		case chunkData:
			n := min(int64(len(b)), t.left)
			t.left -= n
			b = b[n:]

			if t.left == 0 {
				t.state = chunkPostLF
			}
		case chunkPostLF:
			switch c {
			case '\n':
				t.state = chunkHex
			case '\r':
			default:
				return t.fail(chunkBadChunk)
			}

			b = b[1:]
		case chunkTrailer:
			if c != '\r' && c != '\n' {
				if t.line = append(t.line, c); len(t.trailer)+len(t.line) > maxRecordedHead {
					return t.fail(chunkBadChunk)
				}

				b = b[1:]

				continue
			}

			if len(t.line) == 0 {
				// no trailer line: the final CRLF
				t.state = chunkTrailerPostCR

				continue
			}

			t.trailer = append(append(t.trailer, t.line...), '\r', '\n')
			t.line = t.line[:0]
			t.state = chunkTrailerCR

			if c != '\n' {
				b = b[1:]
			}
		case chunkTrailerCR:
			if c != '\n' {
				return t.fail(chunkBadChunk)
			}

			t.state = chunkTrailerPostCR
			b = b[1:]
		case chunkTrailerPostCR:
			switch c {
			case '\r':
				b = b[1:]
				t.state = chunkStop
			case '\n':
				t.state = chunkStop
			default:
				// another trailer line
				t.state = chunkTrailer
			}
		case chunkStop:
			if c != '\n' {
				return t.fail(chunkBadChunk)
			}

			t.done = true
		}
	}

	return t.done
}

// chunkBadChunk is cw_chunked_write's error for CHUNKE_BAD_CHUNK, which
// the parser fails without a message of its own.
const chunkBadChunk = "Malformed encoding found in chunked-encoding"

func (t *chunkTracker) fail(msg string) bool {
	t.failure, t.done = msg, true

	return true
}

func isHexDigit(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}
