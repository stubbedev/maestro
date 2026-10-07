package rpc

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

func TestFrame_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	for _, payload := range []string{"1", `{"k":"call"}`, strings.Repeat("x", 70000)} {
		frame, err := AppendFrame(nil, []byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		buf.Write(frame)
	}

	r := bufio.NewReader(&buf)
	for _, want := range []string{"1", `{"k":"call"}`, strings.Repeat("x", 70000)} {
		got, err := ReadFrame(r)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("frame = %.40q, want %.40q", got, want)
		}
	}
}

func TestFrame_InvalidLength(t *testing.T) {
	for _, header := range [][]byte{{0, 0, 0, 0}, {0x40, 0, 0, 1}, {0xff, 0xff, 0xff, 0xff}} {
		_, err := ReadFrame(bufio.NewReader(bytes.NewReader(header)))
		if _, ok := errors.AsType[*ProtocolError](err); !ok {
			t.Errorf("ReadFrame(% x) = %v, want a ProtocolError", header, err)
		}
	}
	if _, err := AppendFrame(nil, nil); err == nil {
		t.Error("AppendFrame accepted an empty payload")
	}
}

// TestCodec_Goldens decodes what the shim's PHP codec wrote
// (tools/oracle/plugin/codec.php) and encodes it again: the bytes must
// be the same.
func TestCodec_Goldens(t *testing.T) {
	files, err := filepath.Glob("testdata/codec/*.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 15 {
		t.Fatalf("only %d codec goldens", len(files))
	}

	c, _, _ := newFake(t, Options{})
	for _, file := range files {
		t.Run(strings.TrimSuffix(filepath.Base(file), ".json"), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			raw, err := php.JSONDecodeFlags(string(data), php.JSONObjectAsArray|php.JSONBigintAsString, jsonDepth)
			if err != nil {
				t.Fatal(err)
			}
			v, err := (&Decoder{c: c}).Value(raw)
			if err != nil {
				t.Fatal(err)
			}
			out, err := c.newEncoder().Value(v)
			if err != nil {
				t.Fatal(err)
			}
			if got := jsonOf(t, out); got != string(data) {
				t.Errorf("re-encoded\n got %s\nwant %s", got, data)
			}
		})
	}
}

func TestCodec_DecodedValues(t *testing.T) {
	c, _, _ := newFake(t, Options{})
	decode := func(json string) any {
		t.Helper()
		raw, err := php.JSONDecodeFlags(json, php.JSONObjectAsArray|php.JSONBigintAsString, jsonDepth)
		if err != nil {
			t.Fatal(err)
		}
		v, err := (&Decoder{c: c}).Value(raw)
		if err != nil {
			t.Fatal(err)
		}

		return v
	}

	if v := decode(`{"\u0000b":"//4="}`); v != "\xff\xfe" {
		t.Errorf("binary = %q", v)
	}
	if v, _ := decode(`{"\u0000f":"NAN"}`).(float64); !math.IsNaN(v) {
		t.Errorf("NAN = %v", v)
	}
	if v := decode(`{"\u0000f":"-INF"}`); v != math.Inf(-1) {
		t.Errorf("-INF = %v", v)
	}
	if v := decode(`1.0`); v != 1.0 {
		t.Errorf("1.0 = %#v", v)
	}
	if v := decode(`{"5":"five","07":"x"}`).(*php.Array); !v.Has(int64(5)) || !v.Has("07") {
		t.Errorf("keys = %v", v.Keys())
	}
	pairs := decode(`{"\u0000e":[["\u0000a",1],[{"\u0000b":"/w=="},2],["3",3]]}`).(*php.Array)
	if k, _, _ := pairs.First(); k.String() != "\x00a" || !pairs.Has("\xff") || !pairs.Has(int64(3)) {
		t.Errorf("pairs = %v", pairs.Keys())
	}
	obj, ok := decode(`{"\u0000s":{"a":1}}`).(*php.Object)
	if !ok || obj.Len() != 1 {
		t.Errorf("stdClass = %#v", obj)
	}

	// A PHP-owned object keeps its identity; its class comes once.
	first := decode(`{"\u0000o":-4,"c":"Closure"}`).(*PHPObject)
	if again := decode(`{"\u0000o":-4}`); again != first || first.Class != "Closure" {
		t.Errorf("PHP object = %#v, again %#v", first, again)
	}

	for _, bad := range []string{`{"\u0000o":-99}`, `{"\u0000o":5,"c":"X"}`, `{"\u0000f":"x"}`, `{"\u0000b":"*"}`, `{"\u0000z":1}`, `{"\u0000e":[[1]]}`} {
		raw, _ := php.JSONDecodeFlags(bad, php.JSONObjectAsArray, jsonDepth)
		if _, err := (&Decoder{c: c}).Value(raw); err == nil {
			t.Errorf("%s decoded", bad)
		}
	}
}

func TestCodec_GoValues(t *testing.T) {
	c, _, _ := newFake(t, Options{})
	enc := func(v any) string {
		t.Helper()
		out, err := c.newEncoder().Value(v)
		if err != nil {
			t.Fatal(err)
		}

		return jsonOf(t, out)
	}

	cases := []struct {
		v    any
		want string
	}{
		{nil, `null`},
		{int32(-3), `-3`},
		{uint16(9), `9`},
		{float32(0.5), `0.5`},
		{2.0, `2.0`},
		{[]byte("\x80"), `{"\u0000b":"gA=="}`},
		{[]string{"a", "\xff"}, `["a",{"\u0000b":"/w=="}]`},
		{[]any{1, "x", nil}, `[1,"x",null]`},
		{map[string]any{"b": 1, "a": math.Inf(1)}, `{"a":{"\u0000f":"INF"},"b":1}`},
		{php.ArrayOf("\x00k", 1), `{"\u0000e":[["\u0000k",1]]}`},
		{php.ArrayOf("k", php.ArrayOf("\xfe", 1)), `{"k":{"\u0000e":[[{"\u0000b":"/g=="},1]]}}`},
		{php.ObjectFromArray(php.ArrayOf("p", "\xff")), `{"\u0000s":{"p":{"\u0000b":"/w=="}}}`},
	}
	for _, tc := range cases {
		if got := enc(tc.v); got != tc.want {
			t.Errorf("%#v encoded as %s, want %s", tc.v, got, tc.want)
		}
	}

	if _, err := c.newEncoder().Value(struct{}{}); err == nil {
		t.Error("an arbitrary struct crossed")
	}
	if _, err := c.newEncoder().Value(uint64(math.MaxUint64)); err == nil {
		t.Error("a uint64 beyond PHP's int crossed")
	}
	if _, err := c.newEncoder().Value(Handle(77)); err == nil {
		t.Error("an unknown handle crossed")
	}

	// The input array is not changed by encoding.
	in := php.ArrayOf("a", "\xff")
	_ = enc(in)
	if v, _ := in.Get("a"); v != "\xff" {
		t.Errorf("input changed: %v", v)
	}
	// An array with nothing to encode is not copied.
	plain := php.ArrayOf("a", 1, "b", php.ListOf("x"))
	if out, _ := c.newEncoder().Value(plain); out != plain {
		t.Error("an array needing no change was copied")
	}
}

func TestCodec_GoObjects(t *testing.T) {
	c, f, _ := newFake(t, Options{})
	svc := &service{name: "config"}
	box := &testBox{value: "v", label: "l"}

	done := make(chan error, 1)
	go func() {
		_, err := c.Call("x", php.ListOf(svc, svc, box))
		if err == nil {
			_, err = c.Call("y", php.ListOf(svc, box))
		}
		done <- err
	}()

	first := f.recvRaw()
	if want := `"a":[{"\u0000o":1,"c":"Composer\\Config"},{"\u0000o":1},{"\u0000o":2,"c":"MaestroTestBox","base":"MaestroTestBox","d":{"value":"v","label":"l"}}]`; !strings.Contains(first, want) {
		t.Errorf("first message %s, want %s", first, want)
	}
	f.send(`{"k":"ret","id":1}`)
	second := f.recvRaw()
	if want := `"a":[{"\u0000o":1},{"\u0000o":2}]`; !strings.Contains(second, want) {
		t.Errorf("second message %s, want %s", second, want)
	}
	f.send(`{"k":"ret","id":2}`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if o, ok := c.Lookup(1); !ok || o != svc {
		t.Errorf("Lookup(1) = %v", o)
	}
	if _, err := c.newEncoder().Value(fakeValue{}); err == nil {
		t.Error("a non-pointer object crossed")
	}
}

// fakeValue is an Object that is not a pointer.
type fakeValue struct{}

func (fakeValue) PHPOpaque() {}

func (fakeValue) PHPClass() string { return "X" }

func TestConn_CallsNestBothWays(t *testing.T) {
	c, f, _ := newFake(t, Options{})
	var depths []int
	c.Handle("go.add", func(args any) (any, error) {
		depths = append(depths, c.Depth())
		a := args.(*php.Array)
		n, _ := a.Get("n")
		if n.(int64) == 0 {
			return "bottom", nil
		}
		// Back into PHP, from inside PHP's call.
		return c.Call("php.down", php.ArrayOf("n", n.(int64)-1))
	})

	done := make(chan any, 1)
	go func() {
		v, err := c.Call("php.down", php.ArrayOf("n", int64(2)))
		if err != nil {
			done <- err
		}
		done <- v
	}()

	// Go → PHP php.down(2) → Go go.add(1) → PHP php.down(0) → Go go.add(0).
	m := f.recv()
	if m.Len() == 0 || php.ToString(mustGet(m, "m")) != "php.down" {
		t.Fatalf("got %v", m)
	}
	f.send(`{"k":"call","id":1,"m":"go.add","a":{"n":1}}`)
	if m := f.recv(); php.ToString(mustGet(m, "m")) != "php.down" || php.ToInt(mustGet(m, "id")) != 2 {
		t.Fatalf("nested call %v", m)
	}
	f.send(`{"k":"call","id":2,"m":"go.add","a":{"n":0}}`)
	if m := f.recv(); php.ToString(mustGet(m, "k")) != "ret" || php.ToInt(mustGet(m, "id")) != 2 || mustGet(m, "v") != "bottom" {
		t.Fatalf("innermost reply %v", m)
	}
	f.send(`{"k":"ret","id":2,"v":"up1"}`)
	if m := f.recv(); php.ToString(mustGet(m, "k")) != "ret" || php.ToInt(mustGet(m, "id")) != 1 || mustGet(m, "v") != "up1" {
		t.Fatalf("reply %v", m)
	}
	f.send(`{"k":"ret","id":1,"v":"top"}`)

	if v := <-done; v != "top" {
		t.Fatalf("Call = %v", v)
	}
	if fmt.Sprint(depths) != "[2 4]" {
		t.Errorf("depths = %v", depths)
	}
	if c.Depth() != 0 {
		t.Errorf("Depth after = %d", c.Depth())
	}
}

func mustGet(a *php.Array, k string) any {
	v, _ := a.Get(k)

	return v
}

func TestConn_ProtocolErrors(t *testing.T) {
	cases := map[string]func(f *fakePHP){
		"wrong reply id":       func(f *fakePHP) { f.send(`{"k":"ret","id":9}`) },
		"not json":             func(f *fakePHP) { f.send(`{nope`) },
		"no id":                func(f *fakePHP) { f.send(`{"k":"ret"}`) },
		"unknown kind":         func(f *fakePHP) { f.send(`{"k":"what","id":1}`) },
		"unknown tag":          func(f *fakePHP) { f.send(`{"k":"ret","id":1,"v":{"\u0000q":1}}`) },
		"call id not rising":   func(f *fakePHP) { f.send(`{"k":"call","id":0,"m":"x"}`) },
		"bad frame":            func(f *fakePHP) { _, _ = f.out.Write([]byte{0, 0, 0, 0}) },
		"handler says so":      func(f *fakePHP) { f.send(`{"k":"call","id":1,"m":"bad"}`) },
		"invalid sync":         func(f *fakePHP) { f.send(`{"k":"ret","id":1,"s":5}`) },
		"unknown mirror":       func(f *fakePHP) { f.send(`{"k":"ret","id":1,"s":{"o":[{"h":3,"r":1,"f":{}}]}}`) },
		"invalid exception":    func(f *fakePHP) { f.send(`{"k":"err","id":1,"x":5}`) },
		"unknown handle reply": func(f *fakePHP) { f.send(`{"k":"ret","id":1,"v":{"\u0000o":7}}`) },
	}
	for name, misbehave := range cases {
		t.Run(name, func(t *testing.T) {
			c, f, peer := newFake(t, Options{})
			c.Handle("bad", func(any) (any, error) { return nil, &ProtocolError{Message: "bad"} })

			done := make(chan error, 1)
			go func() {
				_, err := c.Call("x", nil)
				done <- err
			}()
			f.recv()
			misbehave(f)

			err := <-done
			var perr *ProtocolError
			if !errors.As(err, &perr) {
				t.Fatalf("Call = %v, want a ProtocolError", err)
			}
			if !peer.wasKilled() {
				t.Error("the process was not killed")
			}
			if _, err := c.Call("y", nil); !errors.As(err, &perr) {
				t.Errorf("a later Call = %v", err)
			}
		})
	}
}

func TestConn_PHPExit(t *testing.T) {
	c, f, peer := newFake(t, Options{})
	c.Handle("go.nested", func(any) (any, error) {
		_, err := c.Call("php.exit", nil)

		return nil, err
	})

	done := make(chan error, 1)
	go func() {
		_, err := c.Call("x", nil)
		done <- err
	}()
	f.recv()
	f.send(`{"k":"call","id":1,"m":"go.nested"}`)
	f.recv() // php.exit: the process ends mid-call.
	peer.exit(3)

	err := <-done
	var exit *PHPExit
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Fatalf("Call = %v, want PHPExit 3", err)
	}
	if peer.wasKilled() {
		t.Error("an exited process was killed")
	}
	if _, err := c.Call("y", nil); !errors.As(err, &exit) {
		t.Errorf("later Call = %v", err)
	}
	if code, err := c.Shutdown(0); err != nil || code != 3 {
		t.Errorf("Shutdown = %d, %v", code, err)
	}
}

func TestConn_Shutdown(t *testing.T) {
	c, f, peer := newFake(t, Options{})
	c.Handle("go.inShutdown", func(any) (any, error) { return "late", nil })

	done := make(chan int, 1)
	go func() {
		code, err := c.Shutdown(4)
		if err != nil {
			t.Error(err)
		}
		done <- code
	}()

	m := f.recv()
	if mustGet(m, "m") != "shutdown" || php.ToInt(mustGet(mustGet(m, "a").(*php.Array), "code")) != 4 {
		t.Fatalf("shutdown message %v", m)
	}
	// A shutdown function calls maestro, then calls exit(5).
	f.send(`{"k":"call","id":1,"m":"go.inShutdown"}`)
	if m := f.recv(); mustGet(m, "v") != "late" {
		t.Fatalf("reply %v", m)
	}
	peer.exit(5)

	if code := <-done; code != 5 {
		t.Errorf("Shutdown = %d, want 5", code)
	}
}

func TestConn_Exceptions(t *testing.T) {
	c, f, _ := newFake(t, Options{})
	var fromPHP error
	c.Handle("go.call", func(any) (any, error) {
		_, err := c.Call("php.throw", nil)
		fromPHP = err

		return nil, err
	})
	c.Handle("go.wrap", func(any) (any, error) { return nil, fmt.Errorf("context: %w", fromPHP) })
	c.Handle("go.script", func(any) (any, error) {
		return nil, &eventdispatcher.ScriptExecutionError{Message: "Error Output: x", Code: 3}
	})
	c.Handle("go.runtime", func(any) (any, error) { return nil, &util.RuntimeError{Message: "rt"} })
	c.Handle("go.plain", func(any) (any, error) { return nil, errors.New("plain") })

	done := make(chan error, 1)
	go func() {
		_, err := c.Call("x", nil)
		done <- err
	}()
	f.recv()

	// PHP → Go: an exception with its class chain, trace and previous.
	f.send(`{"k":"call","id":1,"m":"go.call"}`)
	f.recv()
	f.send(`{"k":"err","id":2,"x":{"class":"Composer\\EventDispatcher\\ScriptExecutionException","classes":["Composer\\EventDispatcher\\ScriptExecutionException","RuntimeException","Exception","Throwable"],"message":"failed","code":3,"file":"/p/a.php","line":12,"trace":[{"file":"/p/b.php","line":3,"class":"A","type":"->","function":"f"}],"previous":{"class":"LogicException","classes":["LogicException","Exception","Throwable"],"message":"inner","code":0,"file":"","line":0,"trace":[],"previous":null,"h":-2},"h":-1,"extra":{"\u0000s":[]}}}`)

	// Go returns it unchanged: PHP gets its handle back.
	reply := f.recv()
	x := mustGet(reply, "x").(*php.Array)
	if mustGet(reply, "k") != "err" || php.ToInt(mustGet(x, "h")) != -1 {
		t.Fatalf("reply %v", reply)
	}

	pe, ok := fromPHP.(*PHPException) //nolint:errorlint // the exception itself, unwrapped
	if !ok {
		t.Fatalf("error from PHP: %T", fromPHP)
	}
	if pe.Class != `Composer\EventDispatcher\ScriptExecutionException` || pe.Code != 3 || pe.Line != 12 || pe.File != "/p/a.php" || pe.H != -1 || pe.Previous == nil || pe.Previous.Message != "inner" {
		t.Errorf("exception %+v", pe)
	}
	if tr := pe.ThrowableTrace(); len(tr) != 1 || tr[0] != (console.TraceFrame{File: "/p/b.php", Line: 3, Class: "A", Type: "->", Function: "f"}) {
		t.Errorf("trace %+v", tr)
	}
	var se *eventdispatcher.ScriptExecutionError
	if !errors.As(fromPHP, &se) || se.Code != 3 {
		t.Errorf("errors.As ScriptExecutionError = %v", se)
	}
	if !phperr.InstanceOf(fromPHP, "RuntimeException") {
		t.Error("not a RuntimeException")
	}
	if phperr.InstanceOf(fromPHP, "LogicException") {
		t.Error("a RuntimeException is a LogicException")
	}
	if phperr.InstanceOf(pe, "Error") {
		t.Error("an Exception is a \\Error")
	}

	id := 1
	check := func(method, want string) {
		t.Helper()
		id++
		f.send(`{"k":"call","id":` + strconv.Itoa(id) + `,"m":"` + method + `"}`)
		got := jsonOf(t, mustGet(f.recv(), "x"))
		if got != want {
			t.Errorf("%s: x = %s\nwant %s", method, got, want)
		}
	}
	// Wrapped: a new RuntimeException whose previous is the original.
	// The PHP exceptions go back with their traces (docs/PLUGINS.md §5.12).
	check("go.wrap", `{"class":"RuntimeException","message":"context: failed","code":0,"previous":{"class":"Composer\\EventDispatcher\\ScriptExecutionException","message":"failed","code":3,"h":-1,"trace":[{"file":"/p/b.php","line":3,"class":"A","type":"->","function":"f"}],"previous":{"class":"LogicException","message":"inner","code":0,"h":-2,"trace":[]}}}`)
	check("go.script", `{"class":"Composer\\EventDispatcher\\ScriptExecutionException","message":"Error Output: x","code":3}`)
	check("go.runtime", `{"class":"RuntimeException","message":"rt","code":0}`)
	check("go.plain", `{"class":"RuntimeException","message":"plain","code":0}`)
	check("go.missing", `{"class":"Maestro\\Shim\\UnsupportedApiException","message":"maestro has no handler for go.missing","code":0}`)

	f.send(`{"k":"ret","id":1}`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestConn_ReplyValueCannotCross(t *testing.T) {
	c, f, _ := newFake(t, Options{})
	c.Handle("go.bad", func(any) (any, error) { return struct{}{}, nil })
	c.Handle("go.exit", func(any) (any, error) { return Exit{Code: 9}, nil })

	done := make(chan error, 1)
	go func() {
		_, err := c.Call("x", nil)
		done <- err
	}()
	f.recv()
	f.send(`{"k":"call","id":1,"m":"go.bad"}`)
	if m := f.recv(); mustGet(m, "k") != "err" {
		t.Errorf("reply %v", m)
	}
	f.send(`{"k":"call","id":2,"m":"go.exit"}`)
	if raw := f.recvRaw(); raw != `{"k":"ret","id":2,"exit":9}` {
		t.Errorf("exit reply %s", raw)
	}
	f.send(`{"k":"ret","id":1}`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// An argument that cannot cross fails the call without sending it.
	if _, err := c.Call("y", struct{}{}); err == nil || c.Err() != nil {
		t.Errorf("Call = %v, Err = %v", err, c.Err())
	}
}

func TestBaton(t *testing.T) {
	c, f, _ := newFake(t, Options{})
	var fromOther, delegated error
	c.Handle("go.spawn", func(any) (any, error) {
		// A goroutine that does not hold the baton must not call PHP.
		ch := make(chan error)
		go func() {
			_, err := c.Call("php.x", nil)
			ch <- err
		}()
		fromOther = <-ch

		// One the handler lends the baton to may.
		delegated = c.Delegate(func() error {
			v, err := c.Call("php.y", nil)
			if err == nil && v != "y" {
				err = fmt.Errorf("php.y = %v", v)
			}

			return err
		})

		return nil, nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := c.Call("x", nil)
		done <- err
	}()
	f.recv()
	f.send(`{"k":"call","id":1,"m":"go.spawn"}`)
	if m := f.recv(); mustGet(m, "m") != "php.y" {
		t.Fatalf("delegated call %v", m)
	}
	f.send(`{"k":"ret","id":2,"v":"y"}`)
	f.recv()
	f.send(`{"k":"ret","id":1}`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if !errors.Is(fromOther, ErrBaton) {
		t.Errorf("call from another goroutine = %v, want ErrBaton", fromOther)
	}
	if delegated != nil {
		t.Errorf("delegated call = %v", delegated)
	}

	// Free again: any goroutine may take it.
	go func() {
		_, err := c.Call("z", nil)
		done <- err
	}()
	f.recv()
	f.send(`{"k":"ret","id":3}`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
