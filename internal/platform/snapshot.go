package platform

import (
	"bytes"
	"encoding/base64"
	"errors"
	"math"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// probeFormat is the "format" probe.php reports; Parse rejects others.
const probeFormat = 1

// probeMarker precedes the probe's JSON in its output, after anything php
// printed while starting up.
const probeMarker = "\n\x00maestro-probe\x00"

// Snapshot is everything the probe learnt about one php binary. It is
// immutable once parsed and safe for concurrent use; the *php.Array values
// it hands out must not be modified.
type Snapshot struct {
	// Binary is the php executable that was probed.
	Binary string
	// Version is PHP_VERSION and VersionID PHP_VERSION_ID.
	Version   string
	VersionID int64
	// Extensions are the loaded extensions in get_loaded_extensions()
	// order, ZendExtensions get_loaded_extensions(true).
	Extensions     []Extension
	ZendExtensions []string
	// Xdebug is what XdebugHandler learns about xdebug.
	Xdebug Xdebug

	loadedIni     string // php_ini_loaded_file(), "" for false
	scannedIni    string // php_ini_scanned_files()
	hasScannedIni bool   // php_ini_scanned_files() !== false

	configureCommand    string // phpinfo(INFO_GENERAL)'s Configure Command
	hasConfigureCommand bool   // DiagnoseCommand's pattern matched it

	ini            *php.Array          // ini_get_all(null, false)
	constants      map[string]any      // get_defined_constants()
	classConstants *php.Array          // class name => its constants, for the probed classes
	functions      map[string]struct{} // get_defined_functions()['internal'], lower-cased
	classes        map[string]struct{} // get_declared_classes(), lower-cased
	calls          []probeCall
	extIndex       map[string]int // lower-cased extension name => index in Extensions

	// mappedFiles are the files the probe's process loaded: on Linux
	// those it mapped (/proc/self/maps: its binary, libraries and
	// extensions), on Windows the modules of the process that answered
	// (probe_windows.go); hasMappedFiles is false where it could not
	// tell. On macOS they stay empty: the files are walked from the load
	// commands of the php that answered instead (macho_darwin.go).
	mappedFiles    []string
	hasMappedFiles bool
}

// Extension is one loaded extension.
type Extension struct {
	Name string
	// Version is phpversion($name); VersionOK is false when that is false.
	Version   string
	VersionOK bool
	// Info is what ReflectionExtension::info() prints in the CLI SAPI.
	Info string
}

// Xdebug is what XdebugHandler::setXdebugDetails determines, plus what a
// restart without xdebug takes away.
type Xdebug struct {
	Loaded bool
	// Version is phpversion('xdebug') ("unknown" for false) and Mode its
	// mode ("" for null), when loaded.
	Version, Mode string
	// Active is XdebugHandler::isXdebugActive().
	Active bool
	// The functions, constants, classes and ini settings xdebug defines.
	Functions, Constants, Classes, Ini []string
}

// probeCall is the recorded outcome of one call: a function
// ("inet_pton"), a static method ("intlchar::getunicodeversion") or a
// construction ("new imagick"), lower-cased.
type probeCall struct {
	callable string
	args     []any
	value    any
	err      *PHPError
	methods  []probeCall
}

// Resource is a PHP resource, as constants such as STDIN hold.
type Resource struct {
	Type string
}

// objectMarker is an object in the probe output, before parse binds it to
// the methods recorded for it.
type objectMarker struct {
	class string
}

// ProbeError is a probe that did not produce a snapshot.
type ProbeError struct {
	Binary   string
	ExitCode int
	Output   string // what php printed (but its result) on standard output and error
	Reason   string
}

func (e *ProbeError) Error() string {
	msg := "maestro: detecting the platform with " + e.Binary + " failed: " + e.Reason
	if out := strings.TrimSpace(e.Output); out != "" {
		msg += "\n" + out
	}

	return msg
}

// PHPBinary is PHP_BINARY of the probed php, the path Composer prints and
// runs @php scripts with (PhpExecutableFinder): as the system names the
// executable, where Binary is how it was found (on Windows with the
// PATHEXT extension's case, "php.EXE"). Binary when php reports none.
func (s *Snapshot) PHPBinary() string {
	if s == nil {
		return ""
	}

	if b, _ := s.constants["PHP_BINARY"].(string); b != "" {
		return b
	}

	return s.Binary
}

// ParseSnapshot parses the output of probe.php run by binary.
func ParseSnapshot(binary string, output []byte) (*Snapshot, error) {
	i := bytes.LastIndex(output, []byte(probeMarker))

	// What php printed besides the result.
	printed := output
	if i >= 0 {
		printed = output[:i]
	}

	fail := func(reason string) (*Snapshot, error) {
		return nil, &ProbeError{Binary: binary, Output: string(printed), Reason: reason}
	}

	if i < 0 {
		return fail("it printed no result")
	}

	decoded, err := php.JSONDecode(string(output[i+len(probeMarker):]), true)
	if err != nil {
		return fail("its result is not valid JSON: " + err.Error())
	}

	s, reason := snapshotOf(binary, decoded)
	if reason != "" {
		return fail(reason)
	}

	return s, nil
}

// probeResult is the decoded JSON of the probe's output, or nil.
func probeResult(output []byte) any {
	i := bytes.LastIndex(output, []byte(probeMarker))
	if i < 0 {
		return nil
	}

	decoded, err := php.JSONDecode(string(output[i+len(probeMarker):]), true)
	if err != nil {
		return nil
	}

	return decoded
}

// snapshotOf is the snapshot of the probe's decoded result, or why there
// is none.
func snapshotOf(binary string, decoded any) (*Snapshot, string) {
	root, ok := decoded.(*php.Array)
	if !ok {
		return nil, "its result is not an object"
	}

	if v, _ := root.Get("format"); v != int64(probeFormat) {
		return nil, "its result has an unknown format"
	}

	s := &Snapshot{Binary: binary}

	if err := s.fill(root); err != nil {
		return nil, err.Error()
	}

	return s, ""
}

var errMalformed = errors.New("its result is malformed")

func (s *Snapshot) fill(root *php.Array) error {
	s.ini = arrayAt(root, "ini")
	s.classConstants = arrayAt(root, "class_constants")

	unwrapValues(s.ini)

	constants := arrayAt(root, "constants")
	s.constants = make(map[string]any, constants.Len())

	for k, v := range constants.All() {
		s.constants[k.String()] = unwrap(v)
	}

	for _, v := range s.classConstants.All() {
		if a, ok := v.(*php.Array); ok {
			unwrapValues(a)
		}
	}

	version, ok := s.constants["PHP_VERSION"].(string)
	if !ok {
		return errMalformed
	}

	s.Version = version
	s.VersionID, _ = s.constants["PHP_VERSION_ID"].(int64)

	s.functions = lowerSet(arrayAt(root, "functions"))
	s.classes = lowerSet(arrayAt(root, "classes"))
	s.ZendExtensions = stringList(arrayAt(root, "zend_extensions"))

	if files := arrayAt(root, "ini_files"); files.Len() == 2 {
		loaded, _ := files.Get(0)
		s.loadedIni, _ = unwrap(loaded).(string)
		scanned, _ := files.Get(1)
		s.scannedIni, s.hasScannedIni = unwrap(scanned).(string)
	}

	s.configureCommand, s.hasConfigureCommand = unwrap(valueAt(root, "configure_command")).(string)

	exts := arrayAt(root, "extensions")
	s.Extensions = make([]Extension, 0, exts.Len())
	s.extIndex = make(map[string]int, exts.Len())

	for _, v := range exts.All() {
		row, _ := v.(*php.Array)
		if row == nil || row.Len() != 3 {
			return errMalformed
		}

		name, _ := row.GetString(0)
		version, _ := row.Get(1)
		info, _ := row.Get(2)

		ext := Extension{Name: name}
		ext.Version, ext.VersionOK = unwrap(version).(string)
		ext.Info, _ = unwrap(info).(string)

		s.extIndex[php.Strtolower(name)] = len(s.Extensions)
		s.Extensions = append(s.Extensions, ext)
	}

	calls := arrayAt(root, "calls")
	s.calls = make([]probeCall, 0, calls.Len())

	for _, v := range calls.All() {
		s.calls = append(s.calls, parseCall(v))
	}

	x := arrayAt(root, "xdebug")
	s.Xdebug.Loaded = php.ToBool(valueAt(x, "loaded"))
	s.Xdebug.Version, _ = valueAt(x, "version").(string)
	s.Xdebug.Mode, _ = valueAt(x, "mode").(string)
	s.Xdebug.Active = php.ToBool(valueAt(x, "active"))
	s.Xdebug.Functions = stringList(arrayAt(x, "functions"))
	s.Xdebug.Constants = stringList(arrayAt(x, "constants"))
	s.Xdebug.Classes = stringList(arrayAt(x, "classes"))
	s.Xdebug.Ini = stringList(arrayAt(x, "ini"))

	if v, ok := root.GetArray("mapped_files"); ok {
		s.mappedFiles, s.hasMappedFiles = stringList(v), true
	}

	return nil
}

func parseCall(v any) probeCall {
	entry, _ := v.(*php.Array)
	if entry == nil {
		return probeCall{}
	}

	var c probeCall

	switch callable := valueAt(entry, "callable").(type) {
	case string:
		c.callable = php.Strtolower(callable)
	case *php.Array:
		class, _ := callable.GetString(0)
		method, _ := callable.GetString(1)
		c.callable = php.Strtolower(class + "::" + method)
	}

	if args := arrayAt(entry, "args"); args.Len() > 0 {
		unwrapValues(args)
		c.args = args.Values()
	}

	if e := arrayAt(entry, "error"); e.Len() == 2 {
		class, _ := e.GetString(0)
		msg, _ := e.Get(1)
		message, _ := unwrap(msg).(string)
		c.err = &PHPError{Class: class, Message: message}

		return c
	}

	c.value = unwrap(valueAt(entry, "value"))

	if methods := arrayAt(entry, "methods"); methods.Len() > 0 {
		c.methods = make([]probeCall, 0, methods.Len())
		for _, m := range methods.All() {
			c.methods = append(c.methods, parseCall(m))
		}
	}

	if o, ok := c.value.(objectMarker); ok {
		c.value = &probedObject{class: o.class, methods: c.methods}
	}

	return c
}

// unwrap reverses probe.php's maestro_probe_enc on v.
func unwrap(v any) any {
	a, ok := v.(*php.Array)
	if !ok {
		return v
	}

	if a.Len() == 1 {
		if k, x, _ := a.First(); k.IsString() && strings.HasPrefix(k.String(), "\x00") {
			return unwrapMarker(k.String(), x)
		}
	}

	unwrapValues(a)

	return a
}

// unwrapValues unwraps the values of a in place. An array cannot hold
// objects and resources, so they stay wrapped; unwrap them when reading.
func unwrapValues(a *php.Array) {
	for k, v := range a.All() {
		if x, ok := v.(*php.Array); ok {
			switch u := unwrap(x).(type) {
			case objectMarker, Resource:
			default:
				if u != any(x) {
					a.SetKey(k, u)
				}
			}
		}
	}
}

func unwrapMarker(marker string, x any) any {
	switch marker {
	case "\x00s":
		s, _ := x.(string)
		b, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil
		}

		return string(b)
	case "\x00f":
		switch x {
		case "INF":
			return math.Inf(1)
		case "-INF":
			return math.Inf(-1)
		}

		return math.NaN()
	case "\x00p":
		pairs, _ := x.(*php.Array)
		out := php.NewArray()

		for _, p := range pairs.All() {
			pair, _ := p.(*php.Array)
			k, _ := pair.Get(0)
			v, _ := pair.Get(1)
			out.Set(unwrap(k), unwrap(v))
		}

		return out
	case "\x00o":
		class, _ := x.(string)

		return objectMarker{class: class}
	case "\x00r":
		typ, _ := x.(string)

		return Resource{Type: typ}
	}

	return nil
}

// arrayAt is a[key] as an array, empty when missing.
func arrayAt(a *php.Array, key string) *php.Array {
	if a != nil {
		if v, ok := a.GetArray(key); ok {
			return v
		}
	}

	return php.NewArray()
}

func valueAt(a *php.Array, key string) any {
	v, _ := a.Get(key)

	return v
}

func stringList(a *php.Array) []string {
	out := make([]string, 0, a.Len())
	for _, v := range a.All() {
		if s, ok := unwrap(v).(string); ok {
			out = append(out, s)
		}
	}

	return out
}

func lowerSet(a *php.Array) map[string]struct{} {
	set := make(map[string]struct{}, a.Len())
	for _, v := range a.All() {
		if s, ok := v.(string); ok {
			set[php.Strtolower(s)] = struct{}{}
		}
	}

	return set
}

// IniGet ports ini_get($name): false (ok false) for an unknown setting,
// "" for one without a value.
func (s *Snapshot) IniGet(name string) (string, bool) {
	v, ok := s.ini.Get(name)
	if !ok {
		return "", false
	}

	str, _ := v.(string)

	return str, true
}

// IniBool is the setting name as PHP reads a boolean ini setting
// (zend_ini_parse_bool): "on", "yes" and "true" in any case, or a non-zero
// number. Unknown settings are false.
func (s *Snapshot) IniBool(name string) bool {
	v, _ := s.IniGet(name)

	switch php.Strtolower(v) {
	case "on", "yes", "true":
		return true
	}

	return atoi(v) != 0
}

// atoi is C's atoi (ZEND_STRTOL(str, NULL, 10) as zend_ini_parse_bool
// uses it): leading white space, a sign and digits.
func atoi(s string) int64 {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] >= '\t' && s[i] <= '\r') {
		i++
	}

	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}

	var n int64

	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		if n > (math.MaxInt64-9)/10 {
			n = math.MaxInt64

			break
		}

		n = n*10 + int64(s[i]-'0')
	}

	if neg {
		return -n
	}

	return n
}

// IniGetAll is ini_get_all(null, false): every setting by name, a string
// or null. It must not be modified.
func (s *Snapshot) IniGetAll() *php.Array { return s.ini }

// IniFiles ports XdebugHandler::getAllIniFiles without its environment
// override: php_ini_loaded_file() (possibly "") then the files of
// php_ini_scanned_files(). It is the callback util.IniGetAll and
// util.IniGetMessage take.
func (s *Snapshot) IniFiles() []string {
	paths := []string{s.loadedIni}

	if s.hasScannedIni {
		for f := range strings.SplitSeq(s.scannedIni, ",") {
			paths = append(paths, php.Trim(f))
		}
	}

	return paths
}

// LoadedIniFile is php_ini_loaded_file(); ok is false for false.
func (s *Snapshot) LoadedIniFile() (string, bool) { return s.loadedIni, s.loadedIni != "" }

// ConfigureCommand is the "Configure Command" of phpinfo(INFO_GENERAL),
// as DiagnoseCommand::checkPlatform matches it; ok is false when its
// pattern does not match (php built without it).
func (s *Snapshot) ConfigureCommand() (string, bool) {
	return s.configureCommand, s.hasConfigureCommand
}

// ShortOpenTag is the short_open_tag setting, for classmap.Parser.
func (s *Snapshot) ShortOpenTag() bool { return s.IniBool("short_open_tag") }

// Constant is constant($name) for a global constant.
func (s *Snapshot) Constant(name string) (any, bool) {
	v, ok := s.constants[name]

	return v, ok
}

// Uname is php_uname($mode) for mode "s", "n", "r", "v" or "m"; ok is
// false when php_uname is unavailable (Composer then says "Unknown").
func (s *Snapshot) Uname(mode string) (string, bool) {
	v, err := NewRuntime(s).Invoke(Func("php_uname"), mode)
	str, ok := v.(string)

	return str, err == nil && ok
}
