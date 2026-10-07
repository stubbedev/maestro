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
)

// probeCacheMaxAge is how long a cached probe result is used at most,
// however unchanged everything it was taken from looks.
const probeCacheMaxAge = 24 * time.Hour

// probeCacheFormat changes whenever what an entry holds or how it is
// keyed does.
const probeCacheFormat = "maestro-probe-cache-2"

// volatileEnv are the environment variables a shell changes from one
// command to the next, which do not reach php's view of itself; maestro's
// own (MAESTRO_*) are left out of the key too.
var volatileEnv = []string{"PWD", "OLDPWD", "SHLVL", "_"}

// probeCacheHeader is the first line of a cache entry; the probe's result
// follows it, in php's binary form of decoded JSON (php.AppendBinary),
// which reads back in a fraction of the time decoding its JSON takes.
type probeCacheHeader struct {
	Format  string    `json:"format"`
	Created time.Time `json:"created"`
	Uname   string    `json:"uname"`
	Files   []fileSig `json:"files"`
}

// fileSig is what a file (or directory) looked like: a change of any of
// it (a new file in its place, a write, a removal) invalidates the entry.
type fileSig struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists,omitempty"`
	Dev    uint64 `json:"dev,omitempty"`
	Ino    uint64 `json:"ino,omitempty"`
	Mode   uint32 `json:"mode,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Mtime  int64  `json:"mtime,omitempty"`
	Ctime  int64  `json:"ctime,omitempty"`
}

func statSig(path string) fileSig {
	var st unix.Stat_t
	if unix.Stat(path, &st) != nil {
		return fileSig{Path: path}
	}

	return fileSig{
		Path:   path,
		Exists: true,
		Dev:    st.Dev,
		Ino:    st.Ino,
		Mode:   st.Mode,
		Size:   st.Size,
		Mtime:  st.Mtim.Nano(),
		Ctime:  st.Ctim.Nano(),
	}
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
// found and resolved, its file, the environment php starts in and the
// probe script. wrapper says the executable php runs is not the file
// found (a wrapper starting it), which may pick a php by the working
// directory, so that is part of the key too. "" when binary's result is
// not cached: a script (version managers' shims choose a php each run),
// or a file not named php*.
func probeCacheKey(binary string) string {
	resolved, err := filepath.EvalSymlinks(binary)
	if err != nil || !strings.HasPrefix(filepath.Base(resolved), "php") {
		return ""
	}

	f, err := os.Open(resolved)
	if err != nil {
		return ""
	}

	head := make([]byte, 2)
	_, err = io.ReadFull(f, head)
	_ = f.Close()

	if err != nil || string(head) == "#!" {
		return ""
	}

	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")

		return slices.Contains(volatileEnv, name) || strings.HasPrefix(name, "MAESTRO_")
	})
	slices.Sort(env)

	h := sha256.New()
	for _, part := range [...]string{probeCacheFormat, probeScript, binary, resolved} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}

	sig, _ := json.Marshal(statSig(resolved))
	h.Write(sig)
	h.Write([]byte{0})

	for _, kv := range env {
		h.Write([]byte(kv))
		h.Write([]byte{0})
	}

	return hex.EncodeToString(h.Sum(nil))
}

// probeCachePath is where the entry of key lives.
func probeCachePath(key string) string {
	return filepath.Join(cache.Dir(), "platform", key[:2], key)
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

// wrapperKey is key for a wrapper: with the working directory.
func wrapperKey(key string) string {
	cwd, _ := os.Getwd()
	sum := sha256.Sum256([]byte(key + "\x00" + cwd))

	return hex.EncodeToString(sum[:])
}

func loadProbeCacheEntry(path, binary string) *Snapshot {
	data, err := os.ReadFile(path)
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

	return s
}

// storeProbeCache keeps a successful probe's output for later runs, with
// what it was taken from: the files php mapped (its executable, libraries
// and extensions), the ini files it read and the places it looks for more,
// and the binary found. Nothing is kept where the probe could not tell
// what it mapped.
func storeProbeCache(key, binary string, s *Snapshot, output []byte) {
	if !s.hasMappedFiles || len(s.mappedFiles) == 0 {
		return
	}

	result, ok := php.AppendBinary(nil, probeResult(output))
	if !ok {
		return
	}

	resolved, err := filepath.EvalSymlinks(binary)
	if err != nil {
		return
	}

	paths := []string{binary, resolved}

	// the first mapping is the executable; another file than the one
	// found means a wrapper started it
	wrapper := s.mappedFiles[0] != resolved

	for _, f := range s.mappedFiles {
		if strings.HasSuffix(f, " (deleted)") {
			return
		}
		paths = append(paths, f)
	}

	for _, f := range s.IniFiles() {
		if f != "" {
			paths = append(paths, f)
		}
	}

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

	header := probeCacheHeader{Format: probeCacheFormat, Created: time.Now(), Uname: unameString()}

	seen := map[string]bool{}
	for _, p := range paths {
		if p != "" && !seen[p] {
			seen[p] = true
			header.Files = append(header.Files, statSig(p))
		}
	}

	line, err := json.Marshal(header)
	if err != nil {
		return
	}

	if wrapper {
		key = wrapperKey(key)
	}

	path := probeCachePath(key)
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".probe-*")
	if err != nil {
		return
	}

	_, err = tmp.Write(append(append(line, '\n'), result...))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}

	if err != nil || os.Rename(tmp.Name(), path) != nil {
		_ = os.Remove(tmp.Name())
	}
}
