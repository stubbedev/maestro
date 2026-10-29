// Ports src/Composer/Util/Http/ResponseWarnings.php.

package http

import (
	"strconv"
	"sync"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// Infallible (fixed-length): call sites ignore the error.
var ansiColorRegex = php.MustCompile("{\x1b\\[[;\\d]*m}u")

// responseWarnings is ResponseWarnings::$shownWarnings: the warnings
// already shown, keyed by host and warning.
var responseWarnings struct {
	mu    sync.Mutex
	shown map[string]bool
}

// OutputResponseWarnings is ResponseWarnings::output($io, $url, $data): it
// shows the warnings and infos of a response (the legacy warning/info keys
// and the warnings/infos lists) which apply to this Composer version,
// unless the same host showed the same ones before, and reports whether
// any was shown, now or before.
func OutputResponseWarnings(ioi io.IO, url string, data any) (bool, error) {
	a, ok := data.(*php.Array)
	if !ok {
		return false, nil
	}

	shownKey := responseWarningsKey(url, a)

	responseWarnings.mu.Lock()
	shown := responseWarnings.shown[shownKey]
	responseWarnings.mu.Unlock()

	if shown {
		return true, nil
	}

	wrote, err := writeResponseWarnings(ioi, url, a)
	if err != nil || !wrote {
		return wrote, err
	}

	responseWarnings.mu.Lock()
	if responseWarnings.shown == nil {
		responseWarnings.shown = map[string]bool{}
	}

	responseWarnings.shown[shownKey] = true
	responseWarnings.mu.Unlock()

	return true, nil
}

// responseWarningsKey is $host.':'.json_encode(array_intersect_key($data,
// array_flip([...]))): the parts of the body which are not shown do not
// make a warning a different one, and callers passing an origin or a
// repository URL share the same entry.
func responseWarningsKey(url string, data *php.Array) string {
	parts, _ := util.ParseURL(url)

	host := parts.Host
	if host == "" {
		host = url
	} else if parts.HasPort {
		host += ":" + strconv.Itoa(parts.Port)
	}

	if host == "repo.packagist.org" {
		host = "packagist.org"
	}

	shown := php.NewArray()
	for _, key := range [6]string{"warning", "warning-versions", "info", "info-versions", "warnings", "infos"} {
		if v, ok := data.Get(key); ok {
			shown.Set(key, v)
		}
	}

	encoded, _ := php.JSONEncode(shown, 0)

	return host + ":" + encoded
}

// writeResponseWarnings shows the warnings and infos of a decoded response
// body and reports whether it wrote any.
func writeResponseWarnings(ioi io.IO, url string, a *php.Array) (bool, error) {
	wrote := false

	cleanMessage := func(msg string) (string, error) {
		if !ioi.IsDecorated() {
			out, _, err := ansiColorRegex.Replace(msg, "", -1)

			return out, err
		}

		return msg, nil
	}

	var parser semver.VersionParser

	composerVersion, err := parser.Normalize(ComposerVersion)
	if err != nil {
		return false, err
	}

	composer := semver.NewConstraintOp(semver.OpEQ, composerVersion)

	matches := func(versions any) (bool, error) {
		constraint, err := parser.ParseConstraints(php.ToString(versions))
		if err != nil {
			return false, err
		}

		return constraint.Matches(composer), nil
	}

	write := func(typ string, message any) error {
		msg, err := cleanMessage(php.ToString(message))
		if err != nil {
			return err
		}

		ioi.WriteError("<"+typ+">"+php.Ucfirst(typ)+" from "+util.SanitizeURL(url)+": "+msg+"</"+typ+">", true, io.Normal)
		wrote = true

		return nil
	}

	// legacy warning/info keys
	for _, typ := range [2]string{"warning", "info"} {
		if !php.ToBool(a.At(typ)) {
			continue
		}

		if versions := a.At(typ + "-versions"); php.ToBool(versions) {
			ok, err := matches(versions)
			if err != nil {
				return wrote, err
			}

			if !ok {
				continue
			}
		}

		if err := write(typ, a.At(typ)); err != nil {
			return wrote, err
		}
	}

	// modern Composer 2.2+ format with support for multiple warning/info
	// messages
	for _, key := range [2]string{"warnings", "infos"} {
		specs, _ := a.At(key).(*php.Array)
		if specs == nil || specs.Len() == 0 {
			continue
		}

		typ := key[:len(key)-1]

		for _, spec := range specs.All() {
			s, _ := spec.(*php.Array)

			ok, err := matches(s.At("versions"))
			if err != nil {
				return wrote, err
			}

			if !ok {
				continue
			}

			if err := write(typ, s.At("message")); err != nil {
				return wrote, err
			}
		}
	}

	return wrote, nil
}

// ResetResponseWarnings is ResponseWarnings::reset(): it forgets which
// warnings were shown, so long-running processes can show them again in
// their next job.
func ResetResponseWarnings() {
	responseWarnings.mu.Lock()
	responseWarnings.shown = nil
	responseWarnings.mu.Unlock()
}
