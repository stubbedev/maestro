// The PHP side of event dispatch: the plugin runtime that runs PHP
// listeners (docs/PLUGINS.md §5.5, §7), and the PHP Composer would be
// running on, which scripts use.

package eventdispatcher

import (
	"os"
	"sync"

	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/util"
)

// ScriptStatus is the outcome of a call into the ScriptRuntime.
type ScriptStatus string

// The ScriptRuntime outcomes.
const (
	// StatusOK means the PHP code ran (an error returned with it is an
	// exception thrown by that code).
	StatusOK ScriptStatus = "ok"
	// StatusNotAutoloadable is `!class_exists($className)`.
	StatusNotAutoloadable ScriptStatus = "notAutoloadable"
	// StatusNotCallable is `!is_callable($callable)`.
	StatusNotCallable ScriptStatus = "notCallable"
	// StatusNotCommand is `!is_a($className, Command::class, true)`.
	StatusNotCommand ScriptStatus = "notCommand"
	// StatusSkipped means the beforeRun hook of RunCommandClass declined.
	StatusSkipped ScriptStatus = "skipped"
)

// ScriptRuntime runs PHP listeners: the plugin runtime (plugin.Runtime)
// implements it, internal/composer injects it with SetScriptRuntime. The
// dispatcher calls it only for PHP callables, Class::method scripts that
// are not native and command-class scripts, so a run without those never
// needs it.
//
// Each call does the PHP-side checks first and reports a failed one
// through its status without running anything. Once the checks pass it
// calls before (Go writes the "> ..." line there, or vetoes a command
// class), then runs the PHP code. An error returned with StatusOK was
// thrown by the PHP code; an error with an empty status happened before it
// ran. A PHP exception that is a \Error rather than an \Exception
// implements PHPError.
type ScriptRuntime interface {
	// CallListener is `$callable($event)` after `is_callable($callable)`
	// (StatusNotCallable). returnedFalse is `false === $return`.
	CallListener(l PHPCallable, ev Event, before func()) (status ScriptStatus, returnedFalse bool, err error)
	// CallPHPScript is `$className::$methodName($event)` after
	// class_exists (StatusNotAutoloadable) and is_callable
	// (StatusNotCallable).
	CallPHPScript(className, methodName string, ev Event, before func()) (status ScriptStatus, returnedFalse bool, err error)
	// RunCommandClass runs the Symfony command className as
	// EventDispatcher::doDispatch does (a fresh Application, the command
	// named after the event, `$app->run(new StringInput($input), $output)`
	// with the ConsoleIO's output) after class_exists
	// (StatusNotAutoloadable) and is_a Command (StatusNotCommand); when
	// before returns false nothing runs (StatusSkipped). code is run()'s
	// exit code.
	RunCommandClass(className string, ev Event, input string, before func() bool) (status ScriptStatus, code int, err error)
	// InstallAutoloader replaces the dispatcher's class loader in PHP:
	// unregister the previous one, build a ClassLoader from loader and
	// register(false) it (makeAutoloader; `files` are not required).
	InstallAutoloader(loader *LoaderContents) error
	// DispatchBegin is called before the first PHP call of a dispatch, at
	// nesting depth depth; PHP snapshots spl_autoload_functions().
	DispatchBegin(depth int) error
	// DispatchEnd closes DispatchBegin when the dispatch ends: PHP
	// re-appends autoloaders prepended meanwhile (doDispatch's finally).
	DispatchEnd(depth int) error
}

// PHP is the PHP Composer runs on, as the dispatcher asks about it for
// @php scripts and PHP_BINARY.
type PHP interface {
	// Binary is `(new PhpExecutableFinder())->find(false)` inside
	// Composer: the path of the running PHP; ok is false when there is
	// none.
	Binary() (path string, ok bool)
	// IniGet is ini_get($name) in Composer's process (after bin/composer's
	// ini_set calls); "" for false.
	IniGet(name string) (string, error)
}

// PlatformPHP is the PHP internal/platform detects: php on the PATH, with
// the ini settings of Composer's process.
type PlatformPHP struct {
	// View returns the platform as Composer's process sees it
	// (Snapshot.ComposerView, taken at startup as bin/composer applies
	// it).
	View func() (*platform.Snapshot, error)
}

// NewPlatformPHP is a PlatformPHP probing php itself, once, on first
// use. internal/composer passes its process-wide view instead.
func NewPlatformPHP() *PlatformPHP {
	detector := platform.NewDetector()

	return &PlatformPHP{View: sync.OnceValues(func() (*platform.Snapshot, error) {
		s, err := detector.Snapshot()
		if err != nil {
			return nil, err
		}
		view, _ := s.ComposerView(os.LookupEnv)

		return view, nil
	})}
}

// Binary implements PHP: PhpExecutableFinder::find() gives the PHP_BINARY
// environment variable when it names an executable, else PHP_BINARY of
// the CLI php, php on the PATH when it cannot be probed.
func (p *PlatformPHP) Binary() (string, bool) {
	if env, ok := os.LookupEnv("PHP_BINARY"); ok && env != "" {
		if !util.IsExecutable(env) {
			found, ok := util.NewExecutableFinder().Find(env)
			if !ok {
				return "", false
			}
			env = found
		}
		if fi, err := os.Stat(env); err == nil && fi.IsDir() {
			return "", false
		}

		return env, true
	}

	if p.View != nil {
		if view, err := p.View(); err == nil && view != nil {
			if b := view.PHPBinary(); b != "" {
				return b, true
			}
		}
	}

	return platform.FindPHP()
}

// IniGet implements PHP.
func (p *PlatformPHP) IniGet(name string) (string, error) {
	view, err := p.View()
	if err != nil {
		return "", err
	}
	v, _ := view.IniGet(name)

	return v, nil
}
