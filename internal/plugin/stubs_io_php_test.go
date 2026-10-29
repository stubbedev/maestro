package plugin

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
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

// ConsoleIO's writeOsc(), isProgramStatusEnabled(), startProgramStatus(),
// writeProgramStatus() and writeProgress(): maestro's (io.*) on its IO
// mirror, and a ConsoleIO created in PHP, which has no terminal encoders.
func TestShimStubs_ConsoleIOProgramStatus(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "commands")

	got := evalPHP(t, p.rt, `
		$io = $vars['io'];
		$enabled = $io->isProgramStatusEnabled();
		$io->startProgramStatus();
		$io->writeProgramStatus(\Composer\Console\ProgramStatus::working('Working'));
		$io->writeProgress(40);
		$io->writeProgress(null);
		$io->writeProgramStatus(\Composer\Console\ProgramStatus::fromExitCode(0));
		$io->writeOsc('0', 'maestro');
		$own = new \Composer\IO\ConsoleIO(
			new \Symfony\Component\Console\Input\ArrayInput([]),
			new \Symfony\Component\Console\Output\NullOutput(),
			new \Symfony\Component\Console\Helper\HelperSet()
		);
		$own->writeProgramStatus(\Composer\Console\ProgramStatus::blocked('Blocked')->withProgress(10));
		$ownEnabled = $own->isProgramStatusEnabled();
		$own->startProgramStatus();
		$own->writeProgress(20);
		$own->writeOsc('9', 'notification');

		$status = \Maestro\Shim\Values::decode(["\0c" => 'programStatus', 'state' => 'blocked', 'message' => 'W', 'progress' => 40]);

		return [
			'decoded' => get_class($status).' '.$status->getState().' '.$status->getMessage().' '.$status->getProgress(),
			'enabled' => $enabled ? 'yes' : 'no',
			'ownEnabled' => $ownEnabled ? 'yes' : 'no',
			'progress' => \Composer\Console\ProgramStatus::working()->withProgress(50)->getProgress(),
			'notWorking' => \Composer\Console\ProgramStatus::idle()->withProgress(50)->getProgress(),
			'exitCode' => \Composer\Console\ProgramStatus::fromExitCode(0)->getState(),
		];
	`, php.ArrayOf("io", p.rt.value(p.out)))

	for key, want := range map[string]string{"enabled": "no", "ownEnabled": "no", "progress": "50", "notWorking": "", "exitCode": "done", "decoded": `Composer\Console\ProgramStatus blocked W 40`} {
		if g := php.ToString(get(t, got, key)); g != want {
			t.Errorf("%s = %q, want %q", key, g, want)
		}
	}
	if out := p.output(); out != "" {
		t.Errorf("output:\n%s", out)
	}
}

// programStatusValue is the value tag Maestro\Shim\Values decodes into
// Composer's ProgramStatus (see TestShimStubs_ConsoleIOProgramStatus).
func TestShimStubs_ProgramStatusValue(t *testing.T) {
	message := "W"
	progress := 40
	encoded, err := programStatusValue{console.ProgramStatusBlocked(&message).WithProgress(&progress)}.EncodeRPC(nil)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"\x00c": "programStatus", "state": "blocked", "message": "W", "progress": int64(40)} {
		if got, _ := encoded.Get(key); got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}
