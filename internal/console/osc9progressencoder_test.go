// Ports tests/Composer/Test/Console/Osc9ProgressEncoderTest.php (Composer).

package console

import "testing"

func TestOsc9ProgressEncoder_CommandStartDoesNotActivateProgress(t *testing.T) {
	if payload := (&Osc9ProgressEncoder{}).EncodeStart(); payload != nil {
		t.Errorf("encodeStart = %v", payload)
	}
}

func TestOsc9ProgressEncoder_EncodesProgress(t *testing.T) {
	cases := []struct {
		name     string
		status   *ProgramStatus
		expected string
	}{
		{"unknown progress", ProgramStatusWorking(nil), "3"},
		{"zero progress", ProgramStatusWorking(nil).WithProgress(new(0)), "1;0"},
		{"percentage", ProgramStatusWorking(new("Installing")).WithProgress(new(42)), "1;42"},
		{"full progress", ProgramStatusWorking(nil).WithProgress(new(100)), "1;100"},
		{"blocked", ProgramStatusBlocked(new("Password?")).WithProtocolOption(Osc7501Protocol, "kind", "auth"), "4"},
		{"blocked with progress", ProgramStatusBlocked(nil).WithProgress(new(42)), "4;42"},
		{"error", ProgramStatusError(new("Failed")), "2"},
		{"done", ProgramStatusDone(nil), "0"},
		{"idle", ProgramStatusIdle(nil), "0"},
		{"clear", ProgramStatusClear(nil), "0"},
		{"cancelled", ProgramStatusFromExitCode(130), "0"},
	}
	encoder := &Osc9ProgressEncoder{}
	if got := encoder.Command(); got != "9;4" {
		t.Errorf("command = %q", got)
	}
	for _, tc := range cases {
		if got := encoder.Encode(tc.status); got != tc.expected {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.expected)
		}
	}
}

func TestOsc9ProgressEncoder_IgnoresOtherProtocolOptions(t *testing.T) {
	status := ProgramStatusBlocked(nil).
		WithProtocolOption(Osc7501Protocol, "kind", "foobar").
		WithProtocolOption("oscFuture", "whatnot", false)
	if got := (&Osc9ProgressEncoder{}).Encode(status); got != "4" {
		t.Errorf("got %q", got)
	}
}
