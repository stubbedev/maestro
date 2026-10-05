// Ports tests/CursorTest.php (symfony/console).

package console

import (
	"bytes"
	"testing"
)

func cursorOutput() (*StreamOutput, *bytes.Buffer) {
	buf := &bytes.Buffer{}

	return NewStreamOutput(buf, VerbosityNormal, nil, nil), buf
}

func TestCursor_Moves(t *testing.T) {
	cases := []struct {
		name string
		do   func(c *Cursor)
		want string
	}{
		{"MoveUpOneLine", func(c *Cursor) { c.MoveUp(1) }, "\x1b[1A"},
		{"MoveUpMultipleLines", func(c *Cursor) { c.MoveUp(12) }, "\x1b[12A"},
		{"MoveDownOneLine", func(c *Cursor) { c.MoveDown(1) }, "\x1b[1B"},
		{"MoveDownMultipleLines", func(c *Cursor) { c.MoveDown(12) }, "\x1b[12B"},
		{"MoveLeftOneLine", func(c *Cursor) { c.MoveLeft(1) }, "\x1b[1D"},
		{"MoveLeftMultipleLines", func(c *Cursor) { c.MoveLeft(12) }, "\x1b[12D"},
		{"MoveRightOneLine", func(c *Cursor) { c.MoveRight(1) }, "\x1b[1C"},
		{"MoveRightMultipleLines", func(c *Cursor) { c.MoveRight(12) }, "\x1b[12C"},
		{"MoveToColumn", func(c *Cursor) { c.MoveToColumn(6) }, "\x1b[6G"},
		{"MoveToPosition", func(c *Cursor) { c.MoveToPosition(18, 16) }, "\x1b[17;18H"},
		{"ClearLine", func(c *Cursor) { c.ClearLine() }, "\x1b[2K"},
		{"SavePosition", func(c *Cursor) { c.SavePosition() }, "\x1b7"},
		{"Hide", func(c *Cursor) { c.Hide() }, "\x1b[?25l"},
		{"Show", func(c *Cursor) { c.Show() }, "\x1b[?25h\x1b[?0c"},
		{"RestorePosition", func(c *Cursor) { c.RestorePosition() }, "\x1b8"},
		{"ClearOutput", func(c *Cursor) { c.ClearOutput() }, "\x1b[0J"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, buf := cursorOutput()
			tc.do(NewCursor(out, nil))
			if got := buf.String(); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCursor_GetCurrentPosition(t *testing.T) {
	if cursorTTY() {
		t.Skip("a tty is attached; querying it would change the terminal state")
	}
	out, buf := cursorOutput()
	c := NewCursor(out, nil)

	c.MoveToPosition(10, 10)
	col, row := c.CurrentPosition()

	if got := buf.String(); got != "\x1b[11;10H" {
		t.Errorf("got %q", got)
	}
	if col != 1 || row != 1 {
		t.Errorf("position = (%d, %d), want (1, 1)", col, row)
	}
}

func TestCursor_ScanCursorReply(t *testing.T) {
	cases := []struct {
		in       string
		row, col int
	}{
		{"\033[12;34R", 12, 34},
		{"\033[5;7", 5, 7},
		{"\033[5", 5, 0},
		{"junk", 0, 0},
		{"", 0, 0},
	}
	for _, c := range cases {
		row, col := scanCursorReply(c.in)
		if row != c.row || col != c.col {
			t.Errorf("scanCursorReply(%q) = (%d, %d), want (%d, %d)", c.in, row, col, c.row, c.col)
		}
	}
}
