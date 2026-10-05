package processmock

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/util"
)

func TestMock(t *testing.T) {
	m := New()
	m.Expects([]Expectation{
		{Cmd: util.Cmd("git", "status"), Stdout: "clean", Stderr: "warn", Return: 3},
		Shell("echo hi"),
	}, true, nil)

	var out string
	if code, err := m.Execute(util.Cmd("git", "status"), &out, "/tmp"); err != nil || code != 3 || out != "clean" || m.GetErrorOutput() != "warn" {
		t.Fatalf("got %d %v %q %q", code, err, out, m.GetErrorOutput())
	}

	// a list never matches a string expectation
	_, err := m.Execute(util.Cmd("echo", "hi"), nil, "/tmp")

	var ae *AssertionError
	if !errors.As(err, &ae) || ae.Message != "Received unexpected command array (\n  0 => 'echo',\n  1 => 'hi',\n) in \"/tmp\"\nExpected 'echo hi' at this point.\nReceived calls:\ngit status" {
		t.Fatalf("got %v", err)
	}

	if err := m.AssertComplete(); err == nil {
		t.Fatal("complete")
	}

	var chunks []string

	if _, err := m.ExecuteFunc(util.ShellCmd("echo hi"), func(typ, buf string) { chunks = append(chunks, typ+buf) }, ""); err != nil || len(chunks) != 0 {
		t.Fatalf("got %v %v", err, chunks)
	}

	if err := m.AssertComplete(); err != nil {
		t.Fatal(err)
	}
}

func TestMockDefaultHandlerAndAsync(t *testing.T) {
	m := New()
	m.Expects(nil, false, &Expectation{Return: 1, Stdout: "o", Stderr: "e"})

	p, err := m.ExecuteAsync(util.Cmd("x"), "")
	if err != nil {
		t.Fatal(err)
	}

	proc, err := p.Wait()
	if err != nil || proc.GetExitCode() != 1 || proc.GetOutput() != "o" || proc.GetErrorOutput() != "e" || proc.IsSuccessful() || proc.IsRunning() {
		t.Fatalf("got %v %+v", err, proc)
	}
}
