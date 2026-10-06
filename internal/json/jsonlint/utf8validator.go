// Ports src/Seld/JsonLint/Utf8Validator.php.

package jsonlint

import (
	"strconv"
	"unicode/utf8"
)

// Lowest and highest values a continuation octet (10xxxxxx) may take.
const (
	continuationOctetMinimum = 128
	continuationOctetMaximum = 191
)

// ValidateUTF8 ports Utf8Validator::validate: nil when input is
// well-formed UTF-8 (RFC 3629), else an *InvalidEncodingError locating the
// first fault.
func ValidateUTF8(input string) error {
	// Fast-path (mb_check_encoding).
	if utf8.ValidString(input) {
		return nil
	}

	pos := EncodingPosition{
		OffsetInCharactersFromStringStart:   -1,
		Line:                                1,
		OffsetInOctetsFromLineStart:         -1,
		OffsetInCharactersFromLineStart:     -1,
		CharacterStartPositionFromLineStart: -1,
		CurrentContinuationOctetMinimum:     continuationOctetMinimum,
		CurrentContinuationOctetMaximum:     continuationOctetMaximum,
	}
	messageForContinuationOctetAboveMaximum := ""

	for i := range len(input) {
		octet := int(input[i])
		pos.CurrentOctet = octet
		pos.OffsetInOctetsFromStringStart = i
		pos.OffsetInOctetsFromLineStart++

		// The octet values C0, C1, F5 to FF never appear.
		if octet == 192 || octet == 193 || octet == 245 || octet == 255 {
			return encodingError(" which is one of the four forbidden values (C0, C1, F5, FF).", strconv.Itoa(octet), pos, false)
		}

		if pos.ContinuationOctetNeeded > 0 {
			if octet < pos.CurrentContinuationOctetMinimum || octet > pos.CurrentContinuationOctetMaximum {
				reason := " which is not a continuation octet."
				if messageForContinuationOctetAboveMaximum != "" && octet > pos.CurrentContinuationOctetMaximum {
					reason = messageForContinuationOctetAboveMaximum
				}

				return encodingError(reason, strconv.Itoa(octet), pos, false)
			}
			pos.ContinuationOctetNeeded--
			pos.CurrentContinuationOctetMinimum = continuationOctetMinimum
			pos.CurrentContinuationOctetMaximum = continuationOctetMaximum
			messageForContinuationOctetAboveMaximum = ""

			continue
		}

		pos.OffsetInCharactersFromStringStart++
		pos.OffsetInCharactersFromLineStart++
		pos.CharacterStartPositionFromStringStart = pos.OffsetInOctetsFromStringStart
		pos.CharacterStartPositionFromLineStart = pos.OffsetInOctetsFromLineStart

		switch {
		case octet < 128: // 0xxxxxxx ASCII
			if octet == '\n' {
				pos.Line++
				pos.OffsetInOctetsFromLineStart = -1
				pos.OffsetInCharactersFromLineStart = -1
			}
		case octet < 192:
			return encodingError(" which is a continuation octet.", strconv.Itoa(octet), pos, false)
		case octet < 224: // 110xxxxx 10xxxxxx
			pos.ContinuationOctetNeeded = 1
		case octet < 240: // 1110xxxx 10xxxxxx 10xxxxxx
			pos.ContinuationOctetNeeded = 2
			if octet == 224 {
				pos.CurrentContinuationOctetMinimum = 160
				pos.CurrentContinuationOctetMaximum = 191
			}
			if octet == 237 {
				pos.CurrentContinuationOctetMinimum = 128
				pos.CurrentContinuationOctetMaximum = 159
				messageForContinuationOctetAboveMaximum = " which is into the forbidden range of surrogate pairs."
			}
		case octet < 245: // 11110xxx 10xxxxxx 10xxxxxx 10xxxxxx
			pos.ContinuationOctetNeeded = 3
			if octet == 240 {
				pos.CurrentContinuationOctetMinimum = 144
				pos.CurrentContinuationOctetMaximum = 191
			}
			if octet == 244 {
				pos.CurrentContinuationOctetMinimum = 128
				pos.CurrentContinuationOctetMaximum = 143
			}
		default:
			return encodingError(" which is invalid.", strconv.Itoa(octet), pos, false)
		}
	}
	if pos.ContinuationOctetNeeded > 0 {
		return encodingError("", "0", pos, true)
	}

	// The fast-path flagged this input as invalid UTF-8, yet the manual scan
	// found no fault.
	return &InvalidEncodingError{ParsingError{
		Message: "Fast-path detected an error that the manual scan could not find. Please report it at https://github.com/Seldaek/jsonlint/issues.",
		Details: Details{Kind: EncodingDetails, Key: "0", Encoding: pos},
	}}
}

// encodingError ports Utf8Validator::createException.
func encodingError(reason, key string, pos EncodingPosition, endOfInput bool) error {
	var middle string
	if endOfInput {
		middle = "; at octet " + strconv.Itoa(pos.OffsetInOctetsFromLineStart+1) +
			", part of the character " + strconv.Itoa(pos.OffsetInCharactersFromLineStart+1) +
			", end of string was found instead of a continuation octet."
	} else {
		middle = "; the octet " + strconv.Itoa(pos.OffsetInOctetsFromLineStart+1) +
			", part of the character " + strconv.Itoa(pos.OffsetInCharactersFromLineStart+1) +
			", has value " + strconv.Itoa(pos.CurrentOctet) + reason
	}

	message := "Non-UTF8 character found on line " + strconv.Itoa(pos.Line) + middle +
		" This character starts at octet " + strconv.Itoa(pos.CharacterStartPositionFromLineStart+1) +
		" of the current line. (Sequential positions without line splitting: This is at character " +
		strconv.Itoa(pos.OffsetInCharactersFromStringStart+1) + " and octet " +
		strconv.Itoa(pos.OffsetInOctetsFromStringStart+1) + ". This character starts at octet " +
		strconv.Itoa(pos.CharacterStartPositionFromStringStart+1) + ".)"

	return &InvalidEncodingError{ParsingError{Message: message, Details: Details{Kind: EncodingDetails, Key: key, Encoding: pos}}}
}
