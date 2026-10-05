// Framing (docs/PLUGINS.md §6.1): a 4-byte unsigned big-endian length N
// (1 ≤ N ≤ 2^30), then N bytes of UTF-8 JSON holding one message.

package rpc

import (
	"bufio"
	"encoding/binary"
	"io"
	"strconv"
)

// MaxFrame is the largest frame either side accepts.
const MaxFrame = 1 << 30

// ReadFrame reads one frame's payload. A length outside the allowed range
// is a *ProtocolError; a failed read is returned as is.
func ReadFrame(r *bufio.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	n := binary.BigEndian.Uint32(header[:])
	if n < 1 || n > MaxFrame {
		return nil, &ProtocolError{Message: "invalid frame length " + strconv.FormatUint(uint64(n), 10)}
	}

	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}

	return payload, nil
}

// AppendFrame appends the frame of payload to dst.
func AppendFrame(dst, payload []byte) ([]byte, error) {
	if len(payload) < 1 || len(payload) > MaxFrame {
		return dst, &ProtocolError{Message: "invalid frame length " + strconv.Itoa(len(payload))}
	}

	dst = binary.BigEndian.AppendUint32(dst, uint32(len(payload))) //nolint:gosec // bounded by MaxFrame above.

	return append(dst, payload...), nil
}
