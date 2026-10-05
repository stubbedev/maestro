// Ports src/Seld/JsonLint/ParsingException.php,
// src/Seld/JsonLint/DuplicateKeyException.php and
// src/Seld/JsonLint/InvalidEncodingException.php.

package jsonlint

import "github.com/stubbedev/maestro/internal/php"

// DetailsKind tells which keys a Details carries.
type DetailsKind uint8

// The shapes of ParsingException::getDetails().
const (
	// NoDetails is an empty array (lexical errors, BOM).
	NoDetails DetailsKind = iota
	// SyntaxDetails has text, token, line, loc and expected.
	SyntaxDetails
	// DuplicateKeyDetails has line and key.
	DuplicateKeyDetails
	// EncodingDetails has the Utf8Validator positions and key.
	EncodingDetails
)

// Details is ParsingException::getDetails().
type Details struct {
	Kind DetailsKind

	// SyntaxDetails.
	Text     string
	Token    any // a terminal name (string) or a symbol number (int64)
	Loc      Location
	Expected []string

	// SyntaxDetails and DuplicateKeyDetails.
	Line int

	// DuplicateKeyDetails and EncodingDetails.
	Key string

	// EncodingDetails.
	Encoding EncodingPosition
}

// EncodingPosition holds the positions InvalidEncodingException reports.
type EncodingPosition struct {
	CurrentOctet                          int
	ContinuationOctetNeeded               int
	OffsetInOctetsFromStringStart         int
	OffsetInCharactersFromStringStart     int
	CharacterStartPositionFromStringStart int
	Line                                  int
	OffsetInOctetsFromLineStart           int
	OffsetInCharactersFromLineStart       int
	CharacterStartPositionFromLineStart   int
	CurrentContinuationOctetMinimum       int
	CurrentContinuationOctetMaximum       int
}

// Array returns the details as the PHP array getDetails() returns, keys in
// PHP's order.
func (d *Details) Array() *php.Array {
	switch d.Kind {
	case SyntaxDetails:
		expected := php.NewArrayCap(len(d.Expected))
		for _, e := range d.Expected {
			expected.Append(e)
		}

		return php.ArrayOf("text", d.Text, "token", d.Token, "line", d.Line, "loc", d.Loc.Array(), "expected", expected)
	case DuplicateKeyDetails:
		return php.ArrayOf("line", d.Line, "key", d.Key)
	case EncodingDetails:
		e := &d.Encoding

		return php.ArrayOf(
			"current_octet", e.CurrentOctet,
			"continuation_octet_needed", e.ContinuationOctetNeeded,
			"offset_in_octets_from_string_start", e.OffsetInOctetsFromStringStart,
			"offset_in_characters_from_string_start", e.OffsetInCharactersFromStringStart,
			"character_start_position_from_string_start", e.CharacterStartPositionFromStringStart,
			"line", e.Line,
			"offset_in_octets_from_line_start", e.OffsetInOctetsFromLineStart,
			"offset_in_characters_from_line_start", e.OffsetInCharactersFromLineStart,
			"character_start_position_from_line_start", e.CharacterStartPositionFromLineStart,
			"current_continuation_octet_minimum", e.CurrentContinuationOctetMinimum,
			"current_continuation_octet_maximum", e.CurrentContinuationOctetMaximum,
			"key", d.Key,
		)
	}

	return php.NewArray()
}

// Array returns the location as a yylloc array, keys in PHP's order.
func (l Location) Array() *php.Array {
	if l.initial {
		return php.ArrayOf("first_line", l.FirstLine, "first_column", l.FirstColumn, "last_line", l.LastLine, "last_column", l.LastColumn)
	}

	return php.ArrayOf("first_line", l.FirstLine, "last_line", l.LastLine, "first_column", l.FirstColumn, "last_column", l.LastColumn)
}

// ParsingError is Seld\JsonLint\ParsingException.
type ParsingError struct {
	Message string
	Details Details
}

func (e *ParsingError) Error() string { return e.Message }

// DuplicateKeyError is Seld\JsonLint\DuplicateKeyException. It unwraps to
// its *ParsingError, as the PHP class extends ParsingException.
type DuplicateKeyError struct{ ParsingError }

// Key ports DuplicateKeyException::getKey.
func (e *DuplicateKeyError) Key() string { return e.Details.Key }

func (e *DuplicateKeyError) Unwrap() error { return &e.ParsingError }

// InvalidEncodingError is Seld\JsonLint\InvalidEncodingException. It
// unwraps to its *ParsingError, as the PHP class extends ParsingException.
type InvalidEncodingError struct{ ParsingError }

// Key ports InvalidEncodingException::getKey.
func (e *InvalidEncodingError) Key() string { return e.Details.Key }

func (e *InvalidEncodingError) Unwrap() error { return &e.ParsingError }

// PHPError is a PHP \Error the parser runs into (property names starting
// with a NUL byte, [] on a non-array in ALLOW_DUPLICATE_KEYS_TO_ARRAY
// mode). PHP's lint does not catch these either.
type PHPError struct{ Message string }

func (e *PHPError) Error() string { return e.Message }
