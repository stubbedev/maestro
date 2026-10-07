// Ports src/Style/SymfonyStyle.php (symfony/console).
//
// Not ported here yet: table(), horizontalTable(), definitionList(),
// createTable() and the progress*() methods, which depend on Table and
// ProgressBar.

package console

import (
	"iter"
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// SymfonyStyleMaxLineLength is SymfonyStyle::MAX_LINE_LENGTH.
const SymfonyStyleMaxLineLength = 120

// SymfonyStyle is the output decorator of the Symfony Style Guide.
type SymfonyStyle struct {
	*OutputStyle
	input          Input
	questionHelper *SymfonyQuestionHelper
	lineLength     int
	bufferedOutput *TrimmedBufferOutput
	progressBar    *ProgressBar
}

// NewSymfonyStyle mirrors new SymfonyStyle($input, $output).
func NewSymfonyStyle(in Input, out Output) *SymfonyStyle {
	// The history holds the last two line breaks: two PHP_EOLs, 4 bytes on
	// Windows (DIRECTORY_SEPARATOR === '\\').
	historyLength, windows := 2, 0
	if php.IsWindows() {
		historyLength, windows = 4, 1
	}
	buffered, _ := NewTrimmedBufferOutput(historyLength, out.Verbosity(), false, cloneFormatter(out.Formatter()))
	width := Terminal{}.Width()
	if width == 0 {
		width = SymfonyStyleMaxLineLength
	}

	return &SymfonyStyle{
		OutputStyle: NewOutputStyle(out),
		input:       in,
		// Windows cmd wraps lines as soon as the terminal width is
		// reached, whether there are following chars or not.
		lineLength:     min(width-windows, SymfonyStyleMaxLineLength),
		bufferedOutput: buffered,
	}
}

// cloneFormatter is PHP's clone of a formatter.
func cloneFormatter(f Formatter) Formatter {
	switch x := f.(type) {
	case *OutputFormatter:
		return x.Clone()
	case *HTMLOutputFormatter:
		return &HTMLOutputFormatter{OutputFormatter: x.Clone()}
	case *NullOutputFormatter:
		c := *x

		return &c
	}

	return f
}

// Block formats a message as a block of text. An empty typ or style means
// none (PHP null).
func (s *SymfonyStyle) Block(messages []string, typ, style, prefix string, padding, escape bool) {
	s.autoPrependBlock()
	WriteMessages(s, s.createBlock(messages, typ, style, prefix, padding, escape), true, OutputNormal)
	s.NewLine(1)
}

// Title formats a command title.
func (s *SymfonyStyle) Title(message string) {
	s.autoPrependBlock()
	s.writeHeading(message, "=")
}

// Section formats a section title.
func (s *SymfonyStyle) Section(message string) {
	s.autoPrependBlock()
	s.writeHeading(message, "-")
}

func (s *SymfonyStyle) writeHeading(message, underline string) {
	WriteMessages(s, []string{
		"<comment>" + EscapeTrailingBackslash(message) + "</>",
		"<comment>" + strings.Repeat(underline, Width(RemoveDecoration(s.Formatter(), message))) + "</>",
	}, true, OutputNormal)
	s.NewLine(1)
}

// Listing formats a list.
func (s *SymfonyStyle) Listing(elements []string) {
	s.autoPrependText()
	for _, e := range elements {
		s.Writeln(" * " + e)
	}
	s.NewLine(1)
}

// Text formats informational text.
func (s *SymfonyStyle) Text(messages ...string) {
	s.autoPrependText()
	for _, m := range messages {
		s.Writeln(" " + m)
	}
}

// Comment formats a command comment.
func (s *SymfonyStyle) Comment(messages ...string) {
	s.Block(messages, "", "", "<fg=default;bg=default> // </>", false, false)
}

// Success formats a success result bar.
func (s *SymfonyStyle) Success(messages ...string) {
	s.Block(messages, "OK", "fg=black;bg=green", " ", true, true)
}

// Error formats an error result bar.
func (s *SymfonyStyle) Error(messages ...string) {
	s.Block(messages, "ERROR", "fg=white;bg=red", " ", true, true)
}

// Warning formats a warning result bar.
func (s *SymfonyStyle) Warning(messages ...string) {
	s.Block(messages, "WARNING", "fg=black;bg=yellow", " ", true, true)
}

// Note formats a note admonition.
func (s *SymfonyStyle) Note(messages ...string) {
	s.Block(messages, "NOTE", "fg=yellow", " ! ", false, true)
}

// Info formats an info message.
func (s *SymfonyStyle) Info(messages ...string) {
	s.Block(messages, "INFO", "fg=green", " ", true, true)
}

// Caution formats a caution admonition.
func (s *SymfonyStyle) Caution(messages ...string) {
	s.Block(messages, "CAUTION", "fg=white;bg=red", " ! ", true, true)
}

// Ask asks a question; def is nil or a string.
func (s *SymfonyStyle) Ask(question string, def any, validator Validator) (any, error) {
	q := NewQuestion(question, def)
	q.SetValidator(validator)

	return s.AskQuestion(q)
}

// AskHidden asks a question with the user input hidden.
func (s *SymfonyStyle) AskHidden(question string, validator Validator) (any, error) {
	q := NewQuestion(question, nil)
	_ = q.SetHidden(true) // no autocompleter is set
	q.SetValidator(validator)

	return s.AskQuestion(q)
}

// Confirm asks for confirmation.
func (s *SymfonyStyle) Confirm(question string, def bool) (bool, error) {
	answer, err := s.AskQuestion(NewConfirmationQuestion(question, def, nil))
	if err != nil {
		return false, err
	}

	return php.ToBool(answer), nil
}

// Choice asks a choice question; a default given as a choice value is
// replaced by its key.
func (s *SymfonyStyle) Choice(question string, choices *php.Array, def any) (any, error) {
	if def != nil {
		if k, ok := php.ArrayFlip(choices).Get(def); ok {
			def = k
		}
	}

	q, err := NewChoiceQuestion(question, choices, def)
	if err != nil {
		return nil, err
	}

	return s.AskQuestion(q)
}

// AskQuestion asks a question with the SymfonyQuestionHelper.
func (s *SymfonyStyle) AskQuestion(q Questioner) (any, error) {
	if s.input.IsInteractive() {
		s.autoPrependBlock()
	}

	if s.questionHelper == nil {
		s.questionHelper = NewSymfonyQuestionHelper()
	}

	answer, err := s.questionHelper.Ask(s.input, s, q)
	if err != nil {
		return nil, err
	}

	if s.input.IsInteractive() {
		s.NewLine(1)
		s.bufferedOutput.Write("\n", false, OutputNormal)
	}

	return answer, nil
}

// Writeln implements Output.
func (s *SymfonyStyle) Writeln(message string) {
	s.Write(message, true, OutputNormal)
}

// Write implements Output.
func (s *SymfonyStyle) Write(message string, newline bool, options int) {
	s.OutputStyle.Write(message, newline, options)
	s.writeBuffer(message, newline, options)
}

// NewLine writes count line breaks.
func (s *SymfonyStyle) NewLine(count int) {
	s.OutputStyle.NewLine(count)
	s.bufferedOutput.Write(strings.Repeat("\n", max(0, count)), false, OutputNormal)
}

// ErrorStyle returns a SymfonyStyle on the error output.
func (s *SymfonyStyle) ErrorStyle() *SymfonyStyle {
	return NewSymfonyStyle(s.input, s.errorOutput())
}

func (s *SymfonyStyle) autoPrependBlock() {
	chars := php.NormalizeEOL(s.bufferedOutput.Fetch())
	if len(chars) > 2 {
		chars = chars[len(chars)-2:]
	}

	if chars == "" {
		s.NewLine(1) // empty history, so we should start with a new line.

		return
	}
	// Prepend new line for each non LF chars (This means no blank line was output before)
	s.NewLine(2 - strings.Count(chars, "\n"))
}

func (s *SymfonyStyle) autoPrependText() {
	fetched := s.bufferedOutput.Fetch()
	// Prepend new line if last char isn't EOL:
	if !strings.HasSuffix(fetched, "\n") {
		s.NewLine(1)
	}
}

func (s *SymfonyStyle) writeBuffer(message string, newline bool, options int) {
	// We need to know if the last chars are PHP_EOL
	s.bufferedOutput.Write(message, newline, options)
}

func (s *SymfonyStyle) createBlock(messages []string, typ, style, prefix string, padding, escape bool) []string {
	indentLength := 0
	prefixLength := Width(RemoveDecoration(s.Formatter(), prefix))
	lines := make([]string, 0, len(messages)+2)

	var lineIndentation string
	if typ != "" {
		typ = "[" + typ + "] "
		indentLength = len(typ)
		lineIndentation = strings.Repeat(" ", indentLength)
	}

	// wrap and add newlines for each element
	for key, message := range messages {
		if escape {
			message = Escape(message)
		}

		decorationLength := Width(message) - Width(RemoveDecoration(s.Formatter(), message))
		messageLineLength := min(s.lineLength-prefixLength-indentLength+decorationLength, s.lineLength)
		lines = append(lines, strings.Split(php.Wordwrap(message, messageLineLength, php.EOL, true), php.EOL)...)

		if len(messages) > 1 && key < len(messages)-1 {
			lines = append(lines, "")
		}
	}

	firstLineIndex := 0
	if padding && s.IsDecorated() {
		firstLineIndex = 1
		lines = append([]string{""}, lines...)
		lines = append(lines, "")
	}

	for i, line := range lines {
		if typ != "" {
			if firstLineIndex == i {
				line = typ + line
			} else {
				line = lineIndentation + line
			}
		}

		line = prefix + line
		line += strings.Repeat(" ", max(s.lineLength-Width(RemoveDecoration(s.Formatter(), line)), 0))

		if style != "" {
			line = "<" + style + ">" + line + "</>"
		}
		lines[i] = line
	}

	return lines
}

// Table formats a table.
func (s *SymfonyStyle) Table(headers []any, rows []any) error {
	return s.renderTable(false, headers, rows)
}

// HorizontalTable formats a horizontal table.
func (s *SymfonyStyle) HorizontalTable(headers []any, rows []any) error {
	return s.renderTable(true, headers, rows)
}

func (s *SymfonyStyle) renderTable(horizontal bool, headers, rows []any) error {
	table := s.CreateTable().SetHorizontal(horizontal).SetHeaders(headers)
	if err := table.SetRows(rows); err != nil {
		return err
	}
	if err := table.Render(); err != nil {
		return err
	}

	s.NewLine(1)

	return nil
}

// DefinitionList formats a list of key/value pairs horizontally. Each item
// is a title (string), a *TableSeparator, or a *php.Array whose first entry
// is the key/value pair.
func (s *SymfonyStyle) DefinitionList(list ...any) error {
	headers := make([]any, 0, len(list))
	row := make([]any, 0, len(list))
	for _, value := range list {
		switch v := value.(type) {
		case *TableSeparator:
			headers = append(headers, v)
			row = append(row, v)
		case string:
			headers = append(headers, NewTableCell(v, TableCellOptions{Colspan: 2}))
			row = append(row, nil)
		case *php.Array:
			// key()/current() of an empty array are null and false.
			var key, current any = nil, false
			if k, val, ok := v.First(); ok {
				key, current = k.Value(), val
			}
			headers = append(headers, key)
			row = append(row, current)
		default:
			return newError(KindInvalidArgument, "Value should be an array, string, or an instance of TableSeparator.")
		}
	}

	return s.HorizontalTable(headers, []any{row})
}

// CreateTable returns a table in the Symfony style guide style. PHP renders
// it into a new ConsoleSectionOutput of a ConsoleOutput; a section shares the
// stream, verbosity, decoration and formatter, and writes the same bytes for
// the writeln() calls a table makes, so the output itself is used.
func (s *SymfonyStyle) CreateTable() *Table {
	def, _ := TableStyleDefinition("symfony-style-guide")
	style := def.Clone().SetCellHeaderFormat("<info>%s</info>")

	return NewTable(s.output).SetStyleObject(style)
}

// CreateProgressBar returns a progress bar with the shaded bar characters.
func (s *SymfonyStyle) CreateProgressBar(maxSteps int) *ProgressBar {
	bar := NewProgressBar(s.output, maxSteps, DefaultMinSecondsBetweenRedraws)
	if !php.IsWindows() || os.Getenv("TERM_PROGRAM") == "Hyper" {
		bar.SetEmptyBarCharacter("░") // light shade character \u2591
		bar.SetProgressCharacter("")
		bar.SetBarCharacter("▓") // dark shade character \u2593
	}

	return bar
}

// ProgressStart starts a progress bar with maxSteps steps (0 if
// indeterminate).
func (s *SymfonyStyle) ProgressStart(maxSteps int) {
	s.progressBar = s.CreateProgressBar(maxSteps)
	s.progressBar.Start()
}

// ProgressAdvance advances the started progress bar.
func (s *SymfonyStyle) ProgressAdvance(step int) error {
	if s.progressBar == nil {
		return errProgressNotStarted()
	}
	s.progressBar.Advance(step)

	return nil
}

// ProgressFinish finishes the started progress bar.
func (s *SymfonyStyle) ProgressFinish() error {
	if s.progressBar == nil {
		return errProgressNotStarted()
	}
	s.progressBar.Finish()
	s.NewLine(2)
	s.progressBar = nil

	return nil
}

func errProgressNotStarted() *Error {
	return newError(KindRuntime, "The ProgressBar is not started.")
}

// SymfonyStyleProgressIterate iterates over seq while showing a progress bar
// with maxSteps steps (0 if indeterminate), then writes two newlines
// (SymfonyStyle::progressIterate()).
func SymfonyStyleProgressIterate[K, V any](s *SymfonyStyle, seq iter.Seq2[K, V], maxSteps int) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for k, v := range ProgressIterate(s.CreateProgressBar(0), seq, maxSteps) {
			if !yield(k, v) {
				return
			}
		}
		s.NewLine(2)
	}
}
