package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// A FileDownloader subclass written in PHP with overrides of its own,
// given to maestro's DownloadManager: maestro calls its public overrides,
// the inherited code calls its protected ones ($this->getFileName(),
// processUrl(), install(), getInstallOperationAppendix()), and its
// parent:: calls run Composer's code once.
func TestShimStubs_FileDownloaderSubclass(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "commands")
	dist := filepath.Join(p.dir, "dist.txt")
	if err := os.WriteFile(dist, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := evalPHP(t, p.rt, `
		$io = $vars['io'];
		$composer = \Composer\Factory::create($io, null, true);
		$config = $composer->getConfig();
		$dl = new class($io, $config, $composer->getLoop()->getHttpDownloader()) extends \Composer\Downloader\FileDownloader {
			public static $log = [];
			protected function processUrl(\Composer\Package\PackageInterface $package, string $url): string
			{
				self::$log[] = 'processUrl';

				return parent::processUrl($package, $url);
			}
			protected function getFileName(\Composer\Package\PackageInterface $package, string $path): string
			{
				self::$log[] = 'getFileName';

				return dirname($path).'/custom-'.basename($path).'.tmp';
			}
			public function install(\Composer\Package\PackageInterface $package, string $path, bool $output = true): \React\Promise\PromiseInterface
			{
				self::$log[] = 'install';

				return parent::install($package, $path, $output);
			}
			protected function getInstallOperationAppendix(\Composer\Package\PackageInterface $package, string $path): string
			{
				self::$log[] = 'appendix';

				return ': custom';
			}
		};
		$dm = $composer->getDownloadManager();
		$dm->setDownloader('file', $dl);
		$class = get_class($dl);

		$package = new \Composer\Package\Package('acme/file', '1.0.0.0', '1.0.0');
		$package->setDistType('file');
		$package->setDistUrl('file://'.$vars['dist']);
		$target = getcwd().'/target';

		$out = [];
		$out[] = 'same '.var_export($dm->getDownloader('file') === $dl, true).' '.var_export($dm->getDownloaderType($dl) === 'file', true);
		$composer->getLoop()->wait([$dm->download($package, $target)]);
		$out[] = 'downloaded '.var_export(is_file(getcwd().'/custom-target.tmp'), true);
		$composer->getLoop()->wait([$dm->install($package, $target)]);
		$out[] = 'installed '.file_get_contents($target.'/dist.txt');
		$out[] = 'log '.implode(',', $class::$log);
		$class::$log = [];

		$package2 = new \Composer\Package\Package('acme/file', '1.0.1.0', '1.0.1');
		$package2->setDistType('file');
		$package2->setDistUrl('file://'.$vars['dist']);
		$composer->getLoop()->wait([$dm->download($package2, $target, $package)]);
		try {
			// update() writes its line with the subclass's appendix, then
			// remove() needs the asynchronous ProcessExecutor of a Loop,
			// which this downloader was not given (as in Composer).
			$composer->getLoop()->wait([$dm->update($package, $package2, $target)]);
		} catch (\LogicException $e) {
			$out[] = 'update: '.$e->getMessage();
		}
		$out[] = 'log '.implode(',', $class::$log);

		return $out;
	`, php.ArrayOf("io", p.rt.value(p.out), "dist", dist))

	want := strings.Join([]string{
		"same true true",
		"downloaded true",
		"installed payload",
		"log processUrl,getFileName,install,getFileName",
		`update: You must use the ProcessExecutor instance which is part of a Composer\Loop instance to be able to run async processes`,
		"log processUrl,getFileName,appendix",
	}, "\n")
	if g := lines(got); g != want {
		t.Errorf("got\n%s\nwant\n%s\noutput:\n%s", g, want, p.output())
	}
	if out := p.output(); !strings.Contains(out, "  - Upgrading acme/file (1.0.0 => 1.0.1): custom") {
		t.Errorf("no update line with the subclass's appendix:\n%s", out)
	}
}
