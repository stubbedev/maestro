// Ports tests/Composer/Test/Console/OscHelperTest.php (Composer).

package console

import (
	"bytes"
	"testing"
)

func TestOscHelper_WritesRawSequenceWithoutNewline(t *testing.T) {
	output := NewBufferedOutput(VerbosityNormal, true, nil)
	if err := NewOscHelper().Write(output, "2", "<info>Title</info>"); err != nil {
		t.Fatal(err)
	}
	if got, want := output.Fetch(), "\033]2;<info>Title</info>\033\\"; got != want {
		t.Errorf("write = %q, want %q", got, want)
	}
}

func TestOscHelper_SuppressesUndecoratedAndQuietOutput(t *testing.T) {
	for _, output := range []*BufferedOutput{
		NewBufferedOutput(0, false, nil),
		NewBufferedOutput(VerbosityQuiet, true, nil),
	} {
		if err := NewOscHelper().Write(output, "7501", "state=working"); err != nil {
			t.Fatal(err)
		}
		if got := output.Fetch(); got != "" {
			t.Errorf("write = %q", got)
		}
	}
}

func TestOscHelper_ForcedAnsiDoesNotWriteToRedirectedStream(t *testing.T) {
	var stream bytes.Buffer
	output := NewStreamOutput(&stream, VerbosityNormal, new(true), nil)
	if err := NewOscHelper().Write(output, "7501", "state=working"); err != nil {
		t.Fatal(err)
	}
	if got := stream.String(); got != "" {
		t.Errorf("write = %q", got)
	}
}

func TestOscHelper_MsysDoesNotEnableRedirectedOutput(t *testing.T) {
	for _, environment := range []string{"MINGW32", "MINGW64"} {
		t.Run(environment, func(t *testing.T) {
			t.Setenv("MSYSTEM", environment)
			TestOscHelper_ForcedAnsiDoesNotWriteToRedirectedStream(t)
		})
	}
}

func TestOscHelper_RejectsControlCharacters(t *testing.T) {
	for _, payload := range []string{"title\033\\", "title\a", "title\n", "title\u009c"} {
		if err := NewOscHelper().Write(NewBufferedOutput(0, false, nil), "2", payload); err == nil {
			t.Errorf("%q was accepted", payload)
		}
	}
}

func TestOscHelper_WritesTextCommand(t *testing.T) {
	output := NewBufferedOutput(VerbosityNormal, true, nil)
	if err := NewOscHelper().Write(output, "future-command", "status=working"); err != nil {
		t.Fatal(err)
	}
	if got, want := output.Fetch(), "\033]future-command;status=working\033\\"; got != want {
		t.Errorf("write = %q, want %q", got, want)
	}
}

func TestOscHelper_RejectsControlCharactersInCommand(t *testing.T) {
	for _, command := range []string{"title\033\\", "title\a", "title\n", "title\u009c"} {
		if err := NewOscHelper().Write(NewBufferedOutput(0, false, nil), command, "title"); err == nil {
			t.Errorf("%q was accepted", command)
		}
	}
}
