// Ports the static helpers of src/Composer/Installer/BinaryInstaller.php
// (determineBinaryCaller, isBinPathInsidePackage), which the event
// dispatcher and the downloaders use too; they live here because those
// packages sit below internal/installer.

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

	m, err := shebangPattern.MatchStrictGroups(line)
	if err != nil {
		return "", err
	}

	if m != nil {
		return php.Trim(m.Get(1)), nil
	}

	return "php", nil
}

var shebangPattern = php.MustCompile(`{^#!/(?:usr/bin/env )?(?:[^/]+/)*(.+)$}m`)

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
