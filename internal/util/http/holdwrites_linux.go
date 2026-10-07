package http

import (
	"crypto/tls"
	"net"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// maxWriteHold bounds how long holdWrites holds writes back, whatever
// happens to the burst it was asked for.
const maxWriteHold = 20 * time.Millisecond

// holdWrites holds the small writes to conn's TCP socket back (TCP_CORK)
// so that they leave together, in full segments, until the returned
// function is called (at most maxWriteHold). It does nothing for a
// connection that is not over TCP.
func holdWrites(conn net.Conn) (flush func()) {
	tcp := tcpConnOf(conn)
	if tcp == nil {
		return func() {}
	}

	raw, err := tcp.SyscallConn()
	if err != nil {
		return func() {}
	}

	cork := func(on int) bool {
		var serr error
		if err := raw.Control(func(fd uintptr) {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_CORK, on)
		}); err != nil {
			return false
		}

		return serr == nil
	}

	if !cork(1) {
		return func() {}
	}

	var once sync.Once
	flush = func() { once.Do(func() { cork(0) }) }
	time.AfterFunc(maxWriteHold, flush)

	return flush
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
