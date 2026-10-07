// Ports PHP's file_exists(), is_dir() and is_file() (stat(2), symlinks
// followed; a failure is false).

package php

import "os"

// FileExists ports file_exists($path).
func FileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// IsDir ports is_dir($path).
func IsDir(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.IsDir()
}

// IsFile ports is_file($path): a regular file.
func IsFile(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.Mode().IsRegular()
}
