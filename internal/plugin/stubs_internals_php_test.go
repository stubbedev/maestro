package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// EventDispatcher's and BinaryInstaller's protected methods, through
// subclasses as plugins reach them.
func TestShimStubs_DispatcherAndBinaryInternals(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "commands")
	bin := filepath.Join(p.dir, "tool")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho tool\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := evalPHP(t, p.rt, `
		$io = $vars['io'];
		$composer = \Composer\Factory::create($io, null, true);
		$ed = new class($composer, $io) extends \Composer\EventDispatcher\EventDispatcher {
			public function call(string $method, ...$args) { return $this->$method(...$args); }
		};
		$out = [];
		$out[] = 'is '.json_encode([
			$ed->call('isPhpScript', 'A\\B::c'), $ed->call('isPhpScript', 'a b::c'),
			$ed->call('isCommandClass', 'A\\BCommand'), $ed->call('isCommandClass', 'BCommand'),
			$ed->call('isComposerScript', '@other'), $ed->call('isComposerScript', '@php x'),
		]);

		$ed->addListener('custom-event', function () { return 7; }, 10);
		$ed->addListener('custom-event', 'MaestroTestScripts::run');
		$event = new \Composer\EventDispatcher\Event('custom-event');
		$listeners = $ed->call('getListeners', $event);
		$out[] = 'listeners '.implode(',', array_map(function ($l) { return is_string($l) ? $l : get_class($l); }, $listeners));
		$out[] = 'scripts '.json_encode($ed->call('getScriptListeners', new \Composer\EventDispatcher\Event('greet')));

		$out[] = 'push '.$ed->call('pushEvent', $event);
		try {
			$ed->call('pushEvent', $event);
		} catch (\RuntimeException $e) {
			$out[] = 'circular: '.$e->getMessage();
		}
		$out[] = 'pop '.var_export($ed->call('popEvent'), true).' '.var_export($ed->call('popEvent'), true);

		$php = $ed->call('getPhpExecCommand');
		$out[] = 'php '.var_export(strpos($php, ' -d allow_url_fopen=') !== false && strpos($php, ' -d memory_limit=') !== false, true);

		eval('class MaestroTestScripts { public static $calls = 0; public static function run($event) { self::$calls++; return 3; } }');
		$out[] = 'php script '.$ed->call('executeEventPhpScript', 'MaestroTestScripts', 'run', $event);
		$out[] = 'dispatch '.$ed->call('doDispatch', $event).' '.\MaestroTestScripts::$calls;

		$bi = new class($io, getcwd().'/bin', 'auto') extends \Composer\Installer\BinaryInstaller {
			public function call(string $method, ...$args) { return $this->$method(...$args); }
		};
		@mkdir(getcwd().'/bin');
		$code = $bi->call('generateUnixyProxyCode', $vars['bin'], getcwd().'/bin/tool');
		$out[] = 'unixy '.var_export(strpos($code, '#!/usr/bin/env sh') === 0, true);
		$out[] = 'windows '.var_export(strpos($bi->call('generateWindowsProxyCode', $vars['bin'], getcwd().'/bin/tool.bat'), '@ECHO OFF') === 0, true);
		$bi->call('installUnixyProxyBinaries', $vars['bin'], getcwd().'/bin/tool');
		$out[] = 'installed '.var_export(file_get_contents(getcwd().'/bin/tool') === $code, true);

		return $out;
	`, php.ArrayOf("io", p.rt.value(p.out), "bin", bin))

	want := strings.Join([]string{
		"is [true,false,true,false,true,false]",
		"listeners Closure,MaestroTestScripts::run",
		`scripts ["Project\\GreetCommand"]`,
		"push 1",
		"circular: Circular call to script handler 'custom-event' detected",
		"pop 'custom-event' NULL",
		"php true",
		"php script 3",
		"dispatch 0 2",
		"unixy true",
		"windows true",
		"installed true",
	}, "\n")
	if g := lines(got); g != want {
		t.Errorf("got\n%s\nwant\n%s\noutput:\n%s", g, want, p.output())
	}
}
