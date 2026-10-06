package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// Factory's helpers (through a subclass, as they are protected), and
// Composer's services constructed in PHP: Config, RepositoryManager,
// InstallationManager, DownloadManager, PluginManager, AutoloadGenerator
// and its dump().
func TestShimStubs_FactoryAndServices(t *testing.T) {
	requirePHP(t)

	p := newEvalProject(t, "commands")
	home := os.Getenv("COMPOSER_HOME")

	got := evalPHP(t, p.rt, `
		$io = $vars['io'];
		$f = new class extends \Composer\Factory {
			public function call(string $method, ...$args) { return $this->$method(...$args); }
			public static function callStatic(string $method, ...$args) { return static::$method(...$args); }
		};
		$out = [];
		$home = $f::callStatic('getHomeDir');
		$out[] = 'home '.$home;
		$out[] = 'cache '.$f::callStatic('getCacheDir', $home);
		$out[] = 'data '.$f::callStatic('getDataDir', $home);

		$composer = \Composer\Factory::create($io, null, true);

		$config = new \Composer\Config(false, '/srv/app');
		$config->merge(['config' => ['vendor-dir' => 'libs']]);
		$out[] = 'config '.get_class($config).' '.$config->get('vendor-dir').' '.var_export($config->get('lock'), true);

		$http = \Composer\Factory::createHttpDownloader($io, $composer->getConfig());
		$rm = new \Composer\Repository\RepositoryManager($io, $composer->getConfig(), $http, $composer->getEventDispatcher());
		$f->call('addLocalRepository', $io, $rm, 'vendor', $composer->getPackage());
		$out[] = 'rm '.count($rm->getRepositories()).' '.get_class($rm->getLocalRepository());
		try {
			// No repository types until RepositoryFactory::manager() registers them.
			$rm->createRepository('package', ['package' => ['name' => 'a/b', 'version' => '1.0.0']]);
		} catch (\InvalidArgumentException $e) {
			$out[] = 'rm: '.$e->getMessage();
		}
		$rm->addRepository(new \Composer\Repository\ArrayRepository([new \Composer\Package\Package('a/b', '1.0.0.0', '1.0.0')]));
		$out[] = 'rm find '.$rm->findPackage('a/b', '*')->getPrettyString();

		$im = new \Composer\Installer\InstallationManager($composer->getLoop(), $io);
		$f->call('createDefaultInstallers', $im, $composer, $io);
		$out[] = 'im '.get_class($im->getInstaller('library')).' '.get_class($im->getInstaller('composer-plugin')).' '.get_class($im->getInstaller('metapackage'));
		$f->call('purgePackages', $rm->getLocalRepository(), $im);

		$dm = $f->createDownloadManager($io, $composer->getConfig(), $http, new \Composer\Util\ProcessExecutor($io), $composer->getEventDispatcher());
		$out[] = 'dm '.get_class($dm->getDownloader('zip'));
		try {
			(new \Composer\Downloader\DownloadManager($io))->getDownloader('zip');
		} catch (\InvalidArgumentException $e) {
			$out[] = 'empty dm: '.$e->getMessage();
		}
		$out[] = 'am '.get_class($f->createArchiveManager($composer->getConfig(), $dm, $composer->getLoop()));
		$out[] = 'im2 '.get_class($f->createInstallationManager($composer->getLoop(), $io));

		$pm = $f->call('createPluginManager', $io, $composer, null, true);
		$out[] = 'pm '.get_class($pm).' '.count($pm->getPlugins()).' '.var_export($pm->getGlobalComposer(), true);
		$pm->loadInstalledPlugins();

		$out[] = 'global '.var_export($f->call('createGlobalComposer', $io, $composer->getConfig(), 'local', true), true);

		$ag = new \Composer\Autoload\AutoloadGenerator(new \Composer\EventDispatcher\EventDispatcher($composer, $io), $io);
		$ag->setPlatformRequirementFilter(\Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterFactory::ignoreAll());
		$ag->setRunScripts(false);
		$classMap = $ag->dump($composer->getConfig(), $composer->getRepositoryManager()->getLocalRepository(), $composer->getPackage(), $composer->getInstallationManager(), 'composer', true);
		$out[] = 'dump '.get_class($classMap).' '.implode(',', array_keys($classMap->getMap()));

		return $out;
	`, php.ArrayOf("io", p.rt.value(p.out)))

	cacheDir := os.Getenv("COMPOSER_CACHE_DIR")
	want := strings.Join([]string{
		"home " + home,
		"cache " + cacheDir,
		"data " + home,
		"config Composer\\Config /srv/app/libs true",
		"rm 0 Composer\\Repository\\InstalledFilesystemRepository",
		"rm: Repository type is not registered: package",
		"rm find a/b 1.0.0",
		`im Composer\Installer\LibraryInstaller Composer\Installer\PluginInstaller Composer\Installer\MetapackageInstaller`,
		`dm Composer\Downloader\ZipDownloader`,
		"empty dm: Unknown downloader type: zip. Available types: .",
		`am Composer\Package\Archiver\ArchiveManager`,
		`im2 Composer\Installer\InstallationManager`,
		`pm Composer\Plugin\PluginManager 0 NULL`,
		"global NULL",
		`dump Composer\ClassMapGenerator\ClassMap Composer\InstalledVersions,Project\GreetCommand`,
	}, "\n")
	if g := lines(got); g != want {
		t.Errorf("got\n%s\nwant\n%s", g, want)
	}
	if _, err := os.Stat(filepath.Join(p.dir, "vendor", "composer", "autoload_static.php")); err != nil {
		t.Errorf("the dump wrote no autoloader: %v", err)
	}
}
