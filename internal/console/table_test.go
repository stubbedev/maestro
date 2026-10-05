// Ports Tests/Helper/TableTest.php (symfony/console). The data providers
// renderProvider, renderSetTitle and provideRenderHorizontalTests are
// extracted verbatim by tools/oracle/console/table.php into
// testdata/oracle/table.json.gz, together with oracle renderings.
//
// Not ported: testSectionOutput, testSectionOutputDoesntClearIfTableIsntRendered,
// testSectionOutputWithoutDecoration, testSectionOutputHandlesZeroRowsAfterRender
// and testAppendRowWithoutSectionOutput (ConsoleSectionOutput and appendRow()
// are not ported).

package console

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/testutil"
)

func newTableOutput(decorated bool) (*StreamOutput, *bytes.Buffer) {
	var buf bytes.Buffer

	return NewStreamOutput(&buf, VerbosityNormal, &decorated, nil), &buf
}

// decodeTableValue decodes the cell/row encoding of table.php.
func decodeTableValue(t *testing.T, v any) any {
	t.Helper()
	switch x := v.(type) {
	case nil, string:
		return x
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = decodeTableValue(t, e)
		}

		return out
	case map[string]any:
		switch {
		case x["i"] != nil:
			n, err := strconv.ParseInt(string(x["i"].(json.Number)), 10, 64)
			if err != nil {
				t.Fatal(err)
			}

			return int(n)
		case x["f"] != nil:
			f, err := x["f"].(json.Number).Float64()
			if err != nil {
				t.Fatal(err)
			}

			return f
		case x["b"] != nil:
			return x["b"].(bool)
		case x["sep"] != nil:
			return NewTableSeparatorWith(TableCellOptions{Rowspan: jsonInt(t, x["rowspan"]), Colspan: jsonInt(t, x["colspan"])})
		case x["cell"] != nil:
			o := TableCellOptions{Rowspan: jsonInt(t, x["rowspan"]), Colspan: jsonInt(t, x["colspan"])}
			if s, ok := x["style"].(map[string]any); ok {
				str := func(k string) string {
					v, _ := s[k].(string)

					return v
				}
				style, err := NewTableCellStyle(TableCellStyleOptions{Fg: str("fg"), Bg: str("bg"), Options: str("options"), Align: str("align"), CellFormat: str("cellFormat")})
				if err != nil {
					t.Fatal(err)
				}
				o.Style = style
			}

			return NewTableCell(x["cell"].(string), o)
		}
	}
	t.Fatalf("cannot decode %#v", v)

	return nil
}

func jsonInt(t *testing.T, v any) int {
	t.Helper()
	n, err := v.(json.Number).Int64()
	if err != nil {
		t.Fatal(err)
	}

	return int(n)
}

func decodeTableList(t *testing.T, v any) []any {
	t.Helper()
	l, _ := decodeTableValue(t, v).([]any)

	return l
}

type tableOracle struct {
	Provider []struct {
		Name      string `json:"name"`
		Headers   any    `json:"headers"`
		Rows      any    `json:"rows"`
		Style     string `json:"style"`
		Expected  string `json:"expected"`
		Decorated bool   `json:"decorated"`
	} `json:"provider"`
	SetTitle []struct {
		Name        string `json:"name"`
		HeaderTitle string `json:"headerTitle"`
		FooterTitle string `json:"footerTitle"`
		Style       string `json:"style"`
		Expected    string `json:"expected"`
	} `json:"setTitle"`
	Horizontal []struct {
		Name     string `json:"name"`
		Headers  any    `json:"headers"`
		Rows     any    `json:"rows"`
		Expected string `json:"expected"`
	} `json:"horizontal"`
	Oracle []struct {
		Name         string            `json:"name"`
		Headers      any               `json:"headers"`
		Rows         any               `json:"rows"`
		Style        string            `json:"style"`
		Decorated    bool              `json:"decorated"`
		MaxWidths    map[string]int    `json:"maxWidths"`
		ColumnWidths []int             `json:"columnWidths"`
		ColumnStyles map[string]string `json:"columnStyles"`
		Horizontal   bool              `json:"horizontal"`
		HeaderTitle  *string           `json:"headerTitle"`
		FooterTitle  *string           `json:"footerTitle"`
		Output       *string           `json:"output"`
		Error        *string           `json:"error"`
	} `json:"oracle"`
}

func loadTableOracle(t *testing.T) *tableOracle {
	t.Helper()
	data, err := testutil.ReadGoldenFile("testdata/oracle/table.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var o tableOracle
	if err := dec.Decode(&o); err != nil {
		t.Fatal(err)
	}

	return &o
}

// renderTableCatching renders, turning a formatter panic into an error.
func renderTableCatching(table *Table) (err error) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(*Error)
			if !ok {
				panic(r)
			}
			err = e
		}
	}()

	return table.Render()
}

func mustNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestTable_Render(t *testing.T) {
	setOracleEnv(t)
	o := loadTableOracle(t)
	modes := []struct {
		name string
		fill func(*testing.T, *Table, []any)
	}{
		{"testRender", func(t *testing.T, table *Table, rows []any) { mustNoErr(t, table.SetRows(rows)) }},
		{"testRenderAddRows", func(t *testing.T, table *Table, rows []any) { mustNoErr(t, table.AddRows(rows)) }},
		{"testRenderAddRowsOneByOne", func(t *testing.T, table *Table, rows []any) {
			for _, row := range rows {
				mustNoErr(t, table.AddRow(row))
			}
		}},
	}
	for _, mode := range modes {
		for i, c := range o.Provider {
			t.Run(fmt.Sprintf("%s/%d %s", mode.name, i, c.Name), func(t *testing.T) {
				out, buf := newTableOutput(c.Decorated)
				table := NewTable(out)
				table.SetHeaders(decodeTableList(t, c.Headers))
				mode.fill(t, table, decodeTableList(t, c.Rows))
				mustNoErr(t, table.SetStyle(c.Style))
				mustNoErr(t, table.Render())
				if got := buf.String(); got != c.Expected {
					t.Errorf("want:\n%s\ngot:\n%s", c.Expected, got)
				}
			})
		}
	}
	if len(o.Provider) == 0 {
		t.Fatal("no provider cases")
	}
}

func TestTable_SetTitle(t *testing.T) {
	setOracleEnv(t)
	o := loadTableOracle(t)
	for i, c := range o.SetTitle {
		t.Run(strconv.Itoa(i)+" "+c.Name, func(t *testing.T) {
			out, buf := newTableOutput(false)
			table := NewTable(out).
				SetHeaderTitle(&c.HeaderTitle).
				SetFooterTitle(&c.FooterTitle).
				SetHeaders([]any{"ISBN", "Title", "Author"})
			mustNoErr(t, table.SetRows([]any{
				[]any{"99921-58-10-7", "Divine Comedy", "Dante Alighieri"},
				[]any{"9971-5-0210-0", "A Tale of Two Cities", "Charles Dickens"},
				[]any{"960-425-059-0", "The Lord of the Rings", "J. R. R. Tolkien"},
				[]any{"80-902734-1-6", "And Then There Were None", "Agatha Christie"},
			}))
			mustNoErr(t, table.SetStyle(c.Style))
			mustNoErr(t, table.Render())
			if got := buf.String(); got != c.Expected {
				t.Errorf("want:\n%s\ngot:\n%s", c.Expected, got)
			}
		})
	}
}

func TestTable_RenderHorizontal(t *testing.T) {
	o := loadTableOracle(t)
	for i, c := range o.Horizontal {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			out, buf := newTableOutput(false)
			table := NewTable(out).SetHeaders(decodeTableList(t, c.Headers))
			mustNoErr(t, table.SetRows(decodeTableList(t, c.Rows)))
			table.SetHorizontal(true)
			mustNoErr(t, table.Render())
			if got := buf.String(); got != c.Expected {
				t.Errorf("want:\n%s\ngot:\n%s", c.Expected, got)
			}
		})
	}
}

func registerOracleTableStyles(t *testing.T) {
	t.Helper()
	dots := NewTableStyle()
	mustNoErr(t, dots.SetPaddingChar("."))
	mustNoErr(t, dots.SetPadType(php.StrPadBoth))
	SetTableStyleDefinition("oracle-dots", dots)
	SetTableStyleDefinition("oracle-colored", NewTableStyle().SetBorderFormat("<comment>%s</comment>").SetCellRowContentFormat("[%s]").SetCellHeaderFormat("<error>%s</error>").SetCellRowFormat("<info>%s</info>"))
	SetTableStyleDefinition("oracle-zero", NewTableStyle().SetHorizontalBorderChars("0").SetVerticalBorderChars("").SetDefaultCrossingChar(""))
	left := NewTableStyle().SetHeaderTitleFormat("<info>[%s]</info>").SetFooterTitleFormat("%s")
	mustNoErr(t, left.SetPadType(php.StrPadLeft))
	SetTableStyleDefinition("oracle-left", left)
}

func TestOracle_Table(t *testing.T) {
	setOracleEnv(t)
	registerOracleTableStyles(t)
	o := loadTableOracle(t)
	if len(o.Oracle) == 0 {
		t.Fatal("no oracle cases")
	}
	for i, c := range o.Oracle {
		out, buf := newTableOutput(c.Decorated)
		table := NewTable(out).SetHeaders(decodeTableList(t, c.Headers))
		mustNoErr(t, table.SetRows(decodeTableList(t, c.Rows)))
		mustNoErr(t, table.SetStyle(c.Style))
		for col, w := range c.MaxWidths {
			n, _ := strconv.Atoi(col)
			mustNoErr(t, table.SetColumnMaxWidth(n, w))
		}
		table.SetColumnWidths(c.ColumnWidths)
		for col, s := range c.ColumnStyles {
			n, _ := strconv.Atoi(col)
			mustNoErr(t, table.SetColumnStyle(n, s))
		}
		table.SetHorizontal(c.Horizontal).SetHeaderTitle(c.HeaderTitle).SetFooterTitle(c.FooterTitle)
		err := renderTableCatching(table)
		switch {
		case c.Error != nil:
			if err == nil || err.Error() != *c.Error {
				t.Errorf("case %d %s (style %s, decorated %v): want error %q, got %v", i, c.Name, c.Style, c.Decorated, *c.Error, err)
			}
		case err != nil:
			t.Errorf("case %d %s: unexpected error %v", i, c.Name, err)
		case buf.String() != *c.Output:
			t.Errorf("case %d %s (style %s, decorated %v):\nwant:\n%s\ngot:\n%s", i, c.Name, c.Style, c.Decorated, *c.Output, buf.String())
		}
	}
}

func renderSimple(t *testing.T, decorated bool, build func(*Table)) string {
	t.Helper()
	out, buf := newTableOutput(decorated)
	table := NewTable(out)
	build(table)
	mustNoErr(t, table.Render())

	return buf.String()
}

func assertTable(t *testing.T, want, got string) {
	t.Helper()
	if got != want {
		t.Errorf("want:\n%s\ngot:\n%s", want, got)
	}
}

func TestTable_RenderMultiByte(t *testing.T) {
	got := renderSimple(t, false, func(table *Table) {
		table.SetHeaders([]any{"■■"})
		mustNoErr(t, table.SetRows([]any{[]any{1234}}))
		mustNoErr(t, table.SetStyle("default"))
	})
	assertTable(t, `+------+
| ■■   |
+------+
| 1234 |
+------+
`, got)
}

func TestTable_TableCellWithNumericIntValue(t *testing.T) {
	got := renderSimple(t, false, func(table *Table) {
		mustNoErr(t, table.SetRows([]any{[]any{NewTableCell(php.ToString(12345), TableCellOptions{})}}))
	})
	assertTable(t, `+-------+
| 12345 |
+-------+
`, got)
}

func TestTable_TableCellWithNumericFloatValue(t *testing.T) {
	got := renderSimple(t, false, func(table *Table) {
		mustNoErr(t, table.SetRows([]any{[]any{NewTableCell(php.ToString(12345.01), TableCellOptions{})}}))
	})
	assertTable(t, `+----------+
| 12345.01 |
+----------+
`, got)
}

func TestTable_Style(t *testing.T) {
	style := NewTableStyle().
		SetHorizontalBorderChars(".").
		SetVerticalBorderChars(".").
		SetDefaultCrossingChar(".")
	SetTableStyleDefinition("dotfull", style)

	got := renderSimple(t, false, func(table *Table) {
		table.SetHeaders([]any{"Foo"})
		mustNoErr(t, table.SetRows([]any{[]any{"Bar"}}))
		mustNoErr(t, table.SetStyle("dotfull"))
	})
	assertTable(t, `.......
. Foo .
.......
. Bar .
.......
`, got)
}

func TestTable_RowSeparator(t *testing.T) {
	out, buf := newTableOutput(false)
	table := NewTable(out)
	table.SetHeaders([]any{"Foo"})
	mustNoErr(t, table.SetRows([]any{
		[]any{"Bar1"},
		NewTableSeparator(),
		[]any{"Bar2"},
		NewTableSeparator(),
		[]any{"Bar3"},
	}))
	mustNoErr(t, table.Render())
	assertTable(t, `+------+
| Foo  |
+------+
| Bar1 |
+------+
| Bar2 |
+------+
| Bar3 |
+------+
`, buf.String())

	if err := table.AddRow(NewTableSeparator()); err != nil {
		t.Errorf("addRow() with a single TableSeparator() failed: %v", err)
	}
}

func TestTable_RenderMultiCalls(t *testing.T) {
	out, buf := newTableOutput(false)
	table := NewTable(out)
	mustNoErr(t, table.SetRows([]any{[]any{NewTableCell("foo", TableCellOptions{Colspan: 2})}}))
	for range 3 {
		mustNoErr(t, table.Render())
	}
	assertTable(t, `+----+---+
| foo    |
+----+---+
+----+---+
| foo    |
+----+---+
+----+---+
| foo    |
+----+---+
`, buf.String())
}

func padLeftStyle(t *testing.T) *TableStyle {
	t.Helper()
	style := NewTableStyle()
	mustNoErr(t, style.SetPadType(php.StrPadLeft))

	return style
}

var tableColumnStyleRows = []any{
	[]any{"99921-58-10-7", "Divine Comedy", "Dante Alighieri", "9.95"},
	[]any{"9971-5-0210-0", "A Tale of Two Cities", "Charles Dickens", "139.25"},
}

func TestTable_ColumnStyle(t *testing.T) {
	got := renderSimple(t, false, func(table *Table) {
		table.SetHeaders([]any{"ISBN", "Title", "Author", "Price"})
		mustNoErr(t, table.SetRows(tableColumnStyleRows))
		table.SetColumnStyleObject(3, padLeftStyle(t))
	})
	assertTable(t, `+---------------+----------------------+-----------------+--------+
| ISBN          | Title                | Author          |  Price |
+---------------+----------------------+-----------------+--------+
| 99921-58-10-7 | Divine Comedy        | Dante Alighieri |   9.95 |
| 9971-5-0210-0 | A Tale of Two Cities | Charles Dickens | 139.25 |
+---------------+----------------------+-----------------+--------+
`, got)
}

func TestTable_ThrowsWhenTheCellInAnArray(t *testing.T) {
	out, _ := newTableOutput(false)
	table := NewTable(out)
	table.SetHeaders([]any{"ISBN", "Title", "Author", "Price"})
	mustNoErr(t, table.SetRows([]any{[]any{"99921-58-10-7", []any{}, "Dante Alighieri", "9.95"}}))

	err := table.Render()
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindInvalidArgument || e.Message != `A cell must be a TableCell, a scalar or an object implementing "__toString()", "array" given.` {
		t.Fatalf("unexpected error %v", err)
	}
}

const tableColumnWidthExpected = `+-----------------+----------------------+-----------------+------------+
| ISBN            | Title                | Author          |      Price |
+-----------------+----------------------+-----------------+------------+
| 99921-58-10-7   | Divine Comedy        | Dante Alighieri |       9.95 |
| 9971-5-0210-0   | A Tale of Two Cities | Charles Dickens |     139.25 |
+-----------------+----------------------+-----------------+------------+
`

func TestTable_ColumnWidth(t *testing.T) {
	got := renderSimple(t, false, func(table *Table) {
		table.SetHeaders([]any{"ISBN", "Title", "Author", "Price"})
		mustNoErr(t, table.SetRows(tableColumnStyleRows))
		table.SetColumnWidth(0, 15).SetColumnWidth(3, 10)
		table.SetColumnStyleObject(3, padLeftStyle(t))
	})
	assertTable(t, tableColumnWidthExpected, got)
}

func TestTable_ColumnWidths(t *testing.T) {
	got := renderSimple(t, false, func(table *Table) {
		table.SetHeaders([]any{"ISBN", "Title", "Author", "Price"})
		mustNoErr(t, table.SetRows(tableColumnStyleRows))
		table.SetColumnWidths([]int{15, 0, -1, 10})
		table.SetColumnStyleObject(3, padLeftStyle(t))
	})
	assertTable(t, tableColumnWidthExpected, got)
}

func TestTable_IsNotDefinedStyleException(t *testing.T) {
	out, _ := newTableOutput(false)
	err := NewTable(out).SetStyle("absent")
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindInvalidArgument || e.Message != `Style "absent" is not defined.` {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestTable_GetStyleDefinition(t *testing.T) {
	_, err := TableStyleDefinition("absent")
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindInvalidArgument || e.Message != `Style "absent" is not defined.` {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestTable_SetTitleWithoutHeaders(t *testing.T) {
	title := "Reproducer"
	got := renderSimple(t, false, func(table *Table) {
		table.SetHeaderTitle(&title)
		mustNoErr(t, table.SetRows([]any{
			[]any{"Value", "123-456"},
			[]any{"Some other value", "789-0"},
		}))
	})
	assertTable(t, `+-------- Reproducer --------+
| Value            | 123-456 |
| Some other value | 789-0   |
+------------------+---------+
`, got)
}

func TestTable_ColumnMaxWidths(t *testing.T) {
	got := renderSimple(t, false, func(table *Table) {
		mustNoErr(t, table.SetRows([]any{
			[]any{"Divine Comedy", "A Tale of Two Cities", "The Lord of the Rings", "And Then There Were None"},
		}))
		mustNoErr(t, table.SetColumnMaxWidth(1, 5))
		mustNoErr(t, table.SetColumnMaxWidth(2, 10))
		mustNoErr(t, table.SetColumnMaxWidth(3, 15))
	})
	assertTable(t, `+---------------+-------+----------+----------------+
| Divine Comedy | A     | The Lord | And Then There |
|               | Tale  | of the   | Were None      |
|               | of    | Rings    |                |
|               | Two   |          |                |
|               | Citie |          |                |
|               | s     |          |                |
+---------------+-------+----------+----------------+
`, got)
}

func TestTable_ColumnMaxWidthsHeaders(t *testing.T) {
	got := renderSimple(t, false, func(table *Table) {
		table.SetHeaders([]any{[]any{"Publication", "Very long header with a lot of information"}})
		mustNoErr(t, table.SetRows([]any{[]any{"1954", "The Lord of the Rings, by J.R.R. Tolkien"}}))
		mustNoErr(t, table.SetColumnMaxWidth(1, 30))
	})
	assertTable(t, `+-------------+--------------------------------+
| Publication | Very long header with a lot of |
|             | information                    |
+-------------+--------------------------------+
| 1954        | The Lord of the Rings, by      |
|             | J.R.R. Tolkien                 |
+-------------+--------------------------------+
`, got)
}

func TestTable_ColumnMaxWidthsWithTrailingBackslash(t *testing.T) {
	got := renderSimple(t, false, func(table *Table) {
		mustNoErr(t, table.SetColumnMaxWidth(0, 5))
		mustNoErr(t, table.SetRows([]any{[]any{`1234\6`}}))
	})
	assertTable(t, `+-------+
| 1234\ |
| 6     |
+-------+
`, got)
}

func TestTable_SetColumnMaxWidthRequiresWrappableFormatter(t *testing.T) {
	out := NewNullOutput()
	err := NewTable(out).SetColumnMaxWidth(0, 5)
	var e *Error
	want := `Setting a maximum column width is only supported when using a "Symfony\Component\Console\Formatter\WrappableOutputFormatterInterface" formatter, got "Symfony\Component\Console\Formatter\NullOutputFormatter".`
	if !errors.As(err, &e) || e.Kind != KindSPLLogic || e.Message != want {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestTable_BoxedStyleWithColspan(t *testing.T) {
	boxed := NewTableStyle().
		SetHorizontalBorderChars("─").
		SetVerticalBorderChars("│").
		SetCrossingChars("┼", "┌", "┬", "┐", "┤", "┘", "┴", "└", "├")

	got := renderSimple(t, false, func(table *Table) {
		table.SetStyleObject(boxed)
		table.SetHeaders([]any{"ISBN", "Title", "Author"})
		mustNoErr(t, table.SetRows([]any{
			[]any{"99921-58-10-7", "Divine Comedy", "Dante Alighieri"},
			NewTableSeparator(),
			[]any{NewTableCell("This value spans 3 columns.", TableCellOptions{Colspan: 3})},
		}))
	})
	assertTable(t, `┌───────────────┬───────────────┬─────────────────┐
│ ISBN          │ Title         │ Author          │
├───────────────┼───────────────┼─────────────────┤
│ 99921-58-10-7 │ Divine Comedy │ Dante Alighieri │
├───────────────┼───────────────┼─────────────────┤
│ This value spans 3 columns.                     │
└───────────────┴───────────────┴─────────────────┘
`, got)
}

func TestTable_WithColspanAndMaxWith(t *testing.T) {
	got := renderSimple(t, false, func(table *Table) {
		mustNoErr(t, table.SetColumnMaxWidth(0, 15))
		mustNoErr(t, table.SetColumnMaxWidth(1, 15))
		mustNoErr(t, table.SetColumnMaxWidth(2, 15))
		cell := func(s string, colspan int) *TableCell { return NewTableCell(s, TableCellOptions{Colspan: colspan}) }
		mustNoErr(t, table.SetRows([]any{
			[]any{cell("Lorem ipsum dolor sit amet, <fg=white;bg=green>consectetur</> adipiscing elit, <fg=white;bg=red>sed</> do <fg=white;bg=red>eiusmod</> tempor", 3)},
			NewTableSeparator(),
			[]any{cell("Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor", 3)},
			NewTableSeparator(),
			[]any{cell("Lorem ipsum <fg=white;bg=red>dolor</> sit amet, consectetur ", 2), "hello world"},
			NewTableSeparator(),
			[]any{"hello <fg=white;bg=green>world</>", cell("Lorem ipsum dolor sit amet, <fg=white;bg=green>consectetur</> adipiscing elit", 2)},
			NewTableSeparator(),
			[]any{"hello ", cell("world", 1), "Lorem ipsum dolor sit amet, consectetur"},
			NewTableSeparator(),
			[]any{"Symfony ", cell("Test", 1), "Lorem <fg=white;bg=green>ipsum</> dolor sit amet, consectetur"},
		}))
	})
	assertTable(t, `+-----------------+-----------------+-----------------+
| Lorem ipsum dolor sit amet, consectetur adipi       |
| scing elit, sed do eiusmod tempor                   |
+-----------------+-----------------+-----------------+
| Lorem ipsum dolor sit amet, consectetur             |
| adipiscing elit, sed do eiusmod tempor              |
+-----------------+-----------------+-----------------+
| Lorem ipsum dolor sit amet, co    | hello world     |
| nsectetur                         |                 |
+-----------------+-----------------+-----------------+
| hello world     | Lorem ipsum dolor sit amet, co    |
|                 | nsectetur adipiscing elit         |
+-----------------+-----------------+-----------------+
| hello           | world           | Lorem ipsum     |
|                 |                 | dolor sit amet, |
|                 |                 | consectetur     |
+-----------------+-----------------+-----------------+
| Symfony         | Test            | Lorem ipsum dol |
|                 |                 | or sit amet,    |
|                 |                 | consectetur     |
+-----------------+-----------------+-----------------+
`, got)
}

func TestTable_WithHyperlinkAndMaxWidth(t *testing.T) {
	setOracleEnv(t)
	got := renderSimple(t, true, func(table *Table) {
		mustNoErr(t, table.SetRows([]any{
			[]any{"<href=Lorem>Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor</>"},
		}))
		mustNoErr(t, table.SetColumnMaxWidth(0, 20))
	})
	want := strings.ReplaceAll(`+----------------------+
| \033]8;;Lorem\033\\Lorem ipsum dolor\033]8;;\033\\    |
| \033]8;;Lorem\033\\sit amet,\033]8;;\033\\            |
| \033]8;;Lorem\033\\consectetur\033]8;;\033\\          |
| \033]8;;Lorem\033\\adipiscing elit, sed\033]8;;\033\\ |
| \033]8;;Lorem\033\\do eiusmod tempor\033]8;;\033\\    |
+----------------------+
`, `\033`, "\033")
	want = strings.ReplaceAll(want, `\\`, `\`)
	assertTable(t, want, got)
}

func TestTable_AddRowRejectsNonRows(t *testing.T) {
	out, _ := newTableOutput(false)
	err := NewTable(out).AddRow("not a row")
	var e *Error
	if !errors.As(err, &e) || e.Kind != KindInvalidArgument || e.Message != "A row must be an array or a TableSeparator instance." {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestTable_SprintfFormatErrors(t *testing.T) {
	cases := []struct{ format, want string }{
		{"%2C", `Unknown format specifier "C"`},
		{"%s %s", "3 arguments are required, 2 given"},
		{"%", "Missing format specifier at end of string"},
		{"%'", "Missing padding character"},
	}
	for _, c := range cases {
		out, _ := newTableOutput(false)
		table := NewTable(out).SetStyleObject(NewTableStyle().SetCellRowContentFormat(c.format))
		mustNoErr(t, table.AddRow([]any{"x"}))
		err := table.Render()
		if err == nil || err.Error() != c.want {
			t.Errorf("format %q: want %q, got %v", c.format, c.want, err)
		}
	}

	for format, want := range map[string]string{
		"[%5s]": "[    x]", "[%-5s]": "[x    ]", "[%'*5s]": "[****x]", "[%05s]": "[0000x]",
		"[%.0s]": "[]", "[%1$s%1$s]": "[xx]", "100%%": "100%",
	} {
		if got := phpSprintf(format, "x"); got != want {
			t.Errorf("phpSprintf(%q) = %q, want %q", format, got, want)
		}
	}
}
