// The php child process and its transport (docs/PLUGINS.md §5.2, §6.1,
// D5): inherited pipes on fds 3 and 4, or a loopback socket with a
// 256-bit token where fds cannot be inherited (Windows).

package plugin

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"syscall"

	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// Transport selects how maestro and the child talk.
type Transport int

// The transports.
const (
	// TransportDefault is TransportPipes, or TransportTCP on Windows.
	TransportDefault Transport = iota
	// TransportPipes passes two pipes as the child's fds 3 (maestro →
	// PHP) and 4 (PHP → maestro): MAESTRO_IPC=fd:3,4.
	TransportPipes
	// TransportTCP listens on 127.0.0.1 for exactly one connection whose
	// first frame carries the token: MAESTRO_IPC=tcp:127.0.0.1:<port> and
	// MAESTRO_IPC_TOKEN.
	TransportTCP
)

// child is the running php process, as the rpc.Peer of its Conn.
type child struct {
	cmd  *exec.Cmd
	done chan struct{}
	// status is the exit status, set before done closes.
	status int
	// closers end maestro's side of the channel when the process exits,
	// so a read that waits on it returns even if a grandchild keeps the
	// other side open.
	mu      sync.Mutex
	closers []io.Closer
	kill    sync.Once
}

// Wait implements rpc.Peer.
func (c *child) Wait() int {
	<-c.done

	return c.status
}

// Kill implements rpc.Peer.
func (c *child) Kill() {
	c.kill.Do(func() { _ = c.cmd.Process.Kill() })
}

// spawned is a started child with maestro's ends of the channel.
type spawned struct {
	child *child
	r     io.Reader
	w     io.Writer
	token string // "" for pipes
}

// spawn starts cmd (php and its arguments, environment and standard
// streams already set) with the transport's channel.
func spawn(cmd *exec.Cmd, transport Transport) (*spawned, error) {
	if transport == TransportDefault {
		transport = TransportPipes
		if runtime.GOOS == "windows" {
			transport = TransportTCP
		}
	}

	if transport == TransportTCP {
		return spawnTCP(cmd)
	}

	return spawnPipes(cmd)
}

func spawnPipes(cmd *exec.Cmd) (*spawned, error) {
	// PHP → maestro.
	fromPHP, phpOut, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	// maestro → PHP.
	phpIn, toPHP, err := os.Pipe()
	if err != nil {
		_ = fromPHP.Close()
		_ = phpOut.Close()

		return nil, err
	}

	cmd.ExtraFiles = []*os.File{phpIn, phpOut} // fds 3 and 4
	cmd.Env = append(cmd.Env, "MAESTRO_IPC=fd:3,4")

	err = cmd.Start()
	if err == nil {
		tieToMaestro(cmd.Process)
	}
	// The child's ends live on in the child only.
	_ = phpIn.Close()
	_ = phpOut.Close()
	if err != nil {
		_ = fromPHP.Close()
		_ = toPHP.Close()

		return nil, err
	}

	c := watch(cmd, fromPHP, toPHP)

	return &spawned{child: c, r: fromPHP, w: toPHP}, nil
}

func spawnTCP(cmd *exec.Cmd) (*spawned, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer func() { _ = ln.Close() }()

	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(raw[:])

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return nil, errors.New("plugin: unexpected listener address " + ln.Addr().String())
	}
	cmd.Env = append(cmd.Env, "MAESTRO_IPC=tcp:127.0.0.1:"+strconv.Itoa(addr.Port), "MAESTRO_IPC_TOKEN="+token)

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	tieToMaestro(cmd.Process)
	c := watch(cmd)

	type accepted struct {
		conn net.Conn
		err  error
	}
	ch := make(chan accepted, 1)
	go func() {
		conn, err := ln.Accept()
		ch <- accepted{conn, err}
	}()

	select {
	case a := <-ch:
		if a.err != nil {
			c.Kill()
			c.Wait()

			return nil, a.err
		}
		// Exactly one connection: the listener closes on return.
		c.addCloser(a.conn)

		return &spawned{child: c, r: a.conn, w: a.conn, token: token}, nil
	case <-c.done:
		// php ended before connecting (a PHP below 7.2.5 prints
		// Composer's message and exits 1).
		_ = ln.Close()
		<-ch

		return nil, &rpc.PHPExit{Code: c.status}
	}
}

// watch waits for the process in the background; when it exits, its
// status is recorded and maestro's ends of the channel are closed.
func watch(cmd *exec.Cmd, closers ...io.Closer) *child {
	c := &child{cmd: cmd, done: make(chan struct{}), closers: closers}

	go func() {
		_ = cmd.Wait()
		c.status = exitStatus(cmd.ProcessState)

		c.mu.Lock()
		closers := c.closers
		c.closers = nil
		close(c.done)
		c.mu.Unlock()

		for _, cl := range closers {
			_ = cl.Close()
		}
	}()

	return c
}

// addCloser adds a closer, closing it at once when the process has
// already exited.
func (c *child) addCloser(cl io.Closer) {
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		_ = cl.Close()

		return
	default:
	}
	c.closers = append(c.closers, cl)
	c.mu.Unlock()
}

// exitStatus is the process's status as a shell reports it: the exit
// code, or 128+signal.
func exitStatus(ps *os.ProcessState) int {
	if ps == nil {
		return 1
	}
	if ws, ok := ps.Sys().(interface {
		Signaled() bool
		Signal() syscall.Signal
	}); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}

	return ps.ExitCode()
}
