// Ports src/Composer/Console/Osc7501ProgramStatusEncoder.php (Composer).

package console

import (
	"encoding/base64"
	"slices"
	"strconv"
)

// Osc7501ProgramStatusEncoder encodes a root record for the OSC 7501 Program
// Status Protocol (https://www.superlogical.com/rex/docs/build/program-status).
type Osc7501ProgramStatusEncoder struct {
	oscProgramStatusEncoder
}

// Osc7501Protocol is Osc7501ProgramStatusEncoder::PROTOCOL.
const Osc7501Protocol = "osc7501"

// The kinds a blocked status can carry (Osc7501ProgramStatusEncoder::KIND_*).
const (
	Osc7501KindPermission = "permission"
	Osc7501KindQuestion   = "question"
	Osc7501KindAuth       = "auth"
)

var osc7501Kinds = []string{Osc7501KindPermission, Osc7501KindQuestion, Osc7501KindAuth}

// Command implements ProgramStatusEncoder.
func (*Osc7501ProgramStatusEncoder) Command() string { return "7501" }

// EncodeStart implements ProgramStatusEncoder.
func (e *Osc7501ProgramStatusEncoder) EncodeStart() *string {
	payload := e.Encode(ProgramStatusWorking(nil))

	return &payload
}

// Encode implements ProgramStatusEncoder.
func (e *Osc7501ProgramStatusEncoder) Encode(status *ProgramStatus) string {
	payload := "state=" + status.State() + ":app=composer"
	if progress := status.Progress(); progress != nil {
		payload += ":progress=" + strconv.Itoa(*progress)
	}
	if kind := e.kind(status); kind != "" {
		payload += ":kind=" + kind
	}
	if message := status.Message(); message != nil {
		sanitized := e.sanitizeMessage(*message, 2048)
		payload += ":msg=" + base64.StdEncoding.EncodeToString([]byte(sanitized))
	}

	return payload
}

// kind is getKind(): a kind only on a blocked status, and only one of the
// valid kinds.
func (*Osc7501ProgramStatusEncoder) kind(status *ProgramStatus) string {
	value := status.GetProtocolOption(Osc7501Protocol, "kind")
	kind, ok := value.(string)
	if value == nil {
		return ""
	}
	if status.State() != ProgramStatusStateBlocked || !ok || !isOsc7501Kind(kind) {
		return ""
	}

	return kind
}

func isOsc7501Kind(kind string) bool {
	return slices.Contains(osc7501Kinds, kind)
}
