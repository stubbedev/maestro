// Ports src/Helper/ProgressBar.php (symfony/console).

package console

import (
	"iter"
	"math"
	"runtime/metrics"
	"strings"
	"sync"
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// Built-in progress bar formats (ProgressBar::FORMAT_*).
const (
	ProgressFormatVerbose     = "verbose"
	ProgressFormatVeryVerbose = "very_verbose"
	ProgressFormatDebug       = "debug"
	ProgressFormatNormal      = "normal"

	progressFormatVerboseNomax     = "verbose_nomax"
	progressFormatVeryVerboseNomax = "very_verbose_nomax"
	progressFormatDebugNomax       = "debug_nomax"
	progressFormatNormalNomax      = "normal_nomax"
)

// DefaultMinSecondsBetweenRedraws is the constructor default 1 / 25.
const DefaultMinSecondsBetweenRedraws = 1.0 / 25

// PlaceholderFormatter renders a "%name%" placeholder. Like the PHP
// callables it returns a PHP value (string, int, float64, ...), which is
// converted to a string or passed to sprintf with the placeholder's format.
type PlaceholderFormatter func(bar *ProgressBar, out Output) any

// ProgressClock returns the current time; ProgressBar reads it where PHP
// calls time() and microtime(true).
type ProgressClock func() time.Time

// The process-wide format and placeholder registries (PHP's static
// $formats and $formatters), initialised lazily with the built-ins.
var progressRegistry struct {
	mu         sync.Mutex
	formats    map[string]string
	formatters map[string]PlaceholderFormatter
}

func progressRegistryInit() {
	if progressRegistry.formats == nil {
		progressRegistry.formats = progressDefaultFormats()
	}
	if progressRegistry.formatters == nil {
		progressRegistry.formatters = progressDefaultFormatters()
	}
}

// SetPlaceholderFormatterDefinition registers (or overrides) the formatter
// of a placeholder, by name without the "%" delimiters.
func SetPlaceholderFormatterDefinition(name string, formatter PlaceholderFormatter) {
	progressRegistry.mu.Lock()
	defer progressRegistry.mu.Unlock()
	progressRegistryInit()
	progressRegistry.formatters[name] = formatter
}

// PlaceholderFormatterDefinition returns the formatter of a placeholder,
// or nil.
func PlaceholderFormatterDefinition(name string) PlaceholderFormatter {
	progressRegistry.mu.Lock()
	defer progressRegistry.mu.Unlock()
	progressRegistryInit()

	return progressRegistry.formatters[name]
}

// SetFormatDefinition registers (or overrides) a named format.
func SetFormatDefinition(name, format string) {
	progressRegistry.mu.Lock()
	defer progressRegistry.mu.Unlock()
	progressRegistryInit()
	progressRegistry.formats[name] = format
}

// FormatDefinition returns a named format.
func FormatDefinition(name string) (string, bool) {
	progressRegistry.mu.Lock()
	defer progressRegistry.mu.Unlock()
	progressRegistryInit()
	f, ok := progressRegistry.formats[name]

	return f, ok
}

// ProgressBar renders a console progress bar.
type ProgressBar struct {
	barWidth                 int
	barChar                  string
	hasBarChar               bool
	emptyBarChar             string
	progressChar             string
	format                   string
	hasFormat                bool
	internalFormat           string
	redrawFreq               int // 0 means null (automatic)
	writeCount               int
	lastWriteTime            float64
	minSecondsBetweenRedraws float64
	maxSecondsBetweenRedraws float64
	output                   Output
	step                     int
	max                      int
	startTime                int64
	stepWidth                int
	percent                  float64
	messages                 map[string]string
	overwrite                bool
	terminal                 Terminal
	previousMessage          string
	hasPreviousMessage       bool
	cursor                   *Cursor
	clock                    ProgressClock
}

// NewProgressBar mirrors new ProgressBar($output, $max,
// $minSecondsBetweenRedraws); max 0 means unknown. Pass
// DefaultMinSecondsBetweenRedraws for PHP's default.
func NewProgressBar(output Output, maxSteps int, minSecondsBetweenRedraws float64) *ProgressBar {
	return NewProgressBarWithClock(output, maxSteps, minSecondsBetweenRedraws, time.Now)
}

// NewProgressBarWithClock is NewProgressBar reading time from clock.
func NewProgressBarWithClock(output Output, maxSteps int, minSecondsBetweenRedraws float64, clock ProgressClock) *ProgressBar {
	output = ErrorOutputOf(output)

	b := &ProgressBar{
		barWidth:                 28,
		emptyBarChar:             "-",
		progressChar:             ">",
		redrawFreq:               1,
		maxSecondsBetweenRedraws: 1,
		output:                   output,
		messages:                 map[string]string{},
		overwrite:                true,
		clock:                    clock,
	}
	b.SetMaxSteps(maxSteps)

	if minSecondsBetweenRedraws > 0 {
		b.redrawFreq = 0
		b.minSecondsBetweenRedraws = minSecondsBetweenRedraws
	}

	if !output.IsDecorated() {
		// disable overwrite when output does not support ANSI codes.
		b.overwrite = false

		// set a reasonable redraw frequency so output isn't flooded
		b.redrawFreq = 0
	}

	b.startTime = b.time()
	b.cursor = NewCursor(output, nil)

	return b
}

// time is PHP's time().
func (b *ProgressBar) time() int64 { return b.clock().Unix() }

// microtime is PHP's microtime(true).
func (b *ProgressBar) microtime() float64 {
	return float64(b.clock().UnixNano()) / 1e9
}

// SetMessage associates a text with a named placeholder.
func (b *ProgressBar) SetMessage(message, name string) { b.messages[name] = message }

// Message returns the text of a named placeholder (false when unset).
func (b *ProgressBar) Message(name string) (string, bool) {
	m, ok := b.messages[name]

	return m, ok
}

// StartTime returns the start time as a Unix timestamp.
func (b *ProgressBar) StartTime() int64 { return b.startTime }

// MaxSteps returns the maximum number of steps (0 if unknown).
func (b *ProgressBar) MaxSteps() int { return b.max }

// Progress returns the current step.
func (b *ProgressBar) Progress() int { return b.step }

// ProgressPercent returns the progress as a fraction of 1.
func (b *ProgressBar) ProgressPercent() float64 { return b.percent }

// BarOffset returns the number of complete bar characters.
func (b *ProgressBar) BarOffset() float64 {
	if b.max != 0 {
		return math.Floor(b.percent * float64(b.barWidth))
	}
	var n int
	if b.redrawFreq == 0 {
		n = int(math.Min(5, float64(b.barWidth)/15) * float64(b.writeCount))
	} else {
		n = b.step
	}

	return float64(n % b.barWidth)
}

// Estimated returns the estimated total duration in seconds.
func (b *ProgressBar) Estimated() float64 {
	if b.step == 0 {
		return 0
	}

	return math.Round(float64(b.time()-b.startTime) / float64(b.step) * float64(b.max))
}

// Remaining returns the estimated remaining duration in seconds.
func (b *ProgressBar) Remaining() float64 {
	if b.step == 0 {
		return 0
	}

	return math.Round(float64(b.time()-b.startTime) / float64(b.step) * float64(b.max-b.step))
}

// SetBarWidth sets the bar width (at least 1).
func (b *ProgressBar) SetBarWidth(size int) { b.barWidth = max(1, size) }

// BarWidth returns the bar width.
func (b *ProgressBar) BarWidth() int { return b.barWidth }

// SetBarCharacter sets the complete bar character.
func (b *ProgressBar) SetBarCharacter(char string) { b.barChar, b.hasBarChar = char, true }

// BarCharacter returns the complete bar character: "=" with a maximum,
// the empty bar character without one, unless set.
func (b *ProgressBar) BarCharacter() string {
	switch {
	case b.hasBarChar:
		return b.barChar
	case b.max != 0:
		return "="
	}

	return b.emptyBarChar
}

// SetEmptyBarCharacter sets the empty bar character.
func (b *ProgressBar) SetEmptyBarCharacter(char string) { b.emptyBarChar = char }

// EmptyBarCharacter returns the empty bar character.
func (b *ProgressBar) EmptyBarCharacter() string { return b.emptyBarChar }

// SetProgressCharacter sets the progress character.
func (b *ProgressBar) SetProgressCharacter(char string) { b.progressChar = char }

// ProgressCharacter returns the progress character.
func (b *ProgressBar) ProgressCharacter() string { return b.progressChar }

// SetFormat sets the format: a registered format name or a format line.
func (b *ProgressBar) SetFormat(format string) {
	b.format, b.hasFormat = "", false
	b.internalFormat = format
}

// SetRedrawFrequency sets the redraw frequency in steps (at least 1).
func (b *ProgressBar) SetRedrawFrequency(freq int) { b.redrawFreq = max(1, freq) }

// SetAutoRedrawFrequency is setRedrawFrequency(null): redraw every tenth
// of the maximum (or every step without one).
func (b *ProgressBar) SetAutoRedrawFrequency() { b.redrawFreq = 0 }

// MinSecondsBetweenRedraws sets the redraw throttling interval.
func (b *ProgressBar) MinSecondsBetweenRedraws(seconds float64) { b.minSecondsBetweenRedraws = seconds }

// MaxSecondsBetweenRedraws sets the interval after which a redraw happens
// regardless of the redraw frequency.
func (b *ProgressBar) MaxSecondsBetweenRedraws(seconds float64) { b.maxSecondsBetweenRedraws = seconds }

// ProgressIterate returns a sequence that starts bar with maxSteps steps
// (0 if indeterminate), advances it after each element of seq and finishes
// it at the end (ProgressBar::iterate()).
func ProgressIterate[K, V any](bar *ProgressBar, seq iter.Seq2[K, V], maxSteps int) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		bar.StartMax(maxSteps)
		for k, v := range seq {
			if !yield(k, v) {
				return
			}
			bar.Advance(1)
		}
		bar.Finish()
	}
}

// ProgressIterateSlice is ProgressIterate over a slice, whose length is the
// maximum (PHP's count() of a countable).
func ProgressIterateSlice[V any](bar *ProgressBar, s []V) iter.Seq2[int, V] {
	return ProgressIterate(bar, func(yield func(int, V) bool) {
		for i, v := range s {
			if !yield(i, v) {
				return
			}
		}
	}, len(s))
}

// Start starts the progress output, leaving the maximum unchanged
// (start(null)).
func (b *ProgressBar) Start() { b.start(0, true) }

// StartMax starts the progress output with maxSteps steps (0 if
// indeterminate).
func (b *ProgressBar) StartMax(maxSteps int) { b.start(maxSteps, false) }

func (b *ProgressBar) start(maxSteps int, keepMax bool) {
	b.startTime = b.time()
	b.step = 0
	b.percent = 0.0

	if !keepMax {
		b.SetMaxSteps(maxSteps)
	}

	b.Display()
}

// Advance advances the progress by step steps.
func (b *ProgressBar) Advance(step int) { b.SetProgress(b.step + step) }

// SetOverwrite sets whether to overwrite the progress bar, false for a new
// line.
func (b *ProgressBar) SetOverwrite(overwrite bool) { b.overwrite = overwrite }

// SetProgress sets the current step.
func (b *ProgressBar) SetProgress(step int) {
	if b.max != 0 && step > b.max {
		b.max = step
	} else if step < 0 {
		step = 0
	}

	redrawFreq := float64(b.redrawFreq)
	if b.redrawFreq == 0 {
		m := b.max
		if m == 0 {
			m = 10
		}
		redrawFreq = float64(m) / 10
	}
	prevPeriod := int(float64(b.step) / redrawFreq)
	currPeriod := int(float64(step) / redrawFreq)
	b.step = step
	b.percent = 0
	if b.max != 0 {
		b.percent = float64(b.step) / float64(b.max)
	}
	timeInterval := b.microtime() - b.lastWriteTime

	// Draw regardless of other limits
	if b.max == step {
		b.Display()

		return
	}

	// Throttling
	if timeInterval < b.minSecondsBetweenRedraws {
		return
	}

	// Draw each step period, but not too late
	if prevPeriod != currPeriod || timeInterval >= b.maxSecondsBetweenRedraws {
		b.Display()
	}
}

// SetMaxSteps sets the maximum number of steps (0 if unknown).
func (b *ProgressBar) SetMaxSteps(maxSteps int) {
	b.format, b.hasFormat = "", false
	b.max = max(0, maxSteps)
	b.stepWidth = 4
	if b.max != 0 {
		b.stepWidth = Width(php.ToString(b.max))
	}
}

// Finish finishes the progress output.
func (b *ProgressBar) Finish() {
	if b.max == 0 {
		b.max = b.step
	}

	if b.step == b.max && !b.overwrite {
		// prevent double 100% output
		return
	}

	b.SetProgress(b.max)
}

// Display outputs the current progress string.
func (b *ProgressBar) Display() {
	if b.output.Verbosity() == VerbosityQuiet {
		return
	}

	b.ensureFormat()
	b.overwriteWith(b.buildLine())
}

// Clear removes the progress bar from the current line; Display shows it
// again.
func (b *ProgressBar) Clear() {
	if !b.overwrite {
		return
	}

	b.ensureFormat()
	b.overwriteWith("")
}

func (b *ProgressBar) ensureFormat() {
	if b.hasFormat {
		return
	}
	format := b.internalFormat
	if format == "" || format == "0" {
		format = b.determineBestFormat()
	}
	b.setRealFormat(format)
}

func (b *ProgressBar) setRealFormat(format string) {
	b.hasFormat = true
	// try to use the _nomax variant if available
	if b.max == 0 {
		if f, ok := FormatDefinition(format + "_nomax"); ok {
			b.format = f

			return
		}
	}
	if f, ok := FormatDefinition(format); ok {
		b.format = f

		return
	}
	b.format = format
}

// overwriteWith ports ProgressBar::overwrite(): it rewrites the previous
// message (ConsoleSectionOutput is not ported).
func (b *ProgressBar) overwriteWith(message string) {
	if b.hasPreviousMessage && b.previousMessage == message {
		return
	}

	originalMessage := message

	if b.overwrite {
		if b.hasPreviousMessage {
			lineCount := strings.Count(b.previousMessage, "\n")
			for range lineCount {
				b.cursor.MoveToColumn(1)
				b.cursor.ClearLine()
				b.cursor.MoveUp(1)
			}

			b.cursor.MoveToColumn(1)
			b.cursor.ClearLine()
		}
	} else if b.step > 0 {
		message = "\n" + message
	}

	b.previousMessage, b.hasPreviousMessage = originalMessage, true
	b.lastWriteTime = b.microtime()

	b.output.Write(message, false, OutputNormal)
	b.writeCount++
}

func (b *ProgressBar) determineBestFormat() string {
	switch b.output.Verbosity() {
	// OutputInterface::VERBOSITY_QUIET: display is disabled anyway
	case VerbosityVerbose:
		if b.max != 0 {
			return ProgressFormatVerbose
		}

		return progressFormatVerboseNomax
	case VerbosityVeryVerbose:
		if b.max != 0 {
			return ProgressFormatVeryVerbose
		}

		return progressFormatVeryVerboseNomax
	case VerbosityDebug:
		if b.max != 0 {
			return ProgressFormatDebug
		}

		return progressFormatDebugNomax
	}
	if b.max != 0 {
		return ProgressFormatNormal
	}

	return progressFormatNormalNomax
}

func progressDefaultFormatters() map[string]PlaceholderFormatter {
	return map[string]PlaceholderFormatter{
		"bar": func(bar *ProgressBar, out Output) any {
			completeBars := int(bar.BarOffset())
			display := phpStrRepeat(bar.BarCharacter(), completeBars)
			if completeBars < bar.BarWidth() {
				emptyBars := bar.BarWidth() - completeBars - Length(RemoveDecoration(out.Formatter(), bar.ProgressCharacter()))
				display += bar.ProgressCharacter() + phpStrRepeat(bar.EmptyBarCharacter(), emptyBars)
			}

			return display
		},
		"elapsed": func(bar *ProgressBar, _ Output) any {
			return FormatTime(float64(bar.time() - bar.StartTime()))
		},
		"remaining": func(bar *ProgressBar, _ Output) any {
			if bar.MaxSteps() == 0 {
				panic(newError(KindLogic, "ProgressBar.php", 532, "Unable to display the remaining time if the maximum number of steps is not set."))
			}

			return FormatTime(bar.Remaining())
		},
		"estimated": func(bar *ProgressBar, _ Output) any {
			if bar.MaxSteps() == 0 {
				panic(newError(KindLogic, "ProgressBar.php", 539, "Unable to display the estimated time if the maximum number of steps is not set."))
			}

			return FormatTime(bar.Estimated())
		},
		"memory": func(*ProgressBar, Output) any {
			return FormatMemory(memoryUsage())
		},
		"current": func(bar *ProgressBar, _ Output) any {
			return php.StrPad(php.ToString(bar.Progress()), bar.stepWidth, " ", php.StrPadLeft)
		},
		"max": func(bar *ProgressBar, _ Output) any {
			return bar.MaxSteps()
		},
		"percent": func(bar *ProgressBar, _ Output) any {
			return math.Floor(bar.ProgressPercent() * 100)
		},
	}
}

// memoryUsage stands in for memory_get_usage(true), the memory the PHP
// allocator obtained from the system: here the Go runtime's mapped memory
// minus what it released back to the OS.
func memoryUsage() int {
	samples := []metrics.Sample{
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
	}
	metrics.Read(samples)
	var total, released uint64
	if samples[0].Value.Kind() == metrics.KindUint64 {
		total = samples[0].Value.Uint64()
	}
	if samples[1].Value.Kind() == metrics.KindUint64 {
		released = samples[1].Value.Uint64()
	}

	return int(total - released)
}

// phpStrRepeat is str_repeat(), which fails on a negative count.
func phpStrRepeat(s string, times int) string {
	if times < 0 {
		panic("str_repeat(): Argument #2 ($times) must be greater than or equal to 0")
	}

	return strings.Repeat(s, times)
}

func progressDefaultFormats() map[string]string {
	return map[string]string{
		ProgressFormatNormal:      " %current%/%max% [%bar%] %percent:3s%%",
		progressFormatNormalNomax: " %current% [%bar%]",

		ProgressFormatVerbose:      " %current%/%max% [%bar%] %percent:3s%% %elapsed:6s%",
		progressFormatVerboseNomax: " %current% [%bar%] %elapsed:6s%",

		ProgressFormatVeryVerbose:      " %current%/%max% [%bar%] %percent:3s%% %elapsed:6s%/%estimated:-6s%",
		progressFormatVeryVerboseNomax: " %current% [%bar%] %elapsed:6s%",

		ProgressFormatDebug:      " %current%/%max% [%bar%] %percent:3s%% %elapsed:6s%/%estimated:-6s% %memory:6s%",
		progressFormatDebugNomax: " %current% [%bar%] %elapsed:6s% %memory:6s%",
	}
}

var progressPlaceholderRe = php.MustCompile("{%([a-z\\-_]+)(?:\\:([^%]+))?%}i")

// replacePlaceholders is buildLine's preg_replace_callback($regex,
// $callback, $this->format).
func (b *ProgressBar) replacePlaceholders() string {
	line, _, err := progressPlaceholderRe.ReplaceCallback(b.format, func(m *php.Match) string {
		name := m.Get(1)
		var text any
		if formatter := PlaceholderFormatterDefinition(name); formatter != nil {
			text = formatter(b, b.output)
		} else if msg, ok := b.messages[name]; ok {
			text = msg
		} else {
			return m.Get(0)
		}

		if spec, ok := m.Group(2); ok {
			return phpSprintf("%"+spec, text)
		}

		return php.ToString(text)
	}, -1)
	if err != nil {
		// preg_replace_callback() returns null, and buildLine(): string
		// throws a TypeError returning it.
		panic(&php.EngineError{Class: "TypeError", Message: "Symfony\\Component\\Console\\Helper\\ProgressBar::buildLine(): Return value must be of type string, null returned"})
	}

	return line
}

func (b *ProgressBar) buildLine() string {
	line := b.replacePlaceholders()

	// gets string length for each sub line with multiline format
	linesWidth := 0
	for subLine := range strings.SplitSeq(line, "\n") {
		linesWidth = max(linesWidth, Width(RemoveDecoration(b.output.Formatter(), strings.TrimRight(subLine, "\r"))))
	}

	terminalWidth := b.terminal.Width()
	if linesWidth <= terminalWidth {
		return line
	}

	b.SetBarWidth(b.barWidth - linesWidth + terminalWidth)

	return b.replacePlaceholders()
}
