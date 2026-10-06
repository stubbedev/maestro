package phperr

import (
	"reflect"
	"testing"
)

func TestLive(t *testing.T) {
	run := Within(Frame{Function: `Symfony\Component\Console\Application->run`, File: "Application.php", Line: 141},
		Frame{Function: `Composer\Console\Application->run`, File: "bin/composer", Line: 113})
	dispatch := Enter(`Composer\EventDispatcher\EventDispatcher->doDispatch`, "EventDispatcher.php", 142)
	listener := EnterCode("EventDispatcher.php", 232)
	callback := Callback()
	inner := Enter(`Composer\Installer->run`, "InstallCommand.php", 152)

	want := [][]Frame{
		{{Function: `Composer\Installer->run`, File: "InstallCommand.php", Line: 152}},
		{
			{File: "EventDispatcher.php", Line: 232},
			{Function: `Composer\EventDispatcher\EventDispatcher->doDispatch`, File: "EventDispatcher.php", Line: 142},
			{Function: `Symfony\Component\Console\Application->run`, File: "Application.php", Line: 141},
			{Function: `Composer\Console\Application->run`, File: "bin/composer", Line: 113},
		},
	}
	if got := Live(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Live() = %v, want %v", got, want)
	}

	// leaving records the call in the error's trace, as Call does
	err := &testError{msg: "boom"}
	if got := inner(err); got != error(err) { //nolint:errorlint // the error itself is returned
		t.Fatalf("Enter's function returned %v", got)
	}
	if trace := TraceOf(err); len(trace) != 1 || trace[0].Function != `Composer\Installer->run` {
		t.Errorf("trace %v", trace)
	}

	// what an outer call leaves goes with it (a panic left it there)
	_ = Callback()
	callback()
	if got := Live(); len(got) != 1 || len(got[0]) != 4 {
		t.Errorf("after the callback: %v", got)
	}
	_ = listener(nil)
	_ = dispatch(nil)
	run()
	if got := Live(); len(got) != 1 || len(got[0]) != 0 {
		t.Errorf("after all: %v", got)
	}
}

type testError struct {
	msg string
	Frames
}

func (e *testError) Error() string { return e.msg }
