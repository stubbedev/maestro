// Ports src/Cursor.php (symfony/console).

package console

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
)

// Cursor moves the terminal cursor with ANSI escape sequences written to an
// output.
type Cursor struct {
	output Output
	input  io.Reader
}

// NewCursor mirrors new Cursor($output, $input); a nil input is STDIN.
func NewCursor(output Output, input io.Reader) *Cursor {
	if input == nil {
		input = os.Stdin
	}

	return &Cursor{output: output, input: input}
}

func (c *Cursor) write(seq string) *Cursor {
	c.output.Write(seq, false, OutputNormal)

	return c
}

func csi(n int, final byte) string {
	return "\x1b[" + strconv.Itoa(n) + string(final)
}

// MoveUp moves the cursor up lines lines.
func (c *Cursor) MoveUp(lines int) *Cursor { return c.write(csi(lines, 'A')) }

// MoveDown moves the cursor down lines lines.
func (c *Cursor) MoveDown(lines int) *Cursor { return c.write(csi(lines, 'B')) }

// MoveRight moves the cursor right columns columns.
func (c *Cursor) MoveRight(columns int) *Cursor { return c.write(csi(columns, 'C')) }

// MoveLeft moves the cursor left columns columns.
func (c *Cursor) MoveLeft(columns int) *Cursor { return c.write(csi(columns, 'D')) }

// MoveToColumn moves the cursor to a column.
func (c *Cursor) MoveToColumn(column int) *Cursor { return c.write(csi(column, 'G')) }

// MoveToPosition moves the cursor to a column and (zero based) row.
func (c *Cursor) MoveToPosition(column, row int) *Cursor {
	return c.write("\x1b[" + strconv.Itoa(row+1) + ";" + strconv.Itoa(column) + "H")
}

// SavePosition saves the cursor position.
func (c *Cursor) SavePosition() *Cursor { return c.write("\x1b7") }

// RestorePosition restores the saved cursor position.
func (c *Cursor) RestorePosition() *Cursor { return c.write("\x1b8") }

// Hide hides the cursor.
func (c *Cursor) Hide() *Cursor { return c.write("\x1b[?25l") }

// Show shows the cursor.
func (c *Cursor) Show() *Cursor { return c.write("\x1b[?25h\x1b[?0c") }

// ClearLine clears the current line.
func (c *Cursor) ClearLine() *Cursor { return c.write("\x1b[2K") }

// ClearLineAfter clears the current line after the cursor.
func (c *Cursor) ClearLineAfter() *Cursor { return c.write("\x1b[K") }

// ClearOutput clears the output from the cursor to the end of the screen.
func (c *Cursor) ClearOutput() *Cursor { return c.write("\x1b[0J") }

// ClearScreen clears the whole screen.
func (c *Cursor) ClearScreen() *Cursor { return c.write("\x1b[2J") }

// cursorTTY caches whether /dev/tty can be opened (PHP's static
// $isTtySupported, probed with proc_open on /dev/tty).
var cursorTTY = sync.OnceValue(func() bool {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	_ = f.Close()

	return true
})

// CurrentPosition returns the cursor column and row, asking the terminal
// with "\033[6n". Without a tty it is (1, 1). A reply that cannot be parsed
// yields zeros (PHP's nulls from sscanf).
func (c *Cursor) CurrentPosition() (col, row int) {
	if !cursorTTY() {
		return 1, 1
	}

	sttyMode := shellExec("stty -g")
	shellExec("stty -icanon -echo")

	if w, ok := c.input.(io.Writer); ok {
		_, _ = io.WriteString(w, "\033[6n")
	}

	buf := make([]byte, 1024)
	n, _ := c.input.Read(buf)
	code := strings.Trim(string(buf[:n]), " \t\n\r\x00\x0B")

	shellExec("stty " + sttyMode)

	row, col = scanCursorReply(code)

	return col, row
}

// shellExec is shell_exec(): the command runs through /bin/sh with the
// process's stdin and stderr, and its stdout is returned.
func shellExec(command string) string {
	cmd := exec.Command("/bin/sh", "-c", command) //nolint:gosec // fixed stty commands, as in PHP
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	out, _ := cmd.Output()

	return string(out)
}

// scanCursorReply is sscanf($code, "\033[%d;%dR", $row, $col).
func scanCursorReply(code string) (row, col int) {
	rest, ok := strings.CutPrefix(code, "\033[")
	if !ok {
		return 0, 0
	}
	row, rest, ok = scanInt(rest)
	if !ok {
		return 0, 0
	}
	rest, ok = strings.CutPrefix(rest, ";")
	if !ok {
		return row, 0
	}
	col, _, _ = scanInt(rest)

	return row, col
}

// scanInt reads sscanf's %d: optional whitespace, sign and digits.
func scanInt(s string) (int, string, bool) {
	s = strings.TrimLeft(s, " \t\n\r\v\f")
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	j := i
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i {
		return 0, s, false
	}
	n, _ := strconv.Atoi(s[:j])

	return n, s[j:], true
}
