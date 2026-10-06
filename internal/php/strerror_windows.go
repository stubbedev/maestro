//go:build windows

package php

import "syscall"

// The errno values of the Microsoft C runtime (errno.h).
const (
	crtEPERM        = 1
	crtENOENT       = 2
	crtE2BIG        = 7
	crtENOEXEC      = 8
	crtEBADF        = 9
	crtECHILD       = 10
	crtEAGAIN       = 11
	crtENOMEM       = 12
	crtEACCES       = 13
	crtEEXIST       = 17
	crtEXDEV        = 18
	crtENOTDIR      = 20
	crtEISDIR       = 21
	crtEINVAL       = 22
	crtEMFILE       = 24
	crtENOSPC       = 28
	crtEPIPE        = 32
	crtENAMETOOLONG = 38
	crtENOTEMPTY    = 41
	crtEILSEQ       = 42
)

// crtMessages is the C runtime's strerror() table (_sys_errlist).
var crtMessages = [...]string{
	"No error",
	"Operation not permitted",
	"No such file or directory",
	"No such process",
	"Interrupted function call",
	"Input/output error",
	"No such device or address",
	"Arg list too long",
	"Exec format error",
	"Bad file descriptor",
	"No child processes",
	"Resource temporarily unavailable",
	"Not enough space",
	"Permission denied",
	"Bad address",
	"Unknown error",
	"Resource device",
	"File exists",
	"Improper link",
	"No such device",
	"Not a directory",
	"Is a directory",
	"Invalid argument",
	"Too many open files in system",
	"Too many open files",
	"Inappropriate I/O control operation",
	"Unknown error",
	"File too large",
	"No space left on device",
	"Invalid seek",
	"Read-only file system",
	"Too many links",
	"Broken pipe",
	"Domain error",
	"Result too large",
	"Unknown error",
	"Resource deadlock avoided",
	"Unknown error",
	"Filename too long",
	"No locks available",
	"Function not implemented",
	"Directory not empty",
	"Illegal byte sequence",
}

// win32Errno is php_win32_ioutil_posix_error (win32/ioutil.c), which PHP's
// file functions call with GetLastError() to set errno: the C runtime's
// _dosmaperr table, plus ERROR_INVALID_NAME and ERROR_DIRECTORY. Every
// other code is EINVAL.
var win32Errno = map[syscall.Errno]int{
	2:    crtENOENT,       // ERROR_FILE_NOT_FOUND
	3:    crtENOENT,       // ERROR_PATH_NOT_FOUND
	4:    crtEMFILE,       // ERROR_TOO_MANY_OPEN_FILES
	5:    crtEACCES,       // ERROR_ACCESS_DENIED
	6:    crtEBADF,        // ERROR_INVALID_HANDLE
	7:    crtENOMEM,       // ERROR_ARENA_TRASHED
	8:    crtENOMEM,       // ERROR_NOT_ENOUGH_MEMORY
	9:    crtENOMEM,       // ERROR_INVALID_BLOCK
	10:   crtE2BIG,        // ERROR_BAD_ENVIRONMENT
	11:   crtENOEXEC,      // ERROR_BAD_FORMAT
	12:   crtEINVAL,       // ERROR_INVALID_ACCESS
	13:   crtEINVAL,       // ERROR_INVALID_DATA
	14:   crtENOMEM,       // ERROR_OUTOFMEMORY
	15:   crtENOENT,       // ERROR_INVALID_DRIVE
	16:   crtEACCES,       // ERROR_CURRENT_DIRECTORY
	17:   crtEXDEV,        // ERROR_NOT_SAME_DEVICE
	18:   crtENOENT,       // ERROR_NO_MORE_FILES
	53:   crtENOENT,       // ERROR_BAD_NETPATH
	65:   crtEACCES,       // ERROR_NETWORK_ACCESS_DENIED
	67:   crtENOENT,       // ERROR_BAD_NET_NAME
	80:   crtEEXIST,       // ERROR_FILE_EXISTS
	82:   crtEACCES,       // ERROR_CANNOT_MAKE
	83:   crtEACCES,       // ERROR_FAIL_I24
	87:   crtEINVAL,       // ERROR_INVALID_PARAMETER
	89:   crtEAGAIN,       // ERROR_NO_PROC_SLOTS
	108:  crtEACCES,       // ERROR_DRIVE_LOCKED
	109:  crtEPIPE,        // ERROR_BROKEN_PIPE
	112:  crtENOSPC,       // ERROR_DISK_FULL
	114:  crtEBADF,        // ERROR_INVALID_TARGET_HANDLE
	123:  crtENOENT,       // ERROR_INVALID_NAME
	128:  crtECHILD,       // ERROR_WAIT_NO_CHILDREN
	129:  crtECHILD,       // ERROR_CHILD_NOT_COMPLETE
	130:  crtEBADF,        // ERROR_DIRECT_ACCESS_HANDLE
	131:  crtEINVAL,       // ERROR_NEGATIVE_SEEK
	132:  crtEACCES,       // ERROR_SEEK_ON_DEVICE
	145:  crtENOTEMPTY,    // ERROR_DIR_NOT_EMPTY
	158:  crtEACCES,       // ERROR_NOT_LOCKED
	161:  crtENOENT,       // ERROR_BAD_PATHNAME
	164:  crtEAGAIN,       // ERROR_MAX_THRDS_REACHED
	167:  crtEACCES,       // ERROR_LOCK_FAILED
	183:  crtEEXIST,       // ERROR_ALREADY_EXISTS
	206:  crtENAMETOOLONG, // ERROR_FILENAME_EXCED_RANGE
	215:  crtEAGAIN,       // ERROR_NESTING_NOT_ALLOWED
	267:  crtENOTDIR,      // ERROR_DIRECTORY
	1113: crtEILSEQ,       // ERROR_NO_UNICODE_TRANSLATION
	1816: crtENOMEM,       // ERROR_NOT_ENOUGH_QUOTA
}

// goErrno maps the errno values Go's syscall package invents on Windows
// (APPLICATION_ERROR + n) to the C runtime's.
var goErrno = map[syscall.Errno]int{
	syscall.EPERM:        crtEPERM,
	syscall.EEXIST:       crtEEXIST,
	syscall.EISDIR:       crtEISDIR,
	syscall.EINVAL:       crtEINVAL,
	syscall.EACCES:       crtEACCES,
	syscall.ENOTEMPTY:    crtENOTEMPTY,
	syscall.ENAMETOOLONG: crtENAMETOOLONG,
	syscall.EXDEV:        crtEXDEV,
	syscall.EBADF:        crtEBADF,
	syscall.EAGAIN:       crtEAGAIN,
	syscall.ENOSPC:       crtENOSPC,
	syscall.EPIPE:        crtEPIPE,
	syscall.EILSEQ:       crtEILSEQ,
}

// crtErrno maps a Windows error to the errno PHP sets for it. Win32 codes
// 19 to 36 (ERROR_WRITE_PROTECT to ERROR_SHARING_BUFFER_EXCEEDED, which
// include the sharing and lock violations of a file another process holds
// open) are EACCES.
func crtErrno(errno syscall.Errno) int {
	n, ok := win32Errno[errno]
	switch {
	case ok:
	case errno >= 19 && errno <= 36:
		n = crtEACCES
	default:
		if n, ok = goErrno[errno]; !ok {
			n = crtEINVAL
		}
	}

	return n
}

// cErrnoMessage is the C runtime's strerror(n).
func cErrnoMessage(n int) string {
	if n < 0 || n >= len(crtMessages) {
		return "Unknown error"
	}

	return crtMessages[n]
}
