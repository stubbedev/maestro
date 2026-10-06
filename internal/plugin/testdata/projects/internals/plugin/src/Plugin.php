<?php

namespace MaestroTest\InternalsPlugin;

use Composer\Composer;
use Composer\Config;
use Composer\DependencyResolver\Pool;
use Composer\DependencyResolver\Transaction;
use Composer\Downloader\FileDownloader;
use Composer\EventDispatcher\EventSubscriberInterface;
use Composer\Factory;
use Composer\Installer;
use Composer\Installer\InstallerEvent;
use Composer\Installer\InstallerEvents;
use Composer\Installer\SuggestedPackagesReporter;
use Composer\IO\IOInterface;
use Composer\IO\NullIO;
use Composer\Package\Package;
use Composer\Plugin\PluginInterface;
use Composer\Repository\ArrayRepository;
use Composer\Repository\RepositorySet;
use Composer\Script\Event;
use Composer\Script\ScriptEvents;

class Plugin implements PluginInterface, EventSubscriberInterface
{
    /** @var Composer */
    private $composer;

    /** @var IOInterface */
    private $io;

    public function activate(Composer $composer, IOInterface $io): void
    {
        $this->composer = $composer;
        $this->io = $io;

        // The PluginManager and Installer frames on the stack, as Composer
        // has them (docs/PLUGINS.md §5.12).
        $frames = [];
        foreach (debug_backtrace(\DEBUG_BACKTRACE_PROVIDE_OBJECT) as $trace) {
            if (isset($trace['object']) && ($trace['object'] instanceof \Composer\Plugin\PluginManager || $trace['object'] instanceof Installer)) {
                $frames[] = $trace['class'].$trace['type'].$trace['function'].'('.count($trace['args']).')';
            }
        }
        $io->write('activate frames: '.implode(', ', $frames));
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
            ScriptEvents::PRE_UPDATE_CMD => 'preUpdate',
            InstallerEvents::PRE_OPERATIONS_EXEC => 'preOperationsExec',
            ScriptEvents::POST_UPDATE_CMD => 'postUpdate',
        ];
    }

    /** The Installer on the stack, as flex and php-http/discovery find it. */
    private static function installers(): array
    {
        $found = [];
        foreach (debug_backtrace(\DEBUG_BACKTRACE_PROVIDE_OBJECT) as $trace) {
            if (isset($trace['object']) && $trace['object'] instanceof Installer) {
                $found[] = $trace['object'];
            }
        }

        return $found;
    }

    public function preUpdate(Event $event): void
    {
        $installers = self::installers();
        $this->io->write('installers on the stack: '.count($installers).(!empty($GLOBALS['maestroTestNested']) ? ' (nested)' : ''));
        if (!empty($GLOBALS['maestroTestNested'])) {
            return;
        }
        $installer = $installers[0];
        $props = (array) $installer;
        $this->io->write('platform filter: '.get_class($props["\0*\0platformRequirementFilter"]).' devMode: '.var_export($props["\0*\0devMode"], true));
        $ed = $this->composer->getEventDispatcher();
        $this->io->write('runScripts: '.var_export(((array) $ed)["\0*\0runScripts"], true));
        $installer->setSuggestedPackagesReporter(new SuggestedPackagesReporter(new NullIO()));

        // Config's private baseDir, and a clone that does not change the original.
        $config = $this->composer->getConfig();
        $baseDir = new \ReflectionProperty(Config::class, 'baseDir');
        if (PHP_VERSION_ID < 80100) {
            $baseDir->setAccessible(true);
        }
        $this->io->write('baseDir set: '.var_export($baseDir->getValue($config) === getcwd(), true));
        $clone = clone $config;
        $clone->merge(['config' => ['sort-packages' => true]]);
        $this->io->write('clone sort-packages: '.var_export($clone->get('sort-packages'), true).' original: '.var_export($config->get('sort-packages'), true));
    }

    public function preOperationsExec(InstallerEvent $event): void
    {
        if (!empty($GLOBALS['maestroTestNested'])) {
            return;
        }
        $transaction = \Closure::bind(function () use ($event) {
            $map = $event->getTransaction()->resultPackageMap;
            $present = $event->getTransaction()->presentPackages;

            return [new Transaction([], $map), count($map), count($present)];
        }, null, Transaction::class)();
        $names = [];
        foreach ($transaction[0]->getOperations() as $op) {
            $names[] = $op->getOperationType().' '.$op->getPackage()->getName();
        }
        sort($names);
        $this->io->write('transaction: result '.$transaction[1].' present '.$transaction[2].' ops '.implode(', ', $names));
    }

    public function postUpdate(Event $event): void
    {
        if (!empty($GLOBALS['maestroTestNested'])) {
            $this->io->write('nested post-update');

            return;
        }
        // The plugin is loaded again (another class) by the nested
        // Composer: a global marks the nested run.
        $GLOBALS['maestroTestNested'] = true;

        // A clone of the Installer on the stack, constructed again with a
        // new Composer, as php-http/discovery and flex do.
        $installer = clone self::installers()[0];
        $composer = Factory::create($event->getIO(), null, false, true);
        $installer->__construct($event->getIO(), $composer->getConfig(), $composer->getPackage(), $composer->getDownloadManager(), $composer->getRepositoryManager(), $composer->getLocker(), $composer->getInstallationManager(), $composer->getEventDispatcher(), $composer->getAutoloadGenerator());
        $this->io->write('nested run: '.$installer->run());
        unset($GLOBALS['maestroTestNested']);

        $set = new RepositorySet();
        foreach ($this->composer->getRepositoryManager()->getRepositories() as $repo) {
            $set->addRepository($repo);
        }
        $pool = $set->createPoolForPackage('local/lib-a');
        $this->io->write('pool: '.count($pool).' '.$pool->packageById(1)->getName().' provides: '.count($pool->whatProvides('local/lib-a')));
        $advisories = $set->getMatchingSecurityAdvisories([]);
        $this->io->write('advisories: '.count($advisories['advisories']));

        $local = new Pool([new Package('a/b', '1.0.0.0', '1.0.0'), new Package('c/d', '2.0.0.0', '2.0.0')]);
        $this->io->write('local pool: '.trim(str_replace("\n", '|', (string) $local)));

        $rm = $this->composer->getRepositoryManager();
        $repo = new ArrayRepository([new Package('php/made', '1.2.0.0', '1.2.0')]);
        $rm->addRepository($repo);
        $found = $rm->findPackage('php/made', '^1.0');
        $this->io->write('php repository: '.($found !== null ? $found->getPrettyName().' '.$found->getPrettyVersion() : 'none'));

        $rm->setRepositoryClass('custom', CustomRepository::class);
        $custom = $rm->createRepository('custom', ['package' => 'custom/pkg']);
        $this->io->write('custom repository: '.get_class($custom).' '.$custom->getRepoName().' '.count($custom->getPackages()));

        $dm = $this->composer->getDownloadManager();
        $this->io->write('zip downloader source: '.$dm->getDownloader('zip')->getInstallationSource());
        $fd = new FileDownloader($this->io, $this->composer->getConfig(), $this->composer->getLoop()->getHttpDownloader());
        $this->io->write('file downloader: '.get_class($fd).' '.$fd->getInstallationSource());
    }
}
