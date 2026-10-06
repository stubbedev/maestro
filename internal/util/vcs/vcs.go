// Package vcs ports Composer's VCS command-line utilities,
// Composer\Util\{Git,Hg,Svn,Perforce}: running git/hg/svn/p4 with
// authentication retries, and Composer's bare-mirror cache of git
// repositories.
//
// The static caches of the PHP classes (the git, hg and svn binary
// versions, Perforce's p4 executable) describe the machine, not a Composer
// instance, so they stay process-wide as in PHP, as does where git's
// version is kept across runs (UseVersionCache); nothing else here is
// package-level mutable state.
package vcs

import (
	"os"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// Process is the part of Composer\Util\ProcessExecutor the VCS utilities
// use. *util.ProcessExecutor and *processmock.Mock implement it; it is a
// superset of http.Process, so it is handed on to the GitHub, GitLab and
// Bitbucket utilities.
type Process interface {
	// Execute is execute($command, $output, $cwd); a nil output lets the
	// output through, an empty cwd is the current directory.
	Execute(command util.Command, output *string, cwd string) (int, error)
	// ExecuteFunc is execute() with a callable $output.
	ExecuteFunc(command util.Command, handler func(typ, buffer string), cwd string) (int, error)
	GetErrorOutput() string
}

// CommandFunc builds the command to run for a URL (the callables of
// Git::runCommand and Hg::runCommand, which return a string or a list).
type CommandFunc func(url string) util.Command

// asExecutor returns p as a *util.ProcessExecutor, nil when it is another
// implementation (util.Filesystem then creates its own).
func asExecutor(p Process) *util.ProcessExecutor {
	if e, ok := p.(*util.ProcessExecutor); ok {
		return e
	}

	return nil
}

// versionCache is a `private static $version` binary version cache:
// unknown until computed, then the version or null.
type versionCache struct {
	mu      sync.Mutex
	known   bool
	version string
	ok      bool
}

// get returns the cached version, computing it with compute on first use
// (or on every use while uncached, with retry, for Svn's `!self::$version`).
func (c *versionCache) get(compute func() (string, bool, error), retry bool) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.known && (c.ok || !retry) {
		return c.version, c.ok, nil
	}

	v, ok, err := compute()
	if err != nil {
		return "", false, err
	}

	if retry && !ok {
		return "", false, nil
	}

	c.known, c.version, c.ok = true, v, ok

	return v, ok, nil
}

func (c *versionCache) set(version string, known bool) {
	c.mu.Lock()
	c.known, c.version, c.ok = known, version, known
	c.mu.Unlock()
}

// commandString is is_array($command) ? implode(' ', $command) : $command.
func commandString(c util.Command) string {
	if c.IsShell() {
		return c.Line()
	}

	return strings.Join(c.Args(), " ")
}

// versionMatch runs command and returns group 1 of pattern in its output
// when it exits with 0.
func versionMatch(process Process, command util.Command, pattern string) (string, bool, error) {
	var output string

	code, err := process.Execute(command, &output, "")
	if err != nil || code != 0 {
		return "", false, err
	}

	m, err := php.PregMatch(pattern, output)
	if err != nil || m == nil {
		return "", false, err
	}

	return m.Get(1), true, nil
}

// strOf is (string) $value for a nullable string.
func strOf(p *string) string {
	if p == nil {
		return ""
	}

	return *p
}

// anyOf converts a nullable string to a php value (nil for null).
func anyOf(p *string) any {
	if p == nil {
		return nil
	}

	return *p
}

// envTruthy is (bool) Platform::getEnv($name).
func envTruthy(name string) (string, bool) {
	v, ok := util.GetEnv(name)

	return v, ok && php.ToBool(v)
}

// isDir is is_dir($path).
func isDir(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.IsDir()
}
