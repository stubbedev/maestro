//go:build !linux

package http

import "net"

// corkSocket holds writes back only on Linux (TCP_CORK); elsewhere each
// write leaves as it comes.
func corkSocket(net.Conn, bool) bool { return false }
