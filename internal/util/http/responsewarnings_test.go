// Ports tests/Composer/Test/Util/Http/ResponseWarningsTest.php.

package http

import (
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

func TestResponseWarnings_RepeatedWarningsFromAHostAreShownOnce(t *testing.T) {
	ResetResponseWarnings()
	t.Cleanup(ResetResponseWarnings)

	b, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	rateLimited := php.ArrayOf("warning", "Rate limited", "status", "error", "path", "/a")

	for _, tc := range []struct {
		url  string
		data any
	}{
		{"example.org", rateLimited},
		// the parts of the body which are not shown do not make a warning
		// a different one
		{"example.org", php.ArrayOf("path", "/b", "warning", "Rate limited", "status", "error")},
		// repository urls on the same host share the origin's entry
		{"https://example.org/org-a", rateLimited},
		{"other.org", rateLimited},
		{"https://other.org:8443", rateLimited},
		{"packagist.org", rateLimited},
		{"https://repo.packagist.org", rateLimited},
		{"example.org", php.ArrayOf("warning", "Slow down")},
		{"example.org", php.ArrayOf("status", "error")},
		{"example.org", nil},
	} {
		want := tc.data != nil && tc.data.(*php.Array).At("warning") != nil

		if got, err := OutputResponseWarnings(b, tc.url, tc.data); err != nil || got != want {
			t.Fatalf("%s %v: got %v %v, want %v", tc.url, tc.data, got, err, want)
		}
	}

	want := "<warning>Warning from example.org: Rate limited</warning>\n" +
		"<warning>Warning from other.org: Rate limited</warning>\n" +
		"<warning>Warning from https://other.org:8443: Rate limited</warning>\n" +
		"<warning>Warning from packagist.org: Rate limited</warning>\n" +
		"<warning>Warning from example.org: Slow down</warning>\n"

	if got := php.NormalizeEOL(b.Output()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// another IO does not show them again, until the state is reset
	other, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	if got, err := OutputResponseWarnings(other, "example.org", rateLimited); err != nil || !got {
		t.Fatalf("got %v %v", got, err)
	}

	if got := php.NormalizeEOL(other.Output()); got != "" {
		t.Fatalf("got %q", got)
	}

	ResetResponseWarnings()

	if got, err := OutputResponseWarnings(other, "example.org", rateLimited); err != nil || !got {
		t.Fatalf("got %v %v", got, err)
	}

	if got := php.NormalizeEOL(other.Output()); got != "<warning>Warning from example.org: Rate limited</warning>\n" {
		t.Fatalf("got %q", got)
	}
}

func TestResponseWarnings_Output(t *testing.T) {
	ResetResponseWarnings()
	t.Cleanup(ResetResponseWarnings)

	b, err := io.NewBufferIO("", 0, nil)
	if err != nil {
		t.Fatal(err)
	}

	if wrote, err := OutputResponseWarnings(b, "$URL", php.NewArray()); wrote || err != nil {
		t.Fatalf("got %v %v", wrote, err)
	}

	if got := php.NormalizeEOL(b.Output()); got != "" {
		t.Fatalf("got %q", got)
	}

	// warning/info keys present but filtered out by version constraints
	data := mustJSON(t, `{"warning":"old warning msg","warning-versions":"<2.0","warnings":[{"message":"should not appear","versions":"<2.2"}]}`)
	if wrote, err := OutputResponseWarnings(b, "$URL", data); wrote || err != nil {
		t.Fatalf("got %v %v", wrote, err)
	}

	if got := php.NormalizeEOL(b.Output()); got != "" {
		t.Fatalf("got %q", got)
	}

	data = mustJSON(t, `{
		"warning": "old warning msg", "warning-versions": ">=2.0",
		"info": "old info msg", "info-versions": ">=2.0",
		"warnings": [{"message": "should not appear", "versions": "<2.2"}, {"message": "visible warning", "versions": ">=2.2-dev"}],
		"infos": [{"message": "should not appear", "versions": "<2.2"}, {"message": "visible info", "versions": ">=2.2-dev"}]
	}`)

	wrote, err := OutputResponseWarnings(b, "$URL", data)
	if err != nil || !wrote {
		t.Fatalf("expected warnings written: %v", err)
	}

	// the <info> tag is consumed by the OutputFormatter, but not <warning>
	// as that is not a default output format
	want := "<warning>Warning from $URL: old warning msg</warning>\n" +
		"Info from $URL: old info msg\n" +
		"<warning>Warning from $URL: visible warning</warning>\n" +
		"Info from $URL: visible info\n"

	if got := php.NormalizeEOL(b.Output()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
