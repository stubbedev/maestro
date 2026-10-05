// Ports src/Composer/Platform/HhvmDetector.php.

package platform

import (
	"sync"

	"github.com/stubbedev/maestro/internal/util"
)

// HhvmVersionDetector is what PlatformRepository asks for the HHVM
// version; tests mock it as Composer's tests mock HhvmDetector.
type HhvmVersionDetector interface {
	// GetVersion ports HhvmDetector::getVersion; "" is null.
	GetVersion() string
}

// HhvmFinder finds an executable (ExecutableFinder::find).
type HhvmFinder interface {
	Find(name string, extraDirs ...string) (string, bool)
}

// HhvmExecutor runs a command, capturing its output
// (ProcessExecutor::execute).
type HhvmExecutor interface {
	Execute(command util.Command, output *string, cwd string) (int, error)
}

// hhvmVersion is HhvmDetector::$hhvmVersion, a static in PHP and so
// shared by every detector: whether it is known, and the version ("" for
// false).
var hhvmVersion struct {
	sync.Mutex
	known   bool
	version string
}

// HhvmDetector ports Composer\Platform\HhvmDetector. maestro is not HHVM,
// so HHVM_VERSION is never defined and the version comes from an hhvm
// binary on the PATH.
type HhvmDetector struct {
	executableFinder HhvmFinder
	processExecutor  HhvmExecutor
}

// NewHhvmDetector returns a detector; nil arguments get the defaults
// (util.NewExecutableFinder, util.NewProcessExecutor(nil)).
func NewHhvmDetector(executableFinder HhvmFinder, processExecutor HhvmExecutor) *HhvmDetector {
	return &HhvmDetector{executableFinder: executableFinder, processExecutor: processExecutor}
}

// Reset ports HhvmDetector::reset.
func (d *HhvmDetector) Reset() {
	hhvmVersion.Lock()
	hhvmVersion.known, hhvmVersion.version = false, ""
	hhvmVersion.Unlock()
}

// GetVersion ports HhvmDetector::getVersion.
func (d *HhvmDetector) GetVersion() string {
	hhvmVersion.Lock()
	defer hhvmVersion.Unlock()

	if hhvmVersion.known {
		return hhvmVersion.version
	}

	hhvmVersion.known = true

	if util.IsWindows() {
		return ""
	}

	if d.executableFinder == nil {
		d.executableFinder = util.NewExecutableFinder()
	}

	hhvmPath, ok := d.executableFinder.Find("hhvm")
	if !ok {
		return ""
	}

	if d.processExecutor == nil {
		d.processExecutor = util.NewProcessExecutor(nil)
	}

	var output string

	exitCode, err := d.processExecutor.Execute(util.Cmd(hhvmPath, "--php", "-d", "hhvm.jit=0", "-r", "echo HHVM_VERSION;"), &output, "")
	if err == nil && exitCode == 0 {
		hhvmVersion.version = output
	}

	return hhvmVersion.version
}
