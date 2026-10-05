package composer

import (
	"os"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// Runtime is the state Composer keeps in process-wide statics, and the PHP
// its process is considered to run on: Composer::$runningCommand and
// $runningOperation, PlatformRepository::$lastSeenPlatformPhp, whether
// InstalledVersions was loaded, the frame stack the plugin runtime reports
// to debug_backtrace() (docs/PLUGINS.md §5.12), and the platform detector.
//
// There is one Runtime per maestro process (cmd/maestro creates it); nested
// Composer, Installer and Application runs share it, as PHP statics are
// shared. It implements http.Runtime. Its methods may be called
// concurrently.
type Runtime struct {
	clientVersion string
	detector      *platform.Detector
	getenv        func(string) (string, bool)

	view sync.Once
	snap *platform.Snapshot
	skip string
	err  error

	mu                      sync.Mutex
	runningCommand          *string
	runningOperation        *string
	platformPHPVersion      string
	installedVersionsLoaded bool
	installedVersions       *php.Array
	frames                  []Frame

	// installedVersionsSink receives the installed.php data whenever
	// FilesystemRepository::write reloads InstalledVersions (the plugin
	// runtime's copy of the class); nil without a plugin runtime.
	installedVersionsSink func(versions *php.Array)
}

// Frame is an entry of the frame stack: an object Composer would have on
// its PHP call stack (the Application, the running command, the Installer
// during run(), the plugin manager during a load) with its arguments.
type Frame struct {
	Object any
	Args   []any
}

// NewRuntime returns the runtime of a process. clientVersion is maestro's
// own version (the User-Agent's client part). detector finds the php
// Composer would run on; it is started right away, so that its probe
// overlaps loading. A nil detector is platform.NewDetector().
func NewRuntime(clientVersion string, detector *platform.Detector) *Runtime {
	if detector == nil {
		detector = platform.NewDetector()
	}
	detector.Start()

	return &Runtime{clientVersion: clientVersion, detector: detector, getenv: os.LookupEnv}
}

// Detector returns the platform detector.
func (r *Runtime) Detector() *platform.Detector { return r.detector }

// ComposerView returns the PHP as Composer's own process sees it
// (platform.Snapshot.ComposerView: bin/composer's xdebug restart and ini
// changes applied), with XdebugHandler::getSkippedVersion(). Without php it
// returns a *platform.PHPNotFoundError.
func (r *Runtime) ComposerView() (*platform.Snapshot, string, error) {
	r.view.Do(func() {
		snap, err := r.detector.Snapshot()
		if err != nil {
			r.err = err

			return
		}
		r.snap, r.skip = snap.ComposerView(r.getenv)
	})

	return r.snap, r.skip, r.err
}

// PlatformOptions returns the collaborators of a PlatformRepository for
// this process (new PlatformRepository($packages, $overrides) in PHP): the
// Runtime is nil without php.
func (r *Runtime) PlatformOptions(process platform.HhvmExecutor) (repository.PlatformOptions, error) {
	var opts repository.PlatformOptions
	view, skipped, err := r.ComposerView()
	if err != nil && !platformNotFound(err) {
		return opts, err
	}
	if view != nil {
		opts.Runtime = platform.NewRuntime(view)
		opts.SkippedXdebugVersion = skipped
	}
	if process != nil {
		opts.HhvmDetector = platform.NewHhvmDetector(util.NewExecutableFinder(), process)
	}

	return opts, nil
}

func platformNotFound(err error) bool {
	_, ok := err.(*platform.PHPNotFoundError) //nolint:errorlint // the detector returns it unwrapped

	return ok
}

// PlatformPHP returns the PHP the event dispatcher runs `@php` scripts with.
func (r *Runtime) PlatformPHP() *eventdispatcher.PlatformPHP {
	return &eventdispatcher.PlatformPHP{View: func() (*platform.Snapshot, error) {
		view, _, err := r.ComposerView()

		return view, err
	}}
}

// Environment returns what the solver's problem messages read from the PHP
// running Composer (extension_loaded(), IniHelper::getAll()).
func (r *Runtime) Environment() resolver.Environment { return environment{r} }

type environment struct{ r *Runtime }

func (e environment) ExtensionLoaded(name string) bool {
	view, _, err := e.r.ComposerView()
	if err != nil || view == nil {
		return false
	}
	for _, ext := range view.Extensions {
		if strings.EqualFold(ext.Name, name) {
			return true
		}
	}

	return false
}

func (e environment) IniFiles() []string {
	return util.IniGetAll(func() []string {
		view, _, err := e.r.ComposerView()
		if err != nil || view == nil {
			return nil
		}

		return view.IniFiles()
	})
}

// nullable returns PHP's `($s === null || $s === ”) ? null : $s`.
func nullable(s string, ok bool) *string {
	if !ok || s == "" {
		return nil
	}

	return &s
}

func deref(s *string) (string, bool) {
	if s == nil {
		return "", false
	}

	return *s, true
}

// SetRunningCommand ports Composer::setRunningCommand: ok false (or "") is
// null. A new outer command resets the running operation.
func (r *Runtime) SetRunningCommand(command string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runningCommand = nullable(command, ok)
	r.runningOperation = nil
}

// RunningCommand ports Composer::getRunningCommand.
func (r *Runtime) RunningCommand() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return deref(r.runningCommand)
}

// SetRunningOperation ports Composer::setRunningOperation.
func (r *Runtime) SetRunningOperation(operation string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runningOperation = nullable(operation, ok)
}

// RunningOperation ports Composer::getRunningOperation.
func (r *Runtime) RunningOperation() (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	return deref(r.runningOperation)
}

// PHPVersion implements http.Runtime: PHP_VERSION of the php Composer
// would run on, "" without php.
func (r *Runtime) PHPVersion() string {
	view, _, err := r.ComposerView()
	if err != nil || view == nil {
		return ""
	}

	return view.Version
}

// SetPlatformPHPVersion records PlatformRepository::$lastSeenPlatformPhp
// (getPlatformPhpVersion), "" for null.
func (r *Runtime) SetPlatformPHPVersion(version string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.platformPHPVersion = version
}

// PlatformPHPVersion implements http.Runtime.
func (r *Runtime) PlatformPHPVersion() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.platformPHPVersion
}

// ClientVersion implements http.Runtime.
func (r *Runtime) ClientVersion() string { return r.clientVersion }

// MarkInstalledVersionsLoaded records that Composer\InstalledVersions was
// loaded (class_exists('Composer\InstalledVersions', false) in
// Factory::createComposer) and reports whether it already was.
func (r *Runtime) MarkInstalledVersionsLoaded() (alreadyLoaded bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	alreadyLoaded = r.installedVersionsLoaded
	r.installedVersionsLoaded = true

	return alreadyLoaded
}

// SetInstalledVersions records the data Factory::createComposer loads
// into Composer\InstalledVersions (safelyLoadInstalledVersions), for the
// plugin runtime to hand to PHP once it starts.
func (r *Runtime) SetInstalledVersions(data *php.Array) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.installedVersions = data
}

// InstalledVersions returns the data SetInstalledVersions recorded (nil
// for none).
func (r *Runtime) InstalledVersions() *php.Array {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.installedVersions
}

// SetInstalledVersionsSink sets the function the local repositories created
// from now on hand their installed.php data to when they write it
// (FilesystemRepository::write reloading InstalledVersions).
func (r *Runtime) SetInstalledVersionsSink(sink func(versions *php.Array)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.installedVersionsSink = sink
}

// PushFrame pushes a frame on the stack the plugin runtime reports to
// debug_backtrace(); PopFrame removes it.
func (r *Runtime) PushFrame(object any, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, Frame{Object: object, Args: args})
}

// PopFrame removes the innermost frame.
func (r *Runtime) PopFrame() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n := len(r.frames); n > 0 {
		r.frames = r.frames[:n-1]
	}
}

// Frames returns the frames, outermost first.
func (r *Runtime) Frames() []Frame {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]Frame(nil), r.frames...)
}

var _ http.Runtime = (*Runtime)(nil)
