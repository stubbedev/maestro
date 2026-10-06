package plugin

import (
	"bytes"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// ConsoleOutput::section() on one of maestro's outputs that is not its
// console: the section's stream writes to maestro's output.
func TestShimStubs_OutputSection(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	var stdout, stderr bytes.Buffer
	decorated := true
	out := console.NewConsoleOutputStreams(&stdout, &stderr, console.VerbosityNormal, &decorated, nil)
	got := evalPHP(t, rt, `
		$section = $vars['out']->section();
		$section->writeln('one');
		$section->overwrite('two');

		return get_class($vars['out']).' '.get_class($section);
	`, php.ArrayOf("out", rt.outputObject(out)))
	if got != `Maestro\Shim\GoConsoleOutput Symfony\Component\Console\Output\ConsoleSectionOutput` {
		t.Errorf("classes = %v", got)
	}
	// Symfony's: the line, then the cursor moved up and the line erased
	// before the new content.
	if want := "one\n\x1b[1A\x1b[0Jtwo\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}
