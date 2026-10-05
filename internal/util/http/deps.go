// Narrow interfaces the auth utilities (GitHub, GitLab, Bitbucket, Forgejo)
// depend on, so tests and callers can substitute them, and the adapter
// giving util.ProcessExecutor an io.IO.

package http

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// Getter is HttpDownloader::get, the request the auth utilities make.
// *HttpDownloader implements it.
type Getter interface {
	Get(url string, options *php.Array) (*Response, error)
}

// Process is ProcessExecutor::execute, which the auth utilities use to read
// tokens from git config. *util.ProcessExecutor implements it.
type Process interface {
	Execute(command util.Command, output *string, cwd string) (int, error)
}

// utilIO lets util.ProcessExecutor write to an io.IO.
type utilIO struct{ io io.IO }

func (u utilIO) IsDebug() bool { return u.io.IsDebug() }

func (u utilIO) WriteError(message string, newline bool, verbosity int) {
	u.io.WriteError(message, newline, io.Verbosity(verbosity))
}

func (u utilIO) WriteRaw(message string, newline bool, verbosity int) {
	u.io.WriteRaw(message, newline, io.Verbosity(verbosity))
}

func (u utilIO) WriteErrorRaw(message string, newline bool, verbosity int) {
	u.io.WriteErrorRaw(message, newline, io.Verbosity(verbosity))
}

// newProcess is new ProcessExecutor($io).
func newProcess(ioi io.IO) Process {
	if ioi == nil {
		return util.NewProcessExecutor(nil)
	}

	return util.NewProcessExecutor(utilIO{ioi})
}

// gitConfig runs `git config <key>`: the trimmed value and whether git
// exited with 0.
func gitConfig(process Process, key string) (string, bool) {
	var output string

	code, err := process.Execute(util.Cmd("git", "config", key), &output, "")
	if err != nil || code != 0 {
		return "", false
	}

	return php.Trim(output), true
}

// authString is the string value of a nullable credential.
func authString(p *string) string {
	if p == nil {
		return ""
	}

	return *p
}

// authArray is getAuthentication()'s array form.
func authArray(a io.Authentication) *php.Array {
	var user, pass any
	if a.Username != nil {
		user = *a.Username
	}

	if a.Password != nil {
		pass = *a.Password
	}

	return php.ArrayOf("username", user, "password", pass)
}

// sameAuthentication is $a === $b for two getAuthentication() arrays.
func sameAuthentication(a, b io.Authentication) bool {
	eq := func(x, y *string) bool {
		if x == nil || y == nil {
			return x == y
		}

		return *x == *y
	}

	return eq(a.Username, b.Username) && eq(a.Password, b.Password)
}
