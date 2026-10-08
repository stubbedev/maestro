package http

import (
	"crypto/tls"
	"net"

	"golang.org/x/sys/unix"
)

// corkSocket sets or clears TCP_CORK on conn's TCP socket; false when
// conn is not over TCP or the option could not be set.
func corkSocket(conn net.Conn, on bool) bool {
	tcp := tcpConnOf(conn)
	if tcp == nil {
		return false
	}

	raw, err := tcp.SyscallConn()
	if err != nil {
		return false
	}

	v := 0
	if on {
		v = 1
	}

	var serr error
	if err := raw.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_CORK, v)
	}); err != nil {
		return false
	}

	return serr == nil
}

// tcpConnOf is the TCP connection under conn's wrappers and TLS, nil when
// there is none.
func tcpConnOf(conn net.Conn) *net.TCPConn {
	for {
		switch c := conn.(type) {
		case *net.TCPConn:
			return c
		case *h2HeadConn:
			if c.Conn == nil {
				return nil
			}

			conn = c.NetConn()
		case *tls.Conn:
			conn = c.NetConn()
		case *headConn:
			conn = c.Conn
		default:
			return nil
		}
	}
}
