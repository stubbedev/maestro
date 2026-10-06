package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// exeSuffix is what a built executable's name ends in.
const exeSuffix = ".exe"

// fileReadAttributes is FILE_READ_ATTRIBUTES, all GetFileInformationByHandle
// needs.
const fileReadAttributes = 0x80

// linkCount is the number of hard links of the file at path
// (BY_HANDLE_FILE_INFORMATION's nNumberOfLinks: os.FileInfo has none on
// Windows).
func linkCount(path string, _ fs.FileInfo) uint64 {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 1
	}

	h, err := syscall.CreateFile(p, fileReadAttributes,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil,
		syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return 1
	}
	defer syscall.CloseHandle(h)

	var d syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &d); err != nil {
		return 1
	}

	return uint64(d.NumberOfLinks)
}

// shellCommand is Symfony's Process::fromShellCommandline($line) on
// Windows: `cmd /V:ON /E:ON /D /C (<line>)`, the command line passed to
// CreateProcess as is (Process quotes nothing more for a line without
// environment variable references).
func shellCommand(line string) *exec.Cmd {
	comspec := os.Getenv("ComSpec")
	if comspec == "" {
		comspec = filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	}

	cmd := exec.Command(comspec)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd /V:ON /E:ON /D /C (` + line + `)`}

	return cmd
}
