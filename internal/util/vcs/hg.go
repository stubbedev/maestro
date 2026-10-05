// Ports src/Composer/Util/Hg.php.

package vcs

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// hgVersion is Hg::$version.
var hgVersion versionCache

// Hg ports Composer\Util\Hg.
type Hg struct {
	io      io.IO
	config  http.Config
	process Process
}

// NewHg is new Hg($io, $config, $process).
func NewHg(ioi io.IO, config http.Config, process Process) *Hg {
	return &Hg{io: ioi, config: config, process: process}
}

var hgURL = php.MustCompile(`{^(?P<proto>ssh|https?)://(?:(?P<user>[^:@]+)(?::(?P<pass>[^:@]+))?@)?(?P<host>[^/]+)(?P<path>/.*)?}mi`)

// RunCommand ports runCommand(): runs the command for url, then for url
// with the stored credentials of its host. An empty cwd is null.
func (h *Hg) RunCommand(commandCallable CommandFunc, url, cwd string) error {
	if err := h.config.ProhibitURLByConfig(url, h.io, nil); err != nil {
		return err
	}

	// Try as is
	var ignoredOutput string

	if code, err := h.process.Execute(commandCallable(url), &ignoredOutput, cwd); err != nil || code == 0 {
		return err
	}

	var errorMsg string

	// Try with the authentication information available
	matches, err := hgURL.Match(url)
	if err != nil {
		return err
	}

	var host string
	if matches != nil {
		host, _ = matches.Named("host")
	}

	if matches != nil && h.io.HasAuthentication(host) {
		proto, _ := matches.Named("proto")
		path, _ := matches.Named("path")

		var authenticatedURL string

		if proto == "ssh" {
			user := ""
			if u, ok := matches.Named("user"); ok {
				user = php.Rawurlencode(u) + "@"
			}

			authenticatedURL = proto + "://" + user + host + path
		} else {
			auth := h.io.Authentication(host)
			authenticatedURL = proto + "://" + php.Rawurlencode(strOf(auth.Username)) + ":" + php.Rawurlencode(strOf(auth.Password)) + "@" + host + path
		}

		if code, err := h.process.Execute(commandCallable(authenticatedURL), &ignoredOutput, cwd); err != nil || code == 0 {
			return err
		}

		errorMsg = h.process.GetErrorOutput()
	} else {
		errorMsg = "The given URL (" + util.SanitizeURL(url) + ") does not match the required format (ssh|http(s)://(username:password@)example.com/path-to-repository)"
	}

	return h.throwException("Failed to clone "+url+", \n\n"+errorMsg, url)
}

// throwException returns the RuntimeException for message, with URLs
// sanitized, or the one saying hg is missing.
func (h *Hg) throwException(message, url string) error {
	if _, ok, err := GetHgVersion(h.process); err != nil {
		return err
	} else if !ok {
		return &util.RuntimeError{Message: util.SanitizeURL("Failed to clone " + url + ", hg was not found, check that it is installed and in your PATH env." + "\n\n" + h.process.GetErrorOutput())}
	}

	return &util.RuntimeError{Message: util.SanitizeURL(message)}
}

// GetHgVersion ports Hg::getVersion: the hg version, false when hg is not
// found. It runs `hg --version` once per process.
func GetHgVersion(process Process) (string, bool, error) {
	return hgVersion.get(func() (string, bool, error) {
		return versionMatch(process, util.Cmd("hg", "--version"), `/^.+? (\d+(?:\.\d+)+)(?:\+.*?)?\)?\r?\n/`)
	}, false)
}

// SetHgVersion sets the cached hg version (Hg::$version); known false
// resets it.
func SetHgVersion(version string, known bool) { hgVersion.set(version, known) }
