// Ports src/Style/StyleInterface.php and OutputStyle.php (symfony/console).

package console

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// OutputStyle decorates an Output; it is the base of SymfonyStyle.
type OutputStyle struct {
	output Output
}

// NewOutputStyle mirrors new OutputStyle($output).
func NewOutputStyle(out Output) *OutputStyle { return &OutputStyle{output: out} }

// NewLine writes count line breaks.
func (s *OutputStyle) NewLine(count int) {
	s.output.Write(strings.Repeat(php.EOL, max(0, count)), false, OutputNormal)
}

// Write implements Output.
func (s *OutputStyle) Write(message string, newline bool, options int) {
	s.output.Write(message, newline, options)
}

// Writeln implements Output.
func (s *OutputStyle) Writeln(message string) { s.output.Writeln(message) }

// SetVerbosity implements Output.
func (s *OutputStyle) SetVerbosity(level int) { s.output.SetVerbosity(level) }

// Verbosity implements Output.
func (s *OutputStyle) Verbosity() int { return s.output.Verbosity() }

// SetDecorated implements Output.
func (s *OutputStyle) SetDecorated(decorated bool) { s.output.SetDecorated(decorated) }

// IsDecorated implements Output.
func (s *OutputStyle) IsDecorated() bool { return s.output.IsDecorated() }

// SetFormatter implements Output.
func (s *OutputStyle) SetFormatter(formatter Formatter) { s.output.SetFormatter(formatter) }

// Formatter implements Output.
func (s *OutputStyle) Formatter() Formatter { return s.output.Formatter() }

// IsQuiet implements Output.
func (s *OutputStyle) IsQuiet() bool { return s.output.IsQuiet() }

// IsVerbose implements Output.
func (s *OutputStyle) IsVerbose() bool { return s.output.IsVerbose() }

// IsVeryVerbose implements Output.
func (s *OutputStyle) IsVeryVerbose() bool { return s.output.IsVeryVerbose() }

// IsDebug implements Output.
func (s *OutputStyle) IsDebug() bool { return s.output.IsDebug() }

// errorOutput returns the error output of a ConsoleOutputInterface, else
// the output itself.
func (s *OutputStyle) errorOutput() Output { return ErrorOutputOf(s.output) }
