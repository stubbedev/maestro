// Ports vendor/symfony/process/ExecutableFinder.php and
// PhpExecutableFinder.php (5.4).

package util

import (
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// cmdBuiltins are cmd.exe's built-in commands, which exist as no file.
var cmdBuiltins = []string{
	"assoc", "break", "call", "cd", "chdir", "cls", "color", "copy", "date",
	"del", "dir", "echo", "endlocal", "erase", "exit", "for", "ftype", "goto",
	"help", "if", "label", "md", "mkdir", "mklink", "move", "path", "pause",
	"popd", "prompt", "pushd", "rd", "rem", "ren", "rename", "rmdir", "set",
	"setlocal", "shift", "start", "time", "title", "type", "ver", "vol",
}

// ExecutableFinder ports Symfony\Component\Process\ExecutableFinder.
type ExecutableFinder struct {
	suffixes []string
}

// NewExecutableFinder returns an ExecutableFinder with no extra suffixes.
func NewExecutableFinder() *ExecutableFinder {
	return &ExecutableFinder{}
}

// SetSuffixes ports ExecutableFinder::setSuffixes.
func (f *ExecutableFinder) SetSuffixes(suffixes []string) {
	f.suffixes = suffixes
}

// AddSuffix ports ExecutableFinder::addSuffix.
func (f *ExecutableFinder) AddSuffix(suffix string) {
	f.suffixes = append(f.suffixes, suffix)
}

// Find ports ExecutableFinder::find: the path of the executable name in
// PATH or extraDirs (trying PATHEXT suffixes on Windows), falling back on
// `command -v`. ok is false when none was found; callers apply their
// default.
func (f *ExecutableFinder) Find(name string, extraDirs ...string) (string, bool) {
	windows := IsWindows()

	// Windows built-in commands that are present in cmd.exe should not be
	// resolved using PATH as they do not exist as exes.
	if windows && slices.Contains(cmdBuiltins, strings.ToLower(name)) {
		return name, true
	}

	path := os.Getenv("PATH")
	if path == "" {
		path = os.Getenv("Path")
	}

	dirs := append(strings.Split(path, string(os.PathListSeparator)), extraDirs...)

	var suffixes []string

	if windows {
		suffixes = slices.Clone(f.suffixes)
		if pathExt := os.Getenv("PATHEXT"); pathExt != "" {
			suffixes = append(suffixes, strings.Split(pathExt, string(os.PathListSeparator))...)
		} else {
			suffixes = append(suffixes, ".exe", ".bat", ".cmd", ".com")
		}
	}

	if pathinfoExtension(name, windows) != "" {
		suffixes = append([]string{""}, suffixes...)
	} else {
		suffixes = append(suffixes, "")
	}

	sep := string(os.PathSeparator)

	for _, suffix := range suffixes {
		for _, dir := range dirs {
			if dir == "" {
				dir = "."
			}

			if file := dir + sep + name + suffix; isFile(file) && (windows || isExecutable(file)) {
				return file, true
			}

			// The cheap basename test goes first to spare a stat per dir.
			if phpBasename(dir, windows) == name+suffix && !isDir(dir) && isExecutable(dir) {
				return dir, true
			}
		}
	}

	if windows || strings.ContainsAny(name, "/"+sep) {
		return "", false
	}

	// A name with a NUL byte makes escapeshellarg() throw a ValueError in
	// PHP; find() has no error result here, so it is not found.
	quoted, err := php.Escapeshellarg(name)
	if err != nil {
		return "", false
	}

	// `command -v` can only find what the search above did not when the
	// shell knows the name otherwise (a builtin, an exported function: it
	// prints the bare name, which must then be an executable in the
	// working directory) or expands a "~" in PATH, which the search above
	// takes literally. Without either, the shell is not started
	// (deliberate deviation 3): its answer could only be "not found".
	if !strings.Contains(path, "~") && !isExecutable(name) {
		return "", false
	}

	out, _ := exec.Command("/bin/sh", "-c", "command -v -- "+quoted).Output() //nolint:gosec // the name is escaped, as exec() is in Symfony.

	if result := phpExecLastLine(string(out)); result != "" && isExecutable(result) {
		return result, true
	}

	return "", false
}

// phpExecLastLine is what PHP's exec() returns for output: its last line
// with trailing whitespace (isspace) stripped.
func phpExecLastLine(out string) string {
	out = strings.TrimSuffix(out, "\n")
	if i := strings.LastIndexByte(out, '\n'); i >= 0 {
		out = out[i+1:]
	}

	return strings.TrimRight(out, " \t\n\v\f\r")
}

// pathinfoExtension is pathinfo($path, PATHINFO_EXTENSION).
func pathinfoExtension(path string, windows bool) string {
	base := phpBasename(path, windows)
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i+1:]
	}

	return ""
}

// PhpExecutableFinder ports Symfony\Component\Process\PhpExecutableFinder.
// maestro is not PHP, so the steps relying on the running interpreter
// (PHP_BINARY, PHP_BINDIR, the phpdbg arguments) do not apply.
type PhpExecutableFinder struct {
	finder *ExecutableFinder
}

// NewPhpExecutableFinder returns a PhpExecutableFinder.
func NewPhpExecutableFinder() *PhpExecutableFinder {
	return &PhpExecutableFinder{finder: NewExecutableFinder()}
}

// Find ports PhpExecutableFinder::find: the PHP_BINARY, PHP_PATH or
// PHP_PEAR_PHP_BIN environment variables, then php on the PATH.
func (f *PhpExecutableFinder) Find() (string, bool) {
	if php := os.Getenv("PHP_BINARY"); php != "" {
		if !isExecutable(php) {
			found, ok := f.finder.Find(php)
			if !ok {
				return "", false
			}

			php = found
		}

		if isDir(php) {
			return "", false
		}

		return php, true
	}

	if php := os.Getenv("PHP_PATH"); php != "" {
		if !isExecutable(php) || isDir(php) {
			return "", false
		}

		return php, true
	}

	if php := os.Getenv("PHP_PEAR_PHP_BIN"); php != "" && isExecutable(php) && !isDir(php) {
		return php, true
	}

	var extra []string
	if IsWindows() {
		extra = append(extra, `C:\xampp\php\`)
	}

	return f.finder.Find("php", extra...)
}
