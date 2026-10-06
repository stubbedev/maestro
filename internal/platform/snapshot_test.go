package platform

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// goldenSnapshot parses the probe output recorded in testdata/oracle.
func goldenSnapshot(t *testing.T, variant string) *Snapshot {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata/oracle", variant+".json"))
	if err != nil {
		t.Fatal(err)
	}

	probe, _ := loadOracle(t, data).GetString("probe")

	s, err := ParseSnapshot("/usr/bin/php", []byte(probe))
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func TestParseSnapshot_Errors(t *testing.T) {
	for name, output := range map[string]string{
		"no marker":      "PHP Parse error: syntax error",
		"invalid json":   probeMarker + "{",
		"not an object":  probeMarker + "[]",
		"unknown format": probeMarker + `{"format":2}`,
		"no version":     probeMarker + `{"format":1,"constants":{}}`,
	} {
		_, err := ParseSnapshot("php", []byte(output))

		var pe *ProbeError
		if !errors.As(err, &pe) || pe.Binary != "php" || !strings.HasPrefix(err.Error(), "maestro: detecting the platform with php failed: ") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestParseSnapshot_Values(t *testing.T) {
	// Startup output before the marker is ignored; wrapped values unwrap.
	output := "PHP Warning:  Module \"x\" is already loaded in Unknown on line 0\n" + probeMarker + `{"format":1,
		"ini":{"a":"1","b":null,"c":{"\u0000s":"/w=="}},
		"constants":{"PHP_VERSION":"8.3.0","PHP_VERSION_ID":80300,"INF":{"\u0000f":"INF"},"MINF":{"\u0000f":"-INF"},
			"NAN":{"\u0000f":"NAN"},"STDIN":{"\u0000r":"stream"},"F":1.0,
			"P":{"\u0000p":[["\u0000x",1],[{"\u0000s":"/w=="},2],[5,3]]}},
		"extensions":[["Core","8.3.0","info"],["noversion",false,null]],
		"ini_files":[false,"/a.ini,\n/b.ini"],
		"configure_command":" './configure'  '--enable-sigchild' ",
		"calls":[{"callable":"f","args":[1,"x"],"value":{"\u0000s":"AAE="}},
			{"callable":["C","m"],"args":[],"error":["Error","boom"]}]}`

	s, err := ParseSnapshot("php", []byte(output))
	if err != nil {
		t.Fatal(err)
	}

	if s.Version != "8.3.0" || s.VersionID != 80300 {
		t.Errorf("version %q %d", s.Version, s.VersionID)
	}

	if v, ok := s.IniGet("b"); !ok || v != "" {
		t.Errorf("IniGet(b) = %q, %v", v, ok)
	}

	if v, _ := s.IniGet("c"); v != "\xff" {
		t.Errorf("IniGet(c) = %q", v)
	}

	if _, ok := s.IniGet("nope"); ok {
		t.Error("IniGet(nope) should be false")
	}

	checks := map[string]func(any) bool{
		"INF":   func(v any) bool { return v == math.Inf(1) },
		"MINF":  func(v any) bool { return v == math.Inf(-1) },
		"NAN":   func(v any) bool { f, ok := v.(float64); return ok && math.IsNaN(f) },
		"STDIN": func(v any) bool { return v == Resource{Type: "stream"} },
		"F":     func(v any) bool { return v == 1.0 },
		"P": func(v any) bool {
			a, ok := v.(*php.Array)
			want := php.ArrayOf("\x00x", int64(1), "\xff", int64(2), int64(5), int64(3))

			return ok && sameValue(a, want)
		},
	}
	for name, check := range checks {
		if v, ok := s.Constant(name); !ok || !check(v) {
			t.Errorf("Constant(%s) = %#v, %v", name, v, ok)
		}
	}

	r := NewRuntime(s)
	if got := r.GetExtensionVersion("noversion"); got != "0" {
		t.Errorf("GetExtensionVersion(noversion) = %q", got)
	}

	if got := r.GetExtensionVersion("CORE"); got != "8.3.0" {
		t.Errorf("GetExtensionVersion(CORE) = %q", got)
	}

	if files := s.IniFiles(); len(files) != 3 || files[0] != "" || files[1] != "/a.ini" || files[2] != "/b.ini" {
		t.Errorf("IniFiles() = %q", files)
	}

	if _, ok := s.LoadedIniFile(); ok {
		t.Error("LoadedIniFile() should be false")
	}

	if cc, ok := s.ConfigureCommand(); !ok || cc != " './configure'  '--enable-sigchild' " {
		t.Errorf("ConfigureCommand() = %q, %v", cc, ok)
	}

	s.functions = map[string]struct{}{"f": {}}
	s.classes = map[string]struct{}{"c": {}}

	if v, err := r.Invoke(Func("F"), 1, "x"); err != nil || v != "\x00\x01" {
		t.Errorf("Invoke(F) = %q, %v", v, err)
	}

	if _, err := r.Invoke(StaticMethod(`\C`, "M")); err == nil || err.Error() != "boom" {
		t.Errorf("Invoke(C::M) = %v", err)
	}

	var np *NotProbedError
	if _, err := r.Invoke(Func("f"), 2); !errors.As(err, &np) || np.Call != "f(2)" {
		t.Errorf("Invoke(f, 2) = %v", err)
	}
}

func TestSnapshotRuntime_NotProbed(t *testing.T) {
	r := NewRuntime(goldenSnapshot(t, "php84"))

	var np *NotProbedError

	if _, err := r.Invoke(Func("strlen"), "abc"); !errors.As(err, &np) || np.Call != "strlen('abc')" {
		t.Errorf("Invoke(strlen) = %v", err)
	}

	if _, err := r.Construct("ArrayObject"); !errors.As(err, &np) {
		t.Errorf("Construct(ArrayObject) = %v", err)
	}

	if _, err := r.GetConstant("IS_PUBLIC", "ReflectionMethod"); !errors.As(err, &np) {
		t.Errorf("GetConstant(ReflectionMethod::IS_PUBLIC) = %v", err)
	}

	defer func() {
		if _, ok := recover().(*NotProbedError); !ok {
			t.Error("HasConstant of an unprobed class constant should panic")
		}
	}()

	r.HasConstant("IS_PUBLIC", "ReflectionMethod")
}

func TestSnapshot_Ini(t *testing.T) {
	s := goldenSnapshot(t, "php84-custom-ini")

	if s.ShortOpenTag() || s.IniBool("allow_url_fopen") || !s.IniBool("enable_dl") || s.IniBool("nope") {
		t.Error("IniBool")
	}

	if v, _ := s.IniGet("memory_limit"); v != "256M" {
		t.Errorf("memory_limit = %q", v)
	}

	if loaded, ok := s.LoadedIniFile(); !ok || loaded != "<root>/tools/oracle/platform/ini/php.ini" {
		t.Errorf("LoadedIniFile() = %q, %v", loaded, ok)
	}

	if !goldenSnapshot(t, "php84").ShortOpenTag() {
		t.Error("short_open_tag defaults to On")
	}

	for in, want := range map[string]int64{"": 0, "0": 0, "1": 1, " 12abc": 12, "-3": -3, "+4": 4, "x1": 0, "99999999999999999999": math.MaxInt64} {
		if got := atoi(in); got != want {
			t.Errorf("atoi(%q) = %d, want %d", in, got, want)
		}
	}

	s.ini = php.ArrayOf("a", "ON", "b", "Yes", "c", "true", "d", "off", "e", "2", "f", "")
	for name, want := range map[string]bool{"a": true, "b": true, "c": true, "d": false, "e": true, "f": false} {
		if got := s.IniBool(name); got != want {
			t.Errorf("IniBool(%s) = %v", name, got)
		}
	}
}

func TestSnapshot_Uname(t *testing.T) {
	if v, ok := goldenSnapshot(t, "php84").Uname("s"); !ok || v != "Linux" {
		t.Errorf("Uname(s) = %q, %v", v, ok)
	}

	// php_uname is disabled there.
	if v, ok := goldenSnapshot(t, "php84-custom-ini").Uname("s"); ok || v != "" {
		t.Errorf("Uname(s) = %q, %v", v, ok)
	}
}

func TestSnapshot_CheckBinComposer(t *testing.T) {
	s := goldenSnapshot(t, "php84-minimal")
	if err := s.CheckBinComposer(); err != nil {
		t.Fatal(err)
	}

	old := *s
	old.VersionID, old.Version = 70204, "7.2.4"

	var ue *UnsupportedPHPError
	if err := old.CheckBinComposer(); !errors.As(err, &ue) || ue.Message != `Composer 2.3.0 dropped support for PHP <7.2.5 and you are running 7.2.4, please upgrade PHP or use Composer 2.2 LTS via "composer self-update --2.2". Aborting.` {
		t.Errorf("old php: %v", err)
	}

	hhvm := *s
	hhvm.constants = map[string]any{"HHVM_VERSION": "4.1.0", "PHP_EOL": "\n"}

	if err := hhvm.CheckBinComposer(); err == nil || err.Error() != "HHVM 4.0 has dropped support for Composer, please use PHP instead. Aborting." {
		t.Errorf("hhvm: %v", err)
	}

	bare := *s
	bare.extIndex = map[string]int{}

	if err := bare.CheckBinComposer(); err == nil || err.Error() != "The iconv OR mbstring extension is required and both are missing.\nInstall either of them or recompile php without --disable-iconv.\nAborting." {
		t.Errorf("no iconv/mbstring: %v", err)
	}
}

func TestDetector(t *testing.T) {
	d := &Detector{FindPHP: func() (string, bool) { return "", false }}

	_, err := d.Snapshot()
	if !errors.Is(err, ErrPHPNotFound) || err.Error() != "maestro: platform detection requires PHP but no php binary was found in PATH" {
		t.Errorf("Snapshot() = %v", err)
	}

	if _, err := d.Runtime(); !errors.Is(err, ErrPHPNotFound) {
		t.Errorf("Runtime() = %v", err)
	}

	want := goldenSnapshot(t, "php84")

	var probes atomic.Int32

	d = &Detector{
		FindPHP: func() (string, bool) { return "/bin/php", true },
		Probe: func(_ context.Context, binary string) (*Snapshot, error) {
			probes.Add(1)

			if binary != "/bin/php" {
				t.Errorf("probed %q", binary)
			}

			return want, nil
		},
	}
	d.Start()

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if s, err := d.Snapshot(); s != want || err != nil {
				t.Errorf("Snapshot() = %p, %v", s, err)
			}
		})
	}

	wg.Wait()

	if probes.Load() != 1 {
		t.Errorf("probed %d times", probes.Load())
	}
}

func TestProbe_Failures(t *testing.T) {
	var pe *ProbeError

	if _, err := Probe(t.Context(), filepath.Join(t.TempDir(), "php")); !errors.As(err, &pe) || pe.ExitCode != -1 {
		t.Errorf("missing binary: %v", err)
	}

	if runtime.GOOS == "windows" {
		return
	}

	fake := filepath.Join(t.TempDir(), "php")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'PHP Fatal error: nope'\nexit 255\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Probe(t.Context(), fake)
	if !errors.As(err, &pe) || pe.ExitCode != 255 || !strings.Contains(err.Error(), "PHP Fatal error: nope") {
		t.Errorf("failing php: %v", err)
	}

	silent := filepath.Join(t.TempDir(), "php")
	if err := os.WriteFile(silent, []byte("#!/bin/sh\ncat >/dev/null\necho warning >&2\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err = Probe(t.Context(), silent)
	if !errors.As(err, &pe) || pe.Reason != "it printed no result" || !strings.Contains(pe.Output, "warning") {
		t.Errorf("silent php: %v", err)
	}
}

func BenchmarkParseSnapshot(b *testing.B) {
	data, err := os.ReadFile("testdata/oracle/php84.json")
	if err != nil {
		b.Fatal(err)
	}

	v, _ := php.JSONDecode(string(data), true)
	probe, _ := v.(*php.Array).GetString("probe")
	b.SetBytes(int64(len(probe)))

	for b.Loop() {
		if _, err := ParseSnapshot("php", []byte(probe)); err != nil {
			b.Fatal(err)
		}
	}
}
