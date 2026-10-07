package platform

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// The goldens in testdata/oracle are written by
// tools/oracle/platform/runtime.php: the output of probe.php run by one
// php build, and what Composer's own Runtime answers in that php.

func loadOracle(t *testing.T, data []byte) *php.Array {
	t.Helper()

	v, err := php.JSONDecode(string(data), true)
	if err != nil {
		t.Fatal(err)
	}

	a, ok := unwrap(v).(*php.Array)
	if !ok {
		t.Fatal("golden is not an object")
	}

	return a
}

func TestRuntime_Oracle(t *testing.T) {
	files, err := filepath.Glob("testdata/oracle/*.json")
	if err != nil || len(files) == 0 {
		t.Fatal("no goldens", err)
	}

	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}

			g := loadOracle(t, data)
			probe, _ := g.GetString("probe")

			s, err := ParseSnapshot("php", []byte(probe))
			if err != nil {
				t.Fatal(err)
			}

			checkOracle(t, s, g)
		})
	}
}

// TestRuntime_OracleLive probes the php on the PATH with the Go probe and
// compares with Composer's Runtime in that php. It needs php and .ref.
func TestRuntime_OracleLive(t *testing.T) {
	if testing.Short() {
		t.Skip("runs php")
	}

	script, _ := filepath.Abs("../../tools/oracle/platform/runtime.php")
	if _, err := os.Stat("../../.ref/composer/vendor/autoload.php"); err != nil {
		t.Skip(".ref/composer is not installed")
	}

	d := NewDetector()

	s, err := d.Snapshot()
	if err != nil {
		t.Skip(err)
	}

	out, err := exec.CommandContext(t.Context(), s.Binary, script, "-").Output()
	if err != nil {
		t.Fatal(err)
	}

	checkOracle(t, s, loadOracle(t, out))
}

func checkOracle(t *testing.T, s *Snapshot, g *php.Array) {
	t.Helper()

	rt := NewRuntime(s)

	if v, _ := g.GetString("php_version"); s.Version != v {
		t.Errorf("Version = %q, want %q", s.Version, v)
	}

	wantExts := oracleStrings(arrayAt(g, "getExtensions"))
	if got := rt.GetExtensions(); !slices.Equal(got, wantExts) {
		t.Errorf("GetExtensions() = %v, want %v", got, wantExts)
	}

	for _, row := range rows(g, "getExtensionVersion") {
		ext, _ := row[0].(string)
		if got := rt.GetExtensionVersion(ext); got != row[1] {
			t.Errorf("GetExtensionVersion(%q) = %q, want %#v", ext, got, row[1])
		}
	}

	for _, row := range rows(g, "getExtensionInfo") {
		ext, _ := row[0].(string)
		got, err := rt.GetExtensionInfo(ext)
		checkOutcome(t, "GetExtensionInfo("+ext+")", got, err, row[1])
	}

	for _, row := range rows(g, "constants") {
		name, _ := row[0].(string)
		class, _ := row[1].(string)

		if got := rt.HasConstant(name, class); got != row[2] {
			t.Errorf("HasConstant(%q, %q) = %v, want %v", name, class, got, row[2])
		}

		got, err := rt.GetConstant(name, class)
		checkOutcome(t, "GetConstant("+name+", "+class+")", got, err, row[3])
	}

	for _, row := range rows(g, "hasFunction") {
		fn, _ := row[0].(string)
		if got := rt.HasFunction(fn); got != row[1] {
			t.Errorf("HasFunction(%q) = %v, want %v", fn, got, row[1])
		}
	}

	for _, row := range rows(g, "hasClass") {
		class, _ := row[0].(string)
		if got := rt.HasClass(class); got != row[1] {
			t.Errorf("HasClass(%q) = %v, want %v", class, got, row[1])
		}
	}

	for _, row := range rows(g, "invoke") {
		var callable Callable

		switch c := row[0].(type) {
		case string:
			callable = Func(c)
		case *php.Array:
			class, _ := c.GetString(0)
			method, _ := c.GetString(1)
			callable = StaticMethod(class, method)
		}

		args, _ := row[1].(*php.Array)
		got, err := rt.Invoke(callable, args.Values()...)
		checkOutcome(t, "Invoke("+callable.String()+")", got, err, row[2])
	}

	for _, row := range rows(g, "construct") {
		class, _ := row[0].(string)
		got, err := rt.Construct(class)
		checkOutcome(t, "Construct("+class+")", got, err, row[1])
	}

	for k, v := range arrayAt(g, "ini_get").All() {
		got, ok := s.IniGet(k.String())
		if want, isString := v.(string); ok != isString || got != want {
			t.Errorf("IniGet(%q) = %q, %v, want %#v", k.String(), got, ok, v)
		}
	}

	if want := oracleStrings(arrayAt(g, "IniHelper::getAll")); !slices.Equal(s.IniFiles(), want) {
		t.Errorf("IniFiles() = %q, want %q", s.IniFiles(), want)
	}

	loaded, ok := s.LoadedIniFile()
	if want, isString := valueAt(g, "php_ini_loaded_file").(string); ok != isString || loaded != want {
		t.Errorf("LoadedIniFile() = %q, %v, want %#v", loaded, ok, valueAt(g, "php_ini_loaded_file"))
	}

	if want := oracleStrings(arrayAt(g, "zend_extensions")); !slices.Equal(s.ZendExtensions, want) {
		t.Errorf("ZendExtensions = %q, want %q", s.ZendExtensions, want)
	}

	// Goldens recorded before the probe recorded it lack the key.
	if want, recorded := g.Get("configure_command"); recorded {
		got, ok := s.ConfigureCommand()
		if wantStr, isString := want.(string); ok != isString || got != wantStr {
			t.Errorf("ConfigureCommand() = %q, %v, want %#v", got, ok, want)
		}
	}

	x := arrayAt(g, "xdebug")
	version, _ := valueAt(x, "version").(string)
	mode, _ := valueAt(x, "mode").(string)

	if s.Xdebug.Active != valueAt(x, "active") || s.Xdebug.Version != version || s.Xdebug.Mode != mode {
		t.Errorf("Xdebug = %+v, want %v", s.Xdebug, x)
	}

	for _, row := range rows(g, "html_entity_decode") {
		in, _ := row[0].(string)
		if got := php.HTMLEntityDecode(in); got != row[1] {
			t.Errorf("HTMLEntityDecode(%q) = %q, want %q", in, got, row[1])
		}
	}

	for _, row := range rows(g, "parseHtmlExtensionInfo") {
		in, _ := row[0].(string)
		if got, err := ParseHtmlExtensionInfo(in); err != nil || got != row[1] {
			t.Errorf("ParseHtmlExtensionInfo(%q) = %q, %v, want %q", in, got, err, row[1])
		}
	}
}

// rows returns g[key] as a list of lists.
func rows(g *php.Array, key string) [][]any {
	var out [][]any

	for _, v := range arrayAt(g, key).All() {
		if row, ok := v.(*php.Array); ok {
			out = append(out, row.Values())
		}
	}

	return out
}

func oracleStrings(a *php.Array) []string {
	out := []string{}
	for _, v := range a.All() {
		s, _ := v.(string)
		out = append(out, s)
	}

	return out
}

// checkOutcome compares a Runtime result with the outcome runtime.php
// recorded: ['value' => ..., 'methods' => [[name, args, outcome]...]] or
// ['error' => [class, message]].
func checkOutcome(t *testing.T, what string, got any, err error, want any) {
	t.Helper()

	o, _ := want.(*php.Array)

	if e, ok := o.GetArray("error"); ok {
		class, _ := e.GetString(0)
		msg, _ := e.GetString(1)

		pe, isPHP := err.(*PHPError) //nolint:errorlint // never wrapped
		if !isPHP || pe.Class != class || pe.Message != msg {
			t.Errorf("%s: error %v, want %s: %s", what, err, class, msg)
		}

		return
	}

	if err != nil {
		t.Errorf("%s: unexpected error %v", what, err)

		return
	}

	value, _ := o.Get("value")
	value = unwrap(value)

	if marker, isObject := value.(objectMarker); isObject {
		obj, ok := got.(*probedObject)
		if !ok || obj.PHPClass() != marker.class {
			t.Errorf("%s = %#v, want a %s", what, got, marker.class)

			return
		}

		for _, m := range rows(o, "methods") {
			name, _ := m[0].(string)
			args, _ := m[1].(*php.Array)
			v, err := obj.Call(name, args.Values()...)
			checkOutcome(t, what+"->"+name, v, err, m[2])
		}

		return
	}

	if !sameValue(got, value) {
		t.Errorf("%s = %#v, want %#v", what, got, value)
	}
}

// sameValue is ===, except that NaN equals NaN.
func sameValue(a, b any) bool {
	switch a := a.(type) {
	case *php.Array:
		b, ok := b.(*php.Array)
		if !ok || a.Len() != b.Len() {
			return false
		}

		ka, kb := a.Keys(), b.Keys()
		for i, k := range ka {
			if k != kb[i] {
				return false
			}

			va, _ := a.GetKey(k)
			vb, _ := b.GetKey(k)

			if !sameValue(va, vb) {
				return false
			}
		}

		return true
	case float64:
		if bf, ok := b.(float64); ok && math.IsNaN(a) && math.IsNaN(bf) {
			return true
		}
	case Resource:
		return a == b
	}

	return php.StrictEquals(a, b)
}

func TestDetector_Probe(t *testing.T) {
	if testing.Short() {
		t.Skip("runs php")
	}

	binary, ok := FindPHP()
	if !ok {
		t.Skip("no php")
	}

	s, err := Probe(context.Background(), binary)
	if err != nil {
		t.Fatal(err)
	}

	out, err := exec.CommandContext(t.Context(), binary, "-r", "echo PHP_VERSION;").Output()
	if err != nil {
		t.Fatal(err)
	}

	if s.Version != string(out) || s.Binary != binary {
		t.Errorf("Version = %q (binary %q), want %q (%q)", s.Version, s.Binary, out, binary)
	}
}
