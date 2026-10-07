package platform

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// probeScript collects the snapshot; it is fed to php on its standard
// input.
//
//go:embed probe.php
var probeScript string

// ErrPHPNotFound matches a *PHPNotFoundError with errors.Is.
var ErrPHPNotFound = errors.New("no php binary was found in PATH")

// PHPNotFoundError is a value that needs PHP when there is no php binary.
// Purpose names what needed it ("platform detection", `plugin "a/b"`).
type PHPNotFoundError struct {
	Purpose string
}

func (e *PHPNotFoundError) Error() string {
	return "maestro: " + e.Purpose + " requires PHP but no php binary was found in PATH"
}

// Is makes errors.Is(err, ErrPHPNotFound) hold.
func (e *PHPNotFoundError) Is(target error) bool { return target == ErrPHPNotFound }

// FindPHP returns the php binary maestro runs PHP code with, as
// Composer's `#!/usr/bin/env php` finds the PHP Composer runs on: php on
// the PATH (PATHEXT applies on Windows). Composer has no setting choosing
// another php for itself, so neither does maestro; put the wanted php
// first in PATH.
//
// On Windows the file name keeps its case on disk, as a shell finding php
// gives it (and php then reports as PHP_BINARY): ExecutableFinder appends
// PATHEXT's extension in its own case, "php.EXE".
func FindPHP() (string, bool) {
	path, ok := util.NewExecutableFinder().Find("php")
	if ok && php.IsWindows() {
		path = nameOnDisk(path)
	}

	return path, ok
}

// nameOnDisk is path with its last element spelled as the directory lists
// it (on a case-insensitive file system).
func nameOnDisk(path string) string {
	dir, base := filepath.Split(path)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return path
	}

	for _, e := range entries {
		if php.Strcasecmp(e.Name(), base) == 0 {
			return dir + e.Name()
		}
	}

	return path
}

// Probe runs probe.php with binary and parses what it reports.
func Probe(ctx context.Context, binary string) (*Snapshot, error) {
	s, _, err := probe(ctx, binary)

	return s, err
}

// probeCached is Probe, through the cache of earlier runs' results
// (probecache_linux.go): php is started only when binary, the files it
// loads or the environment it reads changed since.
func probeCached(ctx context.Context, binary string) (*Snapshot, error) {
	key := probeCacheKey(binary)
	if key != "" {
		if s := loadProbeCache(key, binary); s != nil {
			return s, nil
		}
	}

	start := time.Now()

	s, output, err := probe(ctx, binary)
	if err == nil && key != "" {
		storeProbeCache(key, binary, s, output, start)
	}

	return s, err
}

// probe is Probe, also returning php's output.
func probe(ctx context.Context, binary string) (*Snapshot, []byte, error) {
	if timeout := util.GetProcessTimeout(); timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
		defer cancel()
	}

	var stdout, stderr bytes.Buffer

	stdout.Grow(256 << 10)

	// The script goes in on standard input rather than with -r: like
	// bin/composer it is then a script file, so auto_prepend_file runs
	// for it too, and no command line quoting is involved.
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

	s, err := ParseSnapshot(binary, stdout.Bytes())
	if err != nil {
		if pe, ok := errors.AsType[*ProbeError](err); ok {
			pe.Output += stderr.String()
		}

		return nil, nil, err
	}

	return s, stdout.Bytes(), nil
}

// Detector probes the php on the PATH once and keeps the result: the
// platform does not change while maestro runs. Start begins probing in
// the background, so that the ~20 ms php takes to start overlap with
// other work; Snapshot waits for the result. Without its own Probe, the
// result of an earlier run is used where nothing it came from changed
// (probeCached). It is safe for concurrent use.
type Detector struct {
	once sync.Once
	done chan struct{}
	snap *Snapshot
	err  error

	// FindPHP and Probe replace the package's functions when set (tests).
	FindPHP func() (string, bool)
	Probe   func(ctx context.Context, binary string) (*Snapshot, error)
}

// NewDetector returns a Detector for the php on the PATH.
func NewDetector() *Detector {
	return &Detector{}
}

// Start begins the detection in the background, once.
func (d *Detector) Start() {
	d.once.Do(func() {
		d.done = make(chan struct{})

		go func() {
			defer close(d.done)

			d.snap, d.err = d.detect()
		}()
	})
}

func (d *Detector) detect() (*Snapshot, error) {
	find, probe := d.FindPHP, d.Probe
	if find == nil {
		find = FindPHP
	}

	if probe == nil {
		probe = probeCached
	}

	binary, ok := find()
	if !ok {
		return nil, &PHPNotFoundError{Purpose: "platform detection"}
	}

	return probe(context.Background(), binary)
}

// Snapshot returns the probed platform, starting the detection if Start
// was not called. The error is a *PHPNotFoundError (errors.Is
// ErrPHPNotFound) without php, or a *ProbeError.
func (d *Detector) Snapshot() (*Snapshot, error) {
	d.Start()
	<-d.done

	return d.snap, d.err
}

// Runtime returns the Runtime of the probed php, or the Snapshot error.
func (d *Detector) Runtime() (*SnapshotRuntime, error) {
	s, err := d.Snapshot()
	if err != nil {
		return nil, err
	}

	return NewRuntime(s), nil
}
