<?php

namespace MaestroTest\ResolverPlugin;

use Composer\Cache;
use Composer\Composer;
use Composer\Config\JsonConfigSource;
use Composer\DependencyResolver\Operation\InstallOperation;
use Composer\EventDispatcher\EventDispatcher;
use Composer\EventDispatcher\EventSubscriberInterface;
use Composer\Installer;
use Composer\Installer\InstallerEvent;
use Composer\Installer\InstallerEvents;
use Composer\Installer\SuggestedPackagesReporter;
use Composer\IO\IOInterface;
use Composer\IO\NullIO;
use Composer\Json\JsonFile;
use Composer\Json\JsonManipulator;
use Composer\Package\CompletePackage;
use Composer\Package\Dumper\ArrayDumper;
use Composer\Package\Loader\ArrayLoader;
use Composer\Package\Package;
use Composer\Package\Version\VersionSelector;
use Composer\Plugin\PluginEvents;
use Composer\Plugin\PluginInterface;
use Composer\Plugin\PrePoolCreateEvent;
use Composer\Repository\CompositeRepository;
use Composer\Repository\InstalledArrayRepository;
use Composer\Repository\InstalledRepository;
use Composer\Repository\PlatformRepository;
use Composer\Repository\RepositoryFactory;
use Composer\Repository\RepositorySet;
use Composer\Repository\RootPackageRepository;
use Composer\Script\Event;
use Composer\Script\ScriptEvents;

class Plugin implements PluginInterface, EventSubscriberInterface
{
    /** @var Composer */
    private $composer;

    /** @var IOInterface */
    private $io;

    /** @var bool */
    private $nested = false;

    public function activate(Composer $composer, IOInterface $io): void
    {
        $this->composer = $composer;
        $this->io = $io;
    }

    public function deactivate(Composer $composer, IOInterface $io): void
    {
    }

    public function uninstall(Composer $composer, IOInterface $io): void
    {
    }

    public static function getSubscribedEvents(): array
    {
        return [
            PluginEvents::PRE_POOL_CREATE => 'prePoolCreate',
            InstallerEvents::PRE_OPERATIONS_EXEC => 'preOperationsExec',
            ScriptEvents::PRE_UPDATE_CMD => 'preUpdate',
            ScriptEvents::POST_UPDATE_CMD => 'postUpdate',
        ];
    }

    public function preUpdate(Event $event): void
    {
        // A requirement added to the root package before solving.
        $root = $this->composer->getPackage();
        $loader = new ArrayLoader();
        $links = $loader->parseLinks($root->getName(), $root->getPrettyVersion(), 'requires', ['local/lib-b' => '^2.0']);
        $root->setRequires(array_merge($root->getRequires(), $links));
        $this->io->write('pre-update requires: '.implode(',', array_keys($root->getRequires())));
    }

    public function prePoolCreate(PrePoolCreateEvent $event): void
    {
        $names = [];
        foreach ($event->getPackages() as $package) {
            if (!PlatformRepository::isPlatformPackage($package->getName())) {
                $names[] = $package->getPrettyString();
            }
            if ($package->getName() === 'local/lib-a') {
                // Not among the fields PRE_POOL_CREATE sends at first.
                $this->io->write('lib-a suggests: '.implode(',', array_keys($package->getSuggests())).' dist='.$package->getDistType());
            }
        }
        sort($names);
        $this->io->write('pool: '.implode(',', $names));
        $this->io->write('request requires: '.implode(',', array_keys($event->getRequest()->getRequires())));
        $this->io->write('stabilities: '.implode(',', array_keys($event->getAcceptableStabilities())));

        // A package created in PHP joins the pool.
        $extra = new CompletePackage('local/extra', '3.0.0.0', '3.0.0');
        $extra->setType('metapackage');
        $packages = $event->getPackages();
        $packages[] = $extra;
        $event->setPackages($packages);
        $this->io->write('pool grew by: '.(count($event->getPackages()) - count($packages) + 1));
    }

    public function preOperationsExec(InstallerEvent $event): void
    {
        $ops = [];
        foreach ($event->getTransaction()->getOperations() as $op) {
            $ops[] = $op->show(false);
        }
        $this->io->write('operations: '.implode('; ', $ops));
    }

    public function postUpdate(Event $event): void
    {
        if ($this->nested) {
            $this->io->write('nested post-update');

            return;
        }
        $this->nested = true;

        $loader = new ArrayLoader();
        $package = $loader->load(['name' => 'Vendor/Loaded', 'version' => '1.2.0', 'require' => ['php' => '>=7.2'], 'extra' => ['branch-alias' => ['dev-main' => '1.x-dev']]]);
        $this->io->write('loaded: '.get_class($package).' '.$package->getPrettyString().' requires '.implode(',', array_keys($package->getRequires())));
        $dumped = (new ArrayDumper())->dump($package);
        $this->io->write('dumped: '.json_encode($dumped));
        $this->io->write('branch alias: '.var_export($loader->getBranchAlias(['version' => 'dev-main', 'extra' => ['branch-alias' => ['dev-main' => '1.x-dev']]]), true));

        // A package created in PHP added to a repository of maestro's.
        $local = $this->composer->getRepositoryManager()->getLocalRepository();
        $born = new Package('local/born', '1.0.0.0', '1.0.0');
        $born->setExtra(['made' => 'in php']);
        $local->addPackage($born);
        $found = $local->findPackage('local/born', '*');
        $this->io->write('born in local repo: '.var_export($found === $born, true).' extra='.json_encode($found->getExtra()).' repo='.get_class($born->getRepository()));
        $local->removePackage($born);
        $this->io->write('born removed: '.var_export($local->findPackage('local/born', '*') === null, true));

        // Repositories built in PHP over maestro's.
        $platform = new PlatformRepository([], ['php' => '7.4.33']);
        $this->io->write('platform php: '.$platform->findPackage('php', '*')->getPrettyVersion());
        $installed = new InstalledRepository([$local, new RootPackageRepository(clone $this->composer->getPackage()), $platform]);
        $this->io->write('installed repo: '.count($installed->findPackagesWithReplacersAndProviders('local/lib-a')).' '.$installed->getRepoName());
        $composite = new CompositeRepository([$local, new InstalledArrayRepository([new Package('local/arr', '1.0.0.0', '1.0.0')])]);
        $this->io->write('composite: '.count($composite->getPackages()).' '.var_export($composite->findPackage('local/arr', '*') !== null, true));

        // A repository of the manager, prepended; version selection over it.
        $rm = $this->composer->getRepositoryManager();
        $repo = $rm->createRepository('package', ['package' => ['name' => 'local/inline', 'version' => '1.5.0']]);
        $rm->prependRepository($repo);
        $repos = $rm->getRepositories();
        $this->io->write('prepended: '.($repos[0] === $repo ? 'first' : 'not first').' '.$repo->getRepoName());
        $set = new RepositorySet('stable');
        $set->addRepository($repo);
        foreach ($rm->getRepositories() as $other) {
            if ($other !== $repo) {
                $set->addRepository($other);
            }
        }
        $selector = new VersionSelector($set, $platform);
        $best = $selector->findBestCandidate('local/lib-b');
        $this->io->write('best: '.$best->getPrettyString().' recommended '.$selector->findRecommendedRequireVersion($best));
        $this->io->write('none: '.var_export($selector->findBestCandidate('local/none'), true));
        $defaults = RepositoryFactory::defaultRepos(null, $this->composer->getConfig(), $rm);
        $this->io->write('default repos: '.implode(',', array_keys($defaults)));

        // JSON editing.
        $manipulator = new JsonManipulator("{\n  \"name\": \"x/y\"\n}\n");
        $manipulator->addLink('require', 'a/b', '^1.0');
        $manipulator->addConfigSetting('sort-packages', true);
        $this->io->write('manipulated: '.json_encode($manipulator->getContents()));
        file_put_contents('extra.json', "{\n}\n");
        $source = new JsonConfigSource(new JsonFile('extra.json'));
        $source->addProperty('extra.foo', 'bar');
        $source->addLink('require', 'c/d', '^2.0');
        $this->io->write('config source: '.$source->getName().' '.json_encode(file_get_contents('extra.json')));

        // The lock.
        $locker = $this->composer->getLocker();
        $this->io->write('lock file: '.$locker->getJsonFile()->getPath());
        $locker->updateHash(new JsonFile('composer.json'), function (array $data): array {
            $data['_maestro'] = 'processed';

            return $data;
        });
        $this->io->write('lock processed: '.var_export(strpos(file_get_contents('composer.lock'), '"_maestro": "processed"') !== false, true));

        // A cache of its own.
        $cache = new Cache(new NullIO(), getcwd().'/plugin-cache');
        $cache->write('k', 'v');
        $this->io->write('cache: '.var_export($cache->read('k'), true).' '.var_export($cache->isEnabled(), true));

        // Suggestions and a second dispatcher.
        $reporter = new SuggestedPackagesReporter(new NullIO());
        $reporter->addPackage('a/b', 'c/d', 'reason');
        $this->io->write('suggestions: '.count($reporter->getPackages()));
        $dispatcher = new EventDispatcher($this->composer, $this->io);
        $dispatcher->addListener('custom-event', function () {
            $this->io->write('second dispatcher listener');
        });
        $dispatcher->dispatch('custom-event');

        // The loop: an asynchronous process, HTTP objects, downloaders.
        $loop = $this->composer->getLoop();
        $promise = $loop->getProcessExecutor()->executeAsync(['echo', 'async hello'])->then(function ($process) {
            $this->io->write('async process: '.trim($process->getOutput()).' '.get_class($process));
        });
        $loop->wait([$promise]);
        $http = new \Composer\Util\HttpDownloader($this->io, $this->composer->getConfig());
        $this->io->write('http downloader: '.get_class($http).' same loop downloader: '.var_export($loop->getHttpDownloader() === $loop->getHttpDownloader(), true));
        $rfs = new \Composer\Util\RemoteFilesystem($this->io, $this->composer->getConfig());
        $this->io->write('rfs tls disabled: '.var_export($rfs->isTlsDisabled(), true).' status: '.\Composer\Util\RemoteFilesystem::findStatusCode(['HTTP/1.1 301 Moved', 'HTTP/1.1 200 OK']));
        $this->io->write('zip downloader: '.get_class($this->composer->getDownloadManager()->getDownloader('zip')));

        // An install run from here, as merge-plugin's first install.
        $installer = Installer::create($this->io, $this->composer);
        $installer->setDryRun(true)->setUpdate(true)->setDevMode(true)->setAudit(false)->setSuggestedPackagesReporter(new SuggestedPackagesReporter(new NullIO()));
        $this->io->write('nested run: '.$installer->run());
    }
}
