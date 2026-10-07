//go:build unix

package util

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// builtinCommand is the in-process equivalent of the argument list args
// run in cwd, or nil when it has none and a process must run it
// (deliberate deviation 3). Only `rm -rf <dir>`, Filesystem's directory
// removal, has one: a process per removed package costs a fork and two
// execs (sh, then rm) for what one goroutine does with the same unlinks.
// Arguments rm would read differently are left to rm: an option-like or
// empty operand, a trailing slash (which follows a symlink), a final "."
// or ".." (refused) and the root directory.
func builtinCommand(args []string, cwd string) func(stderr io.Writer) int {
	if len(args) != 3 || args[0] != "rm" || args[1] != "-rf" {
		return nil
	}

	path := args[2]
	if path == "" || path[0] == '-' || strings.HasSuffix(path, "/") {
		return nil
	}

	if base := path[strings.LastIndexByte(path, '/')+1:]; base == "." || base == ".." {
		return nil
	}

	if path[0] != '/' && cwd != "" {
		path = strings.TrimRight(cwd, "/") + "/" + path
	}

	return func(stderr io.Writer) int {
		err := os.RemoveAll(path)
		if err == nil {
			return 0
		}

		failed, reason := args[2], err.Error()

		if pe, ok := errors.AsType[*fs.PathError](err); ok {
			failed, reason = pe.Path, pe.Err.Error()
		}

		if reason != "" {
			reason = php.Strtoupper(reason[:1]) + reason[1:]
		}

		_, _ = io.WriteString(stderr, "rm: cannot remove '"+failed+"': "+reason+"\n")

		return 1
	}
}
