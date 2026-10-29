// Ports tests/Composer/Test/Console/ProgramStatusTest.php (Composer).

package console

import (
	"os"
	"reflect"
	"testing"
)

func TestProgramStatus_FromExitCode(t *testing.T) {
	cases := []struct {
		name     string
		exitCode int
		state    string
	}{
		{"success", 0, ProgramStatusStateDone},
		{"failure", 1, ProgramStatusStateError},
		{"command not found", 127, ProgramStatusStateError},
		{"hangup", 129, ProgramStatusStateIdle},
		{"interrupted", 130, ProgramStatusStateIdle},
		{"terminated", 143, ProgramStatusStateIdle},
		{"other signal", 137, ProgramStatusStateError},
		{"negative exit code", -1, ProgramStatusStateError},
	}
	for _, tc := range cases {
		if got := ProgramStatusFromExitCode(tc.exitCode).State(); got != tc.state {
			t.Errorf("%s: state = %q, want %q", tc.name, got, tc.state)
		}
	}
}

func TestProgramStatus_ProgressReturnsCloneAndCanBeCleared(t *testing.T) {
	status := ProgramStatusWorking(nil)
	progress := status.WithProgress(new(40))
	if progress == status {
		t.Error("withProgress returned the same status")
	}
	if status.State() != ProgramStatusStateWorking || status.Progress() != nil {
		t.Errorf("original = %#v", status)
	}
	if progress.Progress() == nil || *progress.Progress() != 40 {
		t.Errorf("progress = %v", progress.Progress())
	}
	if progress.WithProgress(nil).Progress() != nil {
		t.Error("progress was not cleared")
	}
}

func TestProgramStatus_RejectsUnknownState(t *testing.T) {
	if _, err := NewProgramStatus("unknown", nil); err == nil {
		t.Error("want an error")
	}
}

func TestProgramStatus_ProtocolOptionsReturnIndependentClones(t *testing.T) {
	original := ProgramStatusBlocked(new("Continue?")).WithProgress(new(42))
	first := original.WithProtocolOption("osc7501", "kind", "foobar")
	second := first.WithProtocolOption("oscFuture", "kind", false)
	replaced := second.WithProtocolOption("osc7501", "kind", "permission")

	if original == first || first == second || second == replaced {
		t.Error("withProtocolOption returned the same status")
	}
	if original.GetProtocolOption("osc7501", "kind") != nil {
		t.Error("original has the option")
	}
	if first.GetProtocolOption("oscFuture", "kind") != nil {
		t.Error("first has the second's option")
	}
	if got := second.GetProtocolOption("osc7501", "kind"); got != "foobar" {
		t.Errorf("second kind = %v", got)
	}
	if got := second.GetProtocolOption("oscFuture", "kind"); got != false {
		t.Errorf("second oscFuture kind = %v", got)
	}
	if got := replaced.GetProtocolOption("osc7501", "kind"); got != "permission" {
		t.Errorf("replaced kind = %v", got)
	}
	if replaced.Message() == nil || *replaced.Message() != "Continue?" {
		t.Errorf("message = %v", replaced.Message())
	}
	if replaced.Progress() == nil || *replaced.Progress() != 42 {
		t.Errorf("progress = %v", replaced.Progress())
	}
	if replaced.WithProtocolOption("osc7501", "kind", nil).GetProtocolOption("osc7501", "kind") != nil {
		t.Error("null option is not null")
	}
	if got := replaced.GetProtocolOption("osc7501", "kind"); got != "permission" {
		t.Errorf("replaced kind after clearing a clone = %v", got)
	}
}

type stdoutObject struct{ name string }

func TestProgramStatus_PreservesProtocolOptionValues(t *testing.T) {
	values := []any{
		false,
		true,
		0,
		42,
		1.5,
		"",
		"value",
		nil,
		[]any{},
		[]any{"a", false, 42},
		map[string]any{"nested": map[string]any{"enabled": false, "limit": nil}},
		&stdoutObject{name: "object"},
		func() {},
		map[string]any{"nested": &stdoutObject{name: "object"}},
	}
	for _, value := range values {
		status := ProgramStatusWorking(nil).WithProtocolOption("oscFuture", "option", value)
		got := status.GetProtocolOption("oscFuture", "option")
		switch {
		case reflect.ValueOf(value).Kind() == reflect.Func:
			if reflect.ValueOf(got).Pointer() != reflect.ValueOf(value).Pointer() {
				t.Errorf("option = %v, want the same function", got)
			}
		case !reflect.DeepEqual(got, value):
			t.Errorf("option = %#v, want %#v", got, value)
		}
	}
}

func TestProgramStatus_PreservesResourceProtocolOption(t *testing.T) {
	stream, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	status := ProgramStatusWorking(nil).WithProtocolOption("oscFuture", "stream", stream)
	if got := status.GetProtocolOption("oscFuture", "stream"); got != stream {
		t.Errorf("option = %v, want the stream", got)
	}
}

func TestProgramStatus_IgnoresInvalidOrInapplicableProgress(t *testing.T) {
	cases := []struct {
		name     string
		original *ProgramStatus
		progress int
	}{
		{"idle", ProgramStatusIdle(nil), 42},
		{"done", ProgramStatusDone(nil), 42},
		{"error", ProgramStatusError(nil), 42},
		{"clear", ProgramStatusClear(nil), 42},
		{"negative working progress", ProgramStatusWorking(nil), -1},
		{"excess working progress", ProgramStatusWorking(nil), 101},
		{"negative blocked progress", ProgramStatusBlocked(nil), -1},
		{"excess blocked progress", ProgramStatusBlocked(nil), 101},
	}
	for _, tc := range cases {
		status := tc.original.WithProgress(new(tc.progress))
		if status == tc.original {
			t.Errorf("%s: withProgress returned the same status", tc.name)
		}
		if status.State() != tc.original.State() || status.Progress() != nil {
			t.Errorf("%s: status = %#v", tc.name, status)
		}
	}
}
