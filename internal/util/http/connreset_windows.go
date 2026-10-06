package http

import (
	"errors"
	"syscall"
)

// isConnReset reports a connection the peer reset: Winsock's
// WSAECONNRESET, which syscall.ECONNRESET (an invented errno on Windows)
// does not match.
func isConnReset(err error) bool {
	return errors.Is(err, syscall.WSAECONNRESET) || errors.Is(err, syscall.ECONNRESET)
}
