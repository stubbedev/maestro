//go:build unix

package main

import (
	"io/fs"
	"os/exec"
	"syscall"
)

// exeSuffix is what a built executable's name ends in.
const exeSuffix = ""

// linkCount is the number of hard links of the file info describes.
func linkCount(_ string, info fs.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink) //nolint:unconvert // Nlink is uint16 on darwin
	}

	return 1
}

// shellCommand is Symfony's Process::fromShellCommandline($line) on Unix:
// the line run by /bin/sh.
func shellCommand(line string) *exec.Cmd {
	return exec.Command("/bin/sh", "-c", line)
}
