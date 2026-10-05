package util

import "testing"

func TestProcessSetInput(t *testing.T) {
	if IsWindows() {
		t.Skip("uses cat")
	}

	p := NewProcess([]string{"cat"}, "", nil, 0)
	p.SetInput("hello\n")

	if code, err := p.Run(nil); err != nil || code != 0 || p.GetOutput() != "hello\n" {
		t.Fatalf("got %d %v %q", code, err, p.GetOutput())
	}
}
