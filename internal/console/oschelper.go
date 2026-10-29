// Ports src/Composer/Console/OscHelper.php (Composer).

package console

import "io"

// OscHelper writes Operating System Commands without formatting or adding
// a newline.
type OscHelper struct {
	HelperBase
}

// NewOscHelper returns the "osc" helper.
func NewOscHelper() *OscHelper { return &OscHelper{} }

// Name implements Helper.
func (*OscHelper) Name() string { return "osc" }

// Write writes command and payload as one OSC sequence, raw and without a
// newline. A control character in either is an InvalidArgument error (PHP
// throws an InvalidArgumentException).
func (h *OscHelper) Write(output Output, command, payload string) error {
	if hasOSCControlCharacter(command) || hasOSCControlCharacter(payload) {
		return newError(KindSPLInvalidArgument, "OSC commands and payloads must not contain control characters.")
	}

	if !h.SupportsOutput(output) {
		return nil
	}

	output.Write("\033]"+command+";"+payload+"\033\\", false, OutputRaw)

	return nil
}

// SupportsOutput is supportsOutput().
func (*OscHelper) SupportsOutput(output Output) bool {
	if !output.IsDecorated() || output.Verbosity() <= VerbosityQuiet {
		return false
	}

	// Forced ANSI decoration does not make a redirected stream a terminal.
	if stream, ok := output.(interface{ Stream() io.Writer }); ok {
		return IsTTY(stream.Stream())
	}

	return true
}

// hasOSCControlCharacter is preg_match('{[\x00-\x1f\x7f-\x9f]}u').
func hasOSCControlCharacter(s string) bool {
	for _, r := range s {
		if r <= 0x1f || (r >= 0x7f && r <= 0x9f) {
			return true
		}
	}

	return false
}
