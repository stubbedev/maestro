//go:build windows

package plugin

import (
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// childJob is a job object that kills its processes when its last handle
// closes, which the system does when maestro exits, however it exits: the
// php child must not outlive maestro (on Unix it notices its channel
// closing; on Windows it would go on holding the console and pipe handles
// it inherited, which keeps whoever waits on them, such as `go test`,
// waiting).
var childJob = sync.OnceValue(func() windows.Handle {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil { //nolint:gosec // the API takes the struct by pointer and size
		_ = windows.CloseHandle(job)

		return 0
	}

	return job
})

// tieToMaestro puts a started child in childJob. A failure leaves the
// child as it was.
func tieToMaestro(p *os.Process) {
	job := childJob()
	if job == 0 || p == nil {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid)) //nolint:gosec // a pid fits
	if err != nil {
		return
	}
	defer func() { _ = windows.CloseHandle(h) }()
	_ = windows.AssignProcessToJobObject(job, h)
}
