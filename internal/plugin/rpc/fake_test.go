package rpc

import (
	"bufio"
	"io"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// fakePHP is a Go stand-in for the shim's side of a Conn: tests script the
// messages it reads and writes.
type fakePHP struct {
	t   *testing.T
	in  *bufio.Reader // what Go wrote
	out io.WriteCloser
}

// fakePeer is the process of a fakePHP.
type fakePeer struct {
	mu      sync.Mutex
	status  int
	done    chan struct{}
	once    sync.Once
	closers []io.Closer
	killed  bool
}

func (p *fakePeer) Wait() int {
	<-p.done

	return p.status
}

func (p *fakePeer) Kill() {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	p.exit(137)
}

// exit ends the fake process with status: the channel closes.
func (p *fakePeer) exit(status int) {
	p.once.Do(func() {
		p.mu.Lock()
		p.status = status
		p.mu.Unlock()
		for _, c := range p.closers {
			_ = c.Close()
		}
		close(p.done)
	})
}

func (p *fakePeer) wasKilled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.killed
}

// newFake returns a Conn wired to a fakePHP.
func newFake(t *testing.T, opts Options) (*Conn, *fakePHP, *fakePeer) {
	t.Helper()

	goR, phpW := io.Pipe() // PHP → Go
	phpR, goW := io.Pipe() // Go → PHP

	peer := &fakePeer{done: make(chan struct{}), closers: []io.Closer{goR, phpW, phpR, goW}}
	t.Cleanup(func() { peer.exit(0) })

	c := NewConn(goR, goW, peer, opts)
	f := &fakePHP{t: t, in: bufio.NewReader(phpR), out: phpW}

	return c, f, peer
}

// recv reads the next message Go sent.
func (f *fakePHP) recv() *php.Array {
	f.t.Helper()

	payload, err := ReadFrame(f.in)
	if err != nil {
		f.t.Errorf("fake php: read: %v", err)

		return php.NewArray()
	}
	v, err := php.JSONDecodeFlags(string(payload), php.JSONObjectAsArray|php.JSONBigintAsString, jsonDepth)
	if err != nil {
		f.t.Errorf("fake php: decode %s: %v", payload, err)

		return php.NewArray()
	}
	a, _ := v.(*php.Array)

	return a
}

// recvRaw reads the next message Go sent, undecoded.
func (f *fakePHP) recvRaw() string {
	f.t.Helper()

	payload, err := ReadFrame(f.in)
	if err != nil {
		f.t.Errorf("fake php: read: %v", err)
	}

	return string(payload)
}

// send writes a message (JSON text) to Go.
func (f *fakePHP) send(json string) {
	f.t.Helper()

	frame, err := AppendFrame(nil, []byte(json))
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.out.Write(frame); err != nil {
		f.t.Errorf("fake php: write: %v", err)
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()

	s, err := php.JSONEncodeDepth(v, jsonFlags, jsonDepth)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

// testBox is a Go mirror for the tests.
type testBox struct {
	rev       uint64
	value     any
	label     string
	class     string
	snapshots int
	applied   int
	changed   map[string]bool
}

func (*testBox) PHPOpaque() {}

func (b *testBox) PHPClass() string {
	if b.class != "" {
		return b.class
	}

	return "MaestroTestBox"
}

func (*testBox) MirrorBase() string { return "MaestroTestBox" }

func (b *testBox) Rev() uint64 { return b.rev }

func (b *testBox) MirrorSnapshot() (*php.Array, error) {
	b.snapshots++

	return php.ArrayOf("value", b.value, "label", b.label), nil
}

func (b *testBox) ApplyMirror(fields *php.Array) error {
	b.applied++
	for k, v := range fields.All() {
		switch k.String() {
		case "value":
			b.setValue(v)
		case "label":
			b.label = php.ToString(v)
			b.rev++
		}
	}

	return nil
}

func (b *testBox) setValue(v any) {
	b.value = v
	b.rev++
	if b.changed == nil {
		b.changed = map[string]bool{}
	}
	b.changed["value"] = true
}

// deltaBox also reports its changed fields.
type deltaBox struct {
	testBox
	since uint64
}

func (b *deltaBox) MirrorChanges(since uint64) (*php.Array, bool) {
	if since != b.since {
		return nil, false
	}
	out := php.NewArray()
	for k := range b.changed {
		out.Set(k, b.value)
	}
	b.changed = nil
	b.since = b.rev

	return out, true
}

// service is a plain Go-owned object.
type service struct{ name string }

func (*service) PHPOpaque() {}

func (*service) PHPClass() string { return `Composer\Config` }
