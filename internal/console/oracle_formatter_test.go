package console

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stubbedev/maestro/internal/testutil"
)

type formatterOracle struct {
	Format []struct {
		Message   string  `json:"message"`
		Decorated bool    `json:"decorated"`
		Width     int     `json:"width"`
		Output    *string `json:"output"`
		Error     *string `json:"error"`
		Class     string  `json:"class"`
	} `json:"format"`
	HTML []struct {
		Message string  `json:"message"`
		Output  *string `json:"output"`
		Error   *string `json:"error"`
	} `json:"html"`
	Escape []struct {
		Input    string `json:"input"`
		Output   string `json:"output"`
		Trailing string `json:"trailing"`
	} `json:"escape"`
	Width []struct {
		Input            string `json:"input"`
		Width            int    `json:"width"`
		Length           int    `json:"length"`
		RemoveDecoration string `json:"removeDecoration"`
		Substr           string `json:"substr"`
	} `json:"width"`
	StripTags []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	} `json:"striptags"`
}

func loadOracle(t *testing.T, name string, v any) {
	t.Helper()
	data, err := testutil.ReadGoldenFile("testdata/oracle/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatal(err)
	}
}

func setOracleEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"COLORTERM", "TERMINAL_EMULATOR", "KONSOLE_VERSION", "IDEA_INITIAL_DIRECTORY"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

// formatCatching runs fn, turning a *Error panic into its message.
func formatCatching(fn func() string) (out string, errMsg string, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			errMsg, failed = e.Message, true
		}
	}()

	return fn(), "", false
}

func TestOracle_Formatter(t *testing.T) {
	setOracleEnv(t)
	var o formatterOracle
	loadOracle(t, "formatter.json", &o)

	for _, c := range o.Format {
		f := NewOutputFormatter(c.Decorated, NamedStyle{"warning", MustStyle("black", "yellow")}, NamedStyle{"highlight", MustStyle("red", "")})
		var got string
		err := catchPanic(t, func() { got = f.FormatAndWrap(c.Message, c.Width) })
		switch {
		case c.Error != nil:
			if err == nil || err.Message != *c.Error || kindClass[err.Kind] != c.Class {
				t.Errorf("FormatAndWrap(%q, decorated=%v, width=%d): want %s %q, got %q (err %v)", c.Message, c.Decorated, c.Width, c.Class, *c.Error, got, err)
			}
		case err != nil:
			t.Errorf("FormatAndWrap(%q, decorated=%v, width=%d): unexpected error %v", c.Message, c.Decorated, c.Width, err)
		case got != *c.Output:
			t.Errorf("FormatAndWrap(%q, decorated=%v, width=%d):\nwant %q\ngot  %q", c.Message, c.Decorated, c.Width, *c.Output, got)
		}
	}

	for _, c := range o.HTML {
		f := NewHTMLOutputFormatter(NamedStyle{"warning", MustStyle("black", "yellow")})
		got, errMsg, failed := formatCatching(func() string { return f.Format(c.Message) })
		switch {
		case c.Error != nil:
			if !failed || errMsg != *c.Error {
				t.Errorf("HTML Format(%q): want error %q, got %q (err %q)", c.Message, *c.Error, got, errMsg)
			}
		case failed:
			t.Errorf("HTML Format(%q): unexpected error %q", c.Message, errMsg)
		case got != *c.Output:
			t.Errorf("HTML Format(%q):\nwant %q\ngot  %q", c.Message, *c.Output, got)
		}
	}

	for _, c := range o.Escape {
		if got := Escape(c.Input); got != c.Output {
			t.Errorf("Escape(%q) = %q, want %q", c.Input, got, c.Output)
		}
		if got := EscapeTrailingBackslash(c.Input); got != c.Trailing {
			t.Errorf("EscapeTrailingBackslash(%q) = %q, want %q", c.Input, got, c.Trailing)
		}
	}

	f := NewOutputFormatter(true)
	for _, c := range o.Width {
		if got := Width(c.Input); got != c.Width {
			t.Errorf("Width(%q) = %d, want %d", c.Input, got, c.Width)
		}
		if got := Length(c.Input); got != c.Length {
			t.Errorf("Length(%q) = %d, want %d", c.Input, got, c.Length)
		}
		if got := RemoveDecoration(f, c.Input); got != c.RemoveDecoration {
			t.Errorf("RemoveDecoration(%q) = %q, want %q", c.Input, got, c.RemoveDecoration)
		}
		if got := Substr(c.Input, 1, 3, false); got != c.Substr {
			t.Errorf("Substr(%q, 1, 3) = %q, want %q", c.Input, got, c.Substr)
		}
	}

	for _, c := range o.StripTags {
		if got := StripTags(c.Input); got != c.Output {
			t.Errorf("StripTags(%q) = %q, want %q", c.Input, got, c.Output)
		}
	}
}
