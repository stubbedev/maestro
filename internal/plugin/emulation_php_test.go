package plugin

// The internals emulation of docs/PLUGINS.md §5.12 beyond the surveyed
// plugins' needs (issue #2): what a Pool from maestro holds, asynchronous
// processes of PHP code on maestro's loop, Composer's call stack in the
// exceptions crossing between maestro and PHP, and the frames
// debug_backtrace() shows.

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/resolver"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// An exception crossing between maestro and PHP carries Composer's call
// stack: a Go error thrown into PHP has Composer's file (under
// phperr.Root) and the frames maestro recorded, then the PHP stack it is
// thrown into, without the shim's machinery; a PHP exception going to
// maestro has its frames down to the call maestro made, which maestro
// completes, and going back to PHP keeps them.
func TestInternals_Traces(t *testing.T) {
	requirePHP(t)

	withComposerRoot(t)
	rt, _, _ := newTestRuntime(t)
	rt.Handle("test.goFail", func(any) (any, error) {
		err := &util.RuntimeError{Message: "nope", Site: phperr.At("Factory.php", 317)}

		return nil, phperr.Call(err, `Composer\Factory::createConfig`, "Factory.php", 300)
	})
	// a PHP exception going up maestro's ports and back into PHP
	rt.Handle("test.relay", func(any) (any, error) {
		_, err := rt.Call("test.throw", php.ArrayOf("message", "deep", "code", 1))

		return nil, phperr.Call(err, `Composer\Installer->run`, "InstallCommand.php", 150)
	})
	start(t, rt)

	frame := func(f any) string {
		a, _ := f.(*php.Array)
		s := func(k string) string { v, _ := a.Get(k); return php.ToString(v) }

		return s("class") + s("type") + s("function") + " " + s("file") + ":" + s("line")
	}

	// (a named function: the test handlers' closures have the shim's
	// scope, which plugin code never has)
	got := evalPHP(t, rt, `
		if (!function_exists('maestroTestGoFail')) {
			function maestroTestGoFail() {
				\Maestro\Shim\Rpc::call('test.goFail', []);
			}
		}
		try {
			maestroTestGoFail();
		} catch (\RuntimeException $e) {
			return ['file' => $e->getFile().':'.$e->getLine(), 'trace' => $e->getTrace()];
		}
	`, nil)
	if f := php.ToString(get(t, got, "file")); f != "phar:///maestro/src/Composer/Factory.php:317" {
		t.Errorf("getFile():getLine() = %s", f)
	}
	trace, _ := get(t, got, "trace").(*php.Array)
	if trace == nil || trace.Len() < 2 {
		t.Fatalf("trace %v", get(t, got, "trace"))
	}
	if f := frame(trace.Values()[0]); f != `Composer\Factory::createConfig phar:///maestro/src/Composer/Factory.php:300` {
		t.Errorf("first frame %s", f)
	}
	for _, f := range trace.Values()[1:] {
		if s := frame(f); strings.Contains(s, `Maestro\Shim\Rpc`) || strings.Contains(s, `Exceptions`) {
			t.Errorf("a frame of the shim's machinery: %s", s)
		}
	}

	_, err := rt.Call("test.eval", php.ArrayOf("code", `
		if (!function_exists('maestroTestRelay')) {
			function maestroTestRelay() {
				\Maestro\Shim\Rpc::call('test.relay', []);
			}
		}
		maestroTestRelay();
	`))
	pe, ok := errors.AsType[*rpc.PHPException](err)
	if !ok {
		t.Fatalf("err = %v", err)
	}
	var names []string
	for _, f := range pe.Trace {
		names = append(names, f.Class+f.Type+f.Function)
	}
	// the handler that threw (its call by the shim's dispatcher, which
	// maestro did not locate), the frame maestro added going up, the
	// function maestroTestRelay() the exception went on in, ...
	if len(names) < 3 || names[2] != "maestroTestRelay" || names[1] != `Composer\Installer->run` || pe.Trace[1].File != "phar:///maestro/src/Composer/Command/InstallCommand.php" {
		t.Errorf("trace %+v", pe.Trace)
	}
	if _, ok := pe.OpenFrame(); !ok {
		t.Error("the trace does not end at the call maestro made")
	}
}

// The frames plugin code finds with debug_backtrace() are Composer's
// method calls, with their class, function, object and arguments: the
// Application's doRun() and the running command's initialize(), where
// PRE_COMMAND_RUN is dispatched.
func TestInternals_Frames(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "commands")
	if code, err := p.install(true); err != nil || code != 0 {
		t.Fatalf("install = %d, %v\n%s", code, err, p.output())
	}
	if out, code := p.runApp("licenses"); code != 0 {
		t.Fatalf("licenses = %d\n%s", code, out)
	}
	got := lines(evalPHP(t, p.rt, `return $GLOBALS['maestroTestFrames']['licenses'];`, nil))
	want := `Composer\Command\BaseCommand->initialize(2) Composer\Command\LicensesCommand
Composer\Console\Application->doRun(2) Composer\Console\Application`
	if got != want {
		t.Errorf("frames during licenses' PRE_COMMAND_RUN:\n%s\nwant\n%s", got, want)
	}
}

// The internals project's plugin is activated while the Installer
// installs it (PluginInstaller's registerPackage($package, true)), and
// loaded by the PluginManager when the next run starts: the frames of its
// activate() are Composer's.
func TestInternals_PluginManagerFrames(t *testing.T) {
	requirePHP(t)

	p := newProject(t, "internals", console.VerbosityNormal)
	if code, err := p.install(true); err != nil || code != 0 {
		t.Fatalf("update = %d, %v\n%s", code, err, p.output())
	}
	if !strings.Contains(p.output(), `activate frames: Composer\Plugin\PluginManager->addPlugin(3), Composer\Plugin\PluginManager->registerPackage(2), Composer\Installer->doInstall(2), Composer\Installer->doUpdate(2), Composer\Installer->run(0)`+"\n") {
		t.Errorf("the frames of activate() during the install:\n%s", p.output())
	}
	if code, err := p.install(true); err != nil || code != 0 {
		t.Fatalf("install = %d, %v\n%s", code, err, p.output())
	}
	if !strings.Contains(p.output(), `activate frames: Composer\Plugin\PluginManager->addPlugin(3), Composer\Plugin\PluginManager->registerPackage(3), Composer\Plugin\PluginManager->loadRepository(3), Composer\Plugin\PluginManager->loadInstalledPlugins(0)`+"\n") {
		t.Errorf("the frames of activate() when Composer loads:\n%s", p.output())
	}
}

// A process PHP code starts asynchronously on its loop's executor makes
// progress while maestro's loop waits, not only while PHP does: Composer
// has one loop, whose wait() drives the processes of its executor
// whoever started them, and counts them.
func TestInternals_AsyncProcessesOnMaestrosLoop(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "commands")

	loop := evalPHP(t, p.rt, `
		$loop = \Composer\Factory::create($vars['io'], null, true)->getLoop();
		$GLOBALS['maestroAsync'] = [];
		foreach (['one', 'two'] as $word) {
			$loop->getProcessExecutor()->executeAsync('sleep 0.2; echo '.$word)->then(function ($process) {
				$GLOBALS['maestroAsync'][] = trim($process->getOutput());
			});
		}

		return $loop;
	`, php.ArrayOf("io", p.rt.value(p.out)))
	l, ok := unwrap(loop).(*http.Loop)
	if !ok {
		t.Fatalf("the loop is a %T", unwrap(loop))
	}

	if n, err := l.CountActiveJobs(); err != nil || n != 2 {
		t.Errorf("CountActiveJobs = %d, %v; want PHP's 2 processes", n, err)
	}
	// maestro's own wait (an Installer's, a download's) runs them to
	// the end, and their callbacks (in the order the processes finish)
	if err := l.Wait(nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := lines(evalPHP(t, p.rt, `$seen = $GLOBALS['maestroAsync']; sort($seen); return $seen;`, nil)); got != "one\ntwo" {
		t.Errorf("the callbacks saw %q", got)
	}
	if n, err := l.CountActiveJobs(); err != nil || n != 0 {
		t.Errorf("CountActiveJobs after the wait = %d, %v", n, err)
	}

	// PHP's own wait still does, and a timeout ends it as it ends
	// Composer's.
	got := evalPHP(t, p.rt, `
		$loop = $vars['loop'];
		$out = [];
		$loop->getProcessExecutor()->executeAsync('echo three')->then(function ($process) use (&$out) {
			$out[] = trim($process->getOutput());
		});
		$loop->wait([]);
		\Composer\Util\ProcessExecutor::setTimeout(1);
		$loop->getProcessExecutor()->executeAsync('sleep 5');
		try {
			$loop->wait([]);
		} catch (\Symfony\Component\Process\Exception\ProcessTimedOutException $e) {
			$out[] = get_class($e);
		}
		\Composer\Util\ProcessExecutor::setTimeout(300);

		return $out;
	`, php.ArrayOf("loop", loop))
	if want := "three\nSymfony\\Component\\Process\\Exception\\ProcessTimedOutException"; lines(got) != want {
		t.Errorf("PHP's waits: %q, want %q", lines(got), want)
	}
}

// A Pool from maestro holds what Composer's PoolBuilder passes to new
// Pool(): the optimizer's removals by name and by kept package, and the
// security advisories' and filter lists' removals with Composer's objects,
// one per advisory or entry however many versions it removed.
func TestInternals_PoolRemovals(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	kept := loadPackage(t, `{"name": "a/a", "version": "1.2.0"}`)
	other := loadPackage(t, `{"name": "b/b", "version": "2.0.0"}`)
	constraint := func(s string) semver.ConstraintInterface {
		c, err := semver.VersionParser{}.ParseConstraints(s)
		if err != nil {
			t.Fatal(err)
		}

		return c
	}

	versions := func(pairs ...string) *resolver.VersionMap {
		m := &resolver.VersionMap{}
		for i := 0; i < len(pairs); i += 2 {
			m.Set(pairs[i], pairs[i+1])
		}

		return m
	}
	removed := &repository.NameMap[*resolver.VersionMap]{}
	removed.Set("a/a", versions("1.1.0.0", "1.1.0", "1.0.0.0", "1.0.0"))

	adv := &repository.PartialSecurityAdvisory{AdvisoryID: "PKSA-1", PackageName: "c/c", AffectedVersions: constraint("<2")}
	security := &repository.NameMap[*repository.NameMap[[]repository.Advisory]]{}
	byVersion := &repository.NameMap[[]repository.Advisory]{}
	byVersion.Set("1.0.0.0", []repository.Advisory{adv})
	byVersion.Set("1.5.0.0", []repository.Advisory{adv})
	security.Set("c/c", byVersion)

	entry := &repository.FilterListEntry{
		PackageName: "d/d", ListName: "malware", Constraint: constraint("*"),
		URL: pkg.NullString{S: "https://example.org/d", Valid: true}, Source: pkg.NullString{S: "acme", Valid: true},
	}
	filterList := &repository.NameMap[*repository.NameMap[[]*repository.FilterListEntry]]{}
	entries := &repository.NameMap[[]*repository.FilterListEntry]{}
	entries.Set("3.0.0.0", []*repository.FilterListEntry{entry})
	entries.Set("3.1.0.0", []*repository.FilterListEntry{entry})
	filterList.Set("d/d", entries)

	pool := resolver.NewPool([]pkg.PackageInterface{kept, other}, nil, &resolver.Removed{
		Versions:   removed,
		ByPackage:  map[pkg.PackageInterface]*resolver.VersionMap{kept: versions("1.1.0.0", "1.1.0")},
		Security:   security,
		FilterList: filterList,
	})

	got := evalPHP(t, rt, `
		$pool = \Closure::bind(function ($d) {
			return self::pool($d);
		}, null, \Composer\Repository\RepositorySet::class)($vars['pool']);
		$any = new \Composer\Semver\Constraint\MatchAllConstraint();
		$sec = $pool->getAllSecurityRemovedPackageVersions();
		$lists = $pool->getAllFilterListRemovedPackageVersions();
		$kept = $pool->packageById(1);

		return [
			'removed' => json_encode($pool->getAllRemovedVersions()),
			'byPackage' => json_encode($pool->getRemovedVersionsByPackage(spl_object_id($kept))),
			'security' => $pool->isSecurityRemovedPackageVersion('c/c', $any) ? 'yes' : 'no',
			'ids' => implode(',', $pool->getSecurityAdvisoryIdentifiersForPackageVersion('c/c', $any)),
			'advisoryClass' => get_class($sec['c/c']['1.0.0.0'][0]),
			'sameAdvisory' => $sec['c/c']['1.0.0.0'][0] === $sec['c/c']['1.5.0.0'][0] ? 'yes' : 'no',
			'filtered' => $pool->isFilterListRemovedPackageVersion('d/d', $any) ? 'yes' : 'no',
			'entry' => json_encode($pool->getFilterListEntryForPackageVersion('d/d', $any)),
			'entryClass' => get_class($lists['d/d']['3.0.0.0'][0]),
			'sameEntry' => $lists['d/d']['3.0.0.0'][0] === $lists['d/d']['3.1.0.0'][0] ? 'yes' : 'no',
			'notFiltered' => $pool->isFilterListRemovedPackageVersion('a/a', $any) ? 'yes' : 'no',
		];
	`, php.ArrayOf("pool", rt.poolValue(pool)))

	for key, want := range map[string]string{
		"removed":       `{"a\/a":{"1.1.0.0":"1.1.0","1.0.0.0":"1.0.0"}}`,
		"byPackage":     `{"1.1.0.0":"1.1.0"}`,
		"security":      "yes",
		"ids":           "PKSA-1",
		"advisoryClass": `Composer\Advisory\PartialSecurityAdvisory`,
		"sameAdvisory":  "yes",
		"filtered":      "yes",
		"entry":         `{"malware":"flagged as malware reported by acme (see https:\/\/example.org\/d)"}`,
		"entryClass":    `Composer\FilterList\FilterListEntry`,
		"sameEntry":     "yes",
		"notFiltered":   "no",
	} {
		if g := php.ToString(get(t, got, key)); g != want {
			t.Errorf("%s = %s, want %s", key, g, want)
		}
	}
}
