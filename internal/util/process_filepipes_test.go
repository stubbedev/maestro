package util

import (
	"bytes"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
)

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.b.String()
}

// TestFilePipes runs a shell line with the redirections WindowsPipes add
// (sh takes them as cmd.exe does) and reads its output from the files.
func TestFilePipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh")
	}

	fp, err := newFilePipes()
	if err != nil {
		t.Fatal(err)
	}

	names := []string{fp.files[0].Name(), fp.files[1].Name()}

	// the shell's own complaint about the line goes to its stderr, outside
	// the redirections
	cmd := exec.Command("sh", "-c", `{ echo one; echo err >&2; sleep 0.05; printf two; }`+fp.redirections()+`; echo lost >&2`)

	var shellErr bytes.Buffer

	cmd.Stderr = &shellErr

	if err := cmd.Start(); err != nil {
		fp.cleanup()
		t.Fatal(err)
	}

	var stdout, stderr lockedBuffer

	fp.start(&stdout, &stderr)

	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}

	fp.finish()

	if got := stdout.String(); got != "one\ntwo" {
		t.Errorf("stdout = %q, want %q", got, "one\ntwo")
	}

	if got := stderr.String(); got != "err\n" {
		t.Errorf("stderr = %q, want %q", got, "err\n")
	}

	if got := shellErr.String(); got != "lost\n" {
		t.Errorf("the shell's own stderr = %q, want %q", got, "lost\n")
	}

	for _, name := range names {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Errorf("%s was not removed: %v", name, err)
		}
	}
}
