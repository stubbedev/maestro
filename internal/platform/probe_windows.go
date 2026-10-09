//go:build windows

package platform

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// runProbe starts binary on the probe script, as a file, and parses what
// it reports: as a file the script can hold php at its end (probe.php
// ends waiting for its standard input), which is what the module list
// needs. The file is php's to read in the directory the system keeps
// temporary files in, and maestro's to remove.
func runProbe(ctx context.Context, binary string) (*Snapshot, []byte, error) {
	script, err := os.CreateTemp("", "maestro-probe-*.php")
	if err != nil {
		return nil, nil, &ProbeError{Binary: binary, ExitCode: -1, Reason: err.Error()}
	}
	path := script.Name()

	_, werr := script.WriteString(probeScript)
	cerr := script.Close()
	defer func() { _ = os.Remove(path) }()

	if werr != nil || cerr != nil {
		return nil, nil, &ProbeError{Binary: binary, ExitCode: -1, Reason: errors.Join(werr, cerr).Error()}
	}

	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, binary, path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, &ProbeError{Binary: binary, ExitCode: -1, Reason: err.Error()}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, &ProbeError{Binary: binary, ExitCode: -1, Reason: err.Error()}
	}
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, nil, &ProbeError{Binary: binary, ExitCode: -1, Reason: err.Error()}
	}

	// closed whatever becomes of php: closing the pipe is what ends it
	defer func() { _ = stdin.Close() }()

	var out bytes.Buffer
	out.Grow(256 << 10)

	complete := readProbeOutput(stdout, &out)

	// everything php loaded is in the entry's files while it waits
	modules, listed := processModules(uint32(cmd.Process.Pid))

	waitErr := cmd.Wait()

	if !complete {
		code, reason := -1, "it exited before its result was whole"
		if waitErr != nil {
			reason = waitErr.Error()

			if exitErr, ok := errors.AsType[*exec.ExitError](waitErr); ok {
				code = exitErr.ExitCode()
			}
		}

		return nil, nil, &ProbeError{Binary: binary, ExitCode: code, Output: out.String() + stderr.String(), Reason: reason}
	}

	// the result is whole (hasResultLine); what follows it, a shutdown
	// error or an exit code, cannot change what it reports
	s, _, err := parseProbe(binary, out.Bytes(), stderr.String())

	if s != nil && listed {
		s.mappedFiles, s.hasMappedFiles = modules, true
	}

	return s, out.Bytes(), err
}

// readProbeOutput reads php's standard output into out until it holds the
// probe's result whole (hasResultLine), the stream ends or fails; ok is
// whether the result was seen whole.
func readProbeOutput(r io.Reader, out *bytes.Buffer) (ok bool) {
	buf := make([]byte, 32<<10)

	for {
		n, err := r.Read(buf)
		out.Write(buf[:n])

		if hasResultLine(out.Bytes()) {
			return true
		}

		if err != nil {
			return false
		}
	}
}

// hasResultLine reports whether out holds the probe's result whole: the
// marker, and after it the newline that ends the result (json_encode
// escapes newlines inside strings, so the first one after the marker ends
// it; what php printed while starting up is before the marker, and
// nothing follows the result but the probe's wait).
func hasResultLine(out []byte) bool {
	i := bytes.LastIndex(out, []byte(probeMarker))

	return i >= 0 && bytes.IndexByte(out[i+len(probeMarker):], '\n') >= 0
}

// processModules is the paths of the modules the process pid has loaded:
// its executable, the libraries it linked and those its extensions and
// libraries loaded at runtime, whose upgrades change what the probe
// reports. ok is false when they cannot be told (the process gone, or a
// process out of reach): such a probe's result is not cached. A module's
// path is as the loader reports it, at most MAX_PATH long: a longer one
// comes truncated, names no file, and leaves its directory to key the
// entry.
func processModules(pid uint32) (paths []string, ok bool) {
	// TH32CS_SNAPMODULE32 adds the 32-bit modules of a process this one
	// is 64-bit to (php built for 386 or arm behind a shim): the file
	// signatures of both halves key the entry.
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32, pid)
	if err != nil {
		return nil, false
	}
	defer func() { _ = windows.CloseHandle(snapshot) }()

	var entry windows.ModuleEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	for err = windows.Module32First(snapshot, &entry); err == nil; err = windows.Module32Next(snapshot, &entry) {
		paths = append(paths, windows.UTF16ToString(entry.ExePath[:]))
	}

	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, false
	}

	return paths, true
}
