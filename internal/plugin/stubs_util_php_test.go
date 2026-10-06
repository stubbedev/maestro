package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// The protected helpers of Filesystem, Cache, DownloadManager,
// JsonManipulator and RemoteFilesystem, through subclasses as plugins
// reach them.
func TestShimStubs_UtilHelpers(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "commands")
	dir := filepath.Join(p.dir, "scratch")
	for name, content := range map[string]string{"a.txt": "12345", "sub/b.txt": "123", "sub/deep/c.txt": "1"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := evalPHP(t, p.rt, `
		$io = $vars['io'];
		$dir = $vars['dir'];
		$out = [];

		$fs = new class extends \Composer\Util\Filesystem {
			public function call(string $method, ...$args) { return $this->$method(...$args); }
		};
		$out[] = 'size '.$fs->call('directorySize', $dir);
		$out[] = 'process '.get_class($fs->call('getProcess'));
		try {
			$fs->removeDirectoryAsync($dir.'/sub');
		} catch (\LogicException $e) {
			$out[] = 'async: '.$e->getMessage();
		}
		$loop = \Composer\Factory::create($io, null, true)->getLoop();
		$asyncFs = new \Composer\Util\Filesystem($loop->getProcessExecutor());
		$result = null;
		$asyncFs->removeDirectoryAsync($dir.'/sub')->then(function ($r) use (&$result) { $result = $r; });
		$loop->wait([]);
		$out[] = 'removed '.var_export($result, true).' '.var_export(is_dir($dir.'/sub'), true);
		$asyncFs->removeDirectoryAsync($dir.'/missing')->then(function ($r) use (&$result) { $result = $r; });
		$out[] = 'missing '.var_export($result, true);

		$cache = new class($io, $dir) extends \Composer\Cache {
			public function files(): array
			{
				$names = [];
				foreach ($this->getFinder() as $file) {
					$names[] = $file->getFilename();
				}
				sort($names);

				return $names;
			}
		};
		$out[] = 'finder '.implode(',', $cache->files());

		$dm = new class($io) extends \Composer\Downloader\DownloadManager {
			public function call(string $method, ...$args) { return $this->$method(...$args); }
		};
		$dm->setPreferences(['acme/*' => 'source']);
		$dev = new \Composer\Package\Package('other/dev', 'dev-main', 'dev-main');
		$out[] = 'preference '.$dm->call('resolvePackageInstallPreference', new \Composer\Package\Package('acme/lib', '1.0.0.0', '1.0.0')).' '.$dm->call('resolvePackageInstallPreference', $dev).' '.$dm->call('resolvePackageInstallPreference', new \Composer\Package\Package('other/lib', '1.0.0.0', '1.0.0'));

		$m = new class("{\n\t\"a\": 1\n}") extends \Composer\Json\JsonManipulator {
			public function redetect(): void { $this->detectIndenting(); }
		};
		$m->redetect();
		$m->addMainKey('b', 2);
		$out[] = 'manipulator '.json_encode($m->getContents());

		$rfs = new class($io, \Composer\Factory::createConfig($io)) extends \Composer\Util\RemoteFilesystem {
			public function call(string $method, ...$args) { return $this->$method(...$args); }
			public function contents(string $url, ?int $max = null)
			{
				$headers = ['untouched'];
				$result = $this->getRemoteContents('example.org', $url, stream_context_create(), $headers, $max);

				return [$result, $headers];
			}
		};
		$out[] = 'get '.$rfs->call('get', 'example.org', 'file://'.$dir.'/a.txt', [], null, false);
		$out[] = 'copy '.var_export($rfs->call('get', 'example.org', 'file://'.$dir.'/a.txt', [], $dir.'/copy.txt', false), true).' '.file_get_contents($dir.'/copy.txt');
		$options = $rfs->call('getOptionsForUrl', 'example.org', ['http' => ['header' => ['X-Test: 1']]]);
		$out[] = 'options '.implode('|', $options['http']['header']).' '.$options['http']['follow_location'];
		$rfs->call('callbackGet', STREAM_NOTIFY_FILE_SIZE_IS, 0, null, 0, 0, 10);
		$out[] = 'contents '.json_encode($rfs->contents($dir.'/a.txt'));
		try {
			$rfs->contents($dir.'/a.txt', 3);
		} catch (\Composer\Downloader\MaxFileSizeExceededException $e) {
			$out[] = 'max: '.$e->getMessage();
		}

		return $out;
	`, php.ArrayOf("io", p.rt.value(p.out), "dir", dir))

	want := strings.Join([]string{
		"size 9",
		`process Composer\Util\ProcessExecutor`,
		"async: You must use the ProcessExecutor instance which is part of a Composer\\Loop instance to be able to run async processes",
		"removed true false",
		"missing true",
		"finder a.txt",
		"preference source source dist",
		`manipulator "{\n\t\"a\": 1,\n\t\"b\": 2\n}\n"`,
		"get 12345",
		"copy true 12345",
		"options X-Test: 1|Accept-Encoding: gzip|Connection: close 0",
		`contents ["12345",[]]`,
		"max: Maximum allowed download size reached. Downloaded 3 of allowed 3 bytes for " + dir + "/a.txt",
	}, "\n")
	if g := lines(got); g != want {
		t.Errorf("got\n%s\nwant\n%s", g, want)
	}
}
