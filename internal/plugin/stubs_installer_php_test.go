package plugin

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

// Installer's protected doUpdate() and doInstall(), as a subclass calls
// them: the phase alone, with the Installer's settings.
func TestShimStubs_InstallerPhases(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "commands")
	if code, err := p.install(true); err != nil || code != 0 {
		t.Fatalf("install = %d, %v\n%s", code, err, p.output())
	}
	var err error
	if p.out, err = io.NewBufferIO("", console.VerbosityNormal, console.NewOutputFormatter(false)); err != nil {
		t.Fatal(err)
	}

	got := evalPHP(t, p.rt, `
		$io = $vars['io'];
		$composer = \Composer\Factory::create($io, null, true);
		$installer = \Composer\Installer::create($io, $composer)->setAudit(false);
		$localRepo = $composer->getRepositoryManager()->getLocalRepository();
		$call = function (string $method, ...$args) {
			return $this->$method(...$args);
		};
		$call = \Closure::bind($call, $installer, \Composer\Installer::class);

		$out = [];
		$out[] = 'update '.$call('doUpdate', $localRepo, false).' '.get_class($installer->getLockTransaction() ?? new \stdClass());
		$out[] = 'install '.$call('doInstall', $localRepo, true);

		return $out;
	`, php.ArrayOf("io", p.rt.value(p.out)))

	want := "update 0 Composer\\DependencyResolver\\LockTransaction\ninstall 0"
	if g := lines(got); g != want {
		t.Errorf("got\n%s\nwant\n%s\noutput:\n%s", g, want, p.output())
	}
	out := p.output()
	for _, w := range []string{
		"Updating dependencies\n",
		"Nothing to modify in lock file\n",
		"Installing dependencies from lock file\n",
		"Nothing to install, update or remove\n",
	} {
		if !strings.Contains(out, w) {
			t.Errorf("output lacks %q:\n%s", w, out)
		}
	}
}
