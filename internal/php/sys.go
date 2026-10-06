// Ports sys_get_temp_dir() (php_get_temporary_directory in
// main/php_open_temporary_file.c).

package php

import (
	"os"
	"runtime"
)

// SysGetTempDir returns sys_get_temp_dir() without a sys_temp_dir ini
// setting. On Unix that is $TMPDIR without one trailing slash when it is
// set and not empty, else P_tmpdir ("/tmp"). On Windows PHP asks
// GetTempPathW (TMP, TEMP, USERPROFILE, then the Windows directory) and
// strips the trailing backslash, which is what os.TempDir does there.
func SysGetTempDir() string {
	if runtime.GOOS == "windows" {
		return os.TempDir()
	}

	if dir := os.Getenv("TMPDIR"); dir != "" {
		if dir[len(dir)-1] == '/' {
			return dir[:len(dir)-1]
		}

		return dir
	}

	return "/tmp"
}
