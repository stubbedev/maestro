// Ports src/Helper/TableStyle.php and the style registry of Table.php
// (symfony/console).

package console

import (
	"sync"

	"github.com/stubbedev/maestro/internal/php"
)

// TableStyle defines the borders, crossings, formats and padding of a
// table.
type TableStyle struct {
	paddingChar                 string
	horizontalOutsideBorderChar string
	horizontalInsideBorderChar  string
	verticalOutsideBorderChar   string
	verticalInsideBorderChar    string
	crossingChar                string
	crossingTopRightChar        string
	crossingTopMidChar          string
	crossingTopLeftChar         string
	crossingMidRightChar        string
	crossingBottomRightChar     string
	crossingBottomMidChar       string
	crossingBottomLeftChar      string
	crossingMidLeftChar         string
	crossingTopLeftBottomChar   string
	crossingTopMidBottomChar    string
	crossingTopRightBottomChar  string
	headerTitleFormat           string
	footerTitleFormat           string
	cellHeaderFormat            string
	cellRowFormat               string
	cellRowContentFormat        string
	borderFormat                string
	padType                     int
}

// NewTableStyle mirrors new TableStyle().
func NewTableStyle() *TableStyle {
	return &TableStyle{
		paddingChar:                 " ",
		horizontalOutsideBorderChar: "-",
		horizontalInsideBorderChar:  "-",
		verticalOutsideBorderChar:   "|",
		verticalInsideBorderChar:    "|",
		crossingChar:                "+",
		crossingTopRightChar:        "+",
		crossingTopMidChar:          "+",
		crossingTopLeftChar:         "+",
		crossingMidRightChar:        "+",
		crossingBottomRightChar:     "+",
		crossingBottomMidChar:       "+",
		crossingBottomLeftChar:      "+",
		crossingMidLeftChar:         "+",
		crossingTopLeftBottomChar:   "+",
		crossingTopMidBottomChar:    "+",
		crossingTopRightBottomChar:  "+",
		headerTitleFormat:           "<fg=black;bg=white;options=bold> %s </>",
		footerTitleFormat:           "<fg=black;bg=white;options=bold> %s </>",
		cellHeaderFormat:            "<info>%s</info>",
		cellRowFormat:               "%s",
		cellRowContentFormat:        " %s ",
		borderFormat:                "%s",
		padType:                     php.StrPadRight,
	}
}

// Clone returns a copy of the style (PHP's clone).
func (s *TableStyle) Clone() *TableStyle {
	c := *s

	return &c
}

// SetPaddingChar sets the padding character; it must not be empty (nor
// "0", which PHP also treats as empty).
func (s *TableStyle) SetPaddingChar(paddingChar string) error {
	if !php.ToBool(paddingChar) {
		return newError(KindLogic, "TableStyle.php", 59, "The padding char must not be empty.")
	}
	s.paddingChar = paddingChar

	return nil
}

// PaddingChar returns the padding character.
func (s *TableStyle) PaddingChar() string { return s.paddingChar }

// SetHorizontalBorderChars sets the horizontal border characters; the
// inside one defaults to the outside one.
func (s *TableStyle) SetHorizontalBorderChars(outside string, inside ...string) *TableStyle {
	s.horizontalOutsideBorderChar = outside
	s.horizontalInsideBorderChar = optional(inside, outside)

	return s
}

// SetVerticalBorderChars sets the vertical border characters; the inside
// one defaults to the outside one.
func (s *TableStyle) SetVerticalBorderChars(outside string, inside ...string) *TableStyle {
	s.verticalOutsideBorderChar = outside
	s.verticalInsideBorderChar = optional(inside, outside)

	return s
}

func optional(v []string, def string) string {
	if len(v) > 0 {
		return v[0]
	}

	return def
}

// BorderChars returns [horizontal outside, vertical outside, horizontal
// inside, vertical inside].
func (s *TableStyle) BorderChars() [4]string {
	return [4]string{
		s.horizontalOutsideBorderChar,
		s.verticalOutsideBorderChar,
		s.horizontalInsideBorderChar,
		s.verticalInsideBorderChar,
	}
}

// SetCrossingChars sets the crossing characters. The optional extra
// characters are topLeftBottom (default midLeft), topMidBottom (default
// cross) and topRightBottom (default midRight).
func (s *TableStyle) SetCrossingChars(cross, topLeft, topMid, topRight, midRight, bottomRight, bottomMid, bottomLeft, midLeft string, topBottom ...string) *TableStyle {
	s.crossingChar = cross
	s.crossingTopLeftChar = topLeft
	s.crossingTopMidChar = topMid
	s.crossingTopRightChar = topRight
	s.crossingMidRightChar = midRight
	s.crossingBottomRightChar = bottomRight
	s.crossingBottomMidChar = bottomMid
	s.crossingBottomLeftChar = bottomLeft
	s.crossingMidLeftChar = midLeft
	s.crossingTopLeftBottomChar = midLeft
	s.crossingTopMidBottomChar = cross
	s.crossingTopRightBottomChar = midRight
	if len(topBottom) > 0 {
		s.crossingTopLeftBottomChar = topBottom[0]
	}
	if len(topBottom) > 1 {
		s.crossingTopMidBottomChar = topBottom[1]
	}
	if len(topBottom) > 2 {
		s.crossingTopRightBottomChar = topBottom[2]
	}

	return s
}

// SetDefaultCrossingChar sets every crossing character to char.
func (s *TableStyle) SetDefaultCrossingChar(char string) *TableStyle {
	return s.SetCrossingChars(char, char, char, char, char, char, char, char, char)
}

// CrossingChar returns the crossing character.
func (s *TableStyle) CrossingChar() string { return s.crossingChar }

// CrossingChars returns the 12 crossing characters in getCrossingChars()
// order.
func (s *TableStyle) CrossingChars() [12]string {
	return [12]string{
		s.crossingChar,
		s.crossingTopLeftChar,
		s.crossingTopMidChar,
		s.crossingTopRightChar,
		s.crossingMidRightChar,
		s.crossingBottomRightChar,
		s.crossingBottomMidChar,
		s.crossingBottomLeftChar,
		s.crossingMidLeftChar,
		s.crossingTopLeftBottomChar,
		s.crossingTopMidBottomChar,
		s.crossingTopRightBottomChar,
	}
}

// SetCellHeaderFormat sets the header cell format.
func (s *TableStyle) SetCellHeaderFormat(format string) *TableStyle {
	s.cellHeaderFormat = format

	return s
}

// CellHeaderFormat returns the header cell format.
func (s *TableStyle) CellHeaderFormat() string { return s.cellHeaderFormat }

// SetCellRowFormat sets the row cell format.
func (s *TableStyle) SetCellRowFormat(format string) *TableStyle {
	s.cellRowFormat = format

	return s
}

// CellRowFormat returns the row cell format.
func (s *TableStyle) CellRowFormat() string { return s.cellRowFormat }

// SetCellRowContentFormat sets the row cell content format.
func (s *TableStyle) SetCellRowContentFormat(format string) *TableStyle {
	s.cellRowContentFormat = format

	return s
}

// CellRowContentFormat returns the row cell content format.
func (s *TableStyle) CellRowContentFormat() string { return s.cellRowContentFormat }

// SetBorderFormat sets the table border format.
func (s *TableStyle) SetBorderFormat(format string) *TableStyle {
	s.borderFormat = format

	return s
}

// BorderFormat returns the table border format.
func (s *TableStyle) BorderFormat() string { return s.borderFormat }

// SetPadType sets the cell padding type (php.StrPadLeft, StrPadRight or
// StrPadBoth).
func (s *TableStyle) SetPadType(padType int) error {
	if padType != php.StrPadLeft && padType != php.StrPadRight && padType != php.StrPadBoth {
		return newError(KindInvalidArgument, "TableStyle.php", 329, "Invalid padding type. Expected one of (STR_PAD_LEFT, STR_PAD_RIGHT, STR_PAD_BOTH).")
	}
	s.padType = padType

	return nil
}

// PadType returns the cell padding type.
func (s *TableStyle) PadType() int { return s.padType }

// HeaderTitleFormat returns the header title format.
func (s *TableStyle) HeaderTitleFormat() string { return s.headerTitleFormat }

// SetHeaderTitleFormat sets the header title format.
func (s *TableStyle) SetHeaderTitleFormat(format string) *TableStyle {
	s.headerTitleFormat = format

	return s
}

// FooterTitleFormat returns the footer title format.
func (s *TableStyle) FooterTitleFormat() string { return s.footerTitleFormat }

// SetFooterTitleFormat sets the footer title format.
func (s *TableStyle) SetFooterTitleFormat(format string) *TableStyle {
	s.footerTitleFormat = format

	return s
}

// tableStyles is Table::$styles, the named style definitions.
var tableStyles struct {
	once   sync.Once
	mu     sync.RWMutex
	styles map[string]*TableStyle
}

func initTableStyles() {
	borderless := NewTableStyle().
		SetHorizontalBorderChars("=").
		SetVerticalBorderChars(" ").
		SetDefaultCrossingChar(" ")

	compact := NewTableStyle().
		SetHorizontalBorderChars("").
		SetVerticalBorderChars("").
		SetDefaultCrossingChar("").
		SetCellRowContentFormat("%s ")

	styleGuide := NewTableStyle().
		SetHorizontalBorderChars("-").
		SetVerticalBorderChars(" ").
		SetDefaultCrossingChar(" ").
		SetCellHeaderFormat("%s")

	box := NewTableStyle().
		SetHorizontalBorderChars("─").
		SetVerticalBorderChars("│").
		SetCrossingChars("┼", "┌", "┬", "┐", "┤", "┘", "┴", "└", "├")

	boxDouble := NewTableStyle().
		SetHorizontalBorderChars("═", "─").
		SetVerticalBorderChars("║", "│").
		SetCrossingChars("┼", "╔", "╤", "╗", "╢", "╝", "╧", "╚", "╟", "╠", "╪", "╣")

	tableStyles.styles = map[string]*TableStyle{
		"default":             NewTableStyle(),
		"borderless":          borderless,
		"compact":             compact,
		"symfony-style-guide": styleGuide,
		"box":                 box,
		"box-double":          boxDouble,
	}
}

// SetTableStyleDefinition registers a named style (Table::setStyleDefinition).
func SetTableStyleDefinition(name string, style *TableStyle) {
	tableStyles.once.Do(initTableStyles)
	tableStyles.mu.Lock()
	tableStyles.styles[name] = style
	tableStyles.mu.Unlock()
}

// TableStyleDefinition returns a named style (Table::getStyleDefinition).
func TableStyleDefinition(name string) (*TableStyle, error) {
	tableStyles.once.Do(initTableStyles)
	tableStyles.mu.RLock()
	style, ok := tableStyles.styles[name]
	tableStyles.mu.RUnlock()
	if !ok {
		return nil, newError(KindInvalidArgument, "Table.php", 133, `Style "%s" is not defined.`, name)
	}

	return style, nil
}
