//go:build !windows

package http

import (
	"errors"
	"syscall"
)

// isConnReset reports a connection the peer reset (ECONNRESET).
func isConnReset(err error) bool { return errors.Is(err, syscall.ECONNRESET) }
