package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// allowTestManager returns a plugin manager whose Composer has the
// allow-plugins setting allow, written to (and read from) a composer.json
// in a temporary directory, and its BufferIO.
func allowTestManager(t *testing.T, allow any, sortPackages bool) (*Manager, *io.BufferIO, string) {
	t.Helper()

	file := filepath.Join(t.TempDir(), "composer.json")
	cfgJSON := php.ArrayOf("allow-plugins", allow)
	if sortPackages {
		cfgJSON.Set("sort-packages", true)
	}
	encoded, err := json.Encode(php.ArrayOf("config", cfgJSON), json.DefaultEncodeFlags, json.IndentDefault)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(encoded+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := io.NewBufferIO("", console.VerbosityNormal, console.NewOutputFormatter(false))
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.New(false, filepath.Dir(file))
	if err := cfg.Merge(php.ArrayOf("config", cfgJSON), file); err != nil {
		t.Fatal(err)
	}
	jf, err := json.NewFile(file, nil, out)
	if err != nil {
		t.Fatal(err)
	}
	cfg.SetConfigSource(config.NewJSONConfigSource(jf, false))

	c := &composer.Composer{}
	c.SetConfig(cfg)
	m, err := NewManager(New(Options{CacheDir: t.TempDir()}), out, c, nil, composer.PluginsEnabled)
	if err != nil {
		t.Fatal(err)
	}

	return m, out, file
}

func bufferText(out *io.BufferIO) string { return strings.ReplaceAll(out.Output(), "\r", "") }

// PluginManager::isPluginAllowed joins PluginBlockedException's lines with
// PHP_EOL, "\r\n" on Windows.
func TestManager_IsPluginAllowed_BlockedWindowsEOL(t *testing.T) {
	php.SetEOLForTest(t, "\r\n")

	m, _, _ := allowTestManager(t, php.ArrayOf("vendor/*", true), false)
	_, err := m.IsPluginAllowed("third/party", false, false)
	if want := "third/party contains a Composer plugin which is blocked by your allow-plugins config. You may add it to the list if you consider it safe.\r\n" +
		`You can run "composer config --no-plugins allow-plugins.third/party [true|false]" to enable it (true) or disable it explicitly and suppress this exception (false)` + "\r\n" +
		"See https://getcomposer.org/allow-plugins"; err == nil || err.Error() != want {
		t.Errorf("message: %q", err)
	}
}

func TestManager_IsPluginAllowed_Rules(t *testing.T) {
	m, _, _ := allowTestManager(t, php.ArrayOf("vendor/*", true, "vendor/blocked", false, "other/plugin", false), false)

	for name, want := range map[string]bool{
		"vendor/plugin":  true,
		"vendor/blocked": true, // the first matching rule wins
		"other/plugin":   false,
		"VENDOR/X":       true,
	} {
		got, err := m.IsPluginAllowed(name, false, false)
		if err != nil || got != want {
			t.Errorf("%s: %v, %v", name, got, err)
		}
	}

	// Not matched, not interactive: optional plugins are skipped,
	// others blocked.
	if got, err := m.IsPluginAllowed("third/party", false, true); err != nil || got {
		t.Errorf("optional: %v, %v", got, err)
	}
	_, err := m.IsPluginAllowed("third/party", false, false)
	if _, ok := errors.AsType[*PluginBlockedError](err); !ok {
		t.Fatalf("not blocked: %v", err)
	}
	if want := "third/party contains a Composer plugin which is blocked by your allow-plugins config. You may add it to the list if you consider it safe." + php.EOL +
		`You can run "composer config --no-plugins allow-plugins.third/party [true|false]" to enable it (true) or disable it explicitly and suppress this exception (false)` + php.EOL +
		"See https://getcomposer.org/allow-plugins"; err.Error() != want {
		t.Errorf("message:\n%s", err.Error())
	}
	if _, ok := errors.AsType[*util.UnexpectedValueError](err); !ok {
		t.Error("PluginBlockedException is not an UnexpectedValueException")
	}

	// composer/package-versions-deprecated is never allowed implicitly.
	if got, err := m.IsPluginAllowed("composer/package-versions-deprecated", false, false); err != nil || got {
		t.Errorf("package-versions-deprecated: %v, %v", got, err)
	}
}

func TestManager_IsPluginAllowed_Bool(t *testing.T) {
	m, _, _ := allowTestManager(t, true, false)
	if got, err := m.IsPluginAllowed("any/thing", false, false); err != nil || !got {
		t.Errorf("true: %v, %v", got, err)
	}
	// The global rules default to false (no global Composer).
	if got, err := m.IsPluginAllowed("any/thing", true, true); err != nil || got {
		t.Errorf("global: %v, %v", got, err)
	}
}

func TestManager_IsPluginAllowed_Prompt(t *testing.T) {
	m, out, file := allowTestManager(t, php.ArrayOf("zzz/last", false), true)
	out.SetUserInputs([]string{"x", "?", "y"})

	got, err := m.IsPluginAllowed("acme/plugin", false, false)
	if err != nil || !got {
		t.Fatalf("y: %v, %v", got, err)
	}

	help := "y - add package to allow-plugins in composer.json and let it run immediately\n" +
		"n - add package (as disallowed) to allow-plugins in composer.json to suppress further prompts\n" +
		"d - discard this, do not change composer.json and do not allow the plugin to run\n" +
		"? - print help\n"
	question := `Do you trust "acme/plugin" to execute code and wish to enable it now? (writes "allow-plugins" to composer.json) [y,n,d,?] `
	// (<warning> is not a style of the BufferIO's plain formatter.)
	want := "<warning>acme/plugin contains a Composer plugin which is currently not in your allow-plugins config. See https://getcomposer.org/allow-plugins</warning>\n" +
		question + help + question + help + question
	if got := bufferText(out); got != want {
		t.Errorf("output:\n%q\nwant\n%q", got, want)
	}

	// Written to composer.json, sorted (sort-packages), and to the
	// configuration.
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"acme/plugin\": true,\n            \"zzz/last\": false") {
		t.Errorf("composer.json:\n%s", data)
	}
	allow, _ := m.composer.Config().Get("allow-plugins", 0)
	if v, _ := allow.(*php.Array).Get("acme/plugin"); v != true {
		t.Errorf("config allow-plugins = %v", allow)
	}

	// The answer is remembered: no second prompt.
	if got, err := m.IsPluginAllowed("acme/plugin", false, false); err != nil || !got {
		t.Errorf("again: %v, %v", got, err)
	}

	// "d" discards: not allowed, nothing written.
	before, _ := os.ReadFile(file)
	out.SetUserInputs([]string{"d"})
	if got, err := m.IsPluginAllowed("acme/other", false, false); err != nil || got {
		t.Errorf("d: %v, %v", got, err)
	}
	after, _ := os.ReadFile(file)
	if string(before) != string(after) {
		t.Error("d changed composer.json")
	}

	// No answer after five help messages: aborting, then blocked.
	out.SetUserInputs([]string{"", "", "", "", "", "", "", ""})
	_, err = m.IsPluginAllowed("acme/third", false, false)
	if _, ok := errors.AsType[*PluginBlockedError](err); !ok || !strings.Contains(bufferText(out), "Too many failed prompts, aborting.\n") {
		t.Errorf("err = %v\n%s", err, bufferText(out))
	}
}

func TestManager_ArePluginsDisabled(t *testing.T) {
	for d, want := range map[composer.DisablePlugins][2]bool{
		composer.PluginsEnabled:        {false, false},
		composer.PluginsDisabled:       {true, true},
		composer.PluginsDisabledLocal:  {true, false},
		composer.PluginsDisabledGlobal: {false, true},
	} {
		m := &Manager{disablePlugins: d}
		if got := [2]bool{m.ArePluginsDisabled("local"), m.ArePluginsDisabled("global")}; got != want {
			t.Errorf("%v: %v", d, got)
		}
	}
	m := &Manager{}
	m.DisablePlugins()
	if !m.ArePluginsDisabled("local") || m.Rev() == 0 {
		t.Error("DisablePlugins")
	}
}

func TestManager_IsPluginAllowed_PreAllowPluginsLock(t *testing.T) {
	m, out, file := allowTestManager(t, php.NewArray(), false)

	// A lock file from before Composer 2.2 and no allow-plugins: the BC
	// mode (rules null).
	lockPath := filepath.Join(filepath.Dir(file), "composer.lock")
	if err := os.WriteFile(lockPath, []byte(`{"content-hash": "x", "packages": [], "packages-dev": [], "plugin-api-version": "2.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	lockFile, err := json.NewFile(lockPath, nil, out)
	if err != nil {
		t.Fatal(err)
	}
	l, err := locker.New(out, lockFile, &fixtureIM{}, "{}", nil)
	if err != nil {
		t.Fatal(err)
	}
	m.composer.SetLocker(l)
	m, err = NewManager(m.r, out, m.composer, nil, composer.PluginsEnabled)
	if err != nil {
		t.Fatal(err)
	}

	_, err = m.IsPluginAllowed("acme/plugin", false, false)
	if err == nil || err.Error() != `Your composer.lock was generated before the allow-plugins security feature was introduced and your composer.json does not define allow-plugins. Run "composer update --lock" locally and commit the updated composer.lock, then add an explicit allow-plugins section to composer.json. See https://getcomposer.org/allow-plugins` {
		t.Errorf("non-interactive: %v", err)
	}

	// Interactive, it prompts.
	out.SetUserInputs([]string{"n"})
	if got, err := m.IsPluginAllowed("acme/plugin", false, false); err != nil || got {
		t.Errorf("n: %v, %v", got, err)
	}
}
