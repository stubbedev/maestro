// Ports src/Helper/TableCell.php, TableCellStyle.php and TableSeparator.php
// (symfony/console).

package console

import (
	"fmt"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// TableCellOptions are the TableCell options; a zero Rowspan or Colspan
// means 1.
type TableCellOptions struct {
	Rowspan int
	Colspan int
	Style   *TableCellStyle
}

// TableCell is a table cell spanning rows and/or columns, optionally styled.
type TableCell struct {
	value   string
	rowspan int
	colspan int
	style   *TableCellStyle
}

// NewTableCell mirrors new TableCell($value, $options).
func NewTableCell(value string, options TableCellOptions) *TableCell {
	c := &TableCell{value: value, rowspan: options.Rowspan, colspan: options.Colspan, style: options.Style}
	if c.rowspan == 0 {
		c.rowspan = 1
	}
	if c.colspan == 0 {
		c.colspan = 1
	}

	return c
}

// String returns the cell value (__toString).
func (c *TableCell) String() string { return c.value }

// Colspan returns the number of columns the cell spans.
func (c *TableCell) Colspan() int { return c.colspan }

// Rowspan returns the number of rows the cell spans.
func (c *TableCell) Rowspan() int { return c.rowspan }

// Style returns the cell style (nil when none).
func (c *TableCell) Style() *TableCellStyle { return c.style }

// TableSeparator is a separator row, or, used as a cell, a horizontal line
// in that cell. PHP's TableSeparator extends TableCell; the embedded cell is
// what the table sees when the separator is used as a cell.
type TableSeparator struct {
	TableCell
}

// NewTableSeparator mirrors new TableSeparator().
func NewTableSeparator() *TableSeparator {
	return NewTableSeparatorWith(TableCellOptions{})
}

// NewTableSeparatorWith mirrors new TableSeparator($options).
func NewTableSeparatorWith(options TableCellOptions) *TableSeparator {
	return &TableSeparator{TableCell: *NewTableCell("", options)}
}

// TableCellStyleOptions are the TableCellStyle options. Empty strings mean
// the PHP defaults: Fg and Bg "default", Options and CellFormat null, Align
// "left".
type TableCellStyleOptions struct {
	Fg         string
	Bg         string
	Options    string
	Align      string
	CellFormat string
}

// TableCellStyle styles a single cell: colours, alignment or a format.
type TableCellStyle struct {
	options TableCellStyleOptions
}

// tableAlignMap is TableCellStyle::ALIGN_MAP.
var tableAlignMap = [...]struct {
	align string
	pad   int
}{
	{"left", php.StrPadRight},
	{"center", php.StrPadBoth},
	{"right", php.StrPadLeft},
}

// NewTableCellStyle mirrors new TableCellStyle($options).
func NewTableCellStyle(options TableCellStyleOptions) (*TableCellStyle, error) {
	if options.Fg == "" {
		options.Fg = "default"
	}
	if options.Bg == "" {
		options.Bg = "default"
	}
	if options.Align == "" {
		options.Align = "left"
	} else if _, ok := alignPad(options.Align); !ok {
		return nil, newError(KindInvalidArgument, "TableCellStyle.php", 50, "Wrong align value. Value must be following: 'left', 'center', 'right'.")
	}

	return &TableCellStyle{options: options}, nil
}

func alignPad(align string) (int, bool) {
	for _, a := range tableAlignMap {
		if a.align == align {
			return a.pad, true
		}
	}

	return 0, false
}

// Options returns the resolved options (getOptions()).
func (s *TableCellStyle) Options() TableCellStyleOptions { return s.options }

// tagOptions is http_build_query($this->getTagOptions(), ”, ';'): the set
// fg, bg and options entries as an inline style tag body.
func (s *TableCellStyle) tagOptions() string {
	var b strings.Builder
	for _, kv := range [...][2]string{{"fg", s.options.Fg}, {"bg", s.options.Bg}, {"options", s.options.Options}} {
		if kv[1] == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(';')
		}
		b.WriteString(kv[0])
		b.WriteByte('=')
		b.WriteString(urlencode(kv[1]))
	}

	return b.String()
}

// PadByAlign returns the str_pad() type for the alignment.
func (s *TableCellStyle) PadByAlign() int {
	pad, _ := alignPad(s.options.Align)

	return pad
}

// CellFormat returns the cell format ("" when none).
func (s *TableCellStyle) CellFormat() string { return s.options.CellFormat }

// urlencode ports PHP's urlencode().
func urlencode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := range len(s) {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			b.WriteByte(c)
		case c == ' ':
			b.WriteByte('+')
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}

	return b.String()
}

// tableCellOf returns the TableCell behind a cell value (a *TableCell or a
// *TableSeparator), or nil.
func tableCellOf(v any) *TableCell {
	switch c := v.(type) {
	case *TableCell:
		return c
	case *TableSeparator:
		return &c.TableCell
	}

	return nil
}

// tableCellString converts a cell value to a string as PHP's string
// conversion does.
func tableCellString(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case *TableCell:
		return c.value
	case *TableSeparator:
		return c.value
	case fmt.Stringer:
		return c.String()
	}

	return php.ToString(v)
}

// validTableCell reports whether v is a TableCell, a scalar or an object
// implementing __toString(), returning its get_debug_type() otherwise.
func validTableCell(v any) (string, bool) {
	switch v.(type) {
	case nil, string, bool, int, int64, float64, *TableCell, *TableSeparator, fmt.Stringer:
		return "", true
	case []any, []string, *php.Array:
		return "array", false
	}

	return typeString(v), false
}
