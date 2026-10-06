package plugin

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// ConsoleIO's getTable(), getProgressBar(), enableTimestamps() and
// enableDebugging() on maestro's IO.
func TestShimStubs_ConsoleIO(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "commands")

	evalPHP(t, p.rt, `
		$io = $vars['io'];
		$io->getTable()->setHeaders(['Name', 'Version'])->setRows([['acme/lib', '1.0.0']])->render();
		$bar = $io->getProgressBar(2);
		$bar->start();
		$bar->advance();
		$bar->finish();
		$io->writeError('');
		$io->enableTimestamps('\Y\e\a\r Y');
		$io->write('stamped');
		$io->enableDebugging(microtime(true) - 3);
		$io->write('profiled');
	`, php.ArrayOf("io", p.rt.value(p.out)))

	out := p.output()
	year := time.Now().UTC().Format("2006")
	for _, want := range []string{
		"+----------+---------+\n| Name     | Version |\n+----------+---------+\n| acme/lib | 1.0.0   |\n+----------+---------+\n",
		" 2/2 [============================] 100%\n",
		"[Year " + year + "] stamped\n",
		"[Year " + year + "] [",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !regexp.MustCompile(`\[[0-9.]+MiB/3\.[0-9]{2}s\] profiled\n`).MatchString(out) {
		t.Errorf("no profile prefix:\n%s", out)
	}
}
