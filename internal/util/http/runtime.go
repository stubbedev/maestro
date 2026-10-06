// The process facts Composer's User-Agent reports: Composer::getVersion(),
// Composer::getRunningCommand()/getRunningOperation(),
// PlatformRepository::getPlatformPhpVersion() and the PHP version, which
// live in packages above this one and are injected through Runtime.

package http

import (
	"sync"
)

// ComposerVersion is Composer::getVersion() of the Composer release maestro
// ports and reports itself as.
const ComposerVersion = "2.10.3"

// Runtime supplies the facts of the running process that end up in the
// User-Agent header. Implementations must be safe for concurrent use.
type Runtime interface {
	// PHPVersion is the version of the php binary ("8.3.6"), "" when php
	// is unavailable.
	PHPVersion() string
	// PlatformPHPVersion is PlatformRepository::getPlatformPhpVersion(),
	// "" for null.
	PlatformPHPVersion() string
	// RunningCommand and RunningOperation are Composer's statics; false
	// is null.
	RunningCommand() (string, bool)
	RunningOperation() (string, bool)
	// ClientVersion is maestro's own version, reported where Composer
	// reports its HTTP client.
	ClientVersion() string
}

// StaticRuntime is a Runtime with mutable fields, for callers without live
// statics (and tests).
type StaticRuntime struct {
	mu                       sync.RWMutex
	php, platformPHP, client string
	command, operation       string
	hasCommand, hasOperation bool
}

// NewStaticRuntime returns a runtime reporting the given versions.
func NewStaticRuntime(phpVersion, clientVersion string) *StaticRuntime {
	return &StaticRuntime{php: phpVersion, client: clientVersion}
}

// SetPlatformPHPVersion sets the platform php version, "" for none.
func (r *StaticRuntime) SetPlatformPHPVersion(v string) {
	r.mu.Lock()
	r.platformPHP = v
	r.mu.Unlock()
}

// SetRunningCommand is Composer::setRunningCommand; ok false is null.
func (r *StaticRuntime) SetRunningCommand(command string, ok bool) {
	r.mu.Lock()
	r.command, r.hasCommand = command, ok
	r.mu.Unlock()
}

// SetRunningOperation is Composer::setRunningOperation; ok false is null.
func (r *StaticRuntime) SetRunningOperation(operation string, ok bool) {
	r.mu.Lock()
	r.operation, r.hasOperation = operation, ok
	r.mu.Unlock()
}

// PHPVersion implements Runtime.
func (r *StaticRuntime) PHPVersion() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.php
}

// PlatformPHPVersion implements Runtime.
func (r *StaticRuntime) PlatformPHPVersion() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.platformPHP
}

// RunningCommand implements Runtime.
func (r *StaticRuntime) RunningCommand() (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.command, r.hasCommand
}

// RunningOperation implements Runtime.
func (r *StaticRuntime) RunningOperation() (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.operation, r.hasOperation
}

// ClientVersion implements Runtime.
func (r *StaticRuntime) ClientVersion() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.client
}

// defaultRuntime is used when no Runtime was injected.
var defaultRuntime Runtime = NewStaticRuntime("", "")

// uname is php_uname('s') and php_uname('r'), read once.
var uname = sync.OnceValues(systemUname)

// CurlInfo is what curl_version() reports of PHP's libcurl, as far as
// the transport reproduces it.
type CurlInfo struct {
	// Version is libcurl's version ("8.22.0").
	Version string
	// Features is its feature bit mask (CURL_VERSION_*).
	Features int64
	// SSLVersion is its TLS library ("OpenSSL/3.6.4").
	SSLVersion string
}

// curl_version() feature bits.
const (
	curlVersionLibz   = 1 << 3
	curlVersionBrotli = 1 << 23
	curlVersionZstd   = 1 << 26
)

// defaultCurlInfo stands for PHP's libcurl when nothing describes it: the
// reference environment's (8.22 with zlib, brotli and zstd, on OpenSSL).
var defaultCurlInfo = CurlInfo{Version: "8.22.0", Features: curlVersionLibz | curlVersionBrotli | curlVersionZstd, SSLVersion: "OpenSSL"}

// curlInfoSource is the source of curlInfo (SetCurlInfo).
var curlInfoSource struct {
	mu sync.RWMutex
	fn func() (CurlInfo, bool)
}

// SetCurlInfo sets where the transport learns about the libcurl of the php
// Composer runs on (curl_version() of the probed php): the content
// encodings it advertises and decodes, and the TLS library it names in
// some errors. fn is called when they are needed; nil, or ok false (no
// php, or no curl extension), is defaultCurlInfo.
func SetCurlInfo(fn func() (CurlInfo, bool)) {
	curlInfoSource.mu.Lock()
	curlInfoSource.fn = fn
	curlInfoSource.mu.Unlock()
}

// curlInfo is PHP's libcurl as SetCurlInfo describes it.
func curlInfo() CurlInfo {
	curlInfoSource.mu.RLock()
	fn := curlInfoSource.fn
	curlInfoSource.mu.RUnlock()

	if fn != nil {
		if info, ok := fn(); ok {
			if info.SSLVersion == "" {
				info.SSLVersion = defaultCurlInfo.SSLVersion
			}

			return info
		}
	}

	return defaultCurlInfo
}
