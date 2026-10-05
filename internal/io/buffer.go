// Ports src/Composer/IO/BufferIO.php (Composer).

package io

import (
	"bytes"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// BufferIO is a ConsoleIO writing to memory, used by tests and by code that
// captures output.
type BufferIO struct {
	*ConsoleIO
	buffer *bytes.Buffer
}

// NewBufferIO mirrors new BufferIO($input, $verbosity, $formatter): input is
// parsed as a StringInput and the IO is not interactive. A zero verbosity
// is VERBOSITY_NORMAL; a nil formatter means an undecorated default one.
func NewBufferIO(input string, verbosity int, formatter console.Formatter) (*BufferIO, error) {
	in, err := console.NewStringInput(input)
	if err != nil {
		return nil, err
	}
	in.SetInteractive(false)

	if verbosity == 0 {
		verbosity = console.VerbosityNormal
	}
	decorated := formatter != nil && formatter.IsDecorated()
	buffer := &bytes.Buffer{}
	output := console.NewStreamOutput(buffer, verbosity, &decorated, formatter)

	b := &BufferIO{
		ConsoleIO: NewConsoleIO(in, output, console.NewHelperSet(console.NewQuestionHelper())),
		buffer:    buffer,
	}

	return b, nil
}

// Output returns everything written so far, with overwritten text removed
// (getOutput).
func (b *BufferIO) Output() string {
	return normalizeBackspaces(b.buffer.String())
}

// normalizeBackspaces ports
//
//	Preg::replaceCallback("{(?<=^|\n|\x08)(.+?)(\x08+)}", ...)
//
// A match starts where the previous byte (in the original string) is "\n"
// or "\x08", or at the start; it is the shortest run of at least one byte
// without "\n" that is followed by backspaces, plus all those backspaces.
// The run is dropped when its tag-stripped length equals the number of
// backspaces, otherwise it is kept right-trimmed and followed by "\n".
func normalizeBackspaces(s string) string {
	if strings.IndexByte(s, '\x08') < 0 {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	for p := 0; p < len(s); {
		if (p == 0 || s[p-1] == '\n' || s[p-1] == '\x08') && s[p] != '\n' {
			if j := strings.IndexAny(s[p+1:], "\n\x08"); j >= 0 && s[p+1+j] == '\x08' {
				j += p + 1
				k := j
				for k < len(s) && s[k] == '\x08' {
					k++
				}
				pre := console.StripTags(s[p:j])
				if len(pre) != k-j {
					b.WriteString(php.Rtrim(s[p:j]))
					b.WriteByte('\n')
				}
				p = k

				continue
			}
		}
		b.WriteByte(s[p])
		p++
	}

	return b.String()
}

// SetUserInputs feeds inputs, one per line, to the questions asked next and
// makes the IO interactive.
func (b *BufferIO) SetUserInputs(inputs []string) {
	var sb strings.Builder
	for _, in := range inputs {
		sb.WriteString(in)
		sb.WriteByte('\n')
	}
	b.input.SetStream(strings.NewReader(sb.String()))
	b.input.SetInteractive(true)
}
