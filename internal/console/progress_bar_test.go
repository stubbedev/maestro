// Ports tests/Helper/ProgressBarTest.php (symfony/console). The
// ConsoleSectionOutput tests are not ported (testOverwriteWithSectionOutput,
// testOverwriteWithAnsiSectionOutput,
// testOverwriteMultipleProgressBarsWithSectionOutputs,
// testOverwriteWithSectionOutputWithNewlinesInMessage,
// testMultipleSectionsWithCustomFormat).

package console

import (
	"bytes"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

// pbClock is ClockMock: frozen time that only moves with sleep().
type pbClock struct{ t time.Time }

func (c *pbClock) now() time.Time { return c.t }

func (c *pbClock) sleep(seconds int) { c.t = c.t.Add(time.Duration(seconds) * time.Second) }

type pbEnv struct {
	t     *testing.T
	clock *pbClock
}

// setUpProgressBar is setUp(): COLUMNS=120, a mocked clock, and the format
// registries restored afterwards (PHP's statics leak between tests).
func setUpProgressBar(t *testing.T) *pbEnv {
	t.Helper()
	t.Setenv("COLUMNS", "120")

	progressRegistry.mu.Lock()
	progressRegistryInit()
	formats := maps.Clone(progressRegistry.formats)
	formatters := maps.Clone(progressRegistry.formatters)
	progressRegistry.mu.Unlock()
	t.Cleanup(func() {
		progressRegistry.mu.Lock()
		progressRegistry.formats, progressRegistry.formatters = formats, formatters
		progressRegistry.mu.Unlock()
	})

	return &pbEnv{t: t, clock: &pbClock{t: time.Unix(1_700_000_000, 0)}}
}

func (e *pbEnv) output(decorated bool, verbosity int) (*StreamOutput, *bytes.Buffer) {
	buf := &bytes.Buffer{}

	return NewStreamOutput(buf, verbosity, &decorated, nil), buf
}

func (e *pbEnv) stream() (*StreamOutput, *bytes.Buffer) { return e.output(true, VerbosityNormal) }

func (e *pbEnv) bar(out Output, maxSteps int, minSeconds float64) *ProgressBar {
	return NewProgressBarWithClock(out, maxSteps, minSeconds, e.clock.now)
}

// generateOutput mirrors the PHP test helper.
func generateOutput(expected string) string {
	count := strings.Count(expected, "\n")

	return strings.Repeat("\x1B[1G\x1b[2K\x1B[1A", count) + "\x1B[1G\x1B[2K" + expected
}

func assertPB(t *testing.T, want string, buf *bytes.Buffer) {
	t.Helper()
	if got := buf.String(); got != want {
		t.Errorf("output mismatch\nwant %q\ngot  %q", want, got)
	}
}

func TestProgressBar_MultipleStart(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.Advance(1)
	bar.Start()

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    1 [->--------------------------]")+
		generateOutput("    0 [>---------------------------]"), buf)
}

func TestProgressBar_Advance(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.Advance(1)

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    1 [->--------------------------]"), buf)
}

func TestProgressBar_AdvanceWithStep(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.Advance(5)

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    5 [----->----------------------]"), buf)
}

func TestProgressBar_AdvanceMultipleTimes(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.Advance(3)
	bar.Advance(2)

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    3 [--->------------------------]")+
		generateOutput("    5 [----->----------------------]"), buf)
}

func TestProgressBar_AdvanceOverMax(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 10, 0)
	bar.SetProgress(9)
	bar.Advance(1)
	bar.Advance(1)

	assertPB(t, "  9/10 [=========================>--]  90%"+
		generateOutput(" 10/10 [============================] 100%")+
		generateOutput(" 11/11 [============================] 100%"), buf)
}

func TestProgressBar_Regress(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.Advance(1)
	bar.Advance(1)
	bar.Advance(-1)

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    1 [->--------------------------]")+
		generateOutput("    2 [-->-------------------------]")+
		generateOutput("    1 [->--------------------------]"), buf)
}

func TestProgressBar_RegressWithStep(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.Advance(4)
	bar.Advance(4)
	bar.Advance(-2)

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    4 [---->-----------------------]")+
		generateOutput("    8 [-------->-------------------]")+
		generateOutput("    6 [------>---------------------]"), buf)
}

func TestProgressBar_RegressMultipleTimes(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.Advance(3)
	bar.Advance(3)
	bar.Advance(-1)
	bar.Advance(-2)

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    3 [--->------------------------]")+
		generateOutput("    6 [------>---------------------]")+
		generateOutput("    5 [----->----------------------]")+
		generateOutput("    3 [--->------------------------]"), buf)
}

func TestProgressBar_RegressBelowMin(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 10, 0)
	bar.SetProgress(1)
	bar.Advance(-1)
	bar.Advance(-1)

	assertPB(t, "  1/10 [==>-------------------------]  10%"+
		generateOutput("  0/10 [>---------------------------]   0%"), buf)
}

func TestProgressBar_Format(t *testing.T) {
	e := setUpProgressBar(t)
	expected := "  0/10 [>---------------------------]   0%" +
		generateOutput(" 10/10 [============================] 100%")

	// max in construct, no format
	out, buf := e.stream()
	bar := e.bar(out, 10, 0)
	bar.Start()
	bar.Advance(10)
	bar.Finish()
	assertPB(t, expected, buf)

	// max in start, no format
	out, buf = e.stream()
	bar = e.bar(out, 0, 0)
	bar.StartMax(10)
	bar.Advance(10)
	bar.Finish()
	assertPB(t, expected, buf)

	// max in construct, explicit format before
	out, buf = e.stream()
	bar = e.bar(out, 10, 0)
	bar.SetFormat(ProgressFormatNormal)
	bar.Start()
	bar.Advance(10)
	bar.Finish()
	assertPB(t, expected, buf)

	// max in start, explicit format before
	out, buf = e.stream()
	bar = e.bar(out, 0, 0)
	bar.SetFormat(ProgressFormatNormal)
	bar.StartMax(10)
	bar.Advance(10)
	bar.Finish()
	assertPB(t, expected, buf)
}

func TestProgressBar_Customizations(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 10, 0)
	bar.SetBarWidth(10)
	bar.SetBarCharacter("_")
	bar.SetEmptyBarCharacter(" ")
	bar.SetProgressCharacter("/")
	bar.SetFormat(" %current%/%max% [%bar%] %percent:3s%%")
	bar.Start()
	bar.Advance(1)

	assertPB(t, "  0/10 [/         ]   0%"+
		generateOutput("  1/10 [_/        ]  10%"), buf)
}

func TestProgressBar_DisplayWithoutStart(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 50, 0)
	bar.Display()

	assertPB(t, "  0/50 [>---------------------------]   0%", buf)
}

func TestProgressBar_DisplayWithQuietVerbosity(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.output(true, VerbosityQuiet)
	bar := e.bar(out, 50, 0)
	bar.Display()

	assertPB(t, "", buf)
}

func TestProgressBar_FinishWithoutStart(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 50, 0)
	bar.Finish()

	assertPB(t, " 50/50 [============================] 100%", buf)
}

func TestProgressBar_Percent(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 50, 0)
	bar.Start()
	bar.Display()
	bar.Advance(1)
	bar.Advance(1)

	assertPB(t, "  0/50 [>---------------------------]   0%"+
		generateOutput("  1/50 [>---------------------------]   2%")+
		generateOutput("  2/50 [=>--------------------------]   4%"), buf)
}

func TestProgressBar_OverwriteWithShorterLine(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 50, 0)
	bar.SetFormat(" %current%/%max% [%bar%] %percent:3s%%")
	bar.Start()
	bar.Display()
	bar.Advance(1)

	// set shorter format
	bar.SetFormat(" %current%/%max% [%bar%]")
	bar.Advance(1)

	assertPB(t, "  0/50 [>---------------------------]   0%"+
		generateOutput("  1/50 [>---------------------------]   2%")+
		generateOutput("  2/50 [=>--------------------------]"), buf)
}

const pbLongFormat = "%current%/%max% [%bar%] %percent:3s%% %message% Fruitcake marzipan toffee. Cupcake gummi bears tart dessert ice cream chupa chups cupcake chocolate bar sesame snaps. Croissant halvah cookie jujubes powder macaroon. Fruitcake bear claw bonbon jelly beans oat cake pie muffin Fruitcake marzipan toffee."

func TestProgressBar_OverwritWithNewlinesInMessage(t *testing.T) {
	e := setUpProgressBar(t)
	SetFormatDefinition("test", pbLongFormat)

	out, buf := e.stream()
	bar := e.bar(out, 50, 0)
	bar.SetFormat("test")
	bar.Start()
	bar.Display()
	bar.SetMessage("Twas brillig, and the slithy toves. Did gyre and gimble in the wabe: All mimsy were the borogoves, And the mome raths outgrabe.\nBeware the Jabberwock, my son! The jaws that bite, the claws that catch! Beware the Jubjub bird, and shun The frumious Bandersnatch!", "message")
	bar.Advance(1)
	bar.SetMessage("He took his vorpal sword in hand; Long time the manxome foe he sought— So rested he by the Tumtum tree And stood awhile in thought.\nAnd, as in uffish thought he stood, The Jabberwock, with eyes of flame, Came whiffling through the tulgey wood, And burbled as it came!", "message")
	bar.Advance(1)

	assertPB(t, " 0/50 [>]   0% %message% Fruitcake marzipan toffee. Cupcake gummi bears tart dessert ice cream chupa chups cupcake chocolate bar sesame snaps. Croissant halvah cookie jujubes powder macaroon. Fruitcake bear claw bonbon jelly beans oat cake pie muffin Fruitcake marzipan toffee.\x1b[1G\x1b[2K 1/50 [>]   2% Twas brillig, and the slithy toves. Did gyre and gimble in the wabe: All mimsy were the borogoves, And the mome raths outgrabe.\n"+
		"Beware the Jabberwock, my son! The jaws that bite, the claws that catch! Beware the Jubjub bird, and shun The frumious Bandersnatch! Fruitcake marzipan toffee. Cupcake gummi bears tart dessert ice cream chupa chups cupcake chocolate bar sesame snaps. Croissant halvah cookie jujubes powder macaroon. Fruitcake bear claw bonbon jelly beans oat cake pie muffin Fruitcake marzipan toffee.\x1b[1G\x1b[2K\x1b[1A\x1b[1G\x1b[2K 2/50 [>]   4% He took his vorpal sword in hand; Long time the manxome foe he sought— So rested he by the Tumtum tree And stood awhile in thought.\n"+
		"And, as in uffish thought he stood, The Jabberwock, with eyes of flame, Came whiffling through the tulgey wood, And burbled as it came! Fruitcake marzipan toffee. Cupcake gummi bears tart dessert ice cream chupa chups cupcake chocolate bar sesame snaps. Croissant halvah cookie jujubes powder macaroon. Fruitcake bear claw bonbon jelly beans oat cake pie muffin Fruitcake marzipan toffee.", buf)
}

func TestProgressBar_StartWithMax(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.SetFormat("%current%/%max% [%bar%]")
	bar.StartMax(50)
	bar.Advance(1)

	assertPB(t, " 0/50 [>---------------------------]"+
		generateOutput(" 1/50 [>---------------------------]"), buf)
}

func TestProgressBar_SetCurrentProgress(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 50, 0)
	bar.Start()
	bar.Display()
	bar.Advance(1)
	bar.SetProgress(15)
	bar.SetProgress(25)

	assertPB(t, "  0/50 [>---------------------------]   0%"+
		generateOutput("  1/50 [>---------------------------]   2%")+
		generateOutput(" 15/50 [========>-------------------]  30%")+
		generateOutput(" 25/50 [==============>-------------]  50%"), buf)
}

func TestProgressBar_SetCurrentBeforeStarting(t *testing.T) {
	e := setUpProgressBar(t)
	out, _ := e.stream()
	bar := e.bar(out, 0, 0)
	bar.SetProgress(15)
	if bar.StartTime() == 0 {
		t.Error("start time not set")
	}
}

func TestProgressBar_RedrawFrequency(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 6, 0)
	bar.SetRedrawFrequency(2)
	bar.Start()
	bar.SetProgress(1)
	bar.Advance(2)
	bar.Advance(2)
	bar.Advance(1)

	assertPB(t, " 0/6 [>---------------------------]   0%"+
		generateOutput(" 3/6 [==============>-------------]  50%")+
		generateOutput(" 5/6 [=======================>----]  83%")+
		generateOutput(" 6/6 [============================] 100%"), buf)
}

func TestProgressBar_RedrawFrequencyIsAtLeastOneIfZeroGiven(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.SetRedrawFrequency(0)
	bar.Start()
	bar.Advance(1)

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    1 [->--------------------------]"), buf)
}

func TestProgressBar_RedrawFrequencyIsAtLeastOneIfSmallerOneGiven(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.SetRedrawFrequency(0)
	bar.Start()
	bar.Advance(1)

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    1 [->--------------------------]"), buf)
}

func TestProgressBar_MultiByteSupport(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.SetBarCharacter("■")
	bar.Advance(3)

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    3 [■■■>------------------------]"), buf)
}

func TestProgressBar_Clear(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 50, 0)
	bar.Start()
	bar.SetProgress(25)
	bar.Clear()

	assertPB(t, "  0/50 [>---------------------------]   0%"+
		generateOutput(" 25/50 [==============>-------------]  50%")+
		generateOutput(""), buf)
}

func TestProgressBar_PercentNotHundredBeforeComplete(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 200, 0)
	bar.Start()
	bar.Display()
	bar.Advance(199)
	bar.Advance(1)

	assertPB(t, "   0/200 [>---------------------------]   0%"+
		generateOutput(" 199/200 [===========================>]  99%")+
		generateOutput(" 200/200 [============================] 100%"), buf)
}

func TestProgressBar_NonDecoratedOutput(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.output(false, VerbosityNormal)
	bar := e.bar(out, 200, 0)
	bar.Start()

	for range 200 {
		bar.Advance(1)
	}

	bar.Finish()

	assertPB(t, "   0/200 [>---------------------------]   0%\n"+
		"  20/200 [==>-------------------------]  10%\n"+
		"  40/200 [=====>----------------------]  20%\n"+
		"  60/200 [========>-------------------]  30%\n"+
		"  80/200 [===========>----------------]  40%\n"+
		" 100/200 [==============>-------------]  50%\n"+
		" 120/200 [================>-----------]  60%\n"+
		" 140/200 [===================>--------]  70%\n"+
		" 160/200 [======================>-----]  80%\n"+
		" 180/200 [=========================>--]  90%\n"+
		" 200/200 [============================] 100%", buf)
}

func TestProgressBar_NonDecoratedOutputWithClear(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.output(false, VerbosityNormal)
	bar := e.bar(out, 50, 0)
	bar.Start()
	bar.SetProgress(25)
	bar.Clear()
	bar.SetProgress(50)
	bar.Finish()

	assertPB(t, "  0/50 [>---------------------------]   0%\n"+
		" 25/50 [==============>-------------]  50%\n"+
		" 50/50 [============================] 100%", buf)
}

func TestProgressBar_NonDecoratedOutputWithoutMax(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.output(false, VerbosityNormal)
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.Advance(1)

	assertPB(t, "    0 [>---------------------------]\n"+
		"    1 [->--------------------------]", buf)
}

func TestProgressBar_ParallelBars(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar1 := e.bar(out, 2, 0)
	bar2 := e.bar(out, 3, 0)
	bar2.SetProgressCharacter("#")
	bar3 := e.bar(out, 0, 0)

	bar1.Start()
	out.Write("\n", false, OutputNormal)
	bar2.Start()
	out.Write("\n", false, OutputNormal)
	bar3.Start()

	for i := 1; i <= 3; i++ {
		// up two lines
		out.Write("\033[2A", false, OutputNormal)
		if i <= 2 {
			bar1.Advance(1)
		}
		out.Write("\n", false, OutputNormal)
		bar2.Advance(1)
		out.Write("\n", false, OutputNormal)
		bar3.Advance(1)
	}
	out.Write("\033[2A", false, OutputNormal)
	out.Write("\n", false, OutputNormal)
	out.Write("\n", false, OutputNormal)
	bar3.Finish()

	rtrim := func(s string) string { return strings.TrimRight(s, " \t\n\r\x00\x0B") }
	assertPB(t, " 0/2 [>---------------------------]   0%"+"\n"+
		" 0/3 [#---------------------------]   0%"+"\n"+
		rtrim("    0 [>---------------------------]")+
		"\033[2A"+
		generateOutput(" 1/2 [==============>-------------]  50%")+"\n"+
		generateOutput(" 1/3 [=========#------------------]  33%")+"\n"+
		rtrim(generateOutput("    1 [->--------------------------]"))+
		"\033[2A"+
		generateOutput(" 2/2 [============================] 100%")+"\n"+
		generateOutput(" 2/3 [==================#---------]  66%")+"\n"+
		rtrim(generateOutput("    2 [-->-------------------------]"))+
		"\033[2A"+
		"\n"+
		generateOutput(" 3/3 [============================] 100%")+"\n"+
		rtrim(generateOutput("    3 [--->------------------------]"))+
		"\033[2A"+
		"\n"+
		"\n"+
		rtrim(generateOutput("    3 [============================]")), buf)
}

func TestProgressBar_WithoutMax(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.Advance(1)
	bar.Advance(1)
	bar.Advance(1)
	bar.Finish()

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    1 [->--------------------------]")+
		generateOutput("    2 [-->-------------------------]")+
		generateOutput("    3 [--->------------------------]")+
		generateOutput("    3 [============================]"), buf)
}

func TestProgressBar_SettingMaxStepsDuringProgressing(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.Start()
	bar.SetProgress(2)
	bar.SetMaxSteps(10)
	bar.SetProgress(5)
	bar.SetMaxSteps(100)
	bar.SetProgress(10)
	bar.Finish()

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    2 [-->-------------------------]")+
		generateOutput("  5/10 [==============>-------------]  50%")+
		generateOutput("  10/100 [==>-------------------------]  10%")+
		generateOutput(" 100/100 [============================] 100%"), buf)
}

func TestProgressBar_WithSmallScreen(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	t.Setenv("COLUMNS", "12")
	bar.Start()
	bar.Advance(1)
	t.Setenv("COLUMNS", "120")

	assertPB(t, "    0 [>---]"+
		generateOutput("    1 [->--]"), buf)
}

func TestProgressBar_AddingPlaceholderFormatter(t *testing.T) {
	e := setUpProgressBar(t)
	SetPlaceholderFormatterDefinition("remaining_steps", func(bar *ProgressBar, _ Output) any {
		return bar.MaxSteps() - bar.Progress()
	})
	out, buf := e.stream()
	bar := e.bar(out, 3, 0)
	bar.SetFormat(" %remaining_steps% [%bar%]")

	bar.Start()
	bar.Advance(1)
	bar.Finish()

	assertPB(t, " 3 [>---------------------------]"+
		generateOutput(" 2 [=========>------------------]")+
		generateOutput(" 0 [============================]"), buf)
}

func TestProgressBar_MultilineFormat(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 3, 0)
	bar.SetFormat("%bar%\nfoobar")

	bar.Start()
	bar.Advance(1)
	bar.Clear()
	bar.Finish()

	assertPB(t, ">---------------------------\nfoobar"+
		generateOutput("=========>------------------\nfoobar")+
		"\x1B[1G\x1B[2K\x1B[1A"+
		generateOutput("")+
		generateOutput("============================")+
		"\nfoobar", buf)
}

func TestProgressBar_AnsiColorsAndEmojis(t *testing.T) {
	e := setUpProgressBar(t)
	t.Setenv("COLUMNS", "156")

	out, buf := e.stream()
	bar := e.bar(out, 15, 0)
	i := 0
	SetPlaceholderFormatterDefinition("memory", func(*ProgressBar, Output) any {
		mem := 100000 * i
		colors := "44;37"
		if i != 0 {
			colors = "41;37"
		}
		i++

		return "\033[" + colors + "m " + FormatMemory(mem) + " \033[0m"
	})
	bar.SetFormat(" \033[44;37m %title:-37s% \033[0m\n %current%/%max% %bar% %percent:3s%%\n 🏁  %remaining:-10s% %memory:37s%")
	done := "\033[32m●\033[0m"
	empty := "\033[31m●\033[0m"
	progress := "\033[32m➤ \033[0m"
	bar.SetBarCharacter(done)
	bar.SetEmptyBarCharacter(empty)
	bar.SetProgressCharacter(progress)

	bar.SetMessage("Starting the demo... fingers crossed", "title")
	bar.Start()

	assertPB(t, " \033[44;37m Starting the demo... fingers crossed  \033[0m\n"+
		"  0/15 "+progress+strings.Repeat(empty, 26)+"   0%\n"+
		" \xf0\x9f\x8f\x81  < 1 sec                        \033[44;37m 0 B \033[0m", buf)
	buf.Reset()

	bar.SetMessage("Looks good to me...", "title")
	bar.Advance(4)

	assertPB(t, generateOutput(
		" \033[44;37m Looks good to me...                   \033[0m\n"+
			"  4/15 "+strings.Repeat(done, 7)+progress+strings.Repeat(empty, 19)+"  26%\n"+
			" \xf0\x9f\x8f\x81  < 1 sec                     \033[41;37m 97 KiB \033[0m"), buf)
	buf.Reset()

	bar.SetMessage("Thanks, bye", "title")
	bar.Finish()

	assertPB(t, generateOutput(
		" \033[44;37m Thanks, bye                           \033[0m\n"+
			" 15/15 "+strings.Repeat(done, 28)+" 100%\n"+
			" \xf0\x9f\x8f\x81  < 1 sec                    \033[41;37m 195 KiB \033[0m"), buf)
}

func TestProgressBar_SetFormat(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.SetFormat(ProgressFormatNormal)
	bar.Start()
	assertPB(t, "    0 [>---------------------------]", buf)

	out, buf = e.stream()
	bar = e.bar(out, 10, 0)
	bar.SetFormat(ProgressFormatNormal)
	bar.Start()
	assertPB(t, "  0/10 [>---------------------------]   0%", buf)
}

func TestProgressBar_Unicode(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 10, 0)
	SetFormatDefinition("test", pbLongFormat)
	bar.SetFormat("test")
	bar.SetProgressCharacter("💧")
	bar.Start()
	if !strings.Contains(buf.String(), " 0/10 [💧]   0%") {
		t.Errorf("output %q does not contain the bar", buf.String())
	}
	bar.Finish()
}

func TestProgressBar_FormatsWithoutMax(t *testing.T) {
	for _, format := range []string{"normal", "verbose", "very_verbose", "debug"} {
		t.Run(format, func(t *testing.T) {
			e := setUpProgressBar(t)
			out, buf := e.stream()
			bar := e.bar(out, 0, 0)
			bar.SetFormat(format)
			bar.Start()
			if buf.Len() == 0 {
				t.Error("empty output")
			}
		})
	}
}

func TestProgressBar_Iterate(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)

	var got []int
	for _, v := range ProgressIterateSlice(bar, []int{1, 2}) {
		got = append(got, v)
	}
	if !slices.Equal(got, []int{1, 2}) {
		t.Errorf("iterated %v", got)
	}

	assertPB(t, " 0/2 [>---------------------------]   0%"+
		generateOutput(" 1/2 [==============>-------------]  50%")+
		generateOutput(" 2/2 [============================] 100%"), buf)
}

func TestProgressBar_IterateUncountable(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)

	gen := func(yield func(int, int) bool) {
		if !yield(0, 1) {
			return
		}
		yield(1, 2)
	}
	var got []int
	for _, v := range ProgressIterate(bar, gen, 0) {
		got = append(got, v)
	}
	if !slices.Equal(got, []int{1, 2}) {
		t.Errorf("iterated %v", got)
	}

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    1 [->--------------------------]")+
		generateOutput("    2 [-->-------------------------]")+
		generateOutput("    2 [============================]"), buf)
}

func TestProgressBar_BarWidthWithMultilineFormat(t *testing.T) {
	e := setUpProgressBar(t)
	t.Setenv("COLUMNS", "10")

	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.SetFormat("%bar%\n0123456789")

	// before starting
	bar.SetBarWidth(5)
	if bar.BarWidth() != 5 {
		t.Errorf("bar width %d before start", bar.BarWidth())
	}

	// after starting
	bar.Start()
	if bar.BarWidth() != 5 {
		t.Errorf("bar width %d after start, output %q", bar.BarWidth(), buf.String())
	}
}

func TestProgressBar_MinAndMaxSecondsBetweenRedraws(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, DefaultMinSecondsBetweenRedraws)
	bar.SetRedrawFrequency(1)
	bar.MinSecondsBetweenRedraws(5)
	bar.MaxSecondsBetweenRedraws(10)

	bar.Start()
	bar.SetProgress(1)
	e.clock.sleep(10)
	bar.SetProgress(2)
	e.clock.sleep(20)
	bar.SetProgress(3)

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    2 [-->-------------------------]")+
		generateOutput("    3 [--->------------------------]"), buf)
}

func TestProgressBar_MaxSecondsBetweenRedraws(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.SetRedrawFrequency(4) // disable step based redraws
	bar.Start()

	bar.SetProgress(1) // No threshold hit, no redraw
	bar.MaxSecondsBetweenRedraws(2)
	e.clock.sleep(1)
	bar.SetProgress(2) // Still no redraw because it takes 2 seconds for a redraw
	e.clock.sleep(1)
	bar.SetProgress(3) // 1+1 = 2 -> redraw finally
	bar.SetProgress(4) // step based redraw freq hit, redraw even without sleep
	bar.SetProgress(5) // No threshold hit, no redraw
	bar.MaxSecondsBetweenRedraws(3)
	e.clock.sleep(2)
	bar.SetProgress(6) // No redraw even though 2 seconds passed. Throttling has priority
	bar.MaxSecondsBetweenRedraws(2)
	bar.SetProgress(7) // Throttling relaxed, draw

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    3 [--->------------------------]")+
		generateOutput("    4 [---->-----------------------]")+
		generateOutput("    7 [------->--------------------]"), buf)
}

func TestProgressBar_MinSecondsBetweenRedraws(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 0, 0)
	bar.SetRedrawFrequency(1)
	bar.MinSecondsBetweenRedraws(1)
	bar.Start()
	bar.SetProgress(1) // Too fast, should not draw
	e.clock.sleep(1)
	bar.SetProgress(2) // 1 second passed, draw
	bar.MinSecondsBetweenRedraws(2)
	e.clock.sleep(1)
	bar.SetProgress(3) // 1 second passed but we changed threshold, should not draw
	e.clock.sleep(1)
	bar.SetProgress(4) // 1+1 seconds = 2 seconds passed which conforms threshold, draw
	bar.SetProgress(5) // No threshold hit, no redraw

	assertPB(t, "    0 [>---------------------------]"+
		generateOutput("    2 [-->-------------------------]")+
		generateOutput("    4 [---->-----------------------]"), buf)
}

func TestProgressBar_NoWriteWhenMessageIsSame(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 2, DefaultMinSecondsBetweenRedraws)
	bar.Start()
	bar.Advance(1)
	bar.Display()

	assertPB(t, " 0/2 [>---------------------------]   0%"+
		generateOutput(" 1/2 [==============>-------------]  50%"), buf)
}

func TestProgressBar_MultiLineFormatIsFullyCleared(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.stream()
	bar := e.bar(out, 3, DefaultMinSecondsBetweenRedraws)
	bar.SetFormat("%current%/%max%\n%message%\nFoo")

	bar.SetMessage("1234567890", "message")
	bar.Start()
	bar.Display()

	bar.SetMessage("ABC", "message")
	bar.Advance(1)
	bar.Display()

	bar.SetMessage("A", "message")
	bar.Advance(1)
	bar.Display()

	bar.Finish()

	assertPB(t, "0/3\n1234567890\nFoo"+
		generateOutput("1/3\nABC\nFoo")+
		generateOutput("2/3\nA\nFoo")+
		generateOutput("3/3\nA\nFoo"), buf)
}

func TestProgressBar_MultiLineFormatIsFullyCorrectlyWithManuallyCleanup(t *testing.T) {
	e := setUpProgressBar(t)
	SetFormatDefinition("normal_nomax", "[%bar%]\n%message%")
	out, buf := e.stream()
	bar := e.bar(out, 0, DefaultMinSecondsBetweenRedraws)
	bar.SetMessage(`Processing "foobar"...`, "message")
	bar.Start()
	bar.Clear()
	out.Writeln("Foo!")
	bar.Display()
	bar.Finish()

	assertPB(t, "[>---------------------------]\n"+
		`Processing "foobar"...`+
		"\x1B[1G\x1B[2K\x1B[1A"+
		generateOutput("")+
		"Foo!\n"+
		generateOutput("[--->------------------------]")+
		"\nProcessing \"foobar\"..."+
		generateOutput("[----->----------------------]\nProcessing \"foobar\"..."), buf)
}

func TestProgressBar_GetNotSetMessage(t *testing.T) {
	e := setUpProgressBar(t)
	out, _ := e.stream()
	bar := e.bar(out, 0, DefaultMinSecondsBetweenRedraws)

	if _, ok := bar.Message("message"); ok {
		t.Error("message should be unset")
	}
}

// The remaining tests are not in ProgressBarTest.php; they pin the parts
// Composer relies on.

// TestProgressBar_ComposerLoop follows Composer\Util\Loop::wait() and
// InstallationManager: start($totalJobs), setProgress(max - active),
// finish(), clear() on a non-decorated output.
func TestProgressBar_ComposerLoop(t *testing.T) {
	e := setUpProgressBar(t)
	out, buf := e.output(false, VerbosityNormal)
	bar := e.bar(out, 0, DefaultMinSecondsBetweenRedraws)
	bar.StartMax(4)
	e.clock.sleep(1)
	bar.SetProgress(bar.MaxSteps() - 3)
	e.clock.sleep(1)
	bar.SetProgress(bar.MaxSteps() - 1)
	bar.Finish()
	bar.Clear()

	assertPB(t, " 0/4 [>---------------------------]   0%\n"+
		" 1/4 [=======>--------------------]  25%\n"+
		" 3/4 [=====================>------]  75%\n"+
		" 4/4 [============================] 100%", buf)
}

func TestProgressBar_RemainingWithoutMaxPanics(t *testing.T) {
	e := setUpProgressBar(t)
	out, _ := e.stream()
	bar := e.bar(out, 0, 0)
	bar.SetFormat("%remaining%")
	defer func() {
		e, ok := recover().(*Error)
		if !ok || e.Kind != KindLogic || e.Message != "Unable to display the remaining time if the maximum number of steps is not set." {
			t.Errorf("unexpected panic %v", e)
		}
	}()
	bar.Start()
}

func TestProgressBar_VerbosityFormats(t *testing.T) {
	cases := []struct {
		verbosity int
		maxSteps  int
		want      string
	}{
		{VerbosityVerbose, 10, "  0/10 [>---------------------------]   0% < 1 sec"},
		{VerbosityVerbose, 0, "    0 [>---------------------------] < 1 sec"},
		{VerbosityVeryVerbose, 10, "  0/10 [>---------------------------]   0% < 1 sec/< 1 sec"},
		{VerbosityVeryVerbose, 0, "    0 [>---------------------------] < 1 sec"},
	}
	for _, c := range cases {
		e := setUpProgressBar(t)
		out, buf := e.output(false, c.verbosity)
		bar := e.bar(out, c.maxSteps, 0)
		bar.Start()
		assertPB(t, c.want, buf)
	}
}

func TestPHPSprintf(t *testing.T) {
	cases := []struct {
		format string
		arg    any
		want   string
	}{
		{"%3s", "5", "  5"},
		{"%-6s|", "ab", "ab    |"},
		{"%06s", "ab", "0000ab"},
		{"%'*6s", "ab", "****ab"},
		{"%.2s", "abcdef", "ab"},
		{"%5.1f", 3.14159, "  3.1"},
		{"%05d", -12, "-0012"},
		{"%+d", 5, "+5"},
		{"%-05d", 12, "12000"},
		{"%x", 255, "ff"},
		{"%X", 255, "FF"},
		{"%b", 5, "101"},
		{"%o", 8, "10"},
		{"%e", 1234.5, "1.234500e+3"},
		{"%.1e", 0.000123, "1.2e-4"},
		{"%3s%%", 50.0, " 50%"},
		{"%d", "12abc", "12"},
		{"%u", -1, "18446744073709551615"},
		{"%c", 65, "A"},
		{"%1$s-%1$s", "x", "x-x"},
	}
	for _, c := range cases {
		if got := phpSprintf(c.format, c.arg); got != c.want {
			t.Errorf("sprintf(%q, %v) = %q, want %q", c.format, c.arg, got, c.want)
		}
	}
}
