//go:build !windows

package util

import "path/filepath"

// evalSymlinks resolves path as PHP's realpath() does: symlinks (there are
// no junctions outside Windows).
func evalSymlinks(path string) (string, error) { return filepath.EvalSymlinks(path) }
