// Ports tests/Composer/Test/Platform/HhvmDetectorTest.php.

package platform

import (
	"errors"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

func newTestHhvmDetector(finder HhvmFinder, executor HhvmExecutor) *HhvmDetector {
	d := NewHhvmDetector(finder, executor)
	d.Reset()

	return d
}

func TestHhvmDetector_HHVMVersionWhenExecutingInHHVM(t *testing.T) {
	t.Skip("Not running with HHVM") // maestro never runs in HHVM
}

func TestHhvmDetector_HHVMVersionWhenExecutingInPHP(t *testing.T) {
	if util.IsWindows() {
		t.Skip("Test does not run on Windows")
	}

	hhvm, ok := util.NewExecutableFinder().Find("hhvm")
	if !ok {
		t.Skip("HHVM is not installed")
	}

	detected := newTestHhvmDetector(nil, nil).GetVersion()
	if detected == "" {
		t.Fatal("Failed to detect HHVM version")
	}

	var version string

	code, err := util.NewProcessExecutor(nil).Execute(util.ShellCmd(util.Escape(hhvm)+` --php -d hhvm.jit=0 -r "echo HHVM_VERSION;" 2>/dev/null`), &version, "")
	if err != nil || code != 0 {
		t.Fatal(code, err)
	}

	parser := semver.VersionParser{}
	want, _ := parser.Normalize(version)

	if got, _ := parser.Normalize(detected); got != want {
		t.Errorf("version %q, want %q", got, want)
	}
}

type fakeFinder struct {
	path  string
	found bool
	asked []string
}

func (f *fakeFinder) Find(name string, _ ...string) (string, bool) {
	f.asked = append(f.asked, name)

	return f.path, f.found
}

type fakeExecutor struct {
	output string
	code   int
	err    error
	runs   []string
}

func (e *fakeExecutor) Execute(command util.Command, output *string, _ string) (int, error) {
	e.runs = append(e.runs, command.String())
	*output = e.output

	return e.code, e.err
}

func TestHhvmDetector_Detection(t *testing.T) {
	if util.IsWindows() {
		t.Skip("HhvmDetector does not look for hhvm on Windows")
	}

	t.Cleanup(func() { (&HhvmDetector{}).Reset() })

	cases := []struct {
		name     string
		finder   *fakeFinder
		executor *fakeExecutor
		want     string
	}{
		{"not installed", &fakeFinder{}, &fakeExecutor{}, ""},
		{"installed", &fakeFinder{path: "/usr/bin/hhvm", found: true}, &fakeExecutor{output: "3.30.12"}, "3.30.12"},
		{"failing", &fakeFinder{path: "/usr/bin/hhvm", found: true}, &fakeExecutor{output: "x", code: 1}, ""},
		{"not starting", &fakeFinder{path: "/usr/bin/hhvm", found: true}, &fakeExecutor{err: errors.New("boom")}, ""},
	}

	for _, c := range cases {
		d := newTestHhvmDetector(c.finder, c.executor)

		if got := d.GetVersion(); got != c.want {
			t.Errorf("%s: GetVersion() = %q, want %q", c.name, got, c.want)
		}

		// The result is cached, like PHP's static.
		if got := NewHhvmDetector(c.finder, c.executor).GetVersion(); got != c.want || len(c.finder.asked) != 1 {
			t.Errorf("%s: second GetVersion() = %q after %d lookups", c.name, got, len(c.finder.asked))
		}

		if c.finder.found && !slices.Equal(c.executor.runs, []string{"'/usr/bin/hhvm' '--php' '-d' 'hhvm.jit=0' '-r' 'echo HHVM_VERSION;'"}) {
			t.Errorf("%s: ran %q", c.name, c.executor.runs)
		}
	}
}
