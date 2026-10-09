//go:build !windows

package platform

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
)

// runProbe starts binary on the probe script and parses what it reports.
// The script goes in on standard input rather than with -r: like
// bin/composer it is then a script file, so auto_prepend_file runs for it
// too, and no command line quoting is involved.
func runProbe(ctx context.Context, binary string) (*Snapshot, []byte, error) {
	var stdout, stderr bytes.Buffer

	stdout.Grow(256 << 10)

	cmd := exec.CommandContext(ctx, binary)
	cmd.Stdin = strings.NewReader(probeScript)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		code := -1
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			code = exitErr.ExitCode()
		}

		return nil, nil, &ProbeError{Binary: binary, ExitCode: code, Output: stdout.String() + stderr.String(), Reason: err.Error()}
	}

	return parseProbe(binary, stdout.Bytes(), stderr.String())
}
