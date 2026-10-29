// Ports src/Composer/Console/TerminalStatusPolicy.php (Composer).

package console

import (
	"os"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
)

// TerminalStatusPolicy selects terminal status protocols without reading
// terminal input.
type TerminalStatusPolicy struct {
	encoders []ProgramStatusEncoder
}

// NewTerminalStatusPolicy mirrors new TerminalStatusPolicy($environment).
func NewTerminalStatusPolicy(environment map[string]string) *TerminalStatusPolicy {
	protocols := selectTerminalStatusProtocols(environment)
	policy := &TerminalStatusPolicy{}
	// Send rich status last so a mapped progress report cannot overwrite it.
	if terminalStatusProtocolSelected(protocols, "9;4") {
		policy.encoders = append(policy.encoders, &Osc9ProgressEncoder{})
	}
	if terminalStatusProtocolSelected(protocols, "7501") {
		policy.encoders = append(policy.encoders, &Osc7501ProgramStatusEncoder{})
	}

	return policy
}

// TerminalStatusPolicyFromEnvironment is fromEnvironment().
func TerminalStatusPolicyFromEnvironment() *TerminalStatusPolicy {
	environment := make(map[string]string, 10)
	for _, name := range []string{
		"COMPOSER_TERMINAL_STATUS", "TERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION",
		"WT_SESSION", "ConEmuANSI", "TMUX", "STY", "SSH_TTY", "SSH_CONNECTION",
	} {
		if value, ok := os.LookupEnv(name); ok {
			environment[name] = value
		}
	}

	return NewTerminalStatusPolicy(environment)
}

// Encoders is getEncoders().
func (p *TerminalStatusPolicy) Encoders() []ProgramStatusEncoder { return p.encoders }

// selectTerminalStatusProtocols is selectProtocols().
func selectTerminalStatusProtocols(environment map[string]string) []string {
	setting := php.Trim(environment["COMPOSER_TERMINAL_STATUS"])
	if setting == "" {
		setting = "auto"
	}
	if setting == "auto" {
		if terminalSupportsProgress(environment) {
			return []string{"7501", "9;4"}
		}

		return []string{"7501"}
	}
	if setting == "off" {
		return nil
	}

	// Unknown values are ignored rather than failing, so e.g. "0" or "false"
	// disable reporting.
	var protocols []string
	for part := range strings.SplitSeq(setting, ",") {
		protocol := php.Trim(part)
		if (protocol == "7501" || protocol == "9;4") && !terminalStatusProtocolSelected(protocols, protocol) {
			protocols = append(protocols, protocol)
		}
	}

	return protocols
}

// terminalSupportsProgress is supportsProgress(): the outer terminal's
// identity does not establish support through another terminal.
func terminalSupportsProgress(environment map[string]string) bool {
	for _, name := range []string{"TMUX", "STY", "SSH_TTY", "SSH_CONNECTION"} {
		if environment[name] != "" {
			return false
		}
	}
	if terminalStatusScreenOrTmux(environment["TERM"]) {
		return false
	}
	if environment["WT_SESSION"] != "" || environment["ConEmuANSI"] == "ON" {
		return true
	}

	minimumVersions := map[string]string{"ghostty": "1.2.0", "iTerm.app": "3.6.6"}
	program := environment["TERM_PROGRAM"]
	version := environment["TERM_PROGRAM_VERSION"]
	minimum, ok := minimumVersions[program]

	return ok && terminalStatusNumberVersion(version) && semver.VersionCompare(version, minimum) >= 0
}

func terminalStatusProtocolSelected(protocols []string, protocol string) bool {
	return slices.Contains(protocols, protocol)
}

// terminalStatusScreenOrTmux is preg_match('{^(?:screen|tmux)(?:[-.]|$)}').
func terminalStatusScreenOrTmux(term string) bool {
	for _, name := range []string{"screen", "tmux"} {
		if term == name || strings.HasPrefix(term, name+"-") || strings.HasPrefix(term, name+".") {
			return true
		}
	}

	return false
}

// terminalStatusNumberVersion is preg_match('{^\d+\.\d+\.\d+$}').
func terminalStatusNumberVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return false
		}
	}

	return true
}
