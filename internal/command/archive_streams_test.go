// archive through the Application on a real project: which stream each
// line goes to, and the archive actually written.

package command_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
)

func TestArchiveCommand_Streams(t *testing.T) {
	tests := []struct {
		name string
		args []any
		code int
		// stdout is exact; stderr holds each of these texts, in order
		stdout string
		stderr []string
		// err is the message of the exception run() lets through
		err string
		// written is the archive left on disk ("" = none)
		written string
	}{
		{
			name:    "only the archive's path is on stdout",
			args:    []any{"--dir", "out", "--file", "app"},
			stdout:  "out/app.tar\n",
			stderr:  []string{`Creating the archive into "out".`, "Created: "},
			written: "out/app.tar",
		},
		{
			name:   "an unknown format writes nothing",
			args:   []any{"--format", "rar", "--dir", "out", "--file", "app"},
			stderr: []string{`Creating the archive into "out".`},
			err:    "No archiver found to support rar format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := commandtest.InitTempComposer(t, `{"name": "acme/app", "version": "1.0.0"}`, nil, nil, false)
			if err := os.WriteFile(dir+"/app.php", []byte("<?php\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			got := commandtest.GetApplicationTester(t).RunStreams(append([]any{"command", "archive"}, tt.args...)...)

			if tt.err != "" {
				if got.Err == nil || !strings.Contains(got.Err.Error(), tt.err) {
					t.Fatalf("error = %v, want %q", got.Err, tt.err)
				}
			} else if got.Err != nil {
				t.Fatal(got.Err)
			}
			if got.Code != tt.code {
				t.Errorf("exit code = %d, want %d", got.Code, tt.code)
			}
			if got.Stdout != tt.stdout {
				t.Errorf("stdout = %q, want %q", got.Stdout, tt.stdout)
			}
			rest := got.Stderr
			for _, line := range tt.stderr {
				i := strings.Index(rest, line)
				if i < 0 {
					t.Fatalf("stderr lacks %q (in order):\n%s", line, got.Stderr)
				}
				rest = rest[i+len(line):]
			}
			if tt.written != "" && !strings.HasSuffix(got.Stderr, "Created: ") {
				t.Errorf("stderr = %q, want it to end with %q: the path follows on stdout", got.Stderr, "Created: ")
			}

			entries, _ := os.ReadDir(dir + "/out")
			var written []string
			for _, e := range entries {
				written = append(written, "out/"+e.Name())
			}
			if want := strings.Fields(tt.written); strings.Join(written, " ") != strings.Join(want, " ") {
				t.Errorf("written = %v, want %v", written, want)
			}
		})
	}
}
