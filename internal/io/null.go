// Ports src/Composer/IO/NullIO.php (Composer).

package io

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// NullIO discards output and answers every question with its default.
type NullIO struct {
	BaseIO
}

// NewNullIO returns a NullIO.
func NewNullIO() *NullIO {
	n := &NullIO{}
	n.init(n)

	return n
}

// IsInteractive implements IO.
func (*NullIO) IsInteractive() bool { return false }

// IsVerbose implements IO.
func (*NullIO) IsVerbose() bool { return false }

// IsVeryVerbose implements IO.
func (*NullIO) IsVeryVerbose() bool { return false }

// IsDebug implements IO.
func (*NullIO) IsDebug() bool { return false }

// IsDecorated implements IO.
func (*NullIO) IsDecorated() bool { return false }

// Write implements IO.
func (*NullIO) Write(string, bool, Verbosity) {}

// WriteMessages implements IO.
func (*NullIO) WriteMessages([]string, bool, Verbosity) {}

// WriteError implements IO.
func (*NullIO) WriteError(string, bool, Verbosity) {}

// WriteErrorMessages implements IO.
func (*NullIO) WriteErrorMessages([]string, bool, Verbosity) {}

// Overwrite implements IO.
func (*NullIO) Overwrite(string, bool, int, Verbosity) {}

// OverwriteError implements IO.
func (*NullIO) OverwriteError(string, bool, int, Verbosity) {}

// Ask implements IO.
func (*NullIO) Ask(_ string, def any) (any, error) { return def, nil }

// AskConfirmation implements IO.
func (*NullIO) AskConfirmation(_ string, def bool) (bool, error) { return def, nil }

// AskAndValidate implements IO.
func (*NullIO) AskAndValidate(_ string, _ console.Validator, _ int, def any) (any, error) {
	return def, nil
}

// AskAndHideAnswer implements IO.
func (*NullIO) AskAndHideAnswer(string) (any, error) { return nil, nil }

// Select implements IO.
func (*NullIO) Select(_ string, _ *php.Array, def any, _ int, _ string, _ bool) (any, error) {
	return def, nil
}

var (
	_ IO = (*NullIO)(nil)
	_ IO = (*ConsoleIO)(nil)
	_ IO = (*BufferIO)(nil)
)
