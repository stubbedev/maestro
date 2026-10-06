package rpc

import (
	"testing"

	"github.com/stubbedev/maestro/internal/util"
)

// runCall is Run of a call of method, waiting for its result.
func runCall(c *Conn, method string) (any, error) {
	var (
		v   any
		err error
	)
	done := make(chan struct{})
	if !c.Run(func() {
		v, err = c.Call(method, nil)
		close(done)
	}) {
		<-done
	}

	return v, err
}

// Parallel work's calls of PHP (Run) are made by the goroutine holding
// the baton, in the order they were made, while it waits for the work
// (its wait hooks); with the baton free, by the goroutine making them.
func TestRun(t *testing.T) {
	c, f, _ := newFake(t, Options{})
	defer util.AddWaitHook(c.ServePosted)()

	var got []any
	c.Handle("go.wait", func(any) (any, error) {
		work := make(chan struct{})
		go func() {
			defer close(work)
			for _, m := range []string{"php.a", "php.b"} {
				v, err := runCall(c, m)
				if err != nil {
					t.Error(err)
				}
				got = append(got, v)
			}
		}()
		// the holder waiting for the work runs its calls
		util.WaitServing(work)

		return nil, nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := c.Call("x", nil)
		done <- err
	}()
	f.recv()
	f.send(`{"k":"call","id":1,"m":"go.wait"}`)
	if m := f.recv(); mustGet(m, "m") != "php.a" {
		t.Fatalf("first posted call %v", m)
	}
	f.send(`{"k":"ret","id":2,"v":"A"}`)
	if m := f.recv(); mustGet(m, "m") != "php.b" {
		t.Fatalf("second posted call %v", m)
	}
	f.send(`{"k":"ret","id":3,"v":"B"}`)
	if m := f.recv(); mustGet(m, "k") != "ret" {
		t.Fatalf("reply %v", m)
	}
	f.send(`{"k":"ret","id":1}`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Errorf("results %v", got)
	}

	// Free: the goroutine makes its call itself.
	res := make(chan any, 1)
	go func() {
		v, _ := runCall(c, "php.c")
		res <- v
	}()
	if m := f.recv(); mustGet(m, "m") != "php.c" {
		t.Fatalf("call with the baton free %v", m)
	}
	f.send(`{"k":"ret","id":4,"v":"C"}`)
	if v := <-res; v != "C" {
		t.Errorf("php.c = %v", v)
	}
}

// A call parallel work posts while the holder is in PHP is made before
// the holder gives the baton up.
func TestRun_BeforeTheBatonIsFree(t *testing.T) {
	c, f, _ := newFake(t, Options{})

	posted := make(chan struct{})
	c.Handle("go.post", func(any) (any, error) {
		go func() {
			// a call whose result does not matter: posted at once
			c.Run(func() { _, _ = c.Call("php.out", nil) })
			close(posted)
		}()
		<-posted

		return nil, nil
	})

	done := make(chan error, 1)
	go func() {
		_, err := c.Call("x", nil)
		done <- err
	}()
	f.recv()
	f.send(`{"k":"call","id":1,"m":"go.post"}`)
	if m := f.recv(); mustGet(m, "k") != "ret" {
		t.Fatalf("reply %v", m)
	}
	f.send(`{"k":"ret","id":1}`)
	if m := f.recv(); mustGet(m, "m") != "php.out" {
		t.Fatalf("posted call %v", m)
	}
	f.send(`{"k":"ret","id":2}`)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
