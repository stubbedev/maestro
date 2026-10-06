// Ports src/Helper/Table.php (symfony/console), without appendRow(), which
// needs the unported ConsoleSectionOutput.
//
// PHP rows are heterogeneous arrays. Here a row is either a *TableSeparator
// or a []any of cells (a []string is accepted and converted), and a cell is
// one of: nil, string, bool, int, int64, float64, a *TableCell, a
// *TableSeparator (a horizontal line within the cell) or a fmt.Stringer
// (PHP's __toString()). Any other cell value makes Render fail like PHP.
// Rows are lists, as after PHP's array_values(); where PHP would add a key
// past the end of a row (only reachable with exotic rowspan layouts) the
// value is appended, which keeps PHP's iteration order.

package console

import (
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/stubbedev/maestro/internal/php"
)

// Separator positions (Table::SEPARATOR_*).
const (
	tableSeparatorTop = iota
	tableSeparatorTopBottom
	tableSeparatorMid
	tableSeparatorBottom
)

// Table renders tabular data.
type Table struct {
	headerTitle *string
	footerTitle *string
	headers     [][]any
	rows        []any
	horizontal  bool

	effectiveColumnWidths []int
	numberOfColumns       int

	output          Output
	style           *TableStyle
	columnStyles    map[int]*TableStyle
	columnWidths    map[int]int
	columnMaxWidths map[int]int
}

// NewTable mirrors new Table($output), using the "default" style.
func NewTable(output Output) *Table {
	style, _ := TableStyleDefinition("default")

	return &Table{output: output, style: style}
}

// SetStyle sets the table style by name.
func (t *Table) SetStyle(name string) error {
	style, err := resolveTableStyle(name)
	if err != nil {
		return err
	}
	t.style = style

	return nil
}

// SetStyleObject sets the table style.
func (t *Table) SetStyleObject(style *TableStyle) *Table {
	t.style = style

	return t
}

// Style returns the table style.
func (t *Table) Style() *TableStyle { return t.style }

// SetColumnStyle sets the style of a column by name.
func (t *Table) SetColumnStyle(columnIndex int, name string) error {
	style, err := resolveTableStyle(name)
	if err != nil {
		return err
	}
	t.SetColumnStyleObject(columnIndex, style)

	return nil
}

// SetColumnStyleObject sets the style of a column.
func (t *Table) SetColumnStyleObject(columnIndex int, style *TableStyle) *Table {
	if t.columnStyles == nil {
		t.columnStyles = map[int]*TableStyle{}
	}
	t.columnStyles[columnIndex] = style

	return t
}

// ColumnStyle returns the style of a column, or the table style.
func (t *Table) ColumnStyle(columnIndex int) *TableStyle {
	if s, ok := t.columnStyles[columnIndex]; ok {
		return s
	}

	return t.style
}

func resolveTableStyle(name string) (*TableStyle, error) {
	style, err := TableStyleDefinition(name)
	if err != nil {
		return nil, newError(KindInvalidArgument, "Table.php", 915, `Style "%s" is not defined.`, name)
	}

	return style, nil
}

// SetColumnWidth sets the minimum width of a column.
func (t *Table) SetColumnWidth(columnIndex, width int) *Table {
	if t.columnWidths == nil {
		t.columnWidths = map[int]int{}
	}
	t.columnWidths[columnIndex] = width

	return t
}

// SetColumnWidths sets the minimum width of every column.
func (t *Table) SetColumnWidths(widths []int) *Table {
	t.columnWidths = nil
	for i, w := range widths {
		t.SetColumnWidth(i, w)
	}

	return t
}

// SetColumnMaxWidth sets the maximum width of a column; longer cells are
// wrapped. The output formatter must be a WrappableFormatter.
func (t *Table) SetColumnMaxWidth(columnIndex, width int) error {
	if f := t.output.Formatter(); !isWrappable(f) {
		return newError(KindSPLLogic, "Table.php", 224, `Setting a maximum column width is only supported when using a "Symfony\Component\Console\Formatter\WrappableOutputFormatterInterface" formatter, got "%s".`, formatterClass(f))
	}
	if t.columnMaxWidths == nil {
		t.columnMaxWidths = map[int]int{}
	}
	t.columnMaxWidths[columnIndex] = width

	return nil
}

func isWrappable(f Formatter) bool {
	_, ok := f.(WrappableFormatter)

	return ok
}

// formatterClass is get_debug_type() of a formatter.
func formatterClass(f Formatter) string {
	switch f.(type) {
	case *NullOutputFormatter:
		return `Symfony\Component\Console\Formatter\NullOutputFormatter`
	case *OutputFormatter:
		return `Symfony\Component\Console\Formatter\OutputFormatter`
	}

	return typeString(f)
}

// SetHeaders sets the header row, or several header rows when headers[0]
// is itself a row.
func (t *Table) SetHeaders(headers []any) *Table {
	t.headers = nil
	if len(headers) == 0 {
		return t
	}
	if _, ok := tableRowCells(headers[0]); !ok {
		t.headers = [][]any{slices.Clone(headers)}

		return t
	}
	t.headers = make([][]any, 0, len(headers))
	for _, h := range headers {
		cells, _ := tableRowCells(h)
		t.headers = append(t.headers, cells)
	}

	return t
}

// SetRows replaces the rows.
func (t *Table) SetRows(rows []any) error {
	t.rows = nil

	return t.AddRows(rows)
}

// AddRows adds rows.
func (t *Table) AddRows(rows []any) error {
	for _, row := range rows {
		if err := t.AddRow(row); err != nil {
			return err
		}
	}

	return nil
}

// AddRow adds a row: a []any or []string of cells, or a *TableSeparator.
func (t *Table) AddRow(row any) error {
	if sep, ok := row.(*TableSeparator); ok {
		t.rows = append(t.rows, sep)

		return nil
	}

	cells, ok := tableRowCells(row)
	if !ok {
		return newError(KindInvalidArgument, "Table.php", 278, "A row must be an array or a TableSeparator instance.")
	}
	t.rows = append(t.rows, cells)

	return nil
}

// SetRow replaces the row at index column (appending past the end).
func (t *Table) SetRow(column int, row []any) *Table {
	if column >= 0 && column < len(t.rows) {
		t.rows[column] = row
	} else {
		t.rows = append(t.rows, row)
	}

	return t
}

// tableRowCells returns a copy of a row's cells.
func tableRowCells(row any) ([]any, bool) {
	switch r := row.(type) {
	case []any:
		return slices.Clone(r), true
	case []string:
		cells := make([]any, len(r))
		for i, s := range r {
			cells[i] = s
		}

		return cells, true
	}

	return nil, false
}

// SetHeaderTitle sets the title shown in the top border (nil for none).
func (t *Table) SetHeaderTitle(title *string) *Table {
	t.headerTitle = title

	return t
}

// SetFooterTitle sets the title shown in the bottom border (nil for none).
func (t *Table) SetFooterTitle(title *string) *Table {
	t.footerTitle = title

	return t
}

// SetHorizontal renders headers as the first column instead of the first
// row.
func (t *Table) SetHorizontal(horizontal bool) *Table {
	t.horizontal = horizontal

	return t
}

// Render renders the table to the output.
//
// Example:
//
//	+---------------+-----------------------+------------------+
//	| ISBN          | Title                 | Author           |
//	+---------------+-----------------------+------------------+
//	| 99921-58-10-7 | Divine Comedy         | Dante Alighieri  |
//	| 9971-5-0210-0 | A Tale of Two Cities  | Charles Dickens  |
//	| 960-425-059-0 | The Lord of the Rings | J. R. R. Tolkien |
//	+---------------+-----------------------+------------------+
func (t *Table) Render() (err error) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*phpFormatError)
			if !ok {
				panic(r)
			}
			t.cleanup()
			err = e
		}
	}()

	divider := NewTableSeparator()
	var rows []any
	if t.horizontal {
		if len(t.headers) > 0 {
			rows = make([]any, 0, len(t.headers[0]))
			for i, header := range t.headers[0] {
				row := []any{header}
				for _, r := range t.rows {
					cells, ok := r.([]any)
					if !ok {
						continue
					}
					if i < len(cells) && cells[i] != nil {
						row = append(row, cells[i])
					} else if c := tableCellOf(row[0]); c != nil && c.colspan >= 2 {
						// Noop, there is a "title"
					} else {
						row = append(row, nil)
					}
				}
				rows = append(rows, row)
			}
		}
	} else {
		rows = make([]any, 0, len(t.headers)+1+len(t.rows))
		for _, h := range t.headers {
			rows = append(rows, slices.Clone(h))
		}
		rows = append(rows, divider)
		for _, r := range t.rows {
			if cells, ok := r.([]any); ok {
				r = slices.Clone(cells)
			}
			rows = append(rows, r)
		}
	}

	t.calculateNumberOfColumns(rows)

	rowGroups, err := t.buildTableRows(rows)
	if err != nil {
		t.cleanup()

		return err
	}
	t.calculateColumnsWidth(rowGroups)

	isHeader := !t.horizontal
	isFirstRow := t.horizontal
	hasTitle := t.headerTitle != nil && php.ToBool(*t.headerTitle)

	for _, rowGroup := range rowGroups {
		isHeaderSeparatorRendered := false

		for _, row := range rowGroup {
			if sep, ok := row.(*TableSeparator); ok {
				if sep == divider {
					isHeader = false
					isFirstRow = true
				} else {
					t.renderRowSeparator(tableSeparatorMid, nil, "")
				}

				continue
			}

			cells, _ := row.([]any)
			if len(cells) == 0 {
				continue
			}

			if isHeader && !isHeaderSeparatorRendered {
				t.renderTopSeparator(isHeader, hasTitle)
				hasTitle = false
				isHeaderSeparatorRendered = true
			}

			if isFirstRow {
				t.renderTopSeparator(isHeader, hasTitle)
				isFirstRow = false
				hasTitle = false
			}

			switch {
			case t.horizontal:
				t.renderRow(cells, t.style.cellRowFormat, t.style.cellHeaderFormat)
			case isHeader:
				t.renderRow(cells, t.style.cellHeaderFormat, "")
			default:
				t.renderRow(cells, t.style.cellRowFormat, "")
			}
		}
	}
	t.renderRowSeparator(tableSeparatorBottom, t.footerTitle, t.style.footerTitleFormat)

	t.cleanup()

	return nil
}

func (t *Table) renderTopSeparator(isHeader, hasTitle bool) {
	typ := tableSeparatorTopBottom
	if isHeader {
		typ = tableSeparatorTop
	}
	if hasTitle {
		t.renderRowSeparator(typ, t.headerTitle, t.style.headerTitleFormat)
	} else {
		t.renderRowSeparator(typ, nil, "")
	}
}

// renderRowSeparator renders a horizontal separator, with an optional
// title centred in it.
//
// Example:
//
//	+-----+-----------+-------+
func (t *Table) renderRowSeparator(typ int, title *string, titleFormat string) {
	count := t.numberOfColumns
	if count == 0 {
		return
	}

	borders := t.style.BorderChars()
	if !php.ToBool(borders[0]) && !php.ToBool(borders[2]) && !php.ToBool(t.style.crossingChar) {
		return
	}

	crossings := t.style.CrossingChars()
	var horizontal, leftChar, midChar, rightChar string
	switch typ {
	case tableSeparatorMid:
		horizontal, leftChar, midChar, rightChar = borders[2], crossings[8], crossings[0], crossings[4]
	case tableSeparatorTop:
		horizontal, leftChar, midChar, rightChar = borders[0], crossings[1], crossings[2], crossings[3]
	case tableSeparatorTopBottom:
		horizontal, leftChar, midChar, rightChar = borders[0], crossings[9], crossings[10], crossings[11]
	default:
		horizontal, leftChar, midChar, rightChar = borders[0], crossings[7], crossings[6], crossings[5]
	}

	var b strings.Builder
	b.WriteString(leftChar)
	for column := range count {
		b.WriteString(strings.Repeat(horizontal, t.effectiveColumnWidths[column]))
		if column == count-1 {
			b.WriteString(rightChar)
		} else {
			b.WriteString(midChar)
		}
	}
	markup := b.String()

	if title != nil {
		formatter := t.output.Formatter()
		formattedTitle := phpSprintf(titleFormat, *title)
		titleLength := Width(RemoveDecoration(formatter, formattedTitle))
		markupLength := Width(markup)
		if limit := markupLength - 4; titleLength > limit {
			titleLength = limit
			formatLength := Width(RemoveDecoration(formatter, phpSprintf(titleFormat, "")))
			formattedTitle = phpSprintf(titleFormat, Substr(*title, 0, limit-formatLength-3, false)+"...")
		}

		titleStart := (markupLength - titleLength) / 2
		if utf8.ValidString(markup) {
			markup = Substr(markup, 0, titleStart, false) + formattedTitle + Substr(markup, titleStart+titleLength, 0, true)
		} else {
			markup = substrReplace(markup, formattedTitle, titleStart, titleLength)
		}
	}

	t.output.Writeln(phpSprintf(t.style.borderFormat, markup))
}

// substrReplace ports substr_replace($s, $replacement, $start, $length).
func substrReplace(s, replacement string, start, length int) string {
	n := len(s)
	if start < 0 {
		start = max(start+n, 0)
	}
	start = min(start, n)
	if length < 0 {
		length = max(n-start+length, 0)
	}
	length = min(length, n-start)

	return s[:start] + replacement + s[start+length:]
}

// renderColumnSeparator renders the vertical border between or around cells.
func (t *Table) renderColumnSeparator(outside bool) string {
	borders := t.style.BorderChars()
	if outside {
		return phpSprintf(t.style.borderFormat, borders[1])
	}

	return phpSprintf(t.style.borderFormat, borders[3])
}

// renderRow renders a table row.
//
// Example:
//
//	| 9971-5-0210-0 | A Tale of Two Cities  | Charles Dickens  |
func (t *Table) renderRow(row []any, cellFormat, firstCellFormat string) {
	var b strings.Builder
	b.WriteString(t.renderColumnSeparator(true))
	columns := t.rowColumns(row)
	last := len(columns) - 1
	for _, column := range columns {
		// PHP iterates array_diff() output, whose keys are the column
		// indexes: "$i" below is the column, not its position.
		if php.ToBool(firstCellFormat) && column == 0 {
			b.WriteString(t.renderCell(row, column, firstCellFormat))
		} else {
			b.WriteString(t.renderCell(row, column, cellFormat))
		}
		b.WriteString(t.renderColumnSeparator(last == column))
	}
	t.output.Writeln(b.String())
}

// tableStyledByTag is Table::renderCell's pattern.
var tableStyledByTag = php.MustCompile(`/^<(\w+|(\w+=[\w,]+;?)*)>.+<\/(\w+|(\w+=\w+;?)*)?>$/`)

// renderCell renders a table cell with padding.
func (t *Table) renderCell(row []any, column int, cellFormat string) string {
	var cell any = ""
	if column < len(row) && row[column] != nil {
		cell = row[column]
	}
	width := t.effectiveColumnWidth(column)
	c := tableCellOf(cell)
	if c != nil && c.colspan > 1 {
		// add the width of the following columns(numbers of colspan).
		for next := column + 1; next <= column+c.colspan-1; next++ {
			width += t.columnSeparatorWidth() + t.effectiveColumnWidth(next)
		}
	}

	s := tableCellString(cell)
	// str_pad won't work properly with multi-byte strings, we need to fix the padding
	if utf8.ValidString(s) {
		width += len(s) - mbStrwidth(s)
	}

	style := t.ColumnStyle(column)

	if _, ok := cell.(*TableSeparator); ok {
		return phpSprintf(style.borderFormat, strings.Repeat(style.BorderChars()[2], max(width, 0)))
	}

	width += Length(s) - Length(RemoveDecoration(t.output.Formatter(), s))
	content := phpSprintf(style.cellRowContentFormat, s)

	padType := style.padType
	if c != nil && c.style != nil {
		// !preg_match(): a failed match (false) counts as not styled.
		if styled, _ := tableStyledByTag.IsMatch(s); !styled {
			cellFormat = c.style.CellFormat()
			if cellFormat == "" {
				cellFormat = "<" + c.style.tagOptions() + ">%s</>"
			}

			if strings.Contains(content, "</>") {
				content = strings.ReplaceAll(content, "</>", "")
				width -= 3
			}
			const defaultTag = "<fg=default;bg=default>"
			if strings.Contains(content, defaultTag) {
				content = strings.ReplaceAll(content, defaultTag, "")
				width -= len(defaultTag)
			}
		}

		padType = c.style.PadByAlign()
	}

	return phpSprintf(cellFormat, php.StrPad(content, width, style.paddingChar, padType))
}

// mbStrwidth ports mb_strwidth() for valid UTF-8.
func mbStrwidth(s string) int {
	w := 0
	for _, r := range s {
		w += mbCharWidth(r)
	}

	return w
}

func (t *Table) effectiveColumnWidth(column int) int {
	if column >= 0 && column < len(t.effectiveColumnWidths) {
		return t.effectiveColumnWidths[column]
	}

	return 0
}

// calculateNumberOfColumns computes the number of columns of the table.
func (t *Table) calculateNumberOfColumns(rows []any) {
	columns := 0
	for _, row := range rows {
		if cells, ok := row.([]any); ok {
			columns = max(columns, tableNumberOfColumns(cells))
		}
	}
	t.numberOfColumns = columns
}

// buildTableRows splits multi-line cells and spreads rowspans, returning
// the row groups: each group is one input row followed by the extra lines
// its multi-line cells produced.
func (t *Table) buildTableRows(rows []any) ([][]any, error) {
	formatter := t.output.Formatter()
	unmergedRows := map[int][][]any{}
	for rowKey := 0; rowKey < len(rows); rowKey++ {
		var err error
		if rows, err = t.fillNextRows(rows, rowKey); err != nil {
			return nil, err
		}

		cells, ok := rows[rowKey].([]any)
		if !ok {
			continue
		}
		// Remove any new line breaks and replace it with a new line
		for column, cell := range slices.Clone(cells) {
			colspan := 1
			if c := tableCellOf(cell); c != nil {
				colspan = c.colspan
			}

			s := tableCellString(cell)
			if maxWidth, ok := t.columnMaxWidths[column]; ok && Width(RemoveDecoration(formatter, s)) > maxWidth {
				if w, ok := formatter.(WrappableFormatter); ok {
					s = w.FormatAndWrap(s, maxWidth*colspan)
				}
			}
			if !strings.Contains(s, "\n") {
				continue
			}
			eol := "\n"
			if strings.Contains(s, "\r\n") {
				eol = "\r\n"
			}
			parts := strings.Split(s, eol)
			for i, p := range parts {
				parts[i] = EscapeTrailingBackslash(p)
			}
			escaped := strings.Join(parts, eol)
			lines := strings.Split(strings.ReplaceAll(escaped, eol, "<fg=default;bg=default></>"+eol), eol)
			for lineKey, line := range lines {
				var v any = line
				if colspan > 1 {
					v = NewTableCell(line, TableCellOptions{Colspan: colspan})
				}
				if lineKey == 0 {
					cells[column] = v

					continue
				}
				extra := unmergedRows[rowKey]
				if len(extra) < lineKey {
					extra = append(extra, copyTableRow(rows, rowKey))
					unmergedRows[rowKey] = extra
				}
				extra[lineKey-1][column] = v
			}
		}
	}

	groups := make([][]any, len(rows))
	for rowKey, row := range rows {
		extra := unmergedRows[rowKey]
		group := make([]any, 0, 1+len(extra))
		if cells, ok := row.([]any); ok {
			group = append(group, fillTableCells(cells))
		} else {
			group = append(group, row)
		}
		for _, r := range extra {
			group = append(group, fillTableCells(r))
		}
		groups[rowKey] = group
	}

	return groups, nil
}

// tableUnmergedRow is one row created by a rowspan: cells keyed by column,
// in insertion order.
type tableUnmergedRow struct {
	columns []int
	cells   []any
}

// fillNextRows fills the rows below a row with rowspan cells.
func (t *Table) fillNextRows(rows []any, line int) ([]any, error) {
	cells, ok := rows[line].([]any)
	if !ok {
		return rows, nil
	}

	var unmergedRows []tableUnmergedRow // index j is row line+1+j
	for column, cell := range cells {
		if typ, ok := validTableCell(cell); !ok {
			return nil, newError(KindInvalidArgument, "Table.php", 683, `A cell must be a TableCell, a scalar or an object implementing "__toString()", "%s" given.`, typ)
		}
		c := tableCellOf(cell)
		if c == nil || c.rowspan <= 1 {
			continue
		}

		nbLines := c.rowspan - 1
		var lines []string // lines[0] is unused, as PHP unsets it
		if strings.Contains(c.value, "\n") {
			eol := "\n"
			if strings.Contains(c.value, "\r\n") {
				eol = "\r\n"
			}
			lines = strings.Split(strings.ReplaceAll(c.value, eol, "<fg=default;bg=default>"+eol+"</>"), eol)
			if len(lines) > nbLines {
				nbLines = strings.Count(c.value, eol)
			}

			cells[column] = NewTableCell(lines[0], TableCellOptions{Colspan: c.colspan, Style: c.style})
		}

		// create a two dimensional array (rowspan x colspan)
		for len(unmergedRows) < nbLines {
			unmergedRows = append(unmergedRows, tableUnmergedRow{})
		}
		for j := range nbLines {
			value := ""
			if j+1 < len(lines) {
				value = lines[j+1]
			}
			u := &unmergedRows[j]
			u.columns = append(u.columns, column)
			u.cells = append(u.cells, NewTableCell(value, TableCellOptions{Colspan: c.colspan, Style: c.style}))
		}
	}

	for j, unmerged := range unmergedRows {
		k := line + 1 + j
		// we need to know if unmerged will be merged or inserted into rows
		if k < len(rows) {
			if existing, ok := rows[k].([]any); ok && tableNumberOfColumns(existing)+tableNumberOfColumns(unmerged.cells) <= t.numberOfColumns {
				for i, cellKey := range unmerged.columns {
					// insert cell into row at cellKey position
					existing = slices.Insert(existing, min(cellKey, len(existing)), unmerged.cells[i])
				}
				rows[k] = existing

				continue
			}
		}

		row := copyTableRow(rows, k-1)
		for i, column := range unmerged.columns {
			if column < len(row) {
				row[column] = unmerged.cells[i]
			} else {
				row = append(row, unmerged.cells[i])
			}
		}
		rows = slices.Insert(rows, min(k, len(rows)), any(row))
	}

	return rows, nil
}

// fillTableCells adds the empty cells following colspan cells.
func fillTableCells(row []any) []any {
	extra := 0
	for _, cell := range row {
		if c := tableCellOf(cell); c != nil && c.colspan > 1 {
			extra += c.colspan - 1
		}
	}
	if extra == 0 {
		return row
	}

	newRow := make([]any, 0, len(row)+extra)
	for _, cell := range row {
		newRow = append(newRow, cell)
		if c := tableCellOf(cell); c != nil && c.colspan > 1 {
			for range c.colspan - 1 {
				// insert empty value at column position
				newRow = append(newRow, "")
			}
		}
	}

	return newRow
}

// copyTableRow returns a row of empty cells shaped like rows[line].
func copyTableRow(rows []any, line int) []any {
	cells, _ := rows[line].([]any)
	row := make([]any, len(cells))
	for i, cell := range cells {
		row[i] = ""
		if c := tableCellOf(cell); c != nil {
			row[i] = NewTableCell("", TableCellOptions{Colspan: c.colspan})
		}
	}

	return row
}

// tableNumberOfColumns is the number of columns a row occupies.
func tableNumberOfColumns(row []any) int {
	columns := len(row)
	for _, cell := range row {
		if c := tableCellOf(cell); c != nil {
			columns += c.colspan - 1
		}
	}

	return columns
}

// rowColumns returns the columns of a row that are not covered by a
// preceding colspan cell.
func (t *Table) rowColumns(row []any) []int {
	columns := make([]int, 0, t.numberOfColumns)
	for column := range t.numberOfColumns {
		covered := false
		for cellKey, cell := range row {
			if c := tableCellOf(cell); c != nil && c.colspan > 1 && column > cellKey && column <= cellKey+c.colspan-1 {
				covered = true

				break
			}
		}
		if !covered {
			columns = append(columns, column)
		}
	}

	return columns
}

// calculateColumnsWidth computes the effective width of every column.
func (t *Table) calculateColumnsWidth(groups [][]any) {
	formatter := t.output.Formatter()
	lengths := make([]int, t.numberOfColumns)
	for _, group := range groups {
		for _, r := range group {
			row, ok := r.([]any)
			if !ok {
				continue
			}

			// Spread the content of TableCells over the columns they span.
			work, copied := row, false
			for i, cell := range row {
				c := tableCellOf(cell)
				if c == nil {
					continue
				}
				textContent := RemoveDecoration(formatter, c.value)
				textLength := Width(textContent)
				if textLength <= 0 {
					continue
				}
				chunk := int(math.Ceil(float64(textLength) / float64(c.colspan)))
				for position, content := range mbStrSplit(textContent, chunk) {
					if !copied {
						work, copied = slices.Clone(row), true
					}
					for len(work) <= i+position {
						work = append(work, nil)
					}
					work[i+position] = content
				}
			}

			for column := range lengths {
				lengths[column] = max(lengths[column], t.cellWidth(work, column))
			}
		}
	}

	t.effectiveColumnWidths = make([]int, t.numberOfColumns)
	contentWidth := Width(t.style.cellRowContentFormat) - 2
	for column, l := range lengths {
		t.effectiveColumnWidths[column] = l + contentWidth
	}
}

// mbStrSplit ports mb_str_split($s, $length) for UTF-8 (invalid bytes count
// as one character each).
func mbStrSplit(s string, length int) []string {
	chunks := make([]string, 0, utf8.RuneCountInString(s)/length+1)
	start, n := 0, 0
	for i := 0; i < len(s); {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		n++
		if n == length {
			chunks = append(chunks, s[start:i])
			start, n = i, 0
		}
	}
	if start < len(s) {
		chunks = append(chunks, s[start:])
	}

	return chunks
}

// columnSeparatorWidth is the width of an inside vertical border.
func (t *Table) columnSeparatorWidth() int {
	return Width(phpSprintf(t.style.borderFormat, t.style.BorderChars()[3]))
}

// cellWidth returns the width of a cell, bounded by the column's minimum
// and maximum widths.
func (t *Table) cellWidth(row []any, column int) int {
	cellWidth := 0
	if column < len(row) && row[column] != nil {
		cellWidth = Width(RemoveDecoration(t.output.Formatter(), tableCellString(row[column])))
	}

	cellWidth = max(cellWidth, t.columnWidths[column])
	if maxWidth, ok := t.columnMaxWidths[column]; ok {
		return min(maxWidth, cellWidth)
	}

	return cellWidth
}

// cleanup resets the per-render state.
func (t *Table) cleanup() {
	t.effectiveColumnWidths = nil
	t.numberOfColumns = 0
}

// mbCharWidth is mb_strwidth() for one character: 2 for East Asian wide
// and fullwidth characters, 1 otherwise.
func mbCharWidth(r rune) int {
	if inRuneTable(wcwidthWide[:], r) {
		return 2
	}

	return 1
}
