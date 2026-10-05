// Package util ports the self-contained parts of Composer\Util
// (src/Composer/Util): Filesystem, Platform, ProcessExecutor (with the
// symfony/process pieces it relies on), Url, NoProxyPattern, IniHelper, Tar
// and Zip.
//
// Functions that behave differently on Windows in Composer take the platform
// from runtime.GOOS; their logic lives in unexported helpers taking a
// windows flag so both variants are tested on every platform.
package util
