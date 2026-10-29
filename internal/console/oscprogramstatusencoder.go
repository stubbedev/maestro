// Ports src/Composer/Console/AbstractOscProgramStatusEncoder.php (Composer).

package console

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// ProgramStatusEncoder encodes a ProgramStatus for one terminal protocol.
type ProgramStatusEncoder interface {
	Command() string
	Encode(status *ProgramStatus) string
	// EncodeStart is the payload of a command start, nil for none (PHP null).
	EncodeStart() *string
}

// oscProgramStatusEncoder is the abstract encoder's shared message
// sanitizing.
type oscProgramStatusEncoder struct{}

// sanitizeMessage is sanitizeMessage(): ConsoleIO::sanitize(), the
// remaining control characters removed, the formatter's tags resolved, and
// the result cut to maxBytes bytes without a partial UTF-8 sequence at the
// end. A negative maxBytes means no limit (PHP null).
func (oscProgramStatusEncoder) sanitizeMessage(message string, maxBytes int) string {
	message = strings.Map(func(r rune) rune {
		if r <= 0x1f || (r >= 0x7f && r <= 0x9f) {
			return -1
		}

		return r
	}, Sanitize(message, false))
	message = NewOutputFormatter(false).Format(message)

	if maxBytes >= 0 && len(message) > maxBytes {
		message = truncateUTF8(message, maxBytes)
	}

	return php.Trim(message)
}

// truncateUTF8 is substr($message, 0, $maxBytes) followed by
// preg_replace('{[\xc2-\xdf]$|[\xe0-\xef][\x80-\xbf]{0,1}$|[\xf0-\xf4][\x80-\xbf]{0,2}$}', ”):
// a UTF-8 sequence the cut left incomplete is dropped.
func truncateUTF8(message string, maxBytes int) string {
	message = message[:maxBytes]
	i := len(message)
	for i > 0 && message[i-1] >= 0x80 && message[i-1] <= 0xbf {
		i--
	}
	if i == 0 {
		return message
	}

	var need int
	switch c := message[i-1]; {
	case c >= 0xc2 && c <= 0xdf:
		need = 2
	case c >= 0xe0 && c <= 0xef:
		need = 3
	case c >= 0xf0 && c <= 0xf4:
		need = 4
	default:
		return message
	}
	if len(message)-i+1 < need {
		return message[:i-1]
	}

	return message
}
