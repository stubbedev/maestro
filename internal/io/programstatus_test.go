// Ports the program status tests of tests/Composer/Test/IO/ConsoleIOTest.php
// (Composer).

package io

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
)

func statusOutput(decorated bool) *console.BufferedOutput {
	return console.NewBufferedOutput(console.VerbosityNormal, decorated, nil)
}

func statusHelperSet(t *testing.T) *console.HelperSet {
	t.Helper()

	return console.NewHelperSet(&fakeQuestionHelper{answer: true})
}

func TestConsoleIO_SelectedStatusProtocols(t *testing.T) {
	rich := "\033]7501;state=working:app=composer:progress=42\033\\"
	progress := "\033]9;4;1;42\033\\"
	cases := []struct {
		setting  string
		expected string
	}{
		{"off", ""},
		{"7501", rich},
		{"9;4", progress},
		{"7501,9;4", progress + rich},
	}
	for _, tc := range cases {
		t.Setenv("COMPOSER_TERMINAL_STATUS", tc.setting)
		output := statusOutput(true)
		cio := NewConsoleIO(&fakeInput{}, output, statusHelperSet(t))
		// Selection stays fixed for the lifetime of this IO instance.
		t.Setenv("COMPOSER_TERMINAL_STATUS", "off")
		cio.WriteProgramStatus(console.ProgramStatusWorking(nil).WithProgress(new(42)))
		if got := output.Fetch(); got != tc.expected {
			t.Errorf("%q: wrote %q, want %q", tc.setting, got, tc.expected)
		}
	}
}

func TestConsoleIO_CommandStartDoesNotActivateProgress(t *testing.T) {
	working := "\033]7501;state=working:app=composer\033\\"
	cases := []struct {
		setting  string
		expected string
	}{
		{"off", ""},
		{"7501", working},
		{"9;4", ""},
		{"7501,9;4", working},
	}
	for _, tc := range cases {
		t.Setenv("COMPOSER_TERMINAL_STATUS", tc.setting)
		output := statusOutput(true)
		cio := NewConsoleIO(&fakeInput{}, output, statusHelperSet(t))
		cio.StartProgramStatus()
		if got := output.Fetch(); got != tc.expected {
			t.Errorf("%q: wrote %q, want %q", tc.setting, got, tc.expected)
		}
	}
}

func TestConsoleIO_ProgressActivatesIndicatorAfterCommandStart(t *testing.T) {
	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501,9;4")
	output := statusOutput(true)
	cio := NewConsoleIO(&fakeInput{}, output, statusHelperSet(t))
	cio.StartProgramStatus()
	output.Fetch()

	cio.WriteProgress(42)
	if got, want := output.Fetch(), "\033]9;4;1;42\033\\\033]7501;state=working:app=composer:progress=42\033\\"; got != want {
		t.Errorf("progress = %q, want %q", got, want)
	}
	cio.WriteProgress(-1)
	if got, want := output.Fetch(), "\033]9;4;3\033\\\033]7501;state=working:app=composer\033\\"; got != want {
		t.Errorf("cleared progress = %q, want %q", got, want)
	}
}

func TestConsoleIO_PromptActivatesAndResumesIndicatorAfterCommandStart(t *testing.T) {
	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501,9;4")
	output := statusOutput(true)
	helper := &fakeQuestionHelper{answer: true}
	set := console.NewHelperSet(helper)
	cio := NewConsoleIO(&fakeInput{}, output, set)
	cio.StartProgramStatus()
	output.Fetch()

	if got, err := cio.AskConfirmation("Continue?", true); err != nil || !got {
		t.Fatalf("askConfirmation = %v, %v", got, err)
	}
	want := "\033]9;4;4\033\\\033]7501;state=blocked:app=composer:kind=permission:msg=" + base64.StdEncoding.EncodeToString([]byte("Continue?")) + "\033\\" +
		"\033]9;4;3\033\\\033]7501;state=working:app=composer\033\\"
	if got := output.Fetch(); got != want {
		t.Errorf("wrote %q, want %q", got, want)
	}
}

func TestConsoleIO_ProgramStatusEnabled(t *testing.T) {
	cio := NewConsoleIO(&fakeInput{}, statusOutput(true), statusHelperSet(t))
	if !cio.IsProgramStatusEnabled() {
		t.Error("decorated output: not enabled")
	}

	cio = NewConsoleIO(&fakeInput{}, statusOutput(false), statusHelperSet(t))
	if cio.IsProgramStatusEnabled() {
		t.Error("undecorated output: enabled")
	}

	t.Setenv("COMPOSER_TERMINAL_STATUS", "off")
	cio = NewConsoleIO(&fakeInput{}, statusOutput(true), statusHelperSet(t))
	if cio.IsProgramStatusEnabled() {
		t.Error("reporting off: enabled")
	}
}

func TestConsoleIO_ExplicitProtocolsRespectOutputSettings(t *testing.T) {
	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501,9;4")
	for _, output := range []*console.BufferedOutput{
		console.NewBufferedOutput(console.VerbosityQuiet, true, nil),
		console.NewBufferedOutput(console.VerbosityNormal, false, nil),
	} {
		cio := NewConsoleIO(&fakeInput{}, output, statusHelperSet(t))
		cio.WriteProgramStatus(console.ProgramStatusWorking(nil))
		if got := output.Fetch(); got != "" {
			t.Errorf("wrote %q", got)
		}
	}
}

func TestConsoleIO_ExplicitProtocolsRespectRedirectedStderr(t *testing.T) {
	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501,9;4")
	var stream bytes.Buffer
	stderr := console.NewStreamOutput(&stream, console.VerbosityNormal, new(true), nil)
	out := &fakeConsoleOutput{fakeOutput: &fakeOutput{verbosity: console.VerbosityNormal, errOut: stderr}}
	cio := NewConsoleIO(&fakeInput{}, out, statusHelperSet(t))
	cio.WriteProgramStatus(console.ProgramStatusWorking(nil))
	if got := stream.String(); got != "" {
		t.Errorf("wrote %q to stderr", got)
	}
	if len(out.writes) != 0 {
		t.Errorf("wrote to stdout: %#v", out.writes)
	}
}

func TestConsoleIO_OscHelperIsRegisteredAndReusedAcrossOutputs(t *testing.T) {
	helperSet := console.NewHelperSet()
	firstOutput := statusOutput(true)
	firstIO := NewConsoleIO(&fakeInput{}, firstOutput, helperSet)
	helper, err := helperSet.Get("osc")
	if err != nil {
		t.Fatal(err)
	}
	osc, ok := helper.(*console.OscHelper)
	if !ok {
		t.Fatalf("helper = %T", helper)
	}
	if osc.HelperSet() != helperSet {
		t.Error("helper set not linked")
	}

	secondOutput := statusOutput(true)
	secondIO := NewConsoleIO(&fakeInput{}, secondOutput, helperSet)
	if again, _ := helperSet.Get("osc"); again != helper {
		t.Error("helper not reused")
	}
	if err := firstIO.WriteOsc("2", "First"); err != nil {
		t.Fatal(err)
	}
	if err := secondIO.WriteOsc("2", "Second"); err != nil {
		t.Fatal(err)
	}
	if err := firstIO.WriteOsc("2", "First again"); err != nil {
		t.Fatal(err)
	}

	if got, want := firstOutput.Fetch(), "\033]2;First\033\\\033]2;First again\033\\"; got != want {
		t.Errorf("first = %q, want %q", got, want)
	}
	if got, want := secondOutput.Fetch(), "\033]2;Second\033\\"; got != want {
		t.Errorf("second = %q, want %q", got, want)
	}
}

func TestConsoleIO_OscUsesStderrWithoutProfilingPrefixes(t *testing.T) {
	stderr := statusOutput(true)
	out := &fakeConsoleOutput{fakeOutput: &fakeOutput{verbosity: console.VerbosityNormal, errOut: stderr}}
	cio := NewConsoleIO(&fakeInput{}, out, statusHelperSet(t))
	cio.EnableDebugging(time.Now())
	cio.EnableTimestamps("")
	if err := cio.WriteOsc("7501", "state=working"); err != nil {
		t.Fatal(err)
	}
	if got, want := stderr.Fetch(), "\033]7501;state=working\033\\"; got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}
}

func TestConsoleIO_QuestionReportsBlockedAndRestoresProgress(t *testing.T) {
	cases := []struct {
		method string
		kind   string
	}{
		{"ask", console.Osc7501KindQuestion},
		{"askConfirmation", console.Osc7501KindPermission},
		{"askAndHideAnswer", console.Osc7501KindAuth},
	}
	for _, tc := range cases {
		t.Setenv("COMPOSER_TERMINAL_STATUS", "7501")
		output := statusOutput(true)
		var askErr error
		helper := &fakeQuestionHelper{onAsk: func(console.Questioner) (any, error) {
			want := "\033]7501;state=blocked:app=composer:kind=" + tc.kind + ":msg=" + base64.StdEncoding.EncodeToString([]byte("Continue?")) + "\033\\"
			if got := output.Fetch(); got != want {
				askErr = errors.New(tc.method + ": wrote " + got + ", want " + want)
			}

			return true, nil
		}}
		cio := NewConsoleIO(&fakeInput{}, output, console.NewHelperSet(helper))
		status := console.ProgramStatusWorking(new("Installing")).WithProgress(new(42))
		cio.WriteProgramStatus(status)
		output.Fetch()
		switch tc.method {
		case "askConfirmation":
			_, _ = cio.AskConfirmation("<info>Continue?</info>", true)
		case "askAndHideAnswer":
			_, _ = cio.AskAndHideAnswer("<info>Continue?</info>")
		default:
			_, _ = cio.Ask("<info>Continue?</info>", nil)
		}
		if askErr != nil {
			t.Fatal(askErr)
		}
		want := "\033]7501;" + (&console.Osc7501ProgramStatusEncoder{}).Encode(status) + "\033\\"
		if got := output.Fetch(); got != want {
			t.Errorf("%s: restored %q, want %q", tc.method, got, want)
		}
	}
}

func TestConsoleIO_NonInteractiveQuestionDoesNotReportBlocked(t *testing.T) {
	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501")
	output := statusOutput(true)
	cio := NewConsoleIO(&fakeInput{interactive: []bool{false}}, output, console.NewHelperSet(&fakeQuestionHelper{answer: "default"}))

	if got, err := cio.Ask("Question?", "default"); err != nil || got != "default" {
		t.Fatalf("ask = %v, %v", got, err)
	}
	if got := output.Fetch(); got != "" {
		t.Errorf("wrote %q", got)
	}
}

func TestConsoleIO_FailedQuestionRestoresWorkingStatus(t *testing.T) {
	t.Setenv("COMPOSER_TERMINAL_STATUS", "7501")
	output := statusOutput(true)
	helper := &fakeQuestionHelper{err: errors.New("No input")}
	cio := NewConsoleIO(&fakeInput{}, output, console.NewHelperSet(helper))
	if _, err := cio.Ask("Question?", nil); err == nil || err.Error() != "No input" {
		t.Fatalf("ask = %v", err)
	}
	got := output.Fetch()
	want := "\033]7501;state=working:app=composer\033\\"
	if !strings.HasSuffix(got, want) {
		t.Errorf("wrote %q, want it to end with %q", got, want)
	}
}
