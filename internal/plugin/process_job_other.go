//go:build !windows

package plugin

import "os"

// tieToMaestro: on Unix the php child notices its channel closing when
// maestro exits and exits too (process_job_windows.go says why Windows
// needs more).
func tieToMaestro(*os.Process) {}
