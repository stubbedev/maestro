//go:build windows

package fsstate

import (
	"path/filepath"

	"github.com/stubbedev/maestro/internal/php"
)

// batchName reports whether path is a batch file: its extension is .bat
// or .cmd, as PATHEXT names them, so cmd.exe runs it as a script that
// may start any php (a version manager's shim).
func batchName(path string) bool {
	ext := filepath.Ext(path)

	return php.Strcasecmp(ext, ".bat") == 0 || php.Strcasecmp(ext, ".cmd") == 0
}
