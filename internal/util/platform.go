// Ports src/Composer/Util/Platform.php.

package util

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// GetCwd ports Platform::getCwd: the physical working directory (getcwd(3),
// never $PWD), or "" when allowEmpty and it cannot be determined.
func GetCwd(allowEmpty bool) (string, error) {
	cwd, err := getwd()
	if err == nil {
		return cwd, nil
	}

	if allowEmpty {
		return "", nil
	}

	return "", &RuntimeError{Message: "Could not determine the current working directory", Site: phperr.At("Platform.php", 51)}
}

// Realpath ports Platform::realpath: realpath(3), falling back on path.
func Realpath(path string) string {
	if real, ok := phpRealpath(path); ok {
		return real
	}

	return path
}

// phpRealpath ports PHP's realpath(): the absolute, symlink-free path of an
// existing file; realpath("") is the working directory. Every component
// must exist, also one followed by "..", so the path is not cleaned
// lexically before resolving.
func phpRealpath(path string) (string, bool) {
	abs := path
	if !filepath.IsAbs(path) {
		cwd, err := getwd()
		if err != nil {
			return "", false
		}
		abs = cwd + string(filepath.Separator) + path
	}

	real, err := evalSymlinks(abs)
	if err != nil {
		return "", false
	}

	return real, true
}

// GetEnv ports Platform::getEnv. PHP reads $_SERVER and $_ENV before
// getenv(); PutEnv and ClearEnv keep all three in step, so the process
// environment is the single source here.
func GetEnv(name string) (string, bool) {
	return os.LookupEnv(name)
}

// GetBoolEnv ports Platform::getBoolEnv: set reports whether the variable
// holds a non-empty value; only 0, 1, false, true, off and on are accepted.
func GetBoolEnv(name string) (value, set bool, err error) {
	v, ok := GetEnv(name)
	if !ok || v == "" {
		return false, false, nil
	}

	switch v {
	case "1", "true", "on":
		return true, true, nil
	case "0", "false", "off":
		return false, true, nil
	}

	return false, false, &RuntimeError{Message: "Invalid value for " + name + ": " + v + ". Expected 0, 1, false, true, off, or on.", Site: phperr.At("Platform.php", 106)}
}

// PutEnv ports Platform::putEnv.
func PutEnv(name, value string) {
	_ = os.Setenv(name, value)
}

// ClearEnv ports Platform::clearEnv.
func ClearEnv(name string) {
	_ = os.Unsetenv(name)
}

// ExpandPath ports Platform::expandPath: a leading "~/" becomes the user
// directory, a leading $VAR or %VAR% the variable's value.
func ExpandPath(path string) (string, error) {
	return expandPath(path, IsWindows())
}

func expandPath(path string, windows bool) (string, error) {
	// '#^~[\\/]#' in PHP source is the class [\/], which only holds "/".
	if strings.HasPrefix(path, "~/") {
		home, err := GetUserDirectory()
		if err != nil {
			return "", err
		}

		return home + path[1:], nil
	}

	// #^(\$|(?P<percent>%))(?P<var>\w++)(?(percent)%)(?P<path>.*)#
	if path == "" || (path[0] != '$' && path[0] != '%') {
		return path, nil
	}

	percent := path[0] == '%'
	end := 1

	for end < len(path) && isWordByte(path[end]) {
		end++
	}

	if end == 1 {
		return path, nil
	}

	name := path[1:end]
	if percent {
		if end == len(path) || path[end] != '%' {
			return path, nil
		}

		end++
	}

	// .* stops at the first newline; what follows is not part of the match.
	rest := path[end:]
	tail := ""

	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		rest, tail = rest[:i], rest[i:]
	}

	// Treat HOME as an alias for USERPROFILE on Windows for legacy reasons.
	if windows && name == "HOME" {
		if home, _ := GetEnv("HOME"); phpTruthy(home) {
			return home + rest + tail, nil
		}

		profile, _ := GetEnv("USERPROFILE")

		return profile + rest + tail, nil
	}

	value, _ := GetEnv(name)

	return value + rest + tail, nil
}

// isWordByte reports whether c matches PCRE's \w without UCP.
func isWordByte(c byte) bool {
	return isASCIIAlnum(c) || c == '_'
}

// phpTruthy is PHP's (bool) cast of a string.
func phpTruthy(s string) bool {
	return s != "" && s != "0"
}

// GetUserDirectory ports Platform::getUserDirectory.
func GetUserDirectory() (string, error) {
	if home, ok := GetEnv("HOME"); ok {
		return home, nil
	}

	if IsWindows() {
		if home, ok := GetEnv("USERPROFILE"); ok {
			return home, nil
		}
	} else if u, err := user.LookupId(currentUID()); err == nil {
		return u.HomeDir, nil
	}

	return "", &RuntimeError{Message: "Could not determine user directory", Site: phperr.At("Platform.php", 200)}
}

var (
	wslOnce sync.Once
	isWSL   bool
)

// IsWindowsSubsystemForLinux ports Platform::isWindowsSubsystemForLinux.
func IsWindowsSubsystemForLinux() bool {
	wslOnce.Do(func() {
		if IsWindows() {
			return
		}

		data, err := os.ReadFile("/proc/version")
		isWSL = err == nil && strings.Contains(strings.ToLower(string(data)), "microsoft") && !IsDocker()
	})

	return isWSL
}

// IsWindows ports Platform::isWindows.
func IsWindows() bool {
	return runtime.GOOS == "windows"
}

var (
	dockerOnce sync.Once
	isDocker   bool
)

// IsDocker ports Platform::isDocker.
func IsDocker() bool {
	dockerOnce.Do(func() { isDocker = detectDocker() })

	return isDocker
}

func detectDocker() bool {
	// .dockerenv and .containerenv are present in some cases but not reliably.
	if slices.ContainsFunc([]string{"/.dockerenv", "/run/.containerenv", "/var/run/.containerenv"}, fileExists) {
		return true
	}

	// cgroup v2, then cgroup v1.
	for _, cgroup := range []string{"/proc/self/mountinfo", "/proc/1/cgroup"} {
		data, err := os.ReadFile(cgroup)
		if err != nil {
			continue
		}

		// Detect default mount points created by Docker/containerd.
		s := string(data)
		if strings.Contains(s, "/var/lib/docker/") || strings.Contains(s, "/io.containerd.snapshotter") {
			return true
		}
	}

	return false
}

// Strlen ports Platform::strlen: the byte length.
func Strlen(s string) int {
	return len(s)
}

// IsTty ports Platform::isTty for f (os.Stdout when nil).
func IsTty(f *os.File) bool {
	if f == nil {
		f = os.Stdout
	}

	// Detect msysgit/mingw and assume this is a tty because detection does
	// not work correctly, see https://github.com/composer/composer/issues/9690
	if msystem, _ := GetEnv("MSYSTEM"); php.Strcasecmp(msystem, "MINGW32") == 0 || php.Strcasecmp(msystem, "MINGW64") == 0 {
		return true
	}

	return term.IsTerminal(int(f.Fd()))
}

// IsInputCompletionProcess ports Platform::isInputCompletionProcess.
func IsInputCompletionProcess() bool {
	return len(os.Args) > 1 && os.Args[1] == "_complete"
}

// WorkaroundFilesystemIssues ports Platform::workaroundFilesystemIssues.
func WorkaroundFilesystemIssues() {
	if isVirtualBoxGuest() {
		time.Sleep(200 * time.Millisecond)
	}
}

var (
	vboxOnce sync.Once
	isVBox   bool
)

// isVirtualBoxGuest ports Platform::isVirtualBoxGuest: the process user is
// "vagrant", COMPOSER_RUNTIME_ENV is "virtualbox", or lsmod lists the
// VirtualBox guest additions.
func isVirtualBoxGuest() bool {
	vboxOnce.Do(func() {
		if IsWindows() {
			return
		}

		if u, err := user.LookupId(currentEUID()); err == nil && u.Username == "vagrant" {
			isVBox = true

			return
		}

		if env, _ := GetEnv("COMPOSER_RUNTIME_ENV"); env == "virtualbox" {
			isVBox = true

			return
		}

		if runtime.GOOS == "linux" {
			var output string

			code, err := NewProcessExecutor(nil).Execute(Cmd("lsmod"), &output, "")
			isVBox = err == nil && code == 0 && strings.Contains(output, "vboxguest")
		}
	})

	return isVBox
}

// GetDevNull ports Platform::getDevNull.
func GetDevNull() string {
	if IsWindows() {
		return "NUL"
	}

	return "/dev/null"
}

// fileExists is PHP's file_exists: stat(2) succeeds, following symlinks.
func fileExists(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

// isDir is PHP's is_dir, following symlinks.
func isDir(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.IsDir()
}

// isFile is PHP's is_file, following symlinks.
func isFile(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.Mode().IsRegular()
}

// isLink is PHP's is_link.
func isLink(path string) bool {
	fi, err := os.Lstat(path)

	return err == nil && fi.Mode()&os.ModeSymlink != 0
}
