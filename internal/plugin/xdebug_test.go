package plugin

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/platform"
)

func TestParseIni(t *testing.T) {
	got := parseIni("; comment\n[PHP]\nmemory_limit = 128M\nshort_open_tag = Off\nzend.assertions=-1\nerror_reporting = E_ALL & ~E_DEPRECATED\ninclude_path = \".:/usr/share/php\"\nsession.save_path = \"${HOME}/x\"\nextension=intl\ndisplay_errors = On ; trailing\nopcache.enable=1\ndate.timezone = UTC\narr[] = 1\n")
	want := map[string]string{
		"short_open_tag":  "",
		"zend.assertions": "-1",
		"include_path":    ".:/usr/share/php",
		"extension":       "intl",
		"display_errors":  "1",
		"opcache.enable":  "1",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	// Values that need evaluation are not claimed.
	for _, k := range []string{"memory_limit", "error_reporting", "session.save_path", "date.timezone", "arr[]"} {
		if v, ok := got[k]; ok {
			t.Errorf("%s = %q was claimed", k, v)
		}
	}
}

func TestMergeLoadedConfig(t *testing.T) {
	loaded := php.ArrayOf(
		"display_errors", "1",
		"include_path", ".:/usr/share/php",
		"memory_limit", "128M",
		"xdebug.mode", "debug",
		"apc.mmap_file_mask", "/tmp/x",
		"error_log", nil,
		"user_agent", `say "hi" \o/`,
	)
	got := mergeLoadedConfig(loaded, map[string]string{"display_errors": "1", "include_path": ".:/usr/share/php"}, "\n")
	want := "memory_limit=\"128M\"\nuser_agent=\"say \\\"hi\\\" \\\\o/\"\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func xdebugSnapshot(t *testing.T) *platform.Snapshot {
	t.Helper()

	data, err := os.ReadFile("../platform/testdata/oracle/php83-xdebug.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Probe string `json:"probe"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	s, err := platform.ParseSnapshot("php", []byte(fixture.Probe))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Xdebug.Active {
		t.Fatal("the fixture's xdebug is not active")
	}

	return s
}

func TestTmpIniContent(t *testing.T) {
	s := xdebugSnapshot(t)
	dir := t.TempDir()
	main := filepath.Join(dir, "php.ini")
	scanned := filepath.Join(dir, "20-xdebug.ini")
	if err := os.WriteFile(main, []byte("memory_limit = 256M\n[HOST=example.org]\nmemory_limit = 1G\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(scanned, []byte("zend_extension=/usr/lib/php/xdebug.so\n  zend_extension = opcache.so\nxdebug.mode=debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, ok := tmpIniContent(s, []string{main, scanned}, "\n")
	if !ok {
		t.Fatal("no content")
	}
	if !strings.HasPrefix(content, "memory_limit = 256M\n\n;zend_extension=/usr/lib/php/xdebug.so\n  zend_extension = opcache.so\nxdebug.mode=debug\n\n") {
		t.Errorf("ini files part:\n%s", content[:min(len(content), 300)])
	}
	if !strings.HasSuffix(content, "opcache.enable_cli=0\n") {
		t.Error("no opcache.enable_cli=0 at the end")
	}
	if strings.Contains(content, "\nxdebug.") && strings.Count(content, "xdebug.") > 2 {
		t.Error("xdebug settings were merged")
	}
	if !strings.Contains(content, "\nallow_url_fopen=\"1\"\n") {
		t.Error("loaded settings were not merged")
	}

	if _, ok := tmpIniContent(s, []string{"", filepath.Join(dir, "missing.ini")}, "\n"); ok {
		t.Error("an unreadable ini file did not fail")
	}
}

func TestPlanXdebugRestart(t *testing.T) {
	s := xdebugSnapshot(t)

	// Allowed: no restart.
	allowed := func(name string) (string, bool) {
		if name == "COMPOSER_ALLOW_XDEBUG" {
			return "1", true
		}

		return "", false
	}
	if r := planXdebugRestart(s, allowed); r != nil {
		t.Errorf("restart with COMPOSER_ALLOW_XDEBUG=1: %+v", r)
	}

	// The fixture's php binary and ini files are not on this machine, so
	// either Composer would not restart (PHP_BINARY missing) or the ini
	// files cannot be read; both leave xdebug on.
	if r := planXdebugRestart(s, func(string) (string, bool) { return "", false }); r != nil {
		defer os.Remove(r.tmpIni)
		if r.args[0] != "-n" || r.args[1] != "-c" || r.env["COMPOSER_ORIGINAL_INIS"] == "" || !strings.HasPrefix(r.env["XDEBUG_HANDLER_SETTINGS"], r.tmpIni+"|") {
			t.Errorf("restart %+v", r)
		}
	}
}

// TestTmpIniContent_Equivalent: php started as the restart starts it
// (`-n -c <tmp.ini>`) has the same extensions and settings as php
// started plainly, but opcache.enable_cli.
func TestTmpIniContent_Equivalent(t *testing.T) {
	requirePHP(t)

	phpBinary, ok := platform.FindPHP()
	if !ok {
		t.Fatal("no php")
	}
	s, err := platform.Probe(t.Context(), phpBinary)
	if err != nil {
		t.Fatal(err)
	}

	content, ok := tmpIniContent(s, s.IniFiles(), "\n")
	if !ok {
		t.Fatal("no content")
	}
	tmp := filepath.Join(t.TempDir(), "restart.ini")
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// The restart exists to drop xdebug: the dump leaves out xdebug and its
	// settings, and the restarted php must not have it.
	const dump = `$noXdebug = function (array $l) { return array_values(array_filter($l, function ($e) { return strcasecmp($e, 'xdebug') !== 0; })); };
$ini = ini_get_all(null, false);
unset($ini['opcache.enable_cli']);
foreach (array_keys($ini) as $k) { if (strncmp($k, 'xdebug.', 7) === 0) { unset($ini[$k]); } }
echo json_encode([$noXdebug(get_loaded_extensions()), $noXdebug(get_loaded_extensions(true)), $ini]);`
	plain, err := exec.Command(phpBinary, "-r", dump).Output()
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := exec.Command(phpBinary, "-n", "-c", tmp, "-r", dump).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(restarted) {
		t.Errorf("the restarted php differs:\n%s", phpDumpDiff(plain, restarted))
	}
	loaded, err := exec.Command(phpBinary, "-n", "-c", tmp, "-r", `echo extension_loaded('xdebug') ? 'yes' : 'no';`).Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded) != "no" {
		t.Error("the restarted php loads xdebug")
	}
}

// phpDumpDiff lists what differs between two dumps of
// TestTmpIniContent_Equivalent: extensions loaded on one side only and ini
// settings with different values.
func phpDumpDiff(plain, restarted []byte) string {
	type dump struct {
		ext, zend []string
		ini       map[string]any
	}
	decode := func(b []byte) (d dump) {
		var raw [3]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			return d
		}
		_ = json.Unmarshal(raw[0], &d.ext)
		_ = json.Unmarshal(raw[1], &d.zend)
		_ = json.Unmarshal(raw[2], &d.ini)

		return d
	}
	p, r := decode(plain), decode(restarted)
	var b strings.Builder
	only := func(what string, a, other []string) {
		for _, e := range a {
			if !slices.Contains(other, e) {
				fmt.Fprintf(&b, "  %s only: %s\n", what, e)
			}
		}
	}
	only("extension, plain", p.ext, r.ext)
	only("extension, restarted", r.ext, p.ext)
	only("zend extension, plain", p.zend, r.zend)
	only("zend extension, restarted", r.zend, p.zend)
	if slices.Equal(p.ext, r.ext) && slices.Equal(p.zend, r.zend) {
		b.WriteString("  same extensions\n")
	} else if b.Len() == 0 {
		b.WriteString("  extensions loaded in another order\n")
	}
	keys := slices.Sorted(maps.Keys(p.ini))
	for k := range r.ini {
		if _, ok := p.ini[k]; !ok {
			keys = append(keys, k)
		}
	}
	for _, k := range keys {
		pv, pok := p.ini[k]
		rv, rok := r.ini[k]
		if pok != rok || fmt.Sprint(pv) != fmt.Sprint(rv) {
			fmt.Fprintf(&b, "  ini %s: plain %v, restarted %v\n", k, pv, rv)
		}
	}

	return b.String()
}
