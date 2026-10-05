package plugin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/php"
)

func TestSetup_WiresFactory(t *testing.T) {
	t.Setenv(ComposerBinaryEnv, "")
	cache := t.TempDir()
	f := &composer.Factory{Runtime: composer.NewRuntime("test", nil)}

	rt, err := Setup(f, Options{CacheDir: cache})
	if err != nil {
		t.Fatal(err)
	}
	if f.ScriptRuntime != rt || f.EnsureComposerBinary == nil || f.CreatePluginManagerFunc == nil {
		t.Errorf("factory hooks not set: %+v", f)
	}
	if got := os.Getenv(ComposerBinaryEnv); got != ComposerBinary(cache) {
		t.Errorf("COMPOSER_BINARY = %q", got)
	}

	// The local repositories' InstalledVersions reloads wait for PHP.
	data := php.ArrayOf("root", php.ArrayOf("name", "x/y"))
	f.Runtime.SetInstalledVersions(nil)
	rt.ReloadInstalledVersions(data, "/p/vendor/composer")
	boot := rt.bootArgs()
	iv, _ := boot.GetArray("ivPending")
	if iv == nil {
		t.Fatal("no ivPending")
	}
	if dir, _ := iv.GetString("selfDir"); dir != "/p/vendor/composer" {
		t.Errorf("selfDir %q", dir)
	}

	// Without a write, boot carries what createComposer loaded.
	f.Runtime.SetInstalledVersions(data)
	boot = rt.bootArgs()
	iv, _ = boot.GetArray("ivPending")
	if iv == nil || iv.Has("selfDir") {
		t.Errorf("ivPending %v", iv)
	}
}

func TestSetup_ExportsXdebugRestartEnv(t *testing.T) {
	t.Setenv("COMPOSER_ALLOW_XDEBUG", "0")
	t.Setenv("COMPOSER_ORIGINAL_INIS", "")
	t.Setenv("XDEBUG_HANDLER_SETTINGS", "")
	os.Unsetenv("COMPOSER_ORIGINAL_INIS")
	os.Unsetenv("XDEBUG_HANDLER_SETTINGS")

	tmpIni := filepath.Join(t.TempDir(), "tmp.ini")
	rt := New(Options{CacheDir: t.TempDir()})
	rt.planned = true
	rt.restart = &xdebugRestart{
		args:   []string{"-n", "-c", tmpIni},
		env:    map[string]string{"COMPOSER_ORIGINAL_INIS": "/etc/php.ini", "XDEBUG_HANDLER_SETTINGS": tmpIni + "|0|*|*|/etc/php.ini|3.4.0"},
		unset:  []string{"COMPOSER_ALLOW_XDEBUG"},
		tmpIni: tmpIni,
	}
	if err := rt.exportRestartEnv(); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("COMPOSER_ORIGINAL_INIS"); got != "/etc/php.ini" {
		t.Errorf("COMPOSER_ORIGINAL_INIS = %q", got)
	}
	if got := os.Getenv("XDEBUG_HANDLER_SETTINGS"); got != tmpIni+"|0|*|*|/etc/php.ini|3.4.0" {
		t.Errorf("XDEBUG_HANDLER_SETTINGS = %q", got)
	}
	if _, ok := os.LookupEnv("COMPOSER_ALLOW_XDEBUG"); ok {
		t.Error("COMPOSER_ALLOW_XDEBUG is still set")
	}
}

func TestComposerStatics(t *testing.T) {
	crt := composer.NewRuntime("test", nil)
	st := ComposerStatics(crt)

	crt.SetRunningCommand("update", true)
	crt.SetRunningOperation("op", true)
	if st["runningCommand"].Get() != "update" || st["runningOperation"].Get() != "op" {
		t.Fatalf("Get: %v %v", st["runningCommand"].Get(), st["runningOperation"].Get())
	}

	// PHP's Composer::setRunningCommand() sends both statics, the command
	// first; setting the command alone keeps the operation.
	st["runningCommand"].Set("install")
	if op, ok := crt.RunningOperation(); !ok || op != "op" {
		t.Errorf("operation = %q, %v", op, ok)
	}
	st["runningOperation"].Set(nil)
	if cmd, _ := crt.RunningCommand(); cmd != "install" {
		t.Errorf("command = %q", cmd)
	}
	if _, ok := crt.RunningOperation(); ok {
		t.Error("operation not reset")
	}
	if st["processTimeout"].Get() == nil {
		t.Error("no processTimeout static")
	}
}
