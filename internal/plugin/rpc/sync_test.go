package rpc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/testutil"
)

// call runs c.Call on a goroutine; the returned func waits for it.
func call(t *testing.T, c *Conn, method string, args any) func() (any, error) {
	t.Helper()

	type result struct {
		v   any
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := c.Call(method, args)
		ch <- result{v, err}
	}()

	return func() (any, error) {
		r := <-ch

		return r.v, r.err
	}
}

func TestSync_Env(t *testing.T) {
	t.Setenv("MAESTRO_SYNC_A", "1")
	c, f, _ := newFake(t, Options{})

	// Go's change goes to PHP with the next message.
	t.Setenv("MAESTRO_SYNC_A", "2")
	t.Setenv("MAESTRO_SYNC_B", "\xff")
	wait := call(t, c, "x", nil)
	m := f.recvRaw()
	if !strings.Contains(m, `"env":{"set":{"MAESTRO_SYNC_A":"2","MAESTRO_SYNC_B":{"\u0000b":"/w=="}}}`) {
		t.Errorf("first message %s", m)
	}

	// PHP's change applies before its message is handled, and does not
	// come back.
	f.send(`{"k":"ret","id":1,"s":{"env":{"set":{"MAESTRO_SYNC_C":"from php"},"unset":["MAESTRO_SYNC_A"]}}}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}
	if v := os.Getenv("MAESTRO_SYNC_C"); v != "from php" {
		t.Errorf("MAESTRO_SYNC_C = %q", v)
	}
	if _, ok := os.LookupEnv("MAESTRO_SYNC_A"); ok {
		t.Error("MAESTRO_SYNC_A still set")
	}
	t.Cleanup(func() { os.Unsetenv("MAESTRO_SYNC_C") })

	wait = call(t, c, "y", nil)
	if m := f.recvRaw(); strings.Contains(m, `"s"`) {
		t.Errorf("echo: %s", m)
	}
	f.send(`{"k":"ret","id":2}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("MAESTRO_SYNC_B")
	wait = call(t, c, "z", nil)
	if m := f.recvRaw(); !strings.Contains(m, `"env":{"unset":["MAESTRO_SYNC_B"]}`) {
		t.Errorf("unset message %s", m)
	}
	f.send(`{"k":"ret","id":3}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}
}

func TestSync_Cwd(t *testing.T) {
	start, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(start)

	// Resolved, as getcwd() reports them on both sides (macOS's temporary
	// directory is under the /var symlink).
	a, b := testutil.RealTempDir(t), testutil.RealTempDir(t)
	c, f, _ := newFake(t, Options{})

	if err := os.Chdir(a); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	wait := call(t, c, "x", nil)
	if m := f.recvRaw(); !strings.Contains(m, `"cwd":`+jsonOf(t, wd)) {
		t.Errorf("message %s, want cwd %s", m, wd)
	}
	f.send(`{"k":"ret","id":1,"s":{"cwd":` + jsonOf(t, b) + `}}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Getwd(); filepath.Clean(got) != filepath.Clean(b) {
		t.Errorf("cwd = %s, want %s", got, b)
	}

	wait = call(t, c, "y", nil)
	if m := f.recvRaw(); strings.Contains(m, `"cwd"`) {
		t.Errorf("echo: %s", m)
	}
	f.send(`{"k":"ret","id":2}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}
}

func TestSync_Statics(t *testing.T) {
	values := map[string]any{"runningCommand": nil, "processTimeout": 300}
	static := func(name string) Static {
		return Static{Get: func() any { return values[name] }, Set: func(v any) { values[name] = v }}
	}
	c, f, _ := newFake(t, Options{Statics: map[string]Static{"runningCommand": static("runningCommand"), "processTimeout": static("processTimeout")}})

	// PHP starts with Composer's initial values: nothing to send yet.
	wait := call(t, c, "x", nil)
	if m := f.recvRaw(); strings.Contains(m, `"st"`) {
		t.Errorf("first message %s", m)
	}
	f.send(`{"k":"ret","id":1,"s":{"st":{"runningCommand":"update"}}}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}
	if values["runningCommand"] != "update" {
		t.Errorf("runningCommand = %v", values["runningCommand"])
	}

	values["processTimeout"] = 0
	wait = call(t, c, "y", nil)
	if m := f.recvRaw(); !strings.Contains(m, `"s":{"st":{"processTimeout":0}}`) {
		t.Errorf("message %s", m)
	}
	f.send(`{"k":"ret","id":2}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}
}

func TestSync_MirrorRevisions(t *testing.T) {
	c, f, _ := newFake(t, Options{})
	box := &testBox{value: "a", label: "l", rev: 4}

	// First crossing: the full snapshot, in the object tag.
	wait := call(t, c, "x", php.ListOf(box))
	if m := f.recvRaw(); !strings.Contains(m, `"d":{"value":"a","label":"l"}`) || strings.Contains(m, `"s"`) {
		t.Errorf("first message %s", m)
	}
	f.send(`{"k":"ret","id":1}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}

	// Unchanged: nothing more.
	wait = call(t, c, "y", nil)
	if m := f.recvRaw(); strings.Contains(m, `"s"`) {
		t.Errorf("unchanged: %s", m)
	}
	f.send(`{"k":"ret","id":2}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}

	// Go changed it: a full snapshot with the new revision.
	box.setValue("b")
	wait = call(t, c, "z", nil)
	if m := f.recvRaw(); !strings.Contains(m, `"s":{"o":[{"h":1,"r":5,"full":{"value":"b","label":"l"}}]}`) {
		t.Errorf("changed: %s", m)
	}

	// PHP changed it: applied through the setters, not echoed.
	f.send(`{"k":"ret","id":3,"s":{"o":[{"h":1,"r":1,"f":{"label":"from php"}}]}}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}
	if box.label != "from php" || box.applied != 1 {
		t.Errorf("box = %+v", box)
	}
	snapshots := box.snapshots
	wait = call(t, c, "w", nil)
	if m := f.recvRaw(); strings.Contains(m, `"s"`) {
		t.Errorf("echo: %s", m)
	}
	f.send(`{"k":"ret","id":4}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}
	if box.snapshots != snapshots {
		t.Error("a snapshot was taken for an unchanged mirror")
	}
}

func TestSync_DeltaMirror(t *testing.T) {
	c, f, _ := newFake(t, Options{})
	box := &deltaBox{testBox{value: 1, label: "l"}, 0}

	wait := call(t, c, "x", php.ListOf(box))
	f.recv()
	f.send(`{"k":"ret","id":1}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}

	// Changed fields only.
	box.setValue(2)
	wait = call(t, c, "y", nil)
	if m := f.recvRaw(); !strings.Contains(m, `"s":{"o":[{"h":1,"r":1,"f":{"value":2}}]}`) {
		t.Errorf("delta: %s", m)
	}
	f.send(`{"k":"ret","id":2}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}

	// The changes since PHP's revision are unknown: the full snapshot.
	box.setValue(3)
	box.since = 99
	wait = call(t, c, "z", nil)
	if m := f.recvRaw(); !strings.Contains(m, `"full":{"value":3,"label":"l"}`) {
		t.Errorf("fallback: %s", m)
	}
	f.send(`{"k":"ret","id":3}`)
	if _, err := wait(); err != nil {
		t.Fatal(err)
	}
}

func TestSync_Registration(t *testing.T) {
	c, f, _ := newFake(t, Options{})
	c.RegisterMirrorFactory("MaestroTestBox", func(class string, snapshot *php.Array) (Mirror, error) {
		v, _ := snapshot.Get("value")
		l, _ := snapshot.GetString("label")

		return &testBox{class: class, value: v, label: l}, nil
	})
	var adopted any
	c.Handle("go.adopt", func(args any) (any, error) {
		adopted = args

		return "ok", nil
	})
	go func() { _, _ = c.Call("x", nil) }()
	f.recv()

	// A PHP-born mirror of a user subclass crosses to Go.
	f.send(`{"k":"call","id":1,"m":"go.adopt","a":{"\u0000o":-3,"c":"MaestroTestSubBox","base":"MaestroTestBox","d":{"value":5,"label":"born"}}}`)
	reply := f.recvRaw()
	if want := `"s":{"reg":[{"tmp":-3,"h":1}]}`; !strings.Contains(reply, want) {
		t.Errorf("reply %s, want %s", reply, want)
	}
	box, ok := adopted.(*testBox)
	if !ok || box.class != "MaestroTestSubBox" || box.value != int64(5) || box.label != "born" {
		t.Fatalf("adopted %#v", adopted)
	}

	// Both handles name it; PHP knows the new one, so no class is sent.
	if o, _ := c.Lookup(-3); o != box {
		t.Error("the PHP handle does not resolve to the adopted object")
	}
	f.send(`{"k":"call","id":2,"m":"go.adopt","a":{"\u0000o":1}}`)
	f.recv()
	if adopted != box {
		t.Error("the Go handle does not resolve to the adopted object")
	}
	out, err := c.newEncoder().Value(box)
	if err != nil || jsonOf(t, out) != `{"\u0000o":1}` {
		t.Errorf("encoded %v, %v", out, err)
	}

	// It is a mirror now: Go's changes reach PHP.
	box.setValue(6)
	f.send(`{"k":"call","id":3,"m":"go.adopt","a":null}`)
	if m := f.recvRaw(); !strings.Contains(m, `"o":[{"h":1,"r":1,"full":{"value":6,"label":"born"}}]`) {
		t.Errorf("after adoption: %s", m)
	}
	f.send(`{"k":"ret","id":1}`)
}
