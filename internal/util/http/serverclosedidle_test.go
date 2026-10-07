package http

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// isServerClosedIdle matches net/http's errServerClosedIdle by its text,
// as net/http does not export it: the text must still be net/http's.
func TestServerClosedIdleText(t *testing.T) {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Skip("no go command:", err)
	}
	src, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "src", "net", "http", "transport.go"))
	if err != nil {
		t.Skip("no net/http sources:", err)
	}
	if !bytes.Contains(src, []byte(`errServerClosedIdle = errors.New("`+serverClosedIdleText+`")`)) {
		t.Errorf("net/http's errServerClosedIdle is no longer %q", serverClosedIdleText)
	}
}
