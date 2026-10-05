package plugin

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/util"
)

func TestRuntime_Handshake(t *testing.T) {
	requirePHP(t)

	// The shim is extracted once per cache directory; time the start of
	// php itself, best of three.
	cache := t.TempDir()
	best := time.Hour
	for range 3 {
		rt, _, _ := newTestRuntime(t, func(o *Options) { o.CacheDir = cache })
		if err := rt.EnsureComposerBinary(); err != nil {
			t.Fatal(err)
		}
		began := time.Now()
		start(t, rt)
		best = min(best, time.Since(began))

		info := rt.Info()
		if info.PID == 0 || info.SAPI != "cli" || info.PHPVersion == "" {
			t.Errorf("info %+v", info)
		}
		if code, err := rt.Shutdown(0); err != nil || code != 0 {
			t.Fatalf("Shutdown = %d, %v", code, err)
		}
	}
	t.Logf("handshake: %v", best)
	if best > 40*time.Millisecond {
		t.Errorf("handshake took %v, the budget is 40ms (docs/PLUGINS.md §5.16)", best)
	}
}

func TestRuntime_RoundTrip(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	const n = 200
	began := time.Now()
	for range n {
		if _, err := rt.Call("ping", nil); err != nil {
			t.Fatal(err)
		}
	}
	per := time.Since(began) / n
	t.Logf("round trip: %v", per)
	if per > 2*time.Millisecond {
		t.Errorf("a round trip took %v, the budget is 2ms (docs/PLUGINS.md §5.16)", per)
	}
}

func TestRuntime_BootState(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	v, err := rt.Call("test.server", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := php.ToString(get(t, v, "script")); got != "maestro" {
		t.Errorf("SCRIPT_NAME = %q", got)
	}
	argv := get(t, v, "argv").(*php.Array)
	if argv.Len() != 3 || php.ToInt(get(t, v, "argc")) != 3 {
		t.Errorf("argv = %v", argv.Values())
	}
	if get(t, v, "ipc") != false || get(t, v, "ipcServer") != false {
		t.Error("MAESTRO_IPC is still in PHP's environment")
	}
	if got := php.ToString(get(t, v, "composerBinary")); !strings.HasSuffix(got, filepath.Join("bin", "composer")) {
		t.Errorf("COMPOSER_BINARY = %q", got)
	}
	if got := get(t, v, "locale"); got != "C" {
		t.Errorf("locale = %v", got)
	}
	if get(t, v, "errorReporting") != true {
		t.Error("error_reporting is not E_ALL")
	}
	if got := php.ToString(get(t, v, "memoryLimit")); got != "-1" && got != "1536M" && os.Getenv("COMPOSER_MEMORY_LIMIT") == "" {
		t.Logf("memory_limit = %s", got)
	}
	if get(t, v, "polyfill") != true || get(t, v, "autoloadFiles") != true {
		t.Error("the vendored libraries' autoload files are not loaded")
	}

	// Composer's ErrorHandler is in place.
	msg, err := rt.Call("test.errorHandler", nil)
	if err != nil || !strings.Contains(php.ToString(msg), "missing") {
		t.Errorf("warning = %v, %v", msg, err)
	}

	// Presence parity: Composer's classes exist, ones it lacks do not.
	classes, err := rt.Call("test.classes", php.ListOf(`Composer\Composer`, `Composer\Installer\LibraryInstaller`, `Composer\Plugin\PluginInterface`, `Composer\Installer\PearInstaller`, `Composer\Script\CommandEvent`))
	if err != nil {
		t.Fatal(err)
	}
	for class, want := range map[string]bool{`Composer\Composer`: true, `Composer\Installer\LibraryInstaller`: true, `Composer\Plugin\PluginInterface`: true, `Composer\Installer\PearInstaller`: false, `Composer\Script\CommandEvent`: false} {
		if got := get(t, classes, class); got != want {
			t.Errorf("class_exists(%s) = %v", class, got)
		}
	}

	// A stub says which member is missing.
	_, err = rt.Call("test.unsupported", nil)
	var pe *PHPException
	if !errors.As(err, &pe) || pe.Class != `Maestro\Shim\UnsupportedApiException` || pe.Message != `maestro does not support Composer\Util\Url::sanitize() in plugins yet` {
		t.Errorf("stub call = %v", err)
	}
}

// TestRuntime_Reentrancy is the torture test of docs/PLUGINS.md §8: calls
// alternate between Go and PHP 50 levels deep, and an exception thrown at
// the bottom comes back up through every level as the same object.
func TestRuntime_Reentrancy(t *testing.T) {
	requirePHP(t)

	for _, transport := range []Transport{TransportPipes, TransportTCP} {
		t.Run(fmt.Sprint(transport), func(t *testing.T) {
			rt, _, _ := newTestRuntime(t, func(o *Options) { o.Transport = transport })
			maxDepth := 0
			var bottomHandle rpc.Handle
			rt.Handle("test.down", func(args any) (any, error) {
				n := php.ToInt(get(t, args, "n"))
				trail := php.ToString(get(t, args, "trail")) + fmt.Sprintf("g%d", n)
				conn, _ := rt.started()
				maxDepth = max(maxDepth, conn.Depth())
				if n == 0 {
					return trail, nil
				}
				v, err := rt.Call("test.down", php.ArrayOf("n", n-1, "trail", trail, "throw", get(t, args, "throw")))
				var pe *PHPException
				if errors.As(err, &pe) && bottomHandle == 0 {
					bottomHandle = pe.H
				}

				return v, err
			})
			start(t, rt)

			var want strings.Builder
			for n := 50; n >= 0; n-- {
				side := "p"
				if n%2 == 1 {
					side = "g"
				}
				fmt.Fprintf(&want, "%s%d", side, n)
			}

			v, err := rt.Call("test.down", php.ArrayOf("n", 50, "trail", "", "throw", false))
			if err != nil {
				t.Fatal(err)
			}
			if v != want.String() {
				t.Errorf("trail = %v\nwant %s", v, want.String())
			}
			if maxDepth < 50 {
				t.Errorf("max depth %d", maxDepth)
			}

			_, err = rt.Call("test.down", php.ArrayOf("n", 50, "trail", "", "throw", true))
			pe, ok := err.(*PHPException) //nolint:errorlint // the exception itself must come back
			if !ok || pe.Class != "MaestroTestException" || pe.Code != 42 || !strings.HasPrefix(pe.Message, "bottom ") {
				t.Fatalf("thrown at the bottom: %v", err)
			}
			if pe.H != bottomHandle || pe.H >= 0 {
				t.Errorf("handle %d at the top, %d at the bottom", pe.H, bottomHandle)
			}
		})
	}
}

func TestRuntime_ExceptionIdentity(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	wrap := false
	rt.Handle("test.bounce", func(any) (any, error) {
		_, err := rt.Call("test.storeAndThrow", nil)
		if wrap {
			return nil, fmt.Errorf("context: %w", err)
		}

		return nil, err
	})
	start(t, rt)

	v, err := rt.Call("test.identity", nil)
	if err != nil {
		t.Fatal(err)
	}
	if get(t, v, "same") != true {
		t.Errorf("PHP caught a different object: %v", v)
	}

	wrap = true
	v, err = rt.Call("test.identity", nil)
	if err != nil {
		t.Fatal(err)
	}
	if get(t, v, "class") != "RuntimeException" || get(t, v, "previousSame") != true || get(t, v, "message") != "context: stored" {
		t.Errorf("wrapped: %v", v)
	}
}

func TestRuntime_ExceptionsBothWays(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	rt.Handle("go.script", func(any) (any, error) {
		return nil, &eventdispatcher.ScriptExecutionError{Message: "Error Output: boom", Code: 3}
	})
	rt.Handle("go.runtime", func(any) (any, error) { return nil, &util.RuntimeError{Message: "rt"} })
	rt.Handle("go.transport", func(any) (any, error) { return nil, &util.TransportError{Message: "down", Code: 404} })
	rt.Handle("go.plain", func(any) (any, error) { return nil, errors.New("plain") })
	start(t, rt)

	for method, want := range map[string]string{
		"go.script":    `Composer\EventDispatcher\ScriptExecutionException|Error Output: boom|3`,
		"go.runtime":   "RuntimeException|rt|0",
		"go.transport": `Composer\Downloader\TransportException|down|404`,
		"go.plain":     "RuntimeException|plain|0",
		"go.missing":   `Maestro\Shim\UnsupportedApiException|maestro has no handler for go.missing|0`,
	} {
		v, err := rt.Call("test.catch", php.ArrayOf("m", method))
		if err != nil {
			t.Fatal(err)
		}
		got := fmt.Sprintf("%v|%v|%v", get(t, v, "class"), get(t, v, "message"), get(t, v, "code"))
		if got != want {
			t.Errorf("%s: PHP caught %s, want %s", method, got, want)
		}
	}

	_, err := rt.Call("test.throw", php.ArrayOf("message", "bad \xff bytes", "code", 9))
	pe, ok := err.(*PHPException) //nolint:errorlint // the exception itself
	if !ok {
		t.Fatalf("err = %v", err)
	}
	if pe.Class != "MaestroTestException" || pe.Message != "bad \xff bytes" || pe.Code != 9 || pe.Line == 0 || !strings.HasSuffix(pe.File, "handlers.php") || len(pe.Trace) == 0 {
		t.Errorf("exception %+v", pe)
	}
	if !pe.InstanceOf("DomainException") || !pe.InstanceOf("LogicException") || !pe.InstanceOf("Throwable") {
		t.Errorf("classes %v", pe.Classes)
	}
	var le *util.LogicError
	if !errors.As(err, &le) || le.Message != pe.Message {
		t.Error("not caught as a LogicException")
	}
}

func TestRuntime_ExitMidCall(t *testing.T) {
	requirePHP(t)

	rt, stdout, _ := newTestRuntime(t)
	var inner error
	rt.Handle("go.nested", func(any) (any, error) {
		_, inner = rt.Call("test.exit", php.ArrayOf("code", 3))

		return nil, inner
	})
	start(t, rt)

	_, err := rt.Call("test.call", php.ArrayOf("m", "go.nested"))
	var exit *PHPExit
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Fatalf("Call = %v, want PHPExit 3", err)
	}
	if !errors.As(inner, &exit) {
		t.Errorf("inner call = %v", inner)
	}
	if !strings.Contains(readFile(t, stdout), "exiting") {
		t.Error("PHP's output is missing")
	}
	if _, err := rt.Call("ping", nil); !errors.As(err, &exit) {
		t.Errorf("a later call = %v", err)
	}
	if code, err := rt.Shutdown(0); err != nil || code != 3 {
		t.Errorf("Shutdown = %d, %v; maestro must exit 3", code, err)
	}
}

// TestRuntime_ExitWithGrandchild: a process PHP started keeps the
// channel's fds open after PHP exited; maestro watches the process, not
// EOF (docs/PLUGINS.md §5.15).
func TestRuntime_ExitWithGrandchild(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	began := time.Now()
	_, err := rt.Call("test.exitLeavingGrandchild", php.ArrayOf("seconds", 5, "code", 4))
	var exit *PHPExit
	if !errors.As(err, &exit) || exit.Code != 4 {
		t.Fatalf("Call = %v, want PHPExit 4", err)
	}
	if d := time.Since(began); d > 3*time.Second {
		t.Errorf("noticed the exit after %v", d)
	}
}

// TestRuntime_UnsendableKeepsSync: a call PHP cannot send fails in PHP
// alone; what it would have synced still reaches Go with the next message.
func TestRuntime_UnsendableKeepsSync(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)
	t.Cleanup(func() { os.Unsetenv("MAESTRO_T_PENDING") })

	v, err := rt.Call("test.unsendable", php.ArrayOf("name", "MAESTRO_T_PENDING"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(php.ToString(v), "resource") {
		t.Errorf("PHP caught %v", v)
	}
	if got := os.Getenv("MAESTRO_T_PENDING"); got != "pending" {
		t.Errorf("Go sees %q", got)
	}
}

func TestRuntime_FatalError(t *testing.T) {
	requirePHP(t)

	rt, _, stderr := newTestRuntime(t)
	start(t, rt)

	_, err := rt.Call("test.fatal", nil)
	var exit *PHPExit
	if !errors.As(err, &exit) || exit.Code != 255 {
		t.Fatalf("Call = %v, want PHPExit 255", err)
	}
	if out := readFile(t, stderr); !strings.Contains(out, "Fatal error") || !strings.Contains(out, "Allowed memory size") {
		t.Errorf("stderr = %q", out)
	}
}

func TestRuntime_Shutdown(t *testing.T) {
	requirePHP(t)

	t.Run("shutdown functions run after Go's last output", func(t *testing.T) {
		rt, stdout, _ := newTestRuntime(t)
		rt.Handle("go.inShutdown", func(any) (any, error) { return "from go\n", nil })
		rt.opts.Flush = func() { _, _ = stdout.WriteString("[flush]\n") }
		start(t, rt)

		if _, err := rt.Call("test.registerShutdown", php.ArrayOf("echo", "shutdown\n", "callGo", "go.inShutdown")); err != nil {
			t.Fatal(err)
		}
		_, _ = stdout.WriteString("go last\n")
		code, err := rt.Shutdown(0)
		if err != nil || code != 0 {
			t.Fatalf("Shutdown = %d, %v", code, err)
		}
		out := strings.ReplaceAll(readFile(t, stdout), "[flush]\n", "")
		if out != "go last\nshutdown\nfrom go\n" {
			t.Errorf("stdout = %q", out)
		}
	})

	t.Run("exit in a shutdown function sets the status", func(t *testing.T) {
		rt, _, _ := newTestRuntime(t)
		start(t, rt)
		if _, err := rt.Call("test.registerShutdown", php.ArrayOf("echo", "", "exit", 5)); err != nil {
			t.Fatal(err)
		}
		if code, err := rt.Shutdown(0); err != nil || code != 5 {
			t.Errorf("Shutdown = %d, %v, want 5", code, err)
		}
	})

	t.Run("the code becomes the exit status", func(t *testing.T) {
		rt, _, _ := newTestRuntime(t)
		start(t, rt)
		if code, err := rt.Shutdown(2); err != nil || code != 2 {
			t.Errorf("Shutdown = %d, %v", code, err)
		}
	})

	t.Run("without a child", func(t *testing.T) {
		rt := New(Options{})
		if code, err := rt.Shutdown(6); err != nil || code != 6 {
			t.Errorf("Shutdown = %d, %v", code, err)
		}
	})
}

func TestRuntime_EnvSync(t *testing.T) {
	requirePHP(t)

	t.Setenv("MAESTRO_T_GONE", "x")
	rt, _, _ := newTestRuntime(t)
	start(t, rt)
	t.Cleanup(func() {
		os.Unsetenv("MAESTRO_T_PHP")
		os.Unsetenv("MAESTRO_T_GO")
	})

	// Platform::putEnv() in PHP code is visible to Go and to a script Go
	// runs next; a plain putenv() of a new variable is not, as it is not
	// for the processes Composer starts (Symfony Process passes the
	// variables getenv() and $_SERVER both have).
	if _, err := rt.Call("test.putenv", php.ArrayOf("set", php.ArrayOf("MAESTRO_T_PHP", "from php"), "unset", php.ListOf("MAESTRO_T_GONE"), "plain", php.ArrayOf("MAESTRO_T_PLAIN", "x"))); err != nil {
		t.Fatal(err)
	}
	if _, ok := os.LookupEnv("MAESTRO_T_PLAIN"); ok {
		t.Error("Go sees a variable a plain putenv() set")
	}
	if v := os.Getenv("MAESTRO_T_PHP"); v != "from php" {
		t.Errorf("Go sees MAESTRO_T_PHP = %q", v)
	}
	if _, ok := os.LookupEnv("MAESTRO_T_GONE"); ok {
		t.Error("Go still sees MAESTRO_T_GONE")
	}
	out, err := exec.Command("sh", "-c", `printf %s "$MAESTRO_T_PHP"`).Output()
	if err != nil || string(out) != "from php" {
		t.Errorf("a script sees %q, %v", out, err)
	}

	// Go's changes reach PHP's getenv() and $_SERVER.
	t.Setenv("MAESTRO_T_GO", "from go \xff")
	v, err := rt.Call("test.getenv", php.ListOf("MAESTRO_T_GO", "MAESTRO_T_GONE"))
	if err != nil {
		t.Fatal(err)
	}
	if got := get(t, v, "MAESTRO_T_GO").(*php.Array).Values(); got[0] != "from go \xff" || got[1] != "from go \xff" {
		t.Errorf("PHP sees %v", got)
	}
	if got := get(t, v, "MAESTRO_T_GONE").(*php.Array).Values(); got[0] != false {
		t.Errorf("PHP sees MAESTRO_T_GONE = %v", got)
	}
}

func TestRuntime_CwdSync(t *testing.T) {
	requirePHP(t)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(wd)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	a, b := t.TempDir(), t.TempDir()
	if _, err := rt.Call("test.chdir", a); err != nil {
		t.Fatal(err)
	}
	got, _ := os.Getwd()
	if resolved, _ := filepath.EvalSymlinks(a); got != a && got != resolved {
		t.Errorf("Go's cwd = %s, want %s", got, a)
	}

	if err := os.Chdir(b); err != nil {
		t.Fatal(err)
	}
	v, err := rt.Call("test.getcwd", nil)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, _ := filepath.EvalSymlinks(b); v != b && v != resolved {
		t.Errorf("PHP's cwd = %v, want %s", v, b)
	}
}

func TestRuntime_StaticsSync(t *testing.T) {
	requirePHP(t)

	before := util.GetProcessTimeout()
	t.Cleanup(func() { util.SetProcessTimeout(before) })

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	util.SetProcessTimeout(0)
	v, err := rt.Call("test.statics", php.ArrayOf("set", php.ArrayOf("runningCommand", "update", "processTimeout", 42)))
	if err != nil {
		t.Fatal(err)
	}
	if got := get(t, v, "processTimeout"); got != int64(0) {
		t.Errorf("PHP's timeout = %v", got)
	}
	if got := util.GetProcessTimeout(); got != 42 {
		t.Errorf("Go's timeout = %d", got)
	}
	if got := rt.opts.Statics["runningCommand"].Get(); got != "update" {
		t.Errorf("Go's runningCommand = %v", got)
	}
}

// box is a Go mirror of MaestroTestBox (testdata/php/handlers.php).
type box struct {
	rev          uint64
	class, label string
	value        any
}

func (*box) PHPOpaque()         {}
func (b *box) PHPClass() string { return cmp.Or(b.class, "MaestroTestBox") }
func (*box) MirrorBase() string { return "MaestroTestBox" }
func (b *box) Rev() uint64      { return b.rev }
func (b *box) set(v any)        { b.value = v; b.rev++ }

func (b *box) MirrorSnapshot() (*php.Array, error) {
	return php.ArrayOf("value", b.value, "label", b.label), nil
}

func (b *box) ApplyMirror(fields *php.Array) error {
	if v, ok := fields.Get("value"); ok {
		b.set(v)
	}
	if v, ok := fields.GetString("label"); ok {
		b.label = v
		b.rev++
	}

	return nil
}

func TestRuntime_Mirrors(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	rt.RegisterMirrorFactory("MaestroTestBox", func(class string, snapshot *php.Array) (rpc.Mirror, error) {
		v, _ := snapshot.Get("value")
		l, _ := snapshot.GetString("label")

		return &box{class: class, value: v, label: l}, nil
	})
	var seen any
	rt.Handle("test.see", func(args any) (any, error) {
		seen = args

		return nil, nil
	})
	var adopted *box
	rt.Handle("test.adopt", func(args any) (any, error) {
		adopted, _ = args.(*box)

		return "adopted", nil
	})
	start(t, rt)

	b := &box{value: "go", label: "a"}
	if _, err := rt.Call("test.keep", php.ArrayOf("name", "b", "object", b)); err != nil {
		t.Fatal(err)
	}
	boxGet := func(name string) string {
		t.Helper()
		v, err := rt.Call("test.boxGet", name)
		if err != nil {
			t.Fatal(err)
		}

		return fmt.Sprintf("%v|%v|%v", get(t, v, "class"), get(t, v, "value"), get(t, v, "label"))
	}
	if got := boxGet("b"); got != "MaestroTestBox|go|a" {
		t.Errorf("PHP's box = %s", got)
	}

	// In place, both ways, with identity.
	b.set("changed")
	if got := boxGet("b"); got != "MaestroTestBox|changed|a" {
		t.Errorf("after Go's change: %s", got)
	}
	if _, err := rt.Call("test.boxSet", php.ArrayOf("name", "b", "label", "from php", "callGo", "test.see")); err != nil {
		t.Fatal(err)
	}
	if b.label != "from php" || seen != b {
		t.Errorf("Go's box = %+v; PHP passed %v", b, seen)
	}
	if same, err := rt.Call("test.same", php.ArrayOf("name", "b", "object", b)); err != nil || same != true {
		t.Errorf("same = %v, %v", same, err)
	}

	// A PHP-born mirror of a user subclass becomes Go's.
	v, err := rt.Call("test.newBox", php.ArrayOf("name", "n", "class", "MaestroTestSubBox", "value", 5, "label", "born"))
	if err != nil {
		t.Fatal(err)
	}
	if adopted == nil || adopted.class != "MaestroTestSubBox" || adopted.value != int64(5) || adopted.label != "born" {
		t.Fatalf("adopted %+v", adopted)
	}
	if h := php.ToInt(get(t, v, "handle")); h <= 0 {
		t.Errorf("PHP's handle of the adopted box = %d", h)
	}
	adopted.set("changed in go")
	if got := boxGet("n"); got != "MaestroTestSubBox|changed in go|born" {
		t.Errorf("adopted box in PHP = %s", got)
	}
}

func TestRuntime_Objects(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	v, err := rt.Call("test.closure", nil)
	if err != nil {
		t.Fatal(err)
	}
	list := v.(*php.Array).Values()
	f, ok := list[0].(*rpc.PHPObject)
	if !ok || list[1] != f || f.Class != "Closure" || f.H >= 0 {
		t.Fatalf("closures %#v", list)
	}
	if o, ok := list[2].(*rpc.PHPObject); !ok || o == f || o.Class != "ArrayObject" {
		t.Errorf("ArrayObject %#v", list[2])
	}
	if same, err := rt.Call("test.same", php.ArrayOf("name", "closure", "object", f)); err != nil || same != true {
		t.Errorf("the closure did not come back as itself: %v, %v", same, err)
	}

	// A Go service crosses as an opaque PHP object with its identity.
	svc := &goService{}
	if _, err := rt.Call("test.keep", php.ArrayOf("name", "svc", "object", svc)); err != nil {
		t.Fatal(err)
	}
	if same, err := rt.Call("test.same", php.ArrayOf("name", "svc", "object", svc)); err != nil || same != true {
		t.Errorf("same = %v, %v", same, err)
	}
	if back, err := rt.Call("test.kept", "svc"); err != nil || back != svc {
		t.Errorf("kept = %v, %v", back, err)
	}
}

type goService struct{ _ int }

func (*goService) PHPOpaque()       {}
func (*goService) PHPClass() string { return `Composer\Config` }

// TestRuntime_Values sends Go values through PHP and back.
func TestRuntime_Values(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	in := php.ArrayOf(
		"\x00first", "nul key",
		"bin", "\xff\xfe",
		"floats", php.ListOf(1.0, 0.1, 1e100, math.Inf(1), math.Inf(-1)),
		"ints", php.ArrayOf(5, "five", "07", "seven"),
		"std", php.ObjectFromArray(php.ArrayOf("p", "\x80")),
		"empty", php.NewArray(),
		"nested", php.ListOf(php.ListOf(php.ArrayOf("\xfekey", true))),
	)
	out, err := rt.Call("test.echo", in)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := php.JSONEncode(in, php.JSONPartialOutputOnError)
	got, _ := php.JSONEncode(out, php.JSONPartialOutputOnError)
	if got != want {
		t.Errorf("echoed\n got %s\nwant %s", got, want)
	}
	nan, err := rt.Call("test.echo", math.NaN())
	if f, ok := nan.(float64); err != nil || !ok || !math.IsNaN(f) {
		t.Errorf("NAN echoed as %v, %v", nan, err)
	}
	if k, _, _ := out.(*php.Array).First(); k.String() != "\x00first" {
		t.Errorf("first key %q", k.String())
	}
}

func TestRuntime_Baton(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	var other, delegated error
	rt.Handle("go.parallel", func(any) (any, error) {
		ch := make(chan error)
		go func() {
			_, err := rt.Call("ping", nil)
			ch <- err
		}()
		other = <-ch
		delegated = rt.Delegate(func() error {
			_, err := rt.Call("ping", nil)

			return err
		})

		return nil, nil
	})
	start(t, rt)

	if _, err := rt.Call("test.call", php.ArrayOf("m", "go.parallel")); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(other, rpc.ErrBaton) {
		t.Errorf("a parallel call = %v, want ErrBaton", other)
	}
	if delegated != nil {
		t.Errorf("a delegated call = %v", delegated)
	}
}
