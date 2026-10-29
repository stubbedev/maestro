// Ports the static helpers of src/Composer/Installer/BinaryInstaller.php
// (determineBinaryCaller, shebangCaller, isPhpInterpreter, isSafeBinPath,
// isBinPathInsidePackage), which the event dispatcher, the package loader
// and the downloaders use too; they live here because those packages sit
// below internal/installer.

package util

import (
	"bufio"
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// DetermineBinaryCaller is BinaryInstaller::determineBinaryCaller: the
// interpreter a binary runs with ("call" for .bat/.exe, the shebang's
// program, else "php").
func DetermineBinaryCaller(bin string) (string, error) {
	if strings.HasSuffix(bin, ".bat") || strings.HasSuffix(bin, ".exe") {
		return "call", nil
	}

	f, err := os.Open(bin)
	if err != nil {
		return "", &ErrorException{Message: "fopen(" + bin + "): Failed to open stream: " + php.Strerror(err)}
	}

	defer func() { _ = f.Close() }()

	line, _ := bufio.NewReader(f).ReadString('\n')

	// trailing whitespace covers the CR of a bin using CRLF line endings,
	// leading whitespace is left alone as the kernel only honors a "#!" at
	// the very first byte of the file
	if caller, ok := ShebangCaller(php.RtrimSet(line, " \t\r\n")); ok {
		return caller, nil
	}

	return "php", nil
}

var (
	shebangPattern = php.MustCompile(`{^#!(?:/[a-zA-Z0-9_.+-]+)*/([a-zA-Z0-9_.+][a-zA-Z0-9_.+-]*(?:[ \t].*)?)$}`)
	// UnsafeProxyChars is BinaryInstaller::UNSAFE_PROXY_CHARS: the
	// characters that cannot be embedded safely in the generated proxies.
	UnsafeProxyChars = php.MustCompile("{[*$`\"&^|<>()%!;\x00-\x1f\x7f]}")
	envSplitPattern  = php.MustCompile(`{^env[ \t]+(?:-S[ \t]*)?}`)
	phpInterpreter   = php.MustCompile(`{^php[0-9.]*(?:[ \t]|$)}`)
)

// ShebangCaller is BinaryInstaller::shebangCaller: the command a shebang
// line invokes, incl. its arguments, and whether it can be used. The
// shebang comes from the very bin the proxies run, so its arguments are
// carried along as is; only characters which would break the command line
// of the .bat proxy are refused.
func ShebangCaller(shebang string) (string, bool) {
	m, err := shebangPattern.MatchStrictGroups(shebang)
	if err != nil || m == nil {
		return "", false
	}
	caller := m.Get(1)
	if unsafe, err := UnsafeProxyChars.IsMatch(caller); err != nil || unsafe {
		return "", false
	}

	// "env" is transparent, the interpreter is then its first argument, or
	// the first one after the -S which makes env split a multi-word shebang
	caller, _, err = envSplitPattern.Replace(caller, "", 1)
	if err != nil || caller == "" || caller == "env" {
		return "", false
	}

	// other env options, e.g. "env -i bash", need env itself to run
	if caller[0] == '-' {
		return "env " + caller, true
	}

	return caller, true
}

// IsPhpInterpreter is BinaryInstaller::isPhpInterpreter: whether a shebang
// caller runs a php binary, e.g. "php", "php8.2" or "php -d x=1".
func IsPhpInterpreter(caller string) bool {
	ok, err := phpInterpreter.IsMatch(caller)

	return err == nil && ok
}

// IsSafeBinPath is BinaryInstaller::isSafeBinPath: whether a bin path from a
// package can be embedded in the generated proxies. Only ever pass the
// package-relative bin path: the absolute one carries the user's own
// project path, which may well contain e.g. parentheses.
func IsSafeBinPath(bin string) bool {
	unsafe, err := UnsafeProxyChars.IsMatch(bin)

	return err == nil && !unsafe
}

// IsBinPathInsidePackage is BinaryInstaller::isBinPathInsidePackage: whether
// a bin file resolves to a path inside the package's install directory
// (GHSA-gjfg-22fp-rrxx, GHSA-96h3-5x6v-m776).
func IsBinPathInsidePackage(installPath, binPath string) bool {
	realBinPath, ok1 := php.Realpath(binPath)
	realInstallPath, ok2 := php.Realpath(installPath)

	// fail closed if either path cannot be resolved
	if !ok1 || !ok2 {
		return false
	}

	return strings.HasPrefix(realBinPath, realInstallPath+string(os.PathSeparator))
}
