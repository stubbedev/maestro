// Ports src/Composer/IO/ConsoleIO.php (Composer).

package io

import (
	"fmt"
	"runtime/metrics"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/ui"
)

// HelperGetter is the part of a HelperSet ConsoleIO uses; *console.HelperSet
// implements it.
type HelperGetter interface {
	Get(name string) (console.Helper, error)
}

// HelperRegistrar is the part of a HelperSet ConsoleIO registers a missing
// helper in; *console.HelperSet implements it.
type HelperRegistrar interface {
	Has(name string) bool
	Set(helper console.Helper, alias string)
}

// QuestionAsker is the part of QuestionHelper ConsoleIO uses;
// *console.QuestionHelper implements it.
type QuestionAsker interface {
	Ask(in console.Input, out console.Output, q console.Questioner) (any, error)
}

// ConsoleIO is the IO of a console run: it writes to a console output and
// asks questions through the "question" helper.
type ConsoleIO struct {
	BaseIO
	input          console.Input
	output         console.Output
	helperSet      HelperGetter
	lastMessage    string
	lastMessageErr string

	timestamp func(time.Time) string // nil when timestamps are off
	debugging bool
	startTime time.Time

	osc                   *console.OscHelper
	programStatus         *console.ProgramStatus
	programStatusMu       sync.Mutex
	programStatusEncoders []console.ProgramStatusEncoder

	// recorder records what is written (Record), nil when nothing does
	recorder *Recorder
}

// PHPClass implements php.Classer.
func (*ConsoleIO) PHPClass() string { return `Composer\IO\ConsoleIO` }

// NewConsoleIO mirrors new ConsoleIO($input, $output, $helperSet).
func NewConsoleIO(input console.Input, output console.Output, helperSet HelperGetter) *ConsoleIO {
	c := &ConsoleIO{input: input, output: output, helperSet: helperSet}
	c.osc = oscHelperOf(helperSet)
	c.programStatusEncoders = console.TerminalStatusPolicyFromEnvironment().Encoders()
	c.init(c)

	return c
}

// oscHelperOf is the "osc" helper ConsoleIO needs, registered in the set
// when it has none (the console of the ConsoleIO constructor).
func oscHelperOf(helperSet HelperGetter) *console.OscHelper {
	if helperSet != nil {
		if h, err := helperSet.Get("osc"); err == nil {
			if osc, ok := h.(*console.OscHelper); ok {
				return osc
			}
		}
	}
	osc := console.NewOscHelper()
	if registrar, ok := helperSet.(HelperRegistrar); ok {
		registrar.Set(osc, "")
	}

	return osc
}

// EnableDebugging prefixes every message with the memory usage and the time
// spent since startTime ("[%.1fMiB/%.2fs] "), as --profile does.
func (c *ConsoleIO) EnableDebugging(startTime time.Time) {
	c.debugging = true
	c.startTime = startTime
}

// EnableTimestamps prefixes every message with "[<time>] ". Unlike PHP's
// enableTimestamps($format), layout is a Go time layout; "" selects the
// equivalent of DATE_RFC3339_EXTENDED.
func (c *ConsoleIO) EnableTimestamps(layout string) {
	if layout == "" {
		layout = "2006-01-02T15:04:05.000-07:00"
	}
	c.timestamp = func(t time.Time) string { return t.Format(layout) }
}

// EnableTimestampsFunc is EnableTimestamps with the time formatted by
// format (enableTimestamps($format) from PHP: (new \DateTime())->format()).
func (c *ConsoleIO) EnableTimestampsFunc(format func(time.Time) string) {
	c.timestamp = format
}

// ConsoleOutput returns the output the IO writes to (ConsoleIO's protected
// $output, which EventDispatcher reads for command-class scripts).
func (c *ConsoleIO) ConsoleOutput() console.Output { return c.output }

// ConsoleInput returns the input the IO asks with (ConsoleIO's protected
// $input).
func (c *ConsoleIO) ConsoleInput() console.Input { return c.input }

// IsInteractive implements IO.
func (c *ConsoleIO) IsInteractive() bool { return c.input.IsInteractive() }

// IsDecorated implements IO.
func (c *ConsoleIO) IsDecorated() bool { return c.output.IsDecorated() }

// IsVerbose implements IO.
func (c *ConsoleIO) IsVerbose() bool { return c.output.IsVerbose() }

// IsVeryVerbose implements IO.
func (c *ConsoleIO) IsVeryVerbose() bool { return c.output.IsVeryVerbose() }

// IsDebug implements IO.
func (c *ConsoleIO) IsDebug() bool { return c.output.IsDebug() }

// WriteOsc sends a terminal command to stderr without changing normal
// message output (writeOsc()).
func (c *ConsoleIO) WriteOsc(command, payload string) error {
	return c.osc.Write(c.errorOutput(), command, payload)
}

// IsProgramStatusEnabled is isProgramStatusEnabled().
func (c *ConsoleIO) IsProgramStatusEnabled() bool {
	return len(c.programStatusEncoders) != 0 && c.osc.SupportsOutput(c.errorOutput())
}

// StartProgramStatus is startProgramStatus().
func (c *ConsoleIO) StartProgramStatus() {
	c.programStatusMu.Lock()
	c.programStatus = console.ProgramStatusWorking(nil)
	c.programStatusMu.Unlock()
	for _, encoder := range c.programStatusEncoders {
		if payload := encoder.EncodeStart(); payload != nil {
			_ = c.WriteOsc(encoder.Command(), *payload)
		}
	}
}

// WriteProgramStatus is writeProgramStatus().
func (c *ConsoleIO) WriteProgramStatus(status *console.ProgramStatus) {
	c.programStatusMu.Lock()
	c.programStatus = status
	c.programStatusMu.Unlock()
	for _, encoder := range c.programStatusEncoders {
		_ = c.WriteOsc(encoder.Command(), encoder.Encode(status))
	}
}

// WriteProgress is writeProgress(): the current status, working when there
// is none, with progress (-1 meaning PHP's null).
func (c *ConsoleIO) WriteProgress(progress int) {
	var value *int
	if progress != -1 {
		value = &progress
	}
	c.programStatusMu.Lock()
	status := c.programStatus
	c.programStatusMu.Unlock()
	if status == nil {
		status = console.ProgramStatusWorking(nil)
	}
	c.WriteProgramStatus(status.WithProgress(value))
}

// Write implements IO.
func (c *ConsoleIO) Write(message string, newline bool, verbosity Verbosity) {
	c.doWrite([]string{Sanitize(message, true)}, newline, false, verbosity, false)
}

// WriteMessages implements IO.
func (c *ConsoleIO) WriteMessages(messages []string, newline bool, verbosity Verbosity) {
	c.doWrite(SanitizeMessages(messages, true), newline, false, verbosity, false)
}

// WriteError implements IO.
func (c *ConsoleIO) WriteError(message string, newline bool, verbosity Verbosity) {
	c.doWrite([]string{Sanitize(message, true)}, newline, true, verbosity, false)
}

// WriteErrorMessages implements IO.
func (c *ConsoleIO) WriteErrorMessages(messages []string, newline bool, verbosity Verbosity) {
	c.doWrite(SanitizeMessages(messages, true), newline, true, verbosity, false)
}

// WriteRaw implements IO.
func (c *ConsoleIO) WriteRaw(message string, newline bool, verbosity Verbosity) {
	c.doWrite([]string{message}, newline, false, verbosity, true)
}

// WriteErrorRaw implements IO.
func (c *ConsoleIO) WriteErrorRaw(message string, newline bool, verbosity Verbosity) {
	c.doWrite([]string{message}, newline, true, verbosity, true)
}

// heapBytes stands in for memory_get_usage(): the bytes of live and
// unswept heap objects, read without stopping the world.
func heapBytes() float64 {
	s := []metrics.Sample{{Name: "/memory/classes/heap/objects:bytes"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}

	return float64(s[0].Value.Uint64())
}

func (c *ConsoleIO) doWrite(messages []string, newline, stderr bool, verbosity Verbosity, raw bool) {
	sfVerbosity := consoleVerbosity(verbosity)
	if sfVerbosity > c.output.Verbosity() {
		return
	}
	c.record(recordedStep{kind: stepWrite, messages: messages, newline: newline, stderr: stderr, raw: raw, verbosity: verbosity})

	options := sfVerbosity
	if raw {
		options |= console.OutputRaw
	}

	if c.debugging {
		memoryUsage := heapBytes() / 1024 / 1024
		timeSpent := time.Since(c.startTime).Seconds()
		prefix := fmt.Sprintf("[%.1fMiB/%.2fs] ", memoryUsage, timeSpent)
		for i, m := range messages {
			messages[i] = prefix + m
		}
	}

	if c.timestamp != nil {
		for i, m := range messages {
			// (new \DateTime())->format(...): PHP's default time zone
			messages[i] = "[" + c.timestamp(time.Now().In(php.DefaultTimezone())) + "] " + m
		}
	}

	glue := ""
	if newline {
		glue = "\n"
	}

	if stderr {
		if co, ok := c.output.(console.ConsoleOutputInterface); ok {
			console.WriteMessages(co.ErrorOutput(), messages, newline, options)
			c.lastMessageErr = strings.Join(messages, glue)

			return
		}
	}

	console.WriteMessages(c.output, messages, newline, options)
	c.lastMessage = strings.Join(messages, glue)
}

// Overwrite implements IO.
func (c *ConsoleIO) Overwrite(message string, newline bool, size int, verbosity Verbosity) {
	c.doOverwrite(message, newline, size, false, verbosity)
}

// OverwriteError implements IO.
func (c *ConsoleIO) OverwriteError(message string, newline bool, size int, verbosity Verbosity) {
	c.doOverwrite(message, newline, size, true, verbosity)
}

// OverwriteMessages is overwrite() with an array: the messages are joined
// with "\n" (newline) or "" first.
func (c *ConsoleIO) OverwriteMessages(messages []string, newline bool, size int, verbosity Verbosity) {
	c.doOverwrite(joinMessages(messages, newline), newline, size, false, verbosity)
}

// OverwriteErrorMessages is overwriteError() with an array.
func (c *ConsoleIO) OverwriteErrorMessages(messages []string, newline bool, size int, verbosity Verbosity) {
	c.doOverwrite(joinMessages(messages, newline), newline, size, true, verbosity)
}

func joinMessages(messages []string, newline bool) string {
	if newline {
		return strings.Join(messages, "\n")
	}

	return strings.Join(messages, "")
}

func (c *ConsoleIO) doOverwrite(messages string, newline bool, size int, stderr bool, verbosity Verbosity) {
	var decorated bool
	if stderr {
		decorated = c.errorOutput().IsDecorated()
	} else {
		decorated = c.output.IsDecorated()
	}

	// backspaces corrupt non-decorated output, so write a plain line instead
	if !decorated {
		if messages != "" {
			c.doWrite([]string{messages}, true, stderr, verbosity, false)
		}
		c.setLastMessage(stderr, messages)

		return
	}

	// since overwrite is supposed to overwrite last message...
	if size < 0 {
		// removing possible formatting of lastMessage with strip_tags
		last := c.lastMessage
		if stderr {
			last = c.lastMessageErr
		}
		size = len(php.StripTags(last))
	}
	// ...let's fill its length with backspaces
	c.doWrite([]string{strings.Repeat("\x08", size)}, false, stderr, verbosity, false)

	// write the new message
	c.doWrite([]string{messages}, false, stderr, verbosity, false)

	// In cmd.exe on Win8.1 (possibly 10?), the line can not be cleared, so we need to
	// track the length of previous output and fill it with spaces to make sure the line is cleared.
	// See https://github.com/composer/composer/pull/5836 for more details
	if fill := size - len(php.StripTags(messages)); fill > 0 {
		// whitespace whatever has left
		c.doWrite([]string{strings.Repeat(" ", fill)}, false, stderr, verbosity, false)
		// move the cursor back
		c.doWrite([]string{strings.Repeat("\x08", fill)}, false, stderr, verbosity, false)
	}

	if newline {
		c.doWrite([]string{""}, true, stderr, verbosity, false)
	}

	c.setLastMessage(stderr, messages)
}

func (c *ConsoleIO) setLastMessage(stderr bool, message string) {
	c.record(recordedStep{kind: stepLastMessage, messages: []string{message}, stderr: stderr})
	if stderr {
		c.lastMessageErr = message
	} else {
		c.lastMessage = message
	}
}

func (c *ConsoleIO) questionHelper() (QuestionAsker, error) {
	h, err := c.helperSet.Get("question")
	if err != nil {
		return nil, err
	}
	asker, ok := h.(QuestionAsker)
	if !ok {
		return nil, fmt.Errorf("helper %q cannot ask questions", "question")
	}

	return asker, nil
}

// sanitizeDefault is is_string($default) ? self::sanitize($default) : $default.
func sanitizeDefault(def any) any {
	if s, ok := def.(string); ok {
		return Sanitize(s, true)
	}

	return def
}

func (c *ConsoleIO) ask(q console.Questioner, protocolOptions map[string]map[string]any) (any, error) {
	c.spoil()
	helper, err := c.questionHelper()
	if err != nil {
		return nil, err
	}
	if !c.IsInteractive() {
		return helper.Ask(c.input, c.errorOutput(), q)
	}

	previous := c.currentProgramStatus()
	if previous == nil {
		previous = console.ProgramStatusWorking(nil)
	}
	question := q.Q().Question()
	status := console.ProgramStatusBlocked(&question)
	for protocol, options := range protocolOptions {
		for name, value := range options {
			status = status.WithProtocolOption(protocol, name, value)
		}
	}
	c.WriteProgramStatus(status)
	defer c.WriteProgramStatus(previous)

	return helper.Ask(c.input, c.errorOutput(), q)
}

// askKind is the protocol options of an interactive question of kind (the
// OSC 7501 protocol's kind).
func askKind(kind string) map[string]map[string]any {
	return map[string]map[string]any{console.Osc7501Protocol: {"kind": kind}}
}

func (c *ConsoleIO) currentProgramStatus() *console.ProgramStatus {
	c.programStatusMu.Lock()
	defer c.programStatusMu.Unlock()

	return c.programStatus
}

// Ask implements IO.
func (c *ConsoleIO) Ask(question string, def any) (any, error) {
	return c.ask(console.NewQuestion(Sanitize(question, true), sanitizeDefault(def)), askKind(console.Osc7501KindQuestion))
}

// AskConfirmation implements IO.
func (c *ConsoleIO) AskConfirmation(question string, def bool) (bool, error) {
	answer, err := c.ask(console.NewStrictConfirmationQuestion(Sanitize(question, true), def, nil, nil), askKind(console.Osc7501KindPermission))
	if err != nil {
		return false, err
	}

	return php.ToBool(answer), nil
}

// AskAndValidate implements IO.
func (c *ConsoleIO) AskAndValidate(question string, validator console.Validator, attempts int, def any) (any, error) {
	q := console.NewQuestion(Sanitize(question, true), sanitizeDefault(def))
	q.SetValidator(validator)
	if err := setMaxAttempts(q.Q(), attempts); err != nil {
		return nil, err
	}

	return c.ask(q, askKind(console.Osc7501KindQuestion))
}

// AskAndHideAnswer implements IO.
func (c *ConsoleIO) AskAndHideAnswer(question string) (any, error) {
	q := console.NewQuestion(Sanitize(question, true), nil)
	if err := q.SetHidden(true); err != nil {
		return nil, err
	}

	return c.ask(q, askKind(console.Osc7501KindAuth))
}

// Select implements IO. For a list of choices the answer is the index of
// the choice as a string ("" when it is not found), or a list of them with
// multiselect; for associative choices it is what the question returns.
func (c *ConsoleIO) Select(question string, choices *php.Array, def any, attempts int, errorMessage string, multiselect bool) (any, error) {
	sanitized := php.NewArrayCap(choices.Len())
	for k, v := range choices.All() {
		sanitized.SetKey(k, Sanitize(php.ToString(v), true))
	}
	q, err := console.NewChoiceQuestion(Sanitize(question, true), sanitized, sanitizeDefault(def))
	if err != nil {
		return nil, err
	}
	// IOInterface requires false, and Question requires null or int
	if err := setMaxAttempts(q.Q(), attempts); err != nil {
		return nil, err
	}
	q.SetErrorMessage(errorMessage)
	q.SetMultiselect(multiselect)

	result, err := c.ask(q, askKind(console.Osc7501KindQuestion))
	if err != nil {
		return nil, err
	}

	for k := range choices.All() {
		if k.IsString() {
			return result, nil
		}
	}

	list, ok := result.(*php.Array)
	if !ok {
		if k, found := php.ArraySearch(result, choices, true); found {
			return k.String(), nil
		}

		return "", nil
	}

	results := php.NewArray()
	for index, choice := range choices.All() {
		if php.InArray(choice, list, true) {
			results.Append(index.String())
		}
	}

	return results, nil
}

// ProgressBar returns a progress bar on the error output (getProgressBar).
// On a decorated terminal it is drawn in maestro's style (ui.ProgressBar);
// elsewhere it is Composer's.
func (c *ConsoleIO) ProgressBar(maxSteps int) *console.ProgressBar {
	c.spoil()
	out := c.errorOutput()
	bar := console.NewProgressBar(out, maxSteps, console.DefaultMinSecondsBetweenRedraws)
	if console.IsStyledTerminal(out) {
		s := ui.ProgressBar()
		bar.SetBarCharacter(s.Done)
		bar.SetProgressCharacter(s.Head)
		bar.SetEmptyBarCharacter(s.Todo)
	}

	return bar
}

// Table returns a table on the output (getTable).
func (c *ConsoleIO) Table() *console.Table { return console.NewTable(c.output) }

func (c *ConsoleIO) errorOutput() console.Output {
	if co, ok := c.output.(console.ConsoleOutputInterface); ok {
		return co.ErrorOutput()
	}

	return c.output
}

// setMaxAttempts is $question->setMaxAttempts($attempts) where 0 stands for
// PHP's null (unlimited).
func setMaxAttempts(q *console.Question, attempts int) error {
	if attempts == 0 {
		q.ResetMaxAttempts()

		return nil
	}

	return q.SetMaxAttempts(attempts)
}
