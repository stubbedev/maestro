//go:build !windows && !linux

package php

import "path/filepath"

// EvalSymlinks resolves path as PHP's realpath() does: symlinks (there are
// no junctions outside Windows).
func EvalSymlinks(path string) (string, error) { return filepath.EvalSymlinks(path) }
