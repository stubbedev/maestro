package platform

import (
	"slices"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		v, ok := vars[name]

		return v, ok
	}
}

func TestSnapshot_ComposerViewRestartsWithoutXdebug(t *testing.T) {
	s := goldenSnapshot(t, "php83-xdebug")
	if !s.Xdebug.Loaded || !s.Xdebug.Active || s.Xdebug.Version != "3.5.3" || s.Xdebug.Mode != "develop" {
		t.Fatalf("Xdebug = %+v", s.Xdebug)
	}

	if !s.canRestart() {
		// PHP_BINARY of the recorded php may not exist on this machine.
		bin, _ := s.Constant("PHP_BINARY")
		t.Skipf("%v is not here", bin)
	}

	view, skipped := s.ComposerView(env(nil))
	if skipped != "3.5.3" {
		t.Errorf("skipped version = %q", skipped)
	}

	r, orig := NewRuntime(view), NewRuntime(s)

	if slices.Contains(r.GetExtensions(), "xdebug") || !slices.Contains(orig.GetExtensions(), "xdebug") {
		t.Error("xdebug should be gone from the view only")
	}

	if len(r.GetExtensions()) != len(orig.GetExtensions())-1 || r.GetExtensionVersion("xdebug") != "0" {
		t.Error("the view should only lack xdebug")
	}

	if _, err := r.GetExtensionInfo("xdebug"); err == nil {
		t.Error("xdebug info should be gone")
	}

	if v, _ := r.GetExtensionInfo("date"); v == "" {
		t.Error("the other extensions keep their info")
	}

	if r.HasFunction("xdebug_info") || !orig.HasFunction("xdebug_info") || r.HasConstant("XDEBUG_TRACE_APPEND", "") || !r.HasConstant("PHP_VERSION", "") {
		t.Error("xdebug's functions and constants should be gone")
	}

	if _, ok := view.IniGet("xdebug.mode"); ok {
		t.Error("xdebug's ini settings should be gone")
	}

	if slices.ContainsFunc(view.ZendExtensions, func(n string) bool { return strings.EqualFold(n, "xdebug") }) {
		t.Errorf("ZendExtensions = %q", view.ZendExtensions)
	}

	if v, ok := view.IniGet("opcache.enable_cli"); ok && v != "0" {
		t.Errorf("opcache.enable_cli = %q", v)
	}

	if view.Xdebug.Loaded || view.Xdebug.Active {
		t.Errorf("view Xdebug = %+v", view.Xdebug)
	}

	if v, _ := view.IniGet("memory_limit"); v != "1536M" {
		t.Errorf("memory_limit = %q", v)
	}

	if v, _ := s.IniGet("memory_limit"); v == "1536M" {
		t.Error("the snapshot itself must not change")
	}
}

func TestSnapshot_ComposerViewAllowXdebug(t *testing.T) {
	s := goldenSnapshot(t, "php83-xdebug")

	view, skipped := s.ComposerView(env(map[string]string{"COMPOSER_ALLOW_XDEBUG": "1", "COMPOSER_MEMORY_LIMIT": "-1"}))
	if skipped != "" || !slices.Contains(NewRuntime(view).GetExtensions(), "xdebug") {
		t.Errorf("skipped %q, xdebug kept: %v", skipped, view.Xdebug)
	}

	for name, want := range map[string]string{"xdebug.scream": "0", "xdebug.show_exception_trace": "0", "memory_limit": "-1"} {
		if v, _ := view.IniGet(name); v != want {
			t.Errorf("%s = %q, want %q", name, v, want)
		}
	}

	// Inactive xdebug: no restart.
	off := goldenSnapshot(t, "php83-xdebug-off")
	if off.Xdebug.Active || off.Xdebug.Mode != "off" {
		t.Fatalf("Xdebug = %+v", off.Xdebug)
	}

	view, skipped = off.ComposerView(env(nil))
	if skipped != "" || !slices.Contains(NewRuntime(view).GetExtensions(), "xdebug") {
		t.Errorf("mode off: skipped %q", skipped)
	}
}

func TestSnapshot_ComposerViewRestartSettings(t *testing.T) {
	s := goldenSnapshot(t, "php84")

	// A restarted process: the skipped version comes from the environment.
	if _, skipped := s.ComposerView(env(map[string]string{"COMPOSER_ALLOW_XDEBUG": "internal|3.1.0|1|*|*"})); skipped != "3.1.0" {
		t.Errorf("restarted: skipped %q", skipped)
	}

	if _, skipped := goldenSnapshot(t, "php83-xdebug").ComposerView(env(map[string]string{"COMPOSER_ALLOW_XDEBUG": "internal|3.1.0|1|*|*"})); skipped != "" {
		t.Errorf("restarted with xdebug loaded: skipped %q", skipped)
	}

	// Settings of a restart are only used by the php that loaded its ini.
	settings := "<root>/tools/oracle/platform/ini/php.ini|1|*|*|/a.ini|3.2.0"
	if _, skipped := goldenSnapshot(t, "php84-custom-ini").ComposerView(env(map[string]string{"XDEBUG_HANDLER_SETTINGS": settings})); skipped != "3.2.0" {
		t.Errorf("restart settings: skipped %q", skipped)
	}

	if _, skipped := s.ComposerView(env(map[string]string{"XDEBUG_HANDLER_SETTINGS": settings})); skipped != "" {
		t.Errorf("foreign restart settings: skipped %q", skipped)
	}
}

func TestSnapshot_ComposerViewIni(t *testing.T) {
	s := goldenSnapshot(t, "php84")

	view, _ := s.ComposerView(env(nil))
	if v, _ := view.IniGet("display_errors"); v != "stderr" {
		t.Errorf("display_errors = %q", v)
	}

	logging := *s
	logging.ini = s.ini.Clone()
	logging.ini.Set("log_errors", "1")
	logging.ini.Set("error_log", nil)

	view, _ = logging.ComposerView(env(nil))
	if v, _ := view.IniGet("display_errors"); v != "0" {
		t.Errorf("display_errors when logging to stderr = %q", v)
	}

	for limit, want := range map[string]string{"-1": "-1", "2G": "2G", "1536M": "1536M", "1535M": "1536M", "128M": "1536M", " 4096m ": " 4096m ", "abc": "1536M"} {
		big := *s
		big.ini = s.ini.Clone()
		big.ini.Set("memory_limit", limit)

		view, _ = big.ComposerView(env(nil))
		if v, _ := view.IniGet("memory_limit"); v != want {
			t.Errorf("memory_limit %q became %q, want %q", limit, v, want)
		}
	}

	// Without ini_set nothing changes.
	noIniSet := *s
	noIniSet.functions = without(s.functions, []string{"ini_set"})

	view, _ = noIniSet.ComposerView(env(nil))
	if v, _ := view.IniGet("display_errors"); v == "stderr" {
		t.Error("ini_set is disabled")
	}
}

func TestMemoryInBytes(t *testing.T) {
	for in, want := range map[string]int64{"": 0, "128M": 128 << 20, "1G": 1 << 30, "512k": 512 << 10, "100": 100, "2g": 2 << 30, "-1": -1} {
		if got := memoryInBytes(in); got != want {
			t.Errorf("memoryInBytes(%q) = %d, want %d", in, got, want)
		}
	}
}
