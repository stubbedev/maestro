package command

import (
	"slices"
	"strconv"
	"testing"

	"github.com/stubbedev/maestro/internal/ui"
	"github.com/stubbedev/maestro/internal/util"
)

func TestTransportDiagnostic(t *testing.T) {
	const url = "https://user:pass@repo.example/packages.json"
	curl := func(errno int, text string) *util.TransportError {
		e := util.NewTransportError("curl error "+strconv.Itoa(errno)+" while downloading "+util.SanitizeURL(url)+": "+text, 400)
		e.ResponseInfo = &util.TransferInfo{URL: url, ErrorCode: errno}
		e.Curl = &util.CurlFailure{Errno: errno, Message: text}

		return e
	}
	// the message is maestro's to word: the diagnostic reads the failure
	reworded := curl(7, "Failed to connect to repo.example port 443 after 0 ms: Could not connect to server")
	reworded.Message = "the connection to repo.example failed"

	for _, c := range []struct {
		err     error
		message string
		details []string
	}{
		{
			curl(7, "Failed to connect to repo.example port 443 after 0 ms: Could not connect to server"),
			"Could not download https://user:***@repo.example/packages.json: could not connect to the server",
			[]string{"curl error 7: Failed to connect to repo.example port 443 after 0 ms: Could not connect to server"},
		},
		{
			curl(60, "SSL certificate problem: unable to get local issuer certificate"),
			"Could not download https://user:***@repo.example/packages.json: the server's TLS/SSL certificate could not be verified (SSL certificate problem: unable to get local issuer certificate)",
			[]string{"curl error 60: SSL certificate problem: unable to get local issuer certificate"},
		},
		{
			curl(99, "Something else"),
			"Could not download https://user:***@repo.example/packages.json: Something else",
			[]string{"curl error 99: Something else"},
		},
		{
			reworded,
			"Could not download https://user:***@repo.example/packages.json: could not connect to the server",
			[]string{"curl error 7: Failed to connect to repo.example port 443 after 0 ms: Could not connect to server"},
		},
		{
			// an HTTP status: Composer's message says it already
			&util.TransportError{Message: `The "https://repo.example/packages.json" file could not be downloaded (HTTP/1.1 404 Not Found)`, StatusCode: 404},
			`The "https://repo.example/packages.json" file could not be downloaded (HTTP/1.1 404 Not Found)`,
			nil,
		},
	} {
		d := ui.Diagnostic{Message: c.err.Error()}
		transportDiagnostic(withExitCode(c.err, 100), &d)
		if d.Message != c.message || !slices.Equal(d.Details, c.details) {
			t.Errorf("%q: message %q, details %q; want %q, %q", c.err, d.Message, d.Details, c.message, c.details)
		}
	}
}

func TestExceptionHint(t *testing.T) {
	got := exceptionHint("<error>The following exception probably indicates you are offline or have misconfigured DNS resolver(s)</error>")
	if want := "This error probably indicates you are offline or have misconfigured DNS resolver(s)"; got != want {
		t.Errorf("exceptionHint() = %q, want %q", got, want)
	}
}

// strtoupper() is ASCII-only: a script whose name only Unicode case
// mapping turns into an event's (ſ upper-cases to S) is a script.
func TestIsScriptEvent(t *testing.T) {
	for name, want := range map[string]bool{
		"post-install-cmd":  true,
		"Post-Install-Cmd":  true,
		"post_install_cmd":  true,
		"post-inſtall-cmd":  false,
		"post-install-cmd2": false,
		"test":              false,
	} {
		if got := isScriptEvent(name); got != want {
			t.Errorf("isScriptEvent(%q) = %v, want %v", name, got, want)
		}
	}
}
