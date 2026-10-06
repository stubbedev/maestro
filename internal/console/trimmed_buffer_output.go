// Ports src/Output/TrimmedBufferOutput.php (symfony/console).

package console

import "github.com/stubbedev/maestro/internal/php"

// TrimmedBufferOutput keeps only the last maxLength bytes written.
type TrimmedBufferOutput struct {
	BaseOutput
	maxLength int
	buffer    string
}

// NewTrimmedBufferOutput mirrors new TrimmedBufferOutput($maxLength,
// $verbosity, $decorated, $formatter).
func NewTrimmedBufferOutput(maxLength, verbosity int, decorated bool, formatter Formatter) (*TrimmedBufferOutput, error) {
	if maxLength <= 0 {
		return nil, newError(KindInvalidArgument, "TrimmedBufferOutput.php", 30,
			`"%s()" expects a strictly positive maxLength. Got %d.`, `Symfony\Component\Console\Output\TrimmedBufferOutput::__construct`, maxLength)
	}
	o := &TrimmedBufferOutput{maxLength: maxLength}
	o.init(verbosity, decorated, formatter, o.write)

	return o, nil
}

// Fetch returns and empties the buffer.
func (o *TrimmedBufferOutput) Fetch() string {
	content := o.buffer
	o.buffer = ""

	return content
}

func (o *TrimmedBufferOutput) write(message string, newline bool) {
	o.buffer += message
	if newline {
		o.buffer += php.EOL
	}
	if len(o.buffer) > o.maxLength {
		o.buffer = o.buffer[len(o.buffer)-o.maxLength:]
	}
}
