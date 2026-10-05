// Package util ports the self-contained parts of Composer\Util
// (src/Composer/Util): Filesystem, Platform, ProcessExecutor (with the
// symfony/process pieces it relies on), Url, NoProxyPattern, IniHelper, Tar,
// Zip, ComposerMirror and ForgejoUrl. Composer's exception classes are the
// typed errors of errors.go, plus TransportException and its kin
// (transporterror.go). The HTTP classes are in the http subpackage.
//
// Functions that behave differently on Windows in Composer take the platform
// from runtime.GOOS; their logic lives in unexported helpers taking a
// windows flag so both variants are tested on every platform.
package util
