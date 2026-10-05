// Ports sys_get_temp_dir() (php_get_temporary_directory in
// main/php_open_temporary_file.c) on Unix.

package php

import "os"

// SysGetTempDir returns sys_get_temp_dir() without a sys_temp_dir ini
// setting: $TMPDIR without one trailing slash when it is set and not empty,
// else P_tmpdir ("/tmp").
func SysGetTempDir() string {
	if dir := os.Getenv("TMPDIR"); dir != "" {
		if dir[len(dir)-1] == '/' {
			return dir[:len(dir)-1]
		}

		return dir
	}

	return "/tmp"
}
