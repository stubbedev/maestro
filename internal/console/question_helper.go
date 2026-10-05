// Ports src/Helper/QuestionHelper.php and SymfonyQuestionHelper.php
// (symfony/console).
//
// Input streams. PHP reads answers from the input's stream resource (STDIN
// by default) through its own buffer, so consecutive questions continue
// where the previous read stopped. Here a stream is any io.Reader and is
// read byte by byte (with ReadByte when it is an io.ByteReader), so nothing
// is read ahead and lost between questions or for other readers of the
// same stream. Multiline answers are read from a "clone" of the stream as
// PHP does: for a seekable stream other than an *os.File the read position
// is restored afterwards; other streams (and files, whose PHP clone is a
// dup'd descriptor sharing the offset) are consumed.
//
// Left out: Windows code page switching (sapi_windows_cp_*), the Windows
// hiddeninput.exe helper and ConsoleSectionOutput content tracking.

package console

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/rivo/uniseg"

	"github.com/stubbedev/maestro/internal/php"
)

// QuestionHelper asks questions on the error output and reads the answers
// from the input stream.
type QuestionHelper struct {
	HelperBase
	inputStream io.Reader
	// symfony selects SymfonyQuestionHelper's writePrompt and writeError.
	symfony bool
}

// SymfonyQuestionHelper is the Symfony Style Guide compliant question
// helper.
type SymfonyQuestionHelper struct {
	QuestionHelper
}

// NewQuestionHelper mirrors new QuestionHelper().
func NewQuestionHelper() *QuestionHelper { return &QuestionHelper{} }

// NewSymfonyQuestionHelper mirrors new SymfonyQuestionHelper().
func NewSymfonyQuestionHelper() *SymfonyQuestionHelper {
	return &SymfonyQuestionHelper{QuestionHelper{symfony: true}}
}

// Name implements Helper.
func (*QuestionHelper) Name() string { return "question" }

// sttyEnabled is QuestionHelper::$stty.
var sttyEnabled atomic.Bool

func init() { sttyEnabled.Store(true) }

// DisableStty prevents the use of stty (QuestionHelper::disableStty()).
func DisableStty() { sttyEnabled.Store(false) }

// Hooks replaced by tests: whether stty can be used (Terminal::hasSttyAvailable)
// and running it (shell_exec('stty ...')) with the process stdin.
var (
	sttyAvailable = HasSttyAvailable
	runStty       = func(args ...string) string {
		cmd := exec.Command("stty", args...) //nolint:gosec // fixed binary; the arguments are stty modes.
		cmd.Stdin = os.Stdin
		cmd.Stderr = os.Stderr
		out, _ := cmd.Output()

		return string(out)
	}
)

func canUseStty() bool { return sttyEnabled.Load() && sttyAvailable() }

// Ask asks a question; the answer is normalized and validated, in the
// internal/php value model.
func (h *QuestionHelper) Ask(in Input, out Output, q Questioner) (any, error) {
	out = ErrorOutputOf(out)

	if !in.IsInteractive() {
		return h.defaultAnswer(q)
	}

	if stream := in.Stream(); stream != nil {
		h.inputStream = stream
	}

	var answer any
	var err error
	if q.Q().Validator() == nil {
		answer, err = h.doAsk(out, q)
	} else {
		answer, err = h.validateAttempts(out, q)
	}

	var e *Error
	if err != nil && errors.As(err, &e) && e.Kind == KindMissingInput {
		in.SetInteractive(false)

		fallback, ferr := h.defaultAnswer(q)
		if ferr != nil {
			return nil, ferr
		}
		if fallback == nil {
			return nil, err
		}

		return fallback, nil
	}

	return answer, err
}

func missingInput(line int) error {
	return newError(KindMissingInput, "QuestionHelper.php", line, "Aborted.")
}

func (h *QuestionHelper) doAsk(out Output, q Questioner) (any, error) {
	h.writePrompt(out, q)

	question := q.Q()
	inputStream := h.inputStream
	if inputStream == nil {
		inputStream = os.Stdin
	}
	autocomplete := question.AutocompleterCallback()

	var ret string
	if autocomplete == nil || !canUseStty() {
		got := false
		if question.IsHidden() {
			hidden, err := h.hiddenResponse(out, inputStream, question.IsTrimmable())
			var e *Error
			switch {
			case err == nil:
				ret, got = hidden, true
				if question.IsTrimmable() {
					ret = php.Trim(ret)
				}
			case !errors.As(err, &e) || (e.Kind != KindRuntime && e.Kind != KindMissingInput) || !question.IsHiddenFallback():
				return nil, err
			}
		}

		if !got {
			line, ok, err := readInput(inputStream, question)
			if err != nil {
				return nil, err
			}
			if !ok {
				return nil, missingInput(144)
			}
			ret = line
			if question.IsTrimmable() {
				ret = php.Trim(ret)
			}
		}
	} else {
		full, err := h.autocomplete(out, q, inputStream, autocomplete)
		if err != nil {
			return nil, err
		}
		ret = full
		if question.IsTrimmable() {
			ret = php.Trim(ret)
		}
	}

	var answer any = ret
	if ret == "" {
		answer = question.Default()
	}

	if normalizer := question.Normalizer(); normalizer != nil {
		return normalizer(answer), nil
	}

	return answer, nil
}

func (h *QuestionHelper) defaultAnswer(q Questioner) (any, error) {
	question := q.Q()
	def := question.Default()

	if def == nil {
		return nil, nil
	}

	if validator := question.Validator(); validator != nil {
		return validator(def)
	}

	cq, ok := q.(*ChoiceQuestion)
	if !ok {
		return def, nil
	}
	choices := cq.Choices()

	if !cq.IsMultiselect() {
		if v, ok := choiceAt(choices, def); ok {
			return v, nil
		}

		return def, nil
	}

	parts := strings.Split(php.ToString(def), ",")
	list := php.NewArrayCap(len(parts))
	for _, v := range parts {
		if cq.IsTrimmable() {
			v = php.Trim(v)
		}
		if c, ok := choiceAt(choices, v); ok {
			list.Append(c)
		} else {
			list.Append(v)
		}
	}

	return list, nil
}

func (h *QuestionHelper) writePrompt(out Output, q Questioner) {
	if h.symfony {
		h.writeSymfonyPrompt(out, q)

		return
	}

	message := q.Q().Question()

	if cq, ok := q.(*ChoiceQuestion); ok {
		out.Writeln(cq.Question.Question())
		WriteMessages(out, formatChoiceQuestionChoices(cq, "info"), true, OutputNormal)

		message = cq.Prompt()
	}

	out.Write(message, false, OutputNormal)
}

func (h *QuestionHelper) writeSymfonyPrompt(out Output, q Questioner) {
	question := q.Q()
	text := EscapeTrailingBackslash(question.Question())
	def := question.Default()

	if question.IsMultiline() {
		text += " (press " + eofShortcut() + " to continue)"
	}

	cq, isChoice := q.(*ChoiceQuestion)
	_, isConfirmation := q.(*ConfirmationQuestion)
	switch {
	case def == nil:
		text = " <info>" + text + "</info>:"
	case isConfirmation:
		d := "no"
		if php.ToBool(def) {
			d = "yes"
		}
		text = " <info>" + text + " (yes/no)</info> [<comment>" + d + "</comment>]:"
	case isChoice && cq.IsMultiselect():
		choices := cq.Choices()
		parts := strings.Split(php.ToString(def), ",")
		for i, v := range parts {
			c, _ := choices.Get(php.Trim(v))
			parts[i] = php.ToString(c)
		}
		text = " <info>" + text + "</info> [<comment>" + Escape(strings.Join(parts, ", ")) + "</comment>]:"
	case isChoice:
		shown := def
		if c, ok := choiceAt(cq.Choices(), def); ok {
			shown = c
		}
		text = " <info>" + text + "</info> [<comment>" + Escape(php.ToString(shown)) + "</comment>]:"
	default:
		text = " <info>" + text + "</info> [<comment>" + Escape(php.ToString(def)) + "</comment>]:"
	}

	out.Writeln(text)

	prompt := " > "

	if isChoice {
		WriteMessages(out, formatChoiceQuestionChoices(cq, "comment"), true, OutputNormal)

		prompt = cq.Prompt()
	}

	out.Write(prompt, false, OutputNormal)
}

// eofShortcut is SymfonyQuestionHelper::getEofShortcut() on Unix.
func eofShortcut() string { return "<comment>Ctrl+D</comment>" }

// formatChoiceQuestionChoices renders the choices as "  [<tag>key</tag>] value".
func formatChoiceQuestionChoices(q *ChoiceQuestion, tag string) []string {
	choices := q.Choices()
	maxWidth := 0
	for k := range choices.All() {
		maxWidth = max(maxWidth, Width(k.String()))
	}

	messages := make([]string, 0, choices.Len())
	for k, v := range choices.All() {
		key := k.String()
		padding := strings.Repeat(" ", max(0, maxWidth-Width(key)))
		messages = append(messages, "  [<"+tag+">"+key+padding+"</"+tag+">] "+php.ToString(v))
	}

	return messages
}

// blockFormatter is the formatter helper's formatBlock.
type blockFormatter interface {
	FormatBlock(messages []string, style string, large bool) string
}

func (h *QuestionHelper) writeError(out Output, err error) {
	if h.symfony {
		if style, ok := out.(*SymfonyStyle); ok {
			style.NewLine(1)
			style.Error(err.Error())

			return
		}
	}

	var message string
	if set := h.HelperSet(); set != nil && set.Has("formatter") {
		helper, _ := set.Get("formatter")
		if f, ok := helper.(blockFormatter); ok {
			message = f.FormatBlock([]string{err.Error()}, "error", false)
		}
	} else {
		message = "<error>" + err.Error() + "</error>"
	}

	out.Writeln(message)
}

// streamReader reads a stream byte by byte, tracking end of file like
// feof() does: it is reached once a read returns nothing.
type streamReader struct {
	r   io.Reader
	eof bool
}

func (s *streamReader) readByte() (byte, bool) {
	if br, ok := s.r.(io.ByteReader); ok {
		c, err := br.ReadByte()
		if err != nil {
			s.eof = true

			return 0, false
		}

		return c, true
	}
	var b [1]byte
	for {
		n, err := s.r.Read(b[:])
		if n == 1 {
			return b[0], true
		}
		if err != nil {
			s.eof = true

			return 0, false
		}
	}
}

// read is fread($stream, $n): up to n bytes, fewer at end of file.
func (s *streamReader) read(n int) string {
	buf := make([]byte, 0, n)
	for range n {
		c, ok := s.readByte()
		if !ok {
			break
		}
		buf = append(buf, c)
	}

	return string(buf)
}

// fgets is fgets($stream, 4096): a line including its "\n", at most 4095
// bytes; false at end of file.
func fgets(r io.Reader) (string, bool) {
	s := &streamReader{r: r}
	buf := make([]byte, 0, 64)
	for len(buf) < 4095 {
		c, ok := s.readByte()
		if !ok {
			break
		}
		buf = append(buf, c)
		if c == '\n' {
			break
		}
	}
	if len(buf) == 0 {
		return "", false
	}

	return string(buf), true
}

// autocomplete ports QuestionHelper::autocomplete().
func (h *QuestionHelper) autocomplete(out Output, q Questioner, inputStream io.Reader, autocomplete func(string) []string) (string, error) {
	cur := NewCursor(out, inputStream)
	in := &streamReader{r: inputStream}
	question := q.Q()
	cq, isChoice := q.(*ChoiceQuestion)
	multiselect := isChoice && cq.IsMultiselect()

	fullChoice := ""
	ret := ""

	i := 0
	ofs := -1
	// matches is a PHP array: keys survive array_filter() and stale
	// entries survive partial overwrites.
	matches := listMap(autocomplete(ret))
	numMatches := len(matches)

	sttyMode := runStty("-g")

	// Disable icanon (so we can fread each keypress) and echo (we'll do echoing here instead)
	runStty("-icanon", "-echo")
	restore := func() { runStty(strings.Fields(sttyMode)...) }

	// Add highlighted text style
	out.Formatter().SetStyle("hl", MustStyle("black", "white"))

	// Read a keypress
	for !in.eof {
		c := in.read(1)

		// as opposed to fgets(), fread() returns an empty string when the stream content is empty, not false.
		switch {
		case ret == "" && c == "" && question.Default() == nil:
			restore()

			return "", missingInput(287)
		case c == "\177": // Backspace Character
			if numMatches == 0 && i != 0 {
				i--
				cur.MoveLeft(Width(lastGrapheme(fullChoice)))

				fullChoice = Substr(fullChoice, 0, i, false)
			}

			if i == 0 {
				ofs = -1
				matches = listMap(autocomplete(ret))
				numMatches = len(matches)
			} else {
				numMatches = 0
			}

			// Pop the last character off the end of our string
			ret = Substr(ret, 0, i, false)
		case c == "\033":
			// Did we read an escape sequence?
			c += in.read(2)

			// A = Up Arrow. B = Down Arrow
			if len(c) > 2 && (c[2] == 'A' || c[2] == 'B') {
				if c[2] == 'A' && ofs == -1 {
					ofs = 0
				}

				if numMatches == 0 {
					continue
				}

				if c[2] == 'A' {
					ofs--
				} else {
					ofs++
				}
				ofs = (numMatches + ofs) % numMatches
			}
		case c == "" || c[0] < 32:
			if c == "\t" || c == "\n" {
				if numMatches > 0 && ofs != -1 {
					ret = matches[ofs]
					// Echo out remaining chars for current match
					remainingCharacters := php.Substr(ret, len(php.Trim(mostRecentlyEnteredValue(fullChoice))))
					out.Write(remainingCharacters, false, OutputNormal)
					fullChoice += remainingCharacters
					if utf8.ValidString(fullChoice) {
						i = utf8.RuneCountInString(fullChoice)
					} else {
						i = len(fullChoice)
					}

					filtered := map[int]string{}
					for k, match := range autocomplete(ret) {
						if ret == "" || strings.HasPrefix(match, ret) {
							filtered[k] = match
						}
					}
					matches = filtered
					// PHP recounts numMatches here, but every path below
					// returns or resets it to 0.
					ofs = -1
				}

				if c == "\n" {
					out.Write(c, false, OutputNormal)

					restore()

					return fullChoice, nil
				}

				numMatches = 0
			}

			continue
		default:
			if c[0] >= 0x80 {
				switch c[0] & 0xF0 {
				case 0xC0, 0xD0:
					c += in.read(1)
				case 0xE0:
					c += in.read(2)
				case 0xF0:
					c += in.read(3)
				}
			}

			out.Write(c, false, OutputNormal)
			ret += c
			fullChoice += c
			i++

			tempRet := ret

			if multiselect {
				tempRet = mostRecentlyEnteredValue(fullChoice)
			}

			numMatches = 0
			ofs = 0

			for _, value := range autocomplete(ret) {
				// If typed characters match the beginning chunk of value (e.g. [AcmeDe]moBundle)
				if strings.HasPrefix(value, tempRet) {
					matches[numMatches] = value
					numMatches++
				}
			}
		}

		cur.ClearLineAfter()

		if numMatches > 0 && ofs != -1 {
			cur.SavePosition()
			// Write highlighted text, complete the partially entered response
			charactersEntered := len(php.Trim(mostRecentlyEnteredValue(fullChoice)))
			out.Write("<hl>"+EscapeTrailingBackslash(php.Substr(matches[ofs], charactersEntered))+"</hl>", false, OutputNormal)
			cur.RestorePosition()
		}
	}

	// Reset stty so it behaves normally again
	restore()

	return fullChoice, nil
}

func listMap(list []string) map[int]string {
	m := make(map[int]string, len(list))
	for i, v := range list {
		m[i] = v
	}

	return m
}

// lastGrapheme is s($s)->slice(-1): the last grapheme cluster of valid
// UTF-8, the last byte otherwise.
func lastGrapheme(s string) string {
	if s == "" {
		return ""
	}
	if !utf8.ValidString(s) {
		return s[len(s)-1:]
	}
	last := ""
	g := uniseg.NewGraphemes(s)
	for g.Next() {
		last = g.Str()
	}

	return last
}

func mostRecentlyEnteredValue(entered string) string {
	// Determine the most recent value that the user entered
	if !strings.Contains(entered, ",") {
		return entered
	}

	choices := strings.Split(entered, ",")
	if lastChoice := php.Trim(choices[len(choices)-1]); lastChoice != "" {
		return lastChoice
	}

	return entered
}

// hiddenResponse ports getHiddenResponse() on Unix.
func (h *QuestionHelper) hiddenResponse(out Output, inputStream io.Reader, trimmable bool) (string, error) {
	useStty := canUseStty()
	var sttyMode string
	if useStty {
		sttyMode = runStty("-g")
		runStty("-echo")
	} else if isInteractiveInput(inputStream) {
		return "", newError(KindRuntime, "QuestionHelper.php", 446, "Unable to hide the response.")
	}

	value, ok := fgets(inputStream)

	if useStty {
		runStty(strings.Fields(sttyMode)...)
	}

	if !ok {
		return "", missingInput(456)
	}
	if trimmable {
		value = php.Trim(value)
	}
	out.Writeln("")

	return value, nil
}

// validateAttempts asks until the validator accepts the answer or the
// attempts are exhausted. Symfony RuntimeExceptions (including
// MissingInputException) end it at once.
func (h *QuestionHelper) validateAttempts(out Output, q Questioner) (any, error) {
	question := q.Q()
	var lastErr error
	attempts := question.MaxAttempts()
	unlimited := attempts == 0

	for unlimited || attempts > 0 {
		attempts--
		if lastErr != nil {
			h.writeError(out, lastErr)
		}

		answer, err := h.doAsk(out, q)
		if err == nil {
			answer, err = question.Validator()(answer)
			if err == nil {
				return answer, nil
			}
		}
		var e *Error
		if errors.As(err, &e) && (e.Kind == KindRuntime || e.Kind == KindMissingInput) {
			return nil, err
		}
		lastErr = err
	}

	return nil, lastErr
}

var stdinIsInteractive = sync.OnceValue(func() bool { return IsTTY(os.Stdin) })

// isInteractiveInput reports whether the stream is the process stdin and a
// terminal.
func isInteractiveInput(stream io.Reader) bool {
	if f, ok := stream.(*os.File); !ok || f != os.Stdin {
		return false
	}

	return stdinIsInteractive()
}

// readInput reads one line, or for a multiline question everything up to
// end of file (stopping early only on a leading newline). ok is false
// where PHP returns false.
func readInput(inputStream io.Reader, question *Question) (string, bool, error) {
	if !question.IsMultiline() {
		line, ok := fgets(inputStream)

		return line, ok, nil
	}

	restore, err := cloneInputStream(inputStream)
	if err != nil {
		return "", false, nil //nolint:nilerr // a stream that cannot be cloned reads as false
	}

	in := &streamReader{r: inputStream}
	var ret []byte
	for {
		c, ok := in.readByte()
		if !ok {
			break
		}
		if len(ret) == 0 && c == '\n' {
			break
		}
		ret = append(ret, c)
	}

	if restore != nil {
		if err := restore(); err != nil {
			return "", false, err
		}
	}

	return string(ret), true, nil
}

// cloneInputStream emulates reading from a clone of a seekable stream: it
// returns a func restoring the current offset. Files and non-seekable
// streams are read directly.
func cloneInputStream(inputStream io.Reader) (func() error, error) {
	if _, ok := inputStream.(*os.File); ok {
		return nil, nil
	}
	seeker, ok := inputStream.(io.Seeker)
	if !ok {
		return nil, nil
	}
	offset, err := seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, err
	}

	return func() error {
		_, err := seeker.Seek(offset, io.SeekStart)

		return err
	}, nil
}
