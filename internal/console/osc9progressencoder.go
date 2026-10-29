// Ports src/Composer/Console/Osc9ProgressEncoder.php (Composer).

package console

import "strconv"

// Osc9ProgressEncoder encodes ConEmu-compatible OSC 9 progress reports
// (https://conemu.github.io/en/AnsiEscapeCodes.html#ConEmu_specific_OSC). It
// overrides everything its abstract parent in Composer has, so it embeds
// nothing.
type Osc9ProgressEncoder struct{}

// Command implements ProgramStatusEncoder.
func (*Osc9ProgressEncoder) Command() string { return "9;4" }

// EncodeStart implements ProgramStatusEncoder: progress and prompts activate
// the indicator instead of every command starting a spinner.
func (*Osc9ProgressEncoder) EncodeStart() *string { return nil }

// Encode implements ProgramStatusEncoder.
func (*Osc9ProgressEncoder) Encode(status *ProgramStatus) string {
	progress := status.Progress()
	switch status.State() {
	case ProgramStatusStateWorking:
		if progress == nil {
			return "3"
		}

		return "1;" + strconv.Itoa(*progress)
	case ProgramStatusStateBlocked:
		if progress == nil {
			return "4"
		}

		return "4;" + strconv.Itoa(*progress)
	case ProgramStatusStateError:
		return "2"
	default:
		// Completion and cancellation remove the progress indicator.
		return "0"
	}
}
