// Ports nothing: a cache of probe.php's result across maestro runs
// (deliberate deviation 3, speed).

package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// probeCacheMaxAge is how long a cached probe result is used at most,
// however unchanged everything it was taken from looks.
const probeCacheMaxAge = 24 * time.Hour

// probeCacheMaxEntries is how many entries the cache keeps: storing one
// removes the least recently used beyond it (and any unused for
// probeCacheMaxAge). An entry is about 180 KB.
const probeCacheMaxEntries = 64

// probeCacheFormat changes whenever what an entry holds or how it is
// keyed does.
const probeCacheFormat = "maestro-probe-cache-4"

// probeEnvPrefixes and probeEnvNames are the environment variables that
// may change what probe.php reports whatever php is probed, and so key
// every entry. The rest of the environment cannot reach php's view of
// itself except through the variables an entry adds for its own php
// (probeCacheHeader.EnvNames, EnvPrefixes): those an ini file it read
// refers to and those its extensions read. When in doubt a variable is
// listed: an extra one costs a probe when it changes, a missing one a
// wrong platform.
var probeEnvPrefixes = []string{
	// PHPRC and PHP_INI_SCAN_DIR choose the ini files (php_ini.c); PHP_*
	// more generally is what wrappers and version managers pick a php or
	// its configuration by (PHP_VERSION, PHPENV_VERSION, ...)
	"PHP",
	// xdebug reads XDEBUG_MODE, XDEBUG_CONFIG, XDEBUG_SESSION and
	// XDEBUG_TRIGGER when it starts, and probe.php reads XDEBUG_MODE
	"XDEBUG",
	// the dynamic loader: LD_PRELOAD and LD_LIBRARY_PATH choose the
	// libraries whose versions extensions report
	"LD_",
	// the locale php 8 takes LC_CTYPE from at startup, before probe.php
	// sets its own, while the extensions start and the ini is read
	"LC_",
	// OpenSSL reads its configuration (OPENSSL_CONF, OPENSSL_MODULES),
	// which may load providers, when the extension starts; SSL_CERT_FILE
	// and SSL_CERT_DIR are its default certificate locations
	"OPENSSL_", "SSL_CERT_",
	// libcurl's CA bundle (curl_version reports the SSL backend's setup)
	"CURL_",
	// ImageMagick's configuration (MAGICK_HOME, MAGICK_CONFIGURE_PATH, ...)
	// for Imagick::getVersion and its info
	"MAGICK_",
	// extensions that read variables not named after themselves when they
	// start: ddtrace DD_* (shown as its datadog.* ini values),
	// opentelemetry OTEL_*, newrelic NEW_RELIC_*, elastic_apm
	// ELASTIC_APM_*, oci8 the Oracle client's ORACLE_*, TNS_* and NLS_*,
	// odbc and pdo_odbc unixODBC's ODBC*, snmp net-snmp's MIBS, MIBDIRS
	// and SNMP*
	"DD_", "OTEL_", "NEW_RELIC", "ELASTIC_APM_", "ORACLE_", "TNS_", "NLS_", "ODBC", "MIB", "SNMP",
}

var probeEnvNames = []string{
	// the locale, as LC_* above
	"LANG", "LANGUAGE", "LOCPATH",
	// iconv's conversion modules (glibc)
	"GCONV_PATH",
	// glibc's tunables, read when php starts
	"GLIBC_TUNABLES",
	// libc's time zone (date's default ignores it, libraries may not)
	"TZ",
	// sys_get_temp_dir(), which probe.php calls
	"TMPDIR",
	// PHP_BINARY is looked up in PATH when php's argv[0] has no slash,
	// and a wrapper may find its php there
	"PATH",
	// libraries read their configuration under it (readline's ~/.inputrc,
	// gnupg's ~/.gnupg, ...)
	"HOME", "GNUPGHOME",
	// intl's ICU data, which ResourceBundle::create('root', 'ICUDATA')
	// reads
	"ICU_DATA",
}

// isProbeEnv reports whether the variable name keys every entry
// (probeEnvPrefixes, probeEnvNames).
func isProbeEnv(name string) bool {
	for _, p := range probeEnvPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}

	return slices.Contains(probeEnvNames, name)
}

// volatileEnv are the environment variables a shell changes from one
// command to the next ("_" is the program it ran), which do not reach
// php's view of itself through a wrapper; maestro's own (MAESTRO_*) are
// left out too (isVolatileEnv). Neither an entry keyed on the whole
// environment (probeCacheHeader.AllEnv) nor one keyed on what a wrapper
// names uses them: a wrapper executable holds "_" and the like as symbol
// names, not as variables it reads.
var volatileEnv = []string{"PWD", "OLDPWD", "SHLVL", "_"}

// isVolatileEnv reports whether the variable name is left out of a
// wrapper's names and the whole environment (volatileEnv).
func isVolatileEnv(name string) bool {
	return slices.Contains(volatileEnv, name) || strings.HasPrefix(name, "MAESTRO_")
}

// probeCacheHeader is the first line of a cache entry; the probe's result
// follows it, in php's binary form of decoded JSON (php.AppendBinary),
// which reads back in a fraction of the time decoding its JSON takes.
type probeCacheHeader struct {
	Format  string    `json:"format"`
	Created time.Time `json:"created"`
	Uname   string    `json:"uname"`
	Files   []fileSig `json:"files"`

	// EnvNames and EnvPrefixes are the variables this php's result may
	// depend on beyond those every key has: the names its ini files
	// refer to as ${NAME} (sorted), a wrapper's (wrapperEnvNames), and
	// the extensions' names (envFold) a variable may start with. Env is
	// envSum of them as they were; AllEnv, when the variables a wrapper
	// may read are not known, makes it the whole environment's.
	EnvNames    []string `json:"env_names,omitempty"`
	EnvPrefixes []string `json:"env_prefixes,omitempty"`
	AllEnv      bool     `json:"all_env,omitempty"`
	Env         string   `json:"env"`
}

// fileSig is what a file (or directory) looked like: a change of any of
// it (a new file in its place, a write, a removal) invalidates the entry.
type fileSig struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists,omitempty"`
	fsstate.ID
}

func statSig(path string) fileSig {
	id, ok := fsstate.Stat(path)

	return fileSig{Path: path, Exists: ok, ID: id}
}

// trustedAt reports whether the signature may be recorded for a probe
// started at start: a file changed while php ran, or within a timestamp
// tick of it, may have been read as it was before while the signature
// describes it after (fsstate.Margin).
func (f fileSig) trustedAt(start time.Time) bool {
	return !f.Exists || f.Trusted(start, 0)
}

// unameString is php_uname()'s fields, which the snapshot records.
func unameString() string {
	var u unix.Utsname
	if unix.Uname(&u) != nil {
		return ""
	}

	field := func(b []byte) string { return string(bytes.TrimRight(b, "\x00")) }

	return field(u.Sysname[:]) + "\x00" + field(u.Nodename[:]) + "\x00" + field(u.Release[:]) + "\x00" + field(u.Version[:]) + "\x00" + field(u.Machine[:])
}

// probeCacheKey names the cache entry of binary's probe: the binary as
// found and resolved, its file, the environment variables that may change
// php's view of itself (isProbeEnv) and the probe script. "" when binary's
// result is not cached: a script (version managers' shims choose a php
// each run), or a file not named php*.
func probeCacheKey(binary string) string {
	resolved, err := php.EvalSymlinks(binary)
	if err != nil || !strings.HasPrefix(filepath.Base(resolved), "php") {
		return ""
	}

	f, err := os.Open(resolved)
	if err != nil {
		return ""
	}

	script, err := fsstate.IsScript(f)
	id, known := fsstate.Fstat(f)
	_ = f.Close()

	if err != nil || script || !known {
		return ""
	}

	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")

		return !isProbeEnv(name)
	})
	slices.Sort(env)

	k := fsstate.NewKeyHash()
	for _, part := range [...]string{probeCacheFormat, probeScript, binary, resolved} {
		k.String(part)
	}
	k.ID(id)
	for _, kv := range env {
		k.String(kv)
	}

	return hex.EncodeToString(k.Sum(nil))
}

// envFold is name upper-cased with everything but letters and digits
// left out, so that an extension's name and the variables it reads
// compare alike ("newrelic", NEW_RELIC_APPNAME).
func envFold(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		}

		return -1
	}, name)
}

// envSum is the hash of the environment variables named in names (sorted)
// or whose envFold starts with one of prefixes; all takes every variable
// but the volatile ones.
func envSum(names, prefixes []string, all bool) string {
	var env []string

	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")

		if envMatches(name, names, prefixes, all) {
			env = append(env, kv)
		}
	}

	slices.Sort(env)

	h := sha256.New()
	for _, kv := range env {
		h.Write([]byte(kv))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))
}

// envMatches reports whether envSum takes the variable name.
func envMatches(name string, names, prefixes []string, all bool) bool {
	if all {
		return !isVolatileEnv(name)
	}

	if _, found := slices.BinarySearch(names, name); found {
		return true
	}

	if len(prefixes) == 0 {
		return false
	}

	folded := envFold(name)

	return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(folded, p) })
}

// iniVarNames appends the names data, an ini file, refers to as ${NAME}
// (or ${NAME:-default}) to names: php expands them from its other settings
// or, failing that, from the environment.
func iniVarNames(names []string, data []byte) []string {
	for {
		i := bytes.Index(data, []byte("${"))
		if i < 0 {
			return names
		}

		data = bytes.TrimLeft(data[i+2:], " \t")
		end := bytes.IndexFunc(data, func(r rune) bool {
			return r == '}' || r == ':' || r == '$' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
		})

		if end < 0 {
			end = len(data)
		}

		if end > 0 {
			names = append(names, string(data[:end]))
		}
	}
}

// identifierNames is every run of letters, digits and underscores in data that does not start with a digit: what may be an
// environment variable's name in a wrapper's executable.
func identifierNames(data []byte) []string {
	var names []string

	isWord := func(c byte) bool {
		return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
	}

	for i := 0; i < len(data); {
		if !isWord(data[i]) {
			i++

			continue
		}

		j := i
		for j < len(data) && isWord(data[j]) {
			j++
		}

		if data[i] < '0' || data[i] > '9' {
			names = append(names, string(data[i:j]))
		}

		i = j
	}

	return names
}

// maxWrapperScan is the largest wrapper executable whose strings are
// taken as the variables it may read; a larger one keys its entry on the
// whole environment.
const maxWrapperScan = 1 << 20

// wrapperEnvNames is every name a variable the wrapper executable at path
// reads may have: a variable read by name has it in the file (makeWrapper's
// and makeBinaryWrapper's --set-default, --prefix, ...), but for the
// volatile ones (isVolatileEnv). false when the file is too large to tell.
func wrapperEnvNames(path string) ([]string, bool) {
	var st unix.Stat_t
	if unix.Stat(path, &st) != nil || st.Size > maxWrapperScan {
		return nil, false
	}

	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxWrapperScan {
		return nil, false
	}

	return slices.DeleteFunc(identifierNames(data), isVolatileEnv), true
}

// probeCachePath is where the entry of key lives.
func probeCachePath(key string) string {
	return filepath.Join(probeCacheDir(), key)
}

func probeCacheDir() string {
	return cache.PlatformProbes()
}

// loadProbeCache returns the cached snapshot of binary under key, if
// everything it was taken from is unchanged; the working directory is
// checked for a wrapper's entry.
func loadProbeCache(key, binary string) *Snapshot {
	for _, k := range [...]string{key, wrapperKey(key)} {
		if s := loadProbeCacheEntry(probeCachePath(k), binary); s != nil {
			return s
		}
	}

	return nil
}

// wrapperKey is key for a wrapper: a wrapper (an executable that starts
// another php) may choose its php by the working directory, so that is
// part of its key.
func wrapperKey(key string) string {
	cwd, _ := php.Getcwd()
	sum := sha256.Sum256([]byte(key + "\x00" + cwd))

	return hex.EncodeToString(sum[:])
}

func loadProbeCacheEntry(path, binary string) *Snapshot {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}

	// entries are replaced by rename, never written in place
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()

		return nil
	}

	data := make([]byte, info.Size())
	_, err = io.ReadFull(f, data)
	_ = f.Close()

	if err != nil {
		return nil
	}

	line, output, ok := bytes.Cut(data, []byte("\n"))
	if !ok {
		return nil
	}

	var header probeCacheHeader
	if json.Unmarshal(line, &header) != nil || header.Format != probeCacheFormat {
		return nil
	}

	if age := time.Since(header.Created); age < 0 || age > probeCacheMaxAge || header.Uname != unameString() {
		return nil
	}

	if !slices.IsSorted(header.EnvNames) || envSum(header.EnvNames, header.EnvPrefixes, header.AllEnv) != header.Env {
		return nil
	}

	for _, f := range header.Files {
		if statSig(f.Path) != f {
			return nil
		}
	}

	decoded, err := php.DecodeBinary(output)
	if err != nil {
		return nil
	}

	s, reason := snapshotOf(binary, decoded)
	if reason != "" {
		return nil
	}

	// the modification time is when the entry was last used, which
	// pruning keeps the most recent of (refreshed at most hourly)
	if now := time.Now(); now.Sub(info.ModTime()) > time.Hour {
		_ = os.Chtimes(path, time.Time{}, now)
	}

	return s
}

// storeProbeCache keeps a successful probe's output, from a probe started
// at start, for later runs, with what it was taken from: the files php
// mapped (its executable, libraries and extensions), the ini files it read
// and the places it looks for more, the binary found, and the environment
// variables its ini files and extensions may read. Nothing is kept where
// the probe could not tell what it mapped, or where one of those files
// changed too recently to tell whether php saw it before or after.
func storeProbeCache(key, binary string, s *Snapshot, output []byte, start time.Time) {
	if !s.hasMappedFiles || len(s.mappedFiles) == 0 {
		return
	}

	result, ok := php.AppendBinary(nil, probeResult(output))
	if !ok {
		return
	}

	resolved, err := php.EvalSymlinks(binary)
	if err != nil {
		return
	}

	header := probeCacheHeader{Format: probeCacheFormat, Created: time.Now(), Uname: unameString()}

	// the first mapping is the executable; another file than the one
	// found means a wrapper started it, which may read any variable it
	// names
	wrapper := s.mappedFiles[0] != resolved
	if wrapper {
		header.EnvNames, ok = wrapperEnvNames(resolved)
		header.AllEnv = !ok
	}

	paths := []string{binary, resolved}

	for _, f := range s.mappedFiles {
		if strings.HasSuffix(f, " (deleted)") {
			return
		}
		paths = append(paths, f)
	}

	for _, f := range s.IniFiles() {
		if f == "" {
			continue
		}

		paths = append(paths, f)

		data, err := os.ReadFile(f)
		if err != nil {
			return
		}

		header.EnvNames = iniVarNames(header.EnvNames, data)
	}

	// extensions read their own variables when they start (blackfire
	// BLACKFIRE_*, tideways TIDEWAYS_*, newrelic NEW_RELIC_*, ...)
	for _, e := range s.Extensions {
		header.EnvPrefixes = append(header.EnvPrefixes, envFold(e.Name))
	}

	for _, e := range s.ZendExtensions {
		header.EnvPrefixes = append(header.EnvPrefixes, envFold(e))
	}

	header.EnvPrefixes = slices.DeleteFunc(header.EnvPrefixes, func(p string) bool { return p == "" })
	slices.Sort(header.EnvPrefixes)
	header.EnvPrefixes = slices.Compact(header.EnvPrefixes)
	slices.Sort(header.EnvNames)
	header.EnvNames = slices.Compact(header.EnvNames)
	header.Env = envSum(header.EnvNames, header.EnvPrefixes, header.AllEnv)

	// where php looks for its ini files (php_init_config): PHPRC, the
	// executable's directory and the configured path, then the scan
	// directories
	var dirs []string
	if rc := os.Getenv("PHPRC"); rc != "" {
		dirs = append(dirs, rc)
		paths = append(paths, rc)
	}

	dirs = append(dirs, filepath.Dir(binary), filepath.Dir(resolved), filepath.Dir(s.mappedFiles[0]))
	if v, ok := s.Constant("PHP_CONFIG_FILE_PATH"); ok {
		if dir, ok := v.(string); ok && dir != "" {
			dirs = append(dirs, dir)
		}
	}

	for _, dir := range dirs {
		paths = append(paths, dir+"/php-cli.ini", dir+"/php.ini")
	}

	scanDir, _ := s.Constant("PHP_CONFIG_FILE_SCAN_DIR")
	defaultScanDir, _ := scanDir.(string)

	if env, ok := os.LookupEnv("PHP_INI_SCAN_DIR"); ok {
		for dir := range strings.SplitSeq(env, ":") {
			if dir == "" {
				dir = defaultScanDir
			}
			paths = append(paths, dir)
		}
	} else if defaultScanDir != "" {
		paths = append(paths, defaultScanDir)
	}

	seen := map[string]bool{}
	add := func(p string) fileSig {
		if p == "" || seen[p] {
			return fileSig{Exists: true}
		}

		seen[p] = true
		sig := statSig(p)
		header.Files = append(header.Files, sig)

		return sig
	}

	for _, p := range paths {
		// a file missing now may have been there while php ran: its
		// directory tells
		if !add(p).Exists {
			add(filepath.Dir(p))
		}
	}

	for _, f := range header.Files {
		if !f.trustedAt(start) {
			return
		}
	}

	line, err := json.Marshal(header)
	if err != nil {
		return
	}

	if wrapper {
		key = wrapperKey(key)
	}

	writeProbeCacheEntry(probeCachePath(key), append(append(line, '\n'), result...))
}

// writeProbeCacheEntry replaces the entry at path with data, then prunes
// the cache.
func writeProbeCacheEntry(path string, data []byte) {
	if fsstate.WriteAtomic(path, data) != nil {
		return
	}

	pruneProbeCache(filepath.Dir(path), probeCacheMaxEntries)
}

// pruneProbeCache removes the entries in dir unused for probeCacheMaxAge,
// then the least recently used beyond maxEntries, abandoned
// temporary files and an earlier format's subdirectories. Other processes
// may read, write and prune at the same time: removing an entry costs at
// most a probe, never a wrong result, and a temporary file is removed only
// long after its writer would have renamed it.
func pruneProbeCache(dir string, maxEntries int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type entry struct {
		path  string
		mtime time.Time
	}

	var kept []entry

	now := time.Now()

	for _, e := range entries {
		path := filepath.Join(dir, e.Name())

		if e.IsDir() {
			// maestro-probe-cache-2 kept entries under key[:2]/
			_ = os.RemoveAll(path)

			continue
		}

		info, err := e.Info()
		if err != nil {
			continue
		}

		age := now.Sub(info.ModTime())

		switch {
		case fsstate.IsTemp(e.Name()):
			if age > fsstate.TempMaxAge {
				_ = os.Remove(path)
			}
		case age > probeCacheMaxAge:
			_ = os.Remove(path)
		default:
			kept = append(kept, entry{path, info.ModTime()})
		}
	}

	if len(kept) <= maxEntries {
		return
	}

	slices.SortFunc(kept, func(a, b entry) int { return b.mtime.Compare(a.mtime) })

	for _, e := range kept[maxEntries:] {
		_ = os.Remove(e.path)
	}
}
