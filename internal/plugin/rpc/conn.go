// Messages and the re-entrant call stack (docs/PLUGINS.md §6.2, D3).
//
//	{"k":"call","id":17,"m":"listener.call","a":{...},"s":{...}}
//	{"k":"ret","id":17,"v":<value>,"s":{...}}
//	{"k":"err","id":17,"x":<exception>,"s":{...}}

package rpc

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// Handler serves a call PHP makes: it gets the decoded params and returns
// the value to reply with, or the error to throw in PHP. A handler runs on
// the goroutine holding the baton and may call PHP again.
type Handler func(args any) (any, error)

// Exit is a reply that makes PHP exit with Code instead of returning
// (docs/PLUGINS.md §6.2; app.run and command.run of an Application with
// auto-exit).
type Exit struct {
	Code int
}

// Peer is the PHP process at the other end.
type Peer interface {
	// Wait waits until the process has ended and returns its exit status
	// (128+signal for a signal). It may be called any number of times.
	Wait() int
	// Kill ends the process.
	Kill()
}

// Options configure a Conn.
type Options struct {
	// Flush writes out everything Go buffered for the terminal; it runs
	// before every message to PHP, so output keeps its order
	// (docs/PLUGINS.md D11).
	Flush func()
	// Statics are Composer's statics the sync engine keeps in step, by
	// name (see PHPStatics).
	Statics map[string]Static
}

// Conn is maestro's end of the channel to one PHP process.
type Conn struct {
	r    *bufio.Reader
	w    io.Writer
	peer Peer
	opts Options

	handlers  map[string]Handler
	factories map[string]MirrorFactory
	tags      map[string]TagDecoder

	baton baton

	lastID    int64   // the last call id Go allocated
	pending   []int64 // Go's outstanding calls, innermost last
	lastPHPID int64   // the last call id PHP used
	serving   []int64 // PHP's calls being served, innermost last

	dead error

	h          handles
	sync       syncState
	pendingReg []registration
	buf        []byte
}

// NewConn returns the Conn over r (PHP → Go) and w (Go → PHP). The
// current environment and working directory are taken as what PHP starts
// with.
func NewConn(r io.Reader, w io.Writer, peer Peer, opts Options) *Conn {
	if opts.Flush == nil {
		opts.Flush = func() {}
	}

	return &Conn{
		r:         bufio.NewReaderSize(r, 64<<10),
		w:         w,
		peer:      peer,
		opts:      opts,
		handlers:  map[string]Handler{},
		factories: map[string]MirrorFactory{},
		tags:      map[string]TagDecoder{},
		h:         newHandles(),
		sync:      newSyncState(opts.Statics),
	}
}

// Handle sets the handler of a method PHP calls.
func (c *Conn) Handle(method string, h Handler) { c.handlers[method] = h }

// RegisterMirrorFactory sets the factory of PHP-born mirrors whose shim
// base class is base.
func (c *Conn) RegisterMirrorFactory(base string, f MirrorFactory) { c.factories[base] = f }

// Adopt makes the PHP-born object o the Go object obj (docs/PLUGINS.md
// §5.3): a service maestro created for an object PHP constructed (`new
// Locker(...)`, `new PlatformRepository(...)`). PHP rebinds its object to
// obj's handle with the next message, so from then on it is Go-owned and
// its methods are maestro's. It must run on the goroutine holding the
// baton (a handler of a call from PHP).
func (c *Conn) Adopt(o *PHPObject, obj Object) error {
	if _, ok := c.h.adopted[o.H]; ok {
		return &ProtocolError{Message: "object " + o.Class + " was adopted already"}
	}
	gh, err := c.h.handleOf(obj)
	if err != nil {
		return err
	}
	c.h.adopted[o.H] = obj
	var rev uint64
	if m, ok := obj.(Mirror); ok {
		rev = m.Rev()
	}
	c.pendingReg = append(c.pendingReg, registration{tmp: o.H, h: gh, m: obj, rev: rev})

	return nil
}

// RegisterTag sets the decoder of a value tag (key starts with NUL).
func (c *Conn) RegisterTag(key string, d TagDecoder) { c.tags[key] = d }

// Err returns why the channel is unusable (a *PHPExit or
// *ProtocolError), or nil.
func (c *Conn) Err() error { return c.dead }

// Lookup returns the object of a known handle: the Go object for a
// Go-owned one, the *PHPObject (or the Go object it became) for a
// PHP-owned one.
func (c *Conn) Lookup(h Handle) (any, bool) { return c.h.lookup(h) }

// Depth returns the number of outstanding calls of both sides.
func (c *Conn) Depth() int { return len(c.pending) + len(c.serving) }

// Delegate runs fn on a new goroutine that holds the baton while fn runs
// (docs/PLUGINS.md §5.14: goroutines spawned by handlers inherit it
// explicitly). The caller must hold the baton or no goroutine may.
func (c *Conn) Delegate(fn func() error) error { return c.baton.lend(fn) }

// Call calls method in PHP and returns its result, serving PHP's calls
// meanwhile. An exception PHP throws is a *PHPException.
func (c *Conn) Call(method string, args any) (any, error) {
	leave, err := c.baton.enter()
	if err != nil {
		return nil, err
	}
	defer leave()

	if c.dead != nil {
		return nil, c.dead
	}

	c.lastID++
	id := c.lastID
	if err := c.send(message{kind: "call", id: id, method: method, key: "a", value: args}); err != nil {
		return nil, err
	}

	c.pending = append(c.pending, id)
	defer func() { c.pending = c.pending[:len(c.pending)-1] }()

	return c.await(id)
}

// Accept serves the next message, which must be a call to method (the
// handshake: PHP's first message is `hello`).
func (c *Conn) Accept(method string) error {
	leave, err := c.baton.enter()
	if err != nil {
		return err
	}
	defer leave()

	if c.dead != nil {
		return c.dead
	}

	m, err := c.receive()
	if err != nil {
		return err
	}
	if m.kind != "call" || m.method != method {
		return c.protocol("expected a " + method + " call, got " + m.kind + " " + m.method)
	}

	return c.serve(m)
}

// Shutdown sends `shutdown` with code and serves PHP's calls (shutdown
// functions and destructors may call maestro) until the process ends. It
// returns the process's exit status, which a shutdown function calling
// exit() changes. It must be called with no call outstanding.
func (c *Conn) Shutdown(code int) (int, error) {
	leave, err := c.baton.enter()
	if err != nil {
		return 0, err
	}
	defer leave()

	if c.Depth() > 0 {
		return 0, errors.New("rpc: shutdown with calls outstanding")
	}
	if c.dead != nil {
		if exit, ok := errors.AsType[*PHPExit](c.dead); ok {
			return exit.Code, nil
		}

		return 0, c.dead
	}

	c.lastID++
	id := c.lastID
	if err := c.send(message{kind: "call", id: id, method: "shutdown", key: "a", value: php.ArrayOf("code", int64(code))}); err != nil {
		return 0, err
	}

	c.pending = append(c.pending, id)
	defer func() { c.pending = c.pending[:len(c.pending)-1] }()

	if _, err := c.await(id); err == nil {
		return 0, c.protocol("shutdown returned")
	}

	if exit, ok := errors.AsType[*PHPExit](c.dead); ok {
		return exit.Code, nil
	}

	return 0, c.dead
}

// await reads messages until the reply to call id, serving PHP's calls.
func (c *Conn) await(id int64) (any, error) {
	for {
		m, err := c.receive()
		if err != nil {
			return nil, err
		}

		switch m.kind {
		case "call":
			if err := c.serve(m); err != nil {
				return nil, err
			}
		case "ret", "err":
			if m.id != id {
				return nil, c.protocol("reply " + strconv.FormatInt(m.id, 10) + " does not answer the innermost call " + strconv.FormatInt(id, 10))
			}
			if m.kind == "err" {
				return nil, m.exception
			}

			return m.value, nil
		default:
			return nil, c.protocol("unknown message kind " + strconv.Quote(m.kind))
		}
	}
}

// serve runs the handler of one of PHP's calls and sends the reply. Only a
// dead channel is an error here; what the handler returns goes to PHP.
func (c *Conn) serve(m *inMessage) error {
	c.serving = append(c.serving, m.id)

	var (
		value any
		err   error
	)
	if h, ok := c.handlers[m.method]; ok {
		value, err = h(m.value)
	} else {
		err = &UnsupportedError{Method: m.method}
	}

	c.serving = c.serving[:len(c.serving)-1]

	if c.dead != nil {
		// PHP ended (or broke the protocol) during a nested call.
		return c.dead
	}

	if perr, ok := errors.AsType[*ProtocolError](err); ok {
		return c.protocol(perr.Message)
	}

	reply := message{kind: "ret", id: m.id, key: "v", value: value}
	if exit, ok := value.(Exit); ok && err == nil {
		reply.value = nil
		reply.exit = &exit.Code
	}
	if err != nil {
		reply = message{kind: "err", id: m.id, key: "x", value: c.exceptionValue(err, 0)}
	}

	if sendErr := c.send(reply); sendErr != nil {
		if c.dead != nil {
			return c.dead
		}
		// The value cannot cross to PHP: throw that instead.
		return c.send(message{kind: "err", id: m.id, key: "x", value: c.exceptionValue(sendErr, 0)})
	}

	return nil
}

// message is a message to PHP.
type message struct {
	kind   string
	id     int64
	method string
	key    string // "a", "v" or "x"
	value  any
	exit   *int
}

// send encodes and writes a message. An error that leaves the channel
// usable (a value that cannot cross) is returned before anything is
// written; a write failure kills the channel.
func (c *Conn) send(m message) error {
	e := c.newEncoder()

	// The sync block is encoded first: objects it defines are known when
	// the payload refers to them, as PHP applies it first.
	s, commitSync, err := c.outgoingSync(e)
	if err != nil {
		return err
	}

	var value any
	if m.value != nil {
		if value, err = e.Value(m.value); err != nil {
			return err
		}
	}

	buf := c.buf[:0]
	buf = append(buf, `{"k":"`...)
	buf = append(buf, m.kind...)
	buf = append(buf, `","id":`...)
	buf = strconv.AppendInt(buf, m.id, 10)
	if m.method != "" {
		buf = append(buf, `,"m":`...)
		if buf, err = appendJSON(buf, m.method); err != nil {
			return err
		}
	}
	if value != nil {
		buf = append(buf, `,"`...)
		buf = append(buf, m.key...)
		buf = append(buf, `":`...)
		if buf, err = appendJSON(buf, value); err != nil {
			return err
		}
	}
	if m.exit != nil {
		buf = append(buf, `,"exit":`...)
		buf = strconv.AppendInt(buf, int64(*m.exit), 10)
	}
	if s != nil {
		buf = append(buf, `,"s":`...)
		if buf, err = appendJSON(buf, s); err != nil {
			return err
		}
	}
	buf = append(buf, '}')

	frame, err := AppendFrame(make([]byte, 0, len(buf)+4), buf)
	c.buf = buf
	if err != nil {
		return err
	}

	e.commit()
	commitSync()

	c.opts.Flush()
	if _, err := c.w.Write(frame); err != nil {
		return c.lost()
	}

	return nil
}

func appendJSON(buf []byte, v any) ([]byte, error) {
	s, err := php.JSONEncodeDepth(v, jsonFlags, jsonDepth)
	if err != nil {
		return buf, err
	}

	return append(buf, s...), nil
}

// inMessage is a decoded message from PHP.
type inMessage struct {
	kind      string
	id        int64
	method    string
	value     any // "a" of a call, "v" of a ret
	exception *PHPException
}

// receive reads the next message, applies its sync block and decodes its
// payload.
func (c *Conn) receive() (*inMessage, error) {
	payload, err := ReadFrame(c.r)
	if err != nil {
		if perr, ok := errors.AsType[*ProtocolError](err); ok {
			return nil, c.protocol(perr.Message)
		}

		return nil, c.lost()
	}

	raw, err := php.JSONDecodeFlags(string(payload), php.JSONObjectAsArray|php.JSONBigintAsString, jsonDepth)
	if err != nil {
		return nil, c.protocol("undecodable message: " + err.Error())
	}
	msg, ok := raw.(*php.Array)
	if !ok {
		return nil, c.protocol("a message is not an object")
	}

	m := &inMessage{}
	m.kind, _ = msg.GetString("k")
	idv, _ := msg.Get("id")
	m.id, ok = idv.(int64)
	if !ok {
		return nil, c.protocol("a message has no id")
	}
	m.method, _ = msg.GetString("m")

	if m.kind == "call" {
		if m.id <= c.lastPHPID {
			return nil, c.protocol("call id " + strconv.FormatInt(m.id, 10) + " does not ascend")
		}
		c.lastPHPID = m.id
	}

	// Tags only exist where the text has an escaped NUL.
	d := &Decoder{c: c}
	tags := bytes.Contains(payload, []byte(`\u0000`))
	decode := func(v any) (any, error) {
		if !tags {
			return v, nil
		}

		return d.Value(v)
	}

	if s, ok := msg.Get("s"); ok {
		s, err := decode(s)
		if err != nil {
			return nil, c.fail(err)
		}
		if err := c.applySync(s); err != nil {
			return nil, c.fail(err)
		}
	}

	key := "v"
	if m.kind == "call" {
		key = "a"
	}
	if v, ok := msg.Get(key); ok {
		if m.value, err = decode(v); err != nil {
			return nil, c.fail(err)
		}
	}

	if m.kind == "err" {
		x, _ := msg.Get("x")
		if x, err = decode(x); err != nil {
			return nil, c.fail(err)
		}
		if m.exception, err = c.decodeException(x, 0); err != nil {
			return nil, c.fail(err)
		}
	}

	return m, nil
}

// fail turns an error met while reading a message into a protocol error
// (it was malformed) unless it already is one.
func (c *Conn) fail(err error) error {
	if perr, ok := errors.AsType[*ProtocolError](err); ok {
		return c.protocol(perr.Message)
	}

	return c.protocol(err.Error())
}

// lost handles a channel that failed: the PHP process is ending, so wait
// for its status.
func (c *Conn) lost() error {
	if c.dead == nil {
		c.dead = &PHPExit{Code: c.peer.Wait()}
	}

	return c.dead
}

// protocol kills the PHP process after a protocol violation.
func (c *Conn) protocol(message string) error {
	if c.dead == nil {
		c.peer.Kill()
		c.peer.Wait()
		c.dead = &ProtocolError{Message: message}
	}

	return c.dead
}

// maxExceptionDepth bounds previous chains crossing the channel.
const maxExceptionDepth = 64

// exceptionValue is the "x" of an error going to PHP (docs/PLUGINS.md
// §5.10): a PHP exception that comes back unchanged is named by its
// handle, so PHP rethrows the original object; any other error becomes an
// instance of the class it names (console.Throwable), or the PHP class of
// internal/util's error types, or \RuntimeException.
func (c *Conn) exceptionValue(err error, depth int) *php.Array {
	var x *php.Array

	if pe, ok := err.(*PHPException); ok { //nolint:errorlint // only the exception itself keeps its identity.
		x = php.ArrayOf("class", pe.Class, "message", pe.Message, "code", int64(pe.Code), "h", int64(pe.H))
		if pe.Previous != nil && depth < maxExceptionDepth {
			x.Set("previous", c.exceptionValue(pe.Previous, depth+1))
		}

		return x
	}

	class, code := "RuntimeException", 0
	var previous error
	if pe, ok := errors.AsType[*PHPException](err); ok {
		// Go wrapped a PHP exception with context: `new
		// \RuntimeException($context, 0, $e)`, keeping $e itself.
		previous = pe
	} else if t, ok := errors.AsType[console.Throwable](err); ok {
		class, code, previous = t.ThrowableClass(), t.ThrowableCode(), t.ThrowablePrevious()
	} else if pc, ok := err.(util.PHPClasser); ok { //nolint:errorlint // the error itself names its class, as goErrorClass's.
		class, code = pc.PHPClass()
	} else if cl, cd, ok := goErrorClass(err); ok {
		class, code = cl, cd
	}

	if previous == nil {
		previous = phperr.PreviousOf(err)
	}

	x = php.ArrayOf("class", class, "message", err.Error(), "code", int64(code))
	// Composer's throw site (docs/PLUGINS.md §5.10): Symfony renders "In
	// <file> line <n>:" from it.
	if site, ok := phperr.SiteOf(err); ok {
		x.Set("file", site.File)
		x.Set("line", int64(site.Line))
	}
	if previous != nil && depth < maxExceptionDepth {
		x.Set("previous", c.exceptionValue(previous, depth+1))
	}

	return x
}

// decodeException builds the *PHPException of a decoded "x".
func (c *Conn) decodeException(v any, depth int) (*PHPException, error) {
	x, ok := v.(*php.Array)
	if !ok || depth > maxExceptionDepth {
		return nil, &ProtocolError{Message: "invalid exception"}
	}

	e := &PHPException{}
	e.Class, _ = x.GetString("class")
	e.Message, _ = x.GetString("message")
	e.File, _ = x.GetString("file")
	if code, ok := x.Get("code"); ok {
		e.Code = php.ToNativeInt(code)
	}
	if line, ok := x.Get("line"); ok {
		e.Line = php.ToNativeInt(line)
	}
	if h, ok := x.Get("h"); ok {
		e.H = Handle(php.ToInt(h))
	}
	if classes, ok := x.GetArray("classes"); ok {
		for _, cl := range classes.Values() {
			e.Classes = append(e.Classes, php.ToString(cl))
		}
	}
	if trace, ok := x.GetArray("trace"); ok {
		for _, f := range trace.Values() {
			frame, _ := f.(*php.Array)
			if frame == nil {
				continue
			}
			var tf console.TraceFrame
			tf.File, _ = frame.GetString("file")
			tf.Class, _ = frame.GetString("class")
			tf.Type, _ = frame.GetString("type")
			tf.Function, _ = frame.GetString("function")
			if line, ok := frame.Get("line"); ok {
				tf.Line = php.ToNativeInt(line)
			}
			e.Trace = append(e.Trace, tf)
		}
	}
	if prev, ok := x.Get("previous"); ok && prev != nil {
		p, err := c.decodeException(prev, depth+1)
		if err != nil {
			return nil, err
		}
		e.Previous = p
	}
	e.Extra, _ = x.Get("extra")

	return e, nil
}
