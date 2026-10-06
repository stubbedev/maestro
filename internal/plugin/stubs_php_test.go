package plugin

// Members of Composer's API that were stubs (issue #1): each against
// Composer 2.10.3's behaviour, in the shim, on a fixture project.

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/policy"
)

// newEvalProject is a fixture project whose runtime runs the test
// handlers (test.eval), started.
func newEvalProject(t *testing.T, fixture string) *project {
	t.Helper()

	handlers := handlersPHP(t) // before newProject changes the directory
	p := newProject(t, fixture, console.VerbosityNormal, func(o *Options) {
		o.Require = append(o.Require, handlers)
	})
	start(t, p.rt)

	return p
}

// lines is a PHP list of strings as one string, a line each.
func lines(v any) string {
	a, ok := v.(*php.Array)
	if !ok {
		return php.ToString(v)
	}
	var out []string
	for _, x := range a.Values() {
		out = append(out, php.ToString(x))
	}

	return strings.Join(out, "\n")
}

// BaseCommand's audit, policy and platform requirement filter helpers,
// and the AuditConfig they make crossing to maestro both ways.
func TestShimStubs_CommandHelpers(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "api")

	var seen []any
	var audits []advisory.AuditConfig // the AuditConfigs' values when seen
	p.rt.Handle("test.see", func(v any) (any, error) {
		x := unwrap(argsOf("test.see", v).at(0))
		seen = append(seen, x)
		if a, ok := x.(*advisory.AuditConfig); ok {
			audits = append(audits, *a)
		}

		return nil, nil
	})

	got := evalPHP(t, p.rt, `
		$cmd = new class extends \Composer\Command\BaseCommand {
			protected function configure(): void
			{
				$this->setName('helpers')
					->addOption('audit', null, \Symfony\Component\Console\Input\InputOption::VALUE_NONE)
					->addOption('audit-format', null, \Symfony\Component\Console\Input\InputOption::VALUE_REQUIRED, '', 'summary')
					->addOption('no-blocking', null, \Symfony\Component\Console\Input\InputOption::VALUE_NONE)
					->addOption('ignore-platform-reqs', null, \Symfony\Component\Console\Input\InputOption::VALUE_NONE)
					->addOption('ignore-platform-req', null, \Symfony\Component\Console\Input\InputOption::VALUE_REQUIRED | \Symfony\Component\Console\Input\InputOption::VALUE_IS_ARRAY);
			}
			public function call(string $method, ...$args)
			{
				return $this->$method(...$args);
			}
		};
		$out = [];
		$try = function (callable $fn) use (&$out) {
			try {
				$out[] = $fn();
			} catch (\Exception $e) {
				$out[] = get_class($e).': '.$e->getMessage();
			}
		};
		$input = function (array $params) use ($cmd) {
			return new \Symfony\Component\Console\Input\ArrayInput($params, $cmd->getDefinition());
		};

		$f = $cmd->call('getPlatformRequirementFilter', $input(['--ignore-platform-req' => ['ext-foo']]));
		$out[] = get_class($f).' '.var_export($f->isIgnored('ext-foo'), true).' '.var_export($f->isIgnored('ext-bar'), true);
		$out[] = get_class($cmd->call('getPlatformRequirementFilter', $input(['--ignore-platform-reqs' => true])));
		$out[] = get_class($cmd->call('getPlatformRequirementFilter', $input([])));
		$try(function () use ($input) {
			$bare = new class extends \Composer\Command\BaseCommand {
				public function call(string $method, ...$args) { return $this->$method(...$args); }
			};
			return $bare->call('getPlatformRequirementFilter', new \Symfony\Component\Console\Input\ArrayInput([]));
		});

		$out[] = $cmd->call('getAuditFormat', $input(['--audit-format' => 'json']));
		$try(function () use ($cmd, $input) { return $cmd->call('getAuditFormat', $input(['--audit-format' => 'xml'])); });
		$try(function () use ($cmd, $input) { return $cmd->call('getAuditFormat', $input([]), 'format'); });

		$audit = $cmd->call('createAuditConfig', $input(['--audit' => true, '--audit-format' => 'plain']));
		$out[] = get_class($audit).' '.var_export($audit->audit, true).' '.$audit->auditFormat;
		\Maestro\Shim\Rpc::call('test.see', [$audit]);
		$audit->audit = false;
		$audit->auditFormat = 'table';
		\Maestro\Shim\Rpc::call('test.see', [$audit]);

		$config = \Composer\Factory::createConfig();
		$policy = $cmd->call('createPolicyConfig', $config, $input([]));
		\Maestro\Shim\Rpc::call('test.see', [$policy]);
		$policy = $cmd->call('createPolicyConfig', $config, $input(['--no-blocking' => true]));
		\Maestro\Shim\Rpc::call('test.see', [$policy]);
		$out[] = get_class($policy);

		return $out;
	`, nil)

	want := strings.Join([]string{
		`Composer\Filter\PlatformRequirementFilter\IgnoreListPlatformRequirementFilter true false`,
		`Composer\Filter\PlatformRequirementFilter\IgnoreAllPlatformRequirementFilter`,
		`Composer\Filter\PlatformRequirementFilter\IgnoreNothingPlatformRequirementFilter`,
		`LogicException: Calling getPlatformRequirementFilter from a command which does not define the --ignore-platform-req[s] flags is not permitted.`,
		`json`,
		`InvalidArgumentException: --audit-format must be one of table, plain, json, summary.`,
		`LogicException: This should not be called on a Command which has no format option defined.`,
		`Composer\Advisory\AuditConfig true plain`,
		`Composer\Policy\PolicyConfig`,
	}, "\n")
	if g := lines(got); g != want {
		t.Errorf("got\n%s\nwant\n%s", g, want)
	}

	if len(seen) != 4 {
		t.Fatalf("seen %d values: %#v", len(seen), seen)
	}
	// The AuditConfig created in PHP is maestro's, with what PHP wrote
	// into its properties.
	a1, _ := seen[0].(*advisory.AuditConfig)
	if a2, _ := seen[1].(*advisory.AuditConfig); a1 == nil || a2 != a1 {
		t.Fatalf("AuditConfigs = %#v, %#v", seen[0], seen[1])
	}
	if want := []advisory.AuditConfig{{Audit: true, AuditFormat: "plain"}, {Audit: false, AuditFormat: "table"}}; len(audits) != 2 || audits[0] != want[0] || audits[1] != want[1] {
		t.Errorf("AuditConfig values = %#v, want %#v", audits, want)
	}
	// --no-blocking disabled the advisories' blocking.
	p1, ok1 := seen[2].(*policy.PolicyConfig)
	p2, ok2 := seen[3].(*policy.PolicyConfig)
	if !ok1 || !ok2 {
		t.Fatalf("PolicyConfigs = %#v, %#v", seen[2], seen[3])
	}
	if !p1.Advisories.Block || p2.Advisories.Block {
		t.Errorf("advisories block: %v then %v", p1.Advisories.Block, p2.Advisories.Block)
	}

	// maestro's AuditConfig changed in Go shows in PHP.
	a1.Audit, a1.AuditFormat = true, "json"
	got = evalPHP(t, p.rt, `return var_export($vars['a']->audit, true).' '.$vars['a']->auditFormat;`, php.ArrayOf("a", p.rt.value(a1)))
	if got != "true json" {
		t.Errorf("PHP's AuditConfig after maestro's change = %v", got)
	}
}

// An IO created in PHP given to maestro: a Composer and an Installer
// created with it write to it and follow its verbosity; maestro's calls
// of its methods reach the PHP object, and it comes back as itself.
func TestShimStubs_PHPCreatedIO(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "commands")

	got := evalPHP(t, p.rt, `
		$io = new \Composer\IO\BufferIO('', \Symfony\Component\Console\Output\OutputInterface::VERBOSITY_VERBOSE);
		$composer = \Composer\Factory::create($io, null, true);
		$code = \Composer\Installer::create($io, $composer)->setDryRun(true)->setAudit(false)->run();

		return ['code' => $code, 'output' => $io->getOutput(), 'io' => $io];
	`, nil)
	if code := get(t, got, "code"); code != int64(0) {
		t.Errorf("code = %v", code)
	}
	// The BufferIO's StreamOutput ends lines with PHP's PHP_EOL.
	out := php.NormalizeEOL(php.ToString(get(t, got, "output")))
	for _, want := range []string{
		"Loading composer repositories with package information\n",
		"Updating dependencies\n",
		"Lock file operations: 1 install, 0 updates, 0 removals\n",
		"  - Locking maestro-test/commands-plugin (1.0.0)\n",
		// -v only.
		"Dependency resolution completed in ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the PHP IO's output lacks %q:\n%s", want, out)
		}
	}

	// maestro's proxy of the IO: each method is the PHP object's.
	obj, ok := get(t, got, "io").(*rpc.PHPObject)
	if !ok {
		t.Fatalf("io = %#v", get(t, got, "io"))
	}
	pio := p.rt.phpIOFor(obj)
	if !pio.IsVerbose() || pio.IsVeryVerbose() || pio.IsInteractive() || pio.IsDecorated() {
		t.Errorf("flags: verbose %v, very verbose %v, interactive %v, decorated %v", pio.IsVerbose(), pio.IsVeryVerbose(), pio.IsInteractive(), pio.IsDecorated())
	}
	pw := "secret"
	pio.SetAuthentication("example.org", "user", &pw)
	if !pio.HasAuthentication("example.org") || *pio.Authentication("example.org").Username != "user" || len(pio.Authentications()) != 1 {
		t.Errorf("authentications = %#v", pio.Authentications())
	}
	if p.rt.value(pio) != obj {
		t.Errorf("the IO crossed back as %#v", p.rt.value(pio))
	}

	// A question with maestro's validator, answered from PHP's inputs.
	evalPHP(t, p.rt, `$vars['io']->setUserInputs(['bad', 'good']);`, php.ArrayOf("io", obj))
	answer, err := pio.AskAndValidate("Name? ", func(v any) (any, error) {
		if php.ToString(v) != "good" {
			return nil, errors.New("not good")
		}

		return "validated " + php.ToString(v), nil
	}, 3, nil)
	if err != nil || answer != "validated good" {
		t.Errorf("AskAndValidate = %v, %v", answer, err)
	}
	out = php.ToString(evalPHP(t, p.rt, `return $vars['io']->getOutput();`, php.ArrayOf("io", obj)))
	if !strings.Contains(out, "not good") {
		t.Errorf("the validator's error was not shown:\n%s", out)
	}
}
