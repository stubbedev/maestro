// Ports the output side of vendor/symfony/process/Pipes/WindowsPipes.php
// (5.4). It is platform independent so it is tested everywhere; only
// Windows uses it (redirectToFiles).

package util

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// filePollInterval is how long a file pipe waits for more output when it
// has read everything written so far.
const filePollInterval = 5 * time.Millisecond

// filePipes are WindowsPipes' output files. PHP's pipes hang on Windows, so
// Symfony gives the process the null device for stdout and stderr,
// redirects the command line's output into two temporary files (" 1>file
// 2>file" after it) and reads them as they grow. Whatever the shell writes
// outside those redirections, such as cmd.exe's own complaint about a line
// it cannot parse, is lost.
type filePipes struct {
	files  [2]*os.File // stdout's, stderr's, open for reading
	exited chan struct{}
	wg     sync.WaitGroup
}

// newFilePipes creates the two temporary files.
func newFilePipes() (*filePipes, error) {
	fp := &filePipes{exited: make(chan struct{})}

	for i, name := range []string{"out", "err"} {
		// created, then opened for reading only, as Symfony does: the
		// shell opens it for writing beside that handle
		w, err := os.CreateTemp("", "maestro_proc_*."+name)

		var f *os.File
		if err == nil {
			_ = w.Close()

			if f, err = os.Open(w.Name()); err != nil {
				_ = os.Remove(w.Name())
			}
		}

		if err != nil {
			fp.cleanup()

			return nil, &RuntimeError{Class: ClassProcessRuntime, Message: "A temporary file could not be opened to write the process output: " + php.Strerror(err)}
		}

		fp.files[i] = f
	}

	return fp, nil
}

// redirections is what Symfony appends to the command line.
func (fp *filePipes) redirections() string {
	return ` 1>"` + fp.files[0].Name() + `" 2>"` + fp.files[1].Name() + `"`
}

// start copies what the process writes to the files to stdout and stderr
// until finish.
func (fp *filePipes) start(stdout, stderr io.Writer) {
	if fp == nil {
		return
	}

	fp.wg.Add(2)

	go fp.tail(fp.files[0], stdout)
	go fp.tail(fp.files[1], stderr)
}

func (fp *filePipes) tail(f *os.File, w io.Writer) {
	defer fp.wg.Done()

	buf := make([]byte, 32<<10)
	timer := time.NewTimer(filePollInterval)

	defer timer.Stop()

	for {
		if n, _ := f.Read(buf); n > 0 {
			_, _ = w.Write(buf[:n])

			continue
		}

		select {
		case <-fp.exited:
			// everything the process wrote before it exited
			for {
				n, _ := f.Read(buf)
				if n == 0 {
					return
				}

				_, _ = w.Write(buf[:n])
			}
		case <-timer.C:
			timer.Reset(filePollInterval)
		}
	}
}

// finish reads the rest of the output once the process exited, and
// removes the files.
func (fp *filePipes) finish() {
	if fp == nil {
		return
	}

	close(fp.exited)
	fp.wg.Wait()
	fp.cleanup()
}

// cleanup closes and removes the files; one a process the shell started
// still holds stays behind, as with Symfony.
func (fp *filePipes) cleanup() {
	if fp == nil {
		return
	}

	for _, f := range fp.files {
		if f != nil {
			_ = f.Close()
			_ = os.Remove(f.Name())
		}
	}
}
