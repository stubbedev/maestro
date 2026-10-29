// Ports src/Composer/Console/ProgramStatus.php (Composer).

package console

import (
	"maps"
	"slices"
)

// The program status states (ProgramStatus::STATE_*).
const (
	ProgramStatusStateIdle    = "idle"
	ProgramStatusStateWorking = "working"
	ProgramStatusStateDone    = "done"
	ProgramStatusStateBlocked = "blocked"
	ProgramStatusStateError   = "error"
	ProgramStatusStateClear   = "clear"
)

var programStatusStates = []string{
	ProgramStatusStateIdle,
	ProgramStatusStateWorking,
	ProgramStatusStateDone,
	ProgramStatusStateBlocked,
	ProgramStatusStateError,
	ProgramStatusStateClear,
}

// ProgramStatus is the current program state, independent of its terminal
// representation. Its methods that change it return a new value, as PHP's
// clone does.
type ProgramStatus struct {
	state           string
	message         *string
	protocolOptions map[string]map[string]any
	progress        *int
}

// NewProgramStatus mirrors new ProgramStatus($state, $message); an unknown
// state is an InvalidArgument error (PHP throws an InvalidArgumentException).
func NewProgramStatus(state string, message *string) (*ProgramStatus, error) {
	if !slices.Contains(programStatusStates, state) {
		return nil, newError(KindSPLInvalidArgument, "Unknown program status state: %s", state)
	}

	return &ProgramStatus{state: state, message: message}, nil
}

func newProgramStatus(state string, message *string) *ProgramStatus {
	status, _ := NewProgramStatus(state, message)

	return status
}

// ProgramStatusIdle is ProgramStatus::idle().
func ProgramStatusIdle(message *string) *ProgramStatus {
	return newProgramStatus(ProgramStatusStateIdle, message)
}

// ProgramStatusWorking is ProgramStatus::working().
func ProgramStatusWorking(message *string) *ProgramStatus {
	return newProgramStatus(ProgramStatusStateWorking, message)
}

// ProgramStatusDone is ProgramStatus::done().
func ProgramStatusDone(message *string) *ProgramStatus {
	return newProgramStatus(ProgramStatusStateDone, message)
}

// ProgramStatusBlocked is ProgramStatus::blocked().
func ProgramStatusBlocked(message *string) *ProgramStatus {
	return newProgramStatus(ProgramStatusStateBlocked, message)
}

// ProgramStatusError is ProgramStatus::error().
func ProgramStatusError(message *string) *ProgramStatus {
	return newProgramStatus(ProgramStatusStateError, message)
}

// ProgramStatusClear is ProgramStatus::clear().
func ProgramStatusClear(message *string) *ProgramStatus {
	return newProgramStatus(ProgramStatusStateClear, message)
}

// ProgramStatusFromExitCode is ProgramStatus::fromExitCode(): the shell exit
// codes of SIGHUP, SIGINT and SIGTERM represent cancellation.
func ProgramStatusFromExitCode(exitCode int) *ProgramStatus {
	switch exitCode {
	case 129, 130, 143:
		return ProgramStatusIdle(nil)
	case 0:
		return ProgramStatusDone(nil)
	}

	return ProgramStatusError(nil)
}

// WithProgress is withProgress(): a progress outside 0-100, or on a state
// that has no progress, is dropped (PHP null).
func (s *ProgramStatus) WithProgress(progress *int) *ProgramStatus {
	if progress != nil && (*progress < 0 || *progress > 100 ||
		(s.state != ProgramStatusStateWorking && s.state != ProgramStatusStateBlocked)) {
		progress = nil
	}

	status := *s
	status.progress = progress

	return &status
}

// WithProtocolOption is withProtocolOption(): the returned status has the
// option set and shares no map with s.
func (s *ProgramStatus) WithProtocolOption(protocol, name string, value any) *ProgramStatus {
	status := *s
	status.protocolOptions = make(map[string]map[string]any, len(s.protocolOptions)+1)
	maps.Copy(status.protocolOptions, s.protocolOptions)
	options := make(map[string]any, len(s.protocolOptions[protocol])+1)
	maps.Copy(options, s.protocolOptions[protocol])
	options[name] = value
	status.protocolOptions[protocol] = options

	return &status
}

// GetProtocolOption is getProtocolOption(); nil when it is not set (PHP
// null).
func (s *ProgramStatus) GetProtocolOption(protocol, name string) any {
	return s.protocolOptions[protocol][name]
}

// State is getState().
func (s *ProgramStatus) State() string { return s.state }

// Message is getMessage().
func (s *ProgramStatus) Message() *string { return s.message }

// Progress is getProgress().
func (s *ProgramStatus) Progress() *int { return s.progress }
