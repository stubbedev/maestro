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

		return e
	}

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
			// an HTTP status: Composer's message says it already
			&util.TransportError{Message: `The "https://repo.example/packages.json" file could not be downloaded (HTTP/1.1 404 Not Found)`, StatusCode: 404},
			`The "https://repo.example/packages.json" file could not be downloaded (HTTP/1.1 404 Not Found)`,
			nil,
		},
	} {
		d := ui.Diagnostic{Message: c.err.Error()}
		transportDiagnostic(asThrowable(c.err, 100), &d)
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
