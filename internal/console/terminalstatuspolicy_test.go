// Ports tests/Composer/Test/Console/TerminalStatusPolicyTest.php (Composer).

package console

import (
	"maps"
	"slices"
	"testing"
)

func terminalStatusPolicyCommands(environment map[string]string) []string {
	policy := NewTerminalStatusPolicy(environment)
	commands := make([]string, 0, len(policy.Encoders()))
	for _, encoder := range policy.Encoders() {
		commands = append(commands, encoder.Command())
	}

	return commands
}

func TestTerminalStatusPolicy_SelectsProtocols(t *testing.T) {
	ghostty := func(extra map[string]string) map[string]string {
		environment := map[string]string{"TERM_PROGRAM": "ghostty", "TERM_PROGRAM_VERSION": "1.2.0"}
		maps.Copy(environment, extra)

		return environment
	}
	cases := []struct {
		name        string
		environment map[string]string
		expected    []string
	}{
		{"unknown", map[string]string{}, []string{"7501"}},
		{"auto", map[string]string{"COMPOSER_TERMINAL_STATUS": "auto"}, []string{"7501"}},
		{"empty default", map[string]string{"COMPOSER_TERMINAL_STATUS": ""}, []string{"7501"}},
		{"ANSI support alone", map[string]string{"TERM": "xterm-256color"}, []string{"7501"}},
		{"Windows Terminal", map[string]string{"WT_SESSION": "session-id"}, []string{"9;4", "7501"}},
		{"ConEmu", map[string]string{"ConEmuANSI": "ON"}, []string{"9;4", "7501"}},
		{"ConEmu disabled", map[string]string{"ConEmuANSI": "OFF"}, []string{"7501"}},
		{"Ghostty", ghostty(nil), []string{"9;4", "7501"}},
		{"newer Ghostty", map[string]string{"TERM_PROGRAM": "ghostty", "TERM_PROGRAM_VERSION": "1.3.0"}, []string{"9;4", "7501"}},
		{"older Ghostty", map[string]string{"TERM_PROGRAM": "ghostty", "TERM_PROGRAM_VERSION": "1.1.3"}, []string{"7501"}},
		{"missing version", map[string]string{"TERM_PROGRAM": "ghostty"}, []string{"7501"}},
		{"unrecognized version", map[string]string{"TERM_PROGRAM": "ghostty", "TERM_PROGRAM_VERSION": "unknown"}, []string{"7501"}},
		{"iTerm2", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.6.6"}, []string{"9;4", "7501"}},
		{"older iTerm2", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.6.5"}, []string{"7501"}},
		{"unknown program", map[string]string{"TERM_PROGRAM": "Other", "TERM_PROGRAM_VERSION": "99.0.0"}, []string{"7501"}},
		{"tmux", ghostty(map[string]string{"TMUX": "/socket"}), []string{"7501"}},
		{"screen", ghostty(map[string]string{"STY": "session"}), []string{"7501"}},
		{"screen TERM", ghostty(map[string]string{"TERM": "screen-256color"}), []string{"7501"}},
		{"tmux TERM", ghostty(map[string]string{"TERM": "tmux-256color"}), []string{"7501"}},
		{"SSH connection", ghostty(map[string]string{"SSH_CONNECTION": "connection"}), []string{"7501"}},
		{"SSH terminal", ghostty(map[string]string{"SSH_TTY": "/dev/pts/1"}), []string{"7501"}},
		{"off", ghostty(map[string]string{"COMPOSER_TERMINAL_STATUS": "off"}), nil},
		{"7501 only", ghostty(map[string]string{"COMPOSER_TERMINAL_STATUS": "7501"}), []string{"7501"}},
		{"progress only", map[string]string{"COMPOSER_TERMINAL_STATUS": "9;4"}, []string{"9;4"}},
		{"both explicit", map[string]string{"COMPOSER_TERMINAL_STATUS": "7501,9;4"}, []string{"9;4", "7501"}},
		{"explicit through tmux", map[string]string{"COMPOSER_TERMINAL_STATUS": "7501,9;4", "TMUX": "/socket"}, []string{"9;4", "7501"}},
		{"duplicates", map[string]string{"COMPOSER_TERMINAL_STATUS": "9;4,7501,9;4"}, []string{"9;4", "7501"}},
		{"whitespace", map[string]string{"COMPOSER_TERMINAL_STATUS": " 7501, 9;4 "}, []string{"9;4", "7501"}},
	}
	for _, tc := range cases {
		if got := terminalStatusPolicyCommands(tc.environment); !slices.Equal(got, tc.expected) {
			t.Errorf("%s: protocols = %v, want %v", tc.name, got, tc.expected)
		}
	}
}

func TestTerminalStatusPolicy_IgnoresInvalidSetting(t *testing.T) {
	cases := []struct {
		setting  string
		expected []string
	}{
		{"unknown", nil},
		{"0", nil},
		{"false", nil},
		{"auto,7501", []string{"7501"}},
		{"off,9;4", []string{"9;4"}},
		{"7501,", []string{"7501"}},
		{"7501,unknown", []string{"7501"}},
	}
	for _, tc := range cases {
		got := terminalStatusPolicyCommands(map[string]string{"COMPOSER_TERMINAL_STATUS": tc.setting})
		if !slices.Equal(got, tc.expected) {
			t.Errorf("%q: protocols = %v, want %v", tc.setting, got, tc.expected)
		}
	}
}
