// The PHP runtime: one long-lived php child per maestro process, started
// lazily on first need (docs/PLUGINS.md D1, §5.2).

package plugin

import (
	"cmp"
	"crypto/subtle"
	"errors"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util"
)

// The channel's error types, as callers of this package name them.
type (
	// PHPExit is the PHP process ending while maestro needed it; maestro
	// exits with its Code without further output.
	PHPExit = rpc.PHPExit
	// PHPException is an exception thrown by PHP code.
	PHPException = rpc.PHPException
	// ProtocolError is a broken IPC protocol (an internal error).
	ProtocolError = rpc.ProtocolError
)

// Options configure a Runtime.
type Options struct {
	// CacheDir is Composer's cache-dir; the shim is extracted below it
	// (ShimDir).
	CacheDir string
	// FindPHP returns the php binary; platform.FindPHP when nil (the php
	// platform detection uses: php on the PATH).
	FindPHP func() (string, bool)
	// Snapshot returns the platform probe of that php (before Composer's
	// view), for bin/composer's xdebug restart; nil skips the restart.
	Snapshot func() (*platform.Snapshot, error)
	// Getenv reads the environment; os.LookupEnv when nil.
	Getenv func(string) (string, bool)
	// Args is maestro's command line, PHP's $_SERVER['argv'] (os.Args
	// when nil).
	Args []string
	// Flush writes out what maestro buffered for the terminal; it runs
	// before every message to PHP (docs/PLUGINS.md D11).
	Flush func()
	// Statics are Composer's statics PHP shares (internal/composer's);
	// DefaultStatics() when nil.
	Statics map[string]rpc.Static
	// Transport picks the channel; TransportDefault suits the OS.
	Transport Transport
	// Stdin, Stdout and Stderr are the child's standard streams; maestro's
	// own when nil (docs/PLUGINS.md D5).
	Stdin, Stdout, Stderr *os.File
	// Require are PHP files the shim requires at the end of `boot`
	// (tests register PHP handlers this way).
	Require []string
	// InstalledVersions returns the installed.php data
	// Factory::createComposer loaded into Composer\InstalledVersions before
	// PHP started (composer.Runtime.InstalledVersions); nil for none.
	InstalledVersions func() *php.Array
	// PlatformPHPVersion is PlatformRepository::getPlatformPhpVersion()
	// ("" for null): composer.Runtime.PlatformPHPVersion.
	PlatformPHPVersion func() string
}

// Info is what the child reported in its handshake.
type Info struct {
	PHPVersion, PHPBinary, SAPI string
	PID                         int
}

// Runtime is the PHP runtime of a maestro process (plugin.Runtime in
// docs/PLUGINS.md). Nothing runs until Start; services register their
// handlers before or after.
type Runtime struct {
	opts Options

	// startMu serializes starts; mu guards the fields, never across a
	// call into PHP.
	startMu  sync.Mutex
	mu       sync.Mutex
	conn     *rpc.Conn
	child    *child
	startErr error
	info     Info
	restart  *xdebugRestart
	// planned is set once the xdebug restart was planned (planRestart).
	planned bool

	handlers  map[string]rpc.Handler
	factories map[string]rpc.MirrorFactory
	tags      map[string]rpc.TagDecoder

	shimMu  sync.Mutex
	shimDir string

	// bridge holds the Go objects of the Composer API crossing to PHP.
	bridge *bridge
	// installers are the PHP installer objects and their peers
	// (svc_installer.go); promises the promises crossing (promises.go).
	installers *installerTable
	promises   *promiseTable
	// commands are the PHP commands and Applications maestro knows
	// (proxy_command.go).
	commands *commandTable
	// composerFactory creates the Composer instances and Applications PHP
	// asks for (Setup's Factory).
	composerFactory *composer.Factory
	// bootIO is the IO PHP's ErrorHandler reports through (the IO of
	// whatever started PHP).
	bootIO io.IO
	// befores are the dispatch.before hooks of the calls into PHP code
	// in progress, innermost last.
	befores []*beforeFrame
	// What waits for PHP to start: dispatch brackets opened, the class
	// loader of makeAutoloader and InstalledVersions' latest reload.
	pendingBegins []int
	pendingLoader *eventdispatcher.LoaderContents
	pendingIV     *php.Array
	// parsers is the VersionParser PHP's loaders share (svc_loader.go).
	parsers versionParsers
	// frameBases are the depths of the frame stack at the PHP → Go calls
	// in progress, innermost last (frames.go).
	frameBases []int
	// phpObjs are the proxies of the repositories and downloaders written
	// in PHP that maestro uses (proxy_repository.go).
	phpObjs phpObjects
	// phpRunInstallers are the Installers maestro runs for a PHP
	// Installer's run(), whose frame is PHP's own (frames.go).
	phpRunInstallers map[*composer.Installer]bool
}

// New returns a Runtime; it starts nothing.
func New(opts Options) *Runtime {
	if opts.FindPHP == nil {
		opts.FindPHP = platform.FindPHP
	}
	if opts.Getenv == nil {
		opts.Getenv = os.LookupEnv
	}
	if opts.Args == nil {
		opts.Args = os.Args
	}
	if opts.Statics == nil {
		opts.Statics = DefaultStatics()
	}
	opts.Stdin = cmp.Or(opts.Stdin, os.Stdin)
	opts.Stdout = cmp.Or(opts.Stdout, os.Stdout)
	opts.Stderr = cmp.Or(opts.Stderr, os.Stderr)

	r := &Runtime{
		opts:      opts,
		handlers:  map[string]rpc.Handler{},
		factories: map[string]rpc.MirrorFactory{},
		tags:      map[string]rpc.TagDecoder{},
		bridge:    newBridge(),

		installers: newInstallerTable(),
		promises:   newPromiseTable(),
		commands:   newCommandTable(),
	}
	r.registerAPI()

	return r
}

// factory is the Factory the Composer instances and Applications PHP
// creates come from: Setup's, or a plain one.
func (r *Runtime) factory() *composer.Factory {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.composerFactory == nil {
		r.composerFactory = &composer.Factory{}
	}

	return r.composerFactory
}

// DefaultStatics are Composer's statics as this process holds them:
// Composer::$runningCommand and $runningOperation in memory (until
// internal/composer provides its own), ProcessExecutor's timeout in
// internal/util.
func DefaultStatics() map[string]rpc.Static {
	var (
		mu     sync.Mutex
		values = map[string]any{}
	)
	value := func(name string) rpc.Static {
		return rpc.Static{
			Get: func() any {
				mu.Lock()
				defer mu.Unlock()

				return values[name]
			},
			Set: func(v any) {
				mu.Lock()
				defer mu.Unlock()

				values[name] = v
			},
		}
	}

	return map[string]rpc.Static{
		"runningCommand":   value("runningCommand"),
		"runningOperation": value("runningOperation"),
		"processTimeout": {
			Get: func() any { return int64(util.GetProcessTimeout()) },
			Set: func(v any) { util.SetProcessTimeout(php.ToNativeInt(v)) },
		},
	}
}

// Handle sets the handler of a method PHP calls (docs/PLUGINS.md §6.6).
func (r *Runtime) Handle(method string, h rpc.Handler) {
	h = r.frameBoundary(h)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[method] = h
	if r.conn != nil {
		r.conn.Handle(method, h)
	}
}

// RegisterMirrorFactory sets the factory of PHP-born mirrors of a shim
// base class.
func (r *Runtime) RegisterMirrorFactory(base string, f rpc.MirrorFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.factories[base] = f
	if r.conn != nil {
		r.conn.RegisterMirrorFactory(base, f)
	}
}

// RegisterTag sets the decoder of a value tag.
func (r *Runtime) RegisterTag(key string, d rpc.TagDecoder) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.tags[key] = d
	if r.conn != nil {
		r.conn.RegisterTag(key, d)
	}
}

// Started reports whether the child is running (or ran).
func (r *Runtime) Started() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.conn != nil
}

// Info returns what the child reported in its handshake.
func (r *Runtime) Info() Info {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.info
}

// Start starts the child unless it runs already (docs/PLUGINS.md §5.2).
// purpose names what needs PHP, for the error when there is no php:
// `plugin "a/b"` or `script A\B::c`. A failed start fails every later one
// the same way.
func (r *Runtime) Start(purpose string) error {
	r.startMu.Lock()
	defer r.startMu.Unlock()

	r.mu.Lock()
	done, err := r.conn != nil, r.startErr
	r.mu.Unlock()
	if done || err != nil {
		return err
	}

	conn, err := r.start(purpose)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.conn, r.startErr = conn, err

	return err
}

func (r *Runtime) start(purpose string) (*rpc.Conn, error) {
	phpBinary, ok := r.opts.FindPHP()
	if !ok {
		return nil, &platform.PHPNotFoundError{Purpose: purpose}
	}

	dir, err := r.ensureShim()
	if err != nil {
		return nil, err
	}

	if err := r.planRestart(); err != nil {
		return nil, err
	}

	var args []string
	if r.restart != nil {
		args = append(args, r.restart.args...)
	}
	args = append(args, filepath.Join(dir, "bootstrap.php"))

	cmd := exec.Command(phpBinary, args...) //nolint:gosec // the php on the PATH, as Composer's shebang finds it, running the shim.
	cmd.Stdin, cmd.Stdout, cmd.Stderr = r.opts.Stdin, r.opts.Stdout, r.opts.Stderr
	cmd.Env = r.childEnv(dir)

	sp, err := spawn(cmd, r.opts.Transport)
	if err != nil {
		r.removeTmpIni()

		return nil, err
	}

	conn := rpc.NewConn(sp.r, sp.w, sp.child, rpc.Options{Flush: r.opts.Flush, Statics: r.opts.Statics})

	r.mu.Lock()
	r.child = sp.child
	for m, h := range r.handlers {
		conn.Handle(m, h)
	}
	for b, f := range r.factories {
		conn.RegisterMirrorFactory(b, f)
	}
	for k, d := range r.tags {
		conn.RegisterTag(k, d)
	}
	r.mu.Unlock()
	conn.Handle("hello", r.hello(sp.token))

	if err := conn.Accept("hello"); err != nil {
		r.removeTmpIni()

		return nil, err
	}

	if _, err := conn.Call("boot", r.bootArgs()); err != nil {
		sp.child.Kill()
		sp.child.Wait()
		r.removeTmpIni()

		return nil, err
	}

	return conn, nil
}

// childEnv is maestro's environment plus what the child gets on top
// (docs/PLUGINS.md §5.2 "Env"): COMPOSER_BINARY, MAESTRO_BINARY when
// maestro did not set it, and the xdebug restart's variables. The channel's
// variables are added by spawn.
func (r *Runtime) childEnv(shimDir string) []string {
	env := os.Environ()
	set := func(name, value string) {
		env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, name+"=") })
		env = append(env, name+"="+value)
	}

	set(ComposerBinaryEnv, filepath.Join(shimDir, "bin", "composer"))
	if _, ok := os.LookupEnv(MaestroBinaryEnv); !ok {
		if exe, err := os.Executable(); err == nil {
			set(MaestroBinaryEnv, exe)
		}
	}
	if r.restart != nil {
		for _, name := range r.restart.unset {
			env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, name+"=") })
		}
		for _, name := range slices.Sorted(maps.Keys(r.restart.env)) {
			set(name, r.restart.env[name])
		}
	}
	env = slices.DeleteFunc(env, func(kv string) bool { return strings.HasPrefix(kv, "MAESTRO_IPC") })

	return env
}

// hello serves the handshake (docs/PLUGINS.md §6.6 `hello`).
func (r *Runtime) hello(token string) rpc.Handler {
	return func(args any) (any, error) {
		a, ok := args.(*php.Array)
		if !ok {
			return nil, &rpc.ProtocolError{Message: "invalid hello"}
		}
		if proto, _ := a.Get("proto"); proto != int64(1) {
			return nil, &rpc.ProtocolError{Message: "the shim speaks protocol " + php.ToString(proto) + ", maestro 1"}
		}
		if token != "" {
			got, _ := a.GetString("token")
			if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
				return nil, &rpc.ProtocolError{Message: "the channel's token does not match"}
			}
		}

		var info Info
		info.PHPVersion, _ = a.GetString("phpVersion")
		info.PHPBinary, _ = a.GetString("phpBinary")
		info.SAPI, _ = a.GetString("sapi")
		if pid, ok := a.Get("pid"); ok {
			info.PID = php.ToNativeInt(pid)
		}
		r.mu.Lock()
		r.info = info
		r.mu.Unlock()

		return nil, nil
	}
}

// bootArgs are the params of `boot` (docs/PLUGINS.md §5.2 step 7, §6.5).
func (r *Runtime) bootArgs() *php.Array {
	argv0 := ""
	if len(r.opts.Args) > 0 {
		argv0 = r.opts.Args[0]
	}

	a := php.ArrayOf(
		"argv", php.StringList(r.opts.Args),
		"server", php.ArrayOf("SCRIPT_NAME", argv0, "SCRIPT_FILENAME", argv0, "PHP_SELF", argv0),
		"composerVersion", php.ArrayOf(
			`Composer\Composer::VERSION`, repository.ComposerVersion,
			`Composer\Composer::RUNTIME_API_VERSION`, repository.RuntimeAPIVersion,
			`Composer\Plugin\PluginInterface::PLUGIN_API_VERSION`, repository.PluginAPIVersion,
		),
		"io", nil,
		// the directory maestro names Composer's files under, which the
		// shim's traces name them under too
		"composerRoot", phperr.Root(),
	)

	r.mu.Lock()
	bootIO, iv := r.bootIO, r.pendingIV
	r.pendingIV = nil
	r.mu.Unlock()

	if bootIO != nil {
		a.Set("io", r.ioObject(bootIO))
	}
	if iv == nil && r.opts.InstalledVersions != nil {
		if data := r.opts.InstalledVersions(); data != nil {
			iv = php.ArrayOf("data", data)
		}
	}
	if iv != nil {
		a.Set("ivPending", iv)
	}
	if len(r.opts.Require) > 0 {
		a.Set("require", php.StringList(r.opts.Require))
	}

	return a
}

// conn returns the started channel.
func (r *Runtime) started() (*rpc.Conn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.conn == nil {
		if r.startErr != nil {
			return nil, r.startErr
		}

		return nil, errors.New("plugin: the PHP runtime is not started")
	}

	return r.conn, nil
}

// Call calls a method in PHP (docs/PLUGINS.md §6.5). The runtime must be
// started.
func (r *Runtime) Call(method string, args any) (any, error) {
	conn, err := r.started()
	if err != nil {
		return nil, err
	}

	return conn.Call(method, args)
}

// Delegate runs fn on a new goroutine holding the PHP baton
// (docs/PLUGINS.md §5.14); see rpc.Conn.Delegate.
func (r *Runtime) Delegate(fn func() error) error {
	conn, err := r.started()
	if err != nil {
		return fn()
	}

	return conn.Delegate(fn)
}

// Lookup returns the object of a handle (rpc.Conn.Lookup).
func (r *Runtime) Lookup(h rpc.Handle) (any, bool) {
	conn, err := r.started()
	if err != nil {
		return nil, false
	}

	return conn.Lookup(h)
}

// Shutdown ends the child at the end of maestro's run (docs/PLUGINS.md
// §5.2 "Shutdown and exit"): PHP exits with code, running its shutdown
// functions and destructors, which may still call maestro. The result is
// the child's exit status, which maestro exits with (a shutdown function
// calling exit() changes it). Without a child it is code.
func (r *Runtime) Shutdown(code int) (int, error) {
	r.mu.Lock()
	conn := r.conn
	r.mu.Unlock()

	if conn == nil {
		return code, nil
	}
	defer r.removeTmpIni()

	return conn.Shutdown(code)
}

// Close kills a child that is still running (an aborted run) and removes
// the temporary files of its start.
func (r *Runtime) Close() {
	r.mu.Lock()
	c := r.child
	r.mu.Unlock()

	if c != nil {
		c.Kill()
		c.Wait()
	}
	r.removeTmpIni()
}

// planRestart plans bin/composer's xdebug restart for the child, once.
func (r *Runtime) planRestart() error {
	if r.planned || r.opts.Snapshot == nil {
		return nil
	}
	raw, err := r.opts.Snapshot()
	if err != nil {
		return err
	}
	r.restart = planXdebugRestart(raw, r.opts.Getenv)
	r.planned = true

	return nil
}

func (r *Runtime) removeTmpIni() {
	// XdebugHandler unlinks its temporary ini once the restarted process
	// has ended.
	if r.restart != nil && r.restart.tmpIni != "" {
		_ = os.Remove(r.restart.tmpIni)
		r.restart.tmpIni = ""
	}
}

// EnsureComposerBinary extracts the shim, so COMPOSER_BINARY exists
// before a script process starts; it starts no PHP. It is the hook of
// eventdispatcher.SetEnsureComposerBinary.
func (r *Runtime) EnsureComposerBinary() error {
	_, err := r.ensureShim()

	return err
}

// ShimDir extracts the shim and returns its directory.
func (r *Runtime) ShimDir() (string, error) { return r.ensureShim() }

func (r *Runtime) ensureShim() (string, error) {
	r.shimMu.Lock()
	defer r.shimMu.Unlock()

	if r.shimDir != "" {
		return r.shimDir, nil
	}
	if r.opts.CacheDir == "" {
		return "", errors.New("plugin: no cache directory to extract the PHP shim into")
	}

	dir, err := extractShim(r.opts.CacheDir)
	if err != nil {
		return "", err
	}
	r.shimDir = dir

	return dir, nil
}
