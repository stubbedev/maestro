//go:build !linux

package http

import "net"

// holdWrites holds writes back only on Linux (TCP_CORK); elsewhere each
// write leaves as it comes.
func holdWrites(net.Conn) (flush func()) { return func() {} }
