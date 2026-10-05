// Exported faces of util internals (parse_url(), strerror, atime) for the
// packages above util.

package util

import (
	"os"
	"time"
)

// URLParts is the array parse_url() returns; the Has fields say which
// components were present.
type URLParts struct {
	Scheme, User, Pass, Host, Path, Query, Fragment string
	Port                                            int

	HasScheme, HasUser, HasPass, HasHost, HasPort, HasPath, HasQuery, HasFragment bool
}

// ParseURL ports parse_url($url); ok is false where PHP returns false.
func ParseURL(url string) (URLParts, bool) {
	u, ok := parseURL(url)
	if !ok {
		return URLParts{}, false
	}

	return URLParts{
		Scheme: u.scheme, User: u.user, Pass: u.pass, Host: u.host, Path: u.path,
		Query: u.query, Fragment: u.fragment, Port: u.port,
		HasScheme: u.hasScheme, HasUser: u.hasUser, HasPass: u.hasPass, HasHost: u.hasHost,
		HasPort: u.hasPort, HasPath: u.hasPath, HasQuery: u.hasQuery, HasFragment: u.hasFragment,
	}, true
}

// URLScheme is parse_url($url, PHP_URL_SCHEME), "" for null.
func URLScheme(url string) string {
	u, _ := parseURL(url)

	return u.scheme
}

// URLHost is parse_url($url, PHP_URL_HOST), "" for null.
func URLHost(url string) string {
	u, _ := parseURL(url)

	return u.host
}

// URLPath is parse_url($url, PHP_URL_PATH), "" for null.
func URLPath(url string) string {
	u, _ := parseURL(url)

	return u.path
}

// Strerror renders an OS error as PHP's warnings do ("No such file or
// directory"), from the C library's strerror.
func Strerror(err error) string { return strerror(err) }

// FileAtime is a file's last access time (SplFileInfo::getATime()).
func FileAtime(fi os.FileInfo) time.Time { return fileAtime(fi) }

// Dirname is PHP's dirname($path) on this platform.
func Dirname(path string) string { return phpDirname(path, IsWindows()) }

// RealpathOK is PHP's realpath($path): ok is false where PHP returns false.
func RealpathOK(path string) (string, bool) { return phpRealpath(path) }

// PhpRealpath is PHP's realpath(): the absolute, symlink-free path of an
// existing file, ok false where PHP returns false.
func PhpRealpath(path string) (string, bool) { return phpRealpath(path) }

// IsExecutable is PHP's is_executable().
func IsExecutable(path string) bool { return isExecutable(path) }
