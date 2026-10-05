package util

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// escapeOracle is testdata/oracle/escape.json, written by
// tools/oracle/util/escape.php from the PHP sources.
type escapeOracle struct {
	Escape []struct {
		Input   string `json:"input"`
		Posix   string `json:"posix"`
		Windows string `json:"windows"`
	} `json:"escape"`
	Symfony []struct {
		Input   string `json:"input"`
		Posix   string `json:"posix"`
		Windows string `json:"windows"`
	} `json:"symfony"`
	Prepare []struct {
		Input  string   `json:"input"`
		Output string   `json:"output"`
		Env    []string `json:"env"`
	} `json:"prepare"`
	Password []struct {
		Input  string `json:"input"`
		Output string `json:"output"`
	} `json:"password"`
}

func loadEscapeOracle(t *testing.T) *escapeOracle {
	t.Helper()

	data, err := os.ReadFile("testdata/oracle/escape.json")
	if err != nil {
		t.Fatal(err)
	}

	var o escapeOracle
	if err := json.Unmarshal(data, &o); err != nil {
		t.Fatal(err)
	}

	return &o
}

func TestOracle_ProcessExecutorEscape(t *testing.T) {
	o := loadEscapeOracle(t)

	for _, c := range o.Escape {
		if got := escapeArgument(c.Input, false); got != c.Posix {
			t.Errorf("posix escape(%q) = %q, want %q", c.Input, got, c.Posix)
		}

		// The oracle fakes Windows for Composer only; its escapeshellarg('')
		// still quotes the POSIX way, where PHP on Windows gives "" (see
		// TestProcessExecutor_EscapeArgument).
		if c.Input == "" {
			continue
		}

		if got := escapeArgument(c.Input, true); got != c.Windows {
			t.Errorf("windows escape(%q) = %q, want %q", c.Input, got, c.Windows)
		}
	}

	t.Logf("%d cases", len(o.Escape))
}

func TestOracle_SymfonyEscapeArgument(t *testing.T) {
	o := loadEscapeOracle(t)

	for _, c := range o.Symfony {
		if got := symfonyEscapeArgument(c.Input, false); got != c.Posix {
			t.Errorf("posix escapeArgument(%q) = %q, want %q", c.Input, got, c.Posix)
		}

		if got := symfonyEscapeArgument(c.Input, true); got != c.Windows {
			t.Errorf("windows escapeArgument(%q) = %q, want %q", c.Input, got, c.Windows)
		}
	}
}

func TestOracle_PrepareWindowsCommandLine(t *testing.T) {
	o := loadEscapeOracle(t)

	for _, c := range o.Prepare {
		got, env := prepareWindowsCommandLine(c.Input, quoteComSpec("", false), "UID")
		if got != c.Output || strings.Join(env, "\x00") != strings.Join(c.Env, "\x00") || len(env) != len(c.Env) {
			t.Errorf("prepareWindowsCommandLine(%q)\n got %q %q\nwant %q %q", c.Input, got, env, c.Output, c.Env)
		}
	}

	t.Logf("%d cases", len(o.Prepare))
}

func TestOracle_PasswordMasking(t *testing.T) {
	o := loadEscapeOracle(t)

	for _, c := range o.Password {
		if got := passwordArg.ReplaceAllLiteralString(c.Input, `--password '***' `); got != c.Output {
			t.Errorf("mask(%q) = %q, want %q", c.Input, got, c.Output)
		}
	}
}
