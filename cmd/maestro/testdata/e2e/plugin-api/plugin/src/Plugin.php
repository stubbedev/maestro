<?php

namespace MaestroTest\ApiPlugin;

use Composer\Composer;
use Composer\EventDispatcher\EventSubscriberInterface;
use Composer\Installer\PackageEvent;
use Composer\Installer\PackageEvents;
use Composer\IO\IOInterface;
use Composer\Plugin\PluginInterface;
use Composer\Script\Event;
use Composer\Script\ScriptEvents;
use Composer\Util\Filesystem;
use Composer\Util\ProcessExecutor;

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
        $io->write('activate '.get_class($this).' '.get_class($io).' verbose='.var_export($io->isVerbose(), true));
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
            ScriptEvents::PRE_AUTOLOAD_DUMP => 'preAutoloadDump',
            ScriptEvents::POST_INSTALL_CMD => ['postInstall', 10],
            ScriptEvents::POST_UPDATE_CMD => 'postInstall',
            PackageEvents::POST_PACKAGE_INSTALL => [['postPackage', 0]],
            PackageEvents::PRE_PACKAGE_INSTALL => 'prePackage',
        ];
    }

    public function prePackage(PackageEvent $event): void
    {
        // Changed in place before its installation: no binaries.
        $package = $event->getOperation()->getPackage();
        $this->io->write('pre-package-install '.$package->getName().' binaries='.implode(',', $package->getBinaries()));
        $package->setBinaries([]);
    }

    public function postPackage(PackageEvent $event): void
    {
        $operation = $event->getOperation();
        $this->io->write('post-package-install '.$operation->getPackage()->getName().' ops='.count($event->getOperations()).' '.get_class($operation));

        // Changed in place in the local repository: installed.json has it.
        $installed = $event->getLocalRepo()->findPackage($operation->getPackage()->getName(), '*');
        $installed->setExtra(['maestro-test' => 'marked by the plugin'] + $installed->getExtra());
    }

    public function preAutoloadDump(Event $event): void
    {
        $root = $this->composer->getPackage();
        $autoload = $root->getAutoload();
        $autoload['psr-4']['Generated\\'] = 'generated/';
        $root->setAutoload($autoload);
        $fs = new Filesystem();
        $fs->ensureDirectoryExists('generated');
        file_put_contents('generated/Thing.php', "<?php\nnamespace Generated;\nclass Thing {}\n");
        $this->io->write('pre-autoload-dump root psr-4 now '.implode(',', array_keys($root->getAutoload()['psr-4'])));
    }

    public function postInstall(Event $event): void
    {
        $config = $this->composer->getConfig();
        $repo = $this->composer->getRepositoryManager()->getLocalRepository();
        $names = [];
        foreach ($repo->getPackages() as $package) {
            $names[] = $package->getName().'@'.$package->getPrettyVersion();
        }
        $self = $repo->findPackage('maestro-test/api-plugin', '>=1.0');
        $im = $this->composer->getInstallationManager();
        $fs = new Filesystem();
        $process = new ProcessExecutor($this->io);
        $process->execute(['echo', 'captured'], $output);

        $this->io->write([
            'post-install '.$event->getName(),
            'vendor-dir: '.$fs->findShortestPath(getcwd(), $config->get('vendor-dir'), true),
            'packages: '.implode(' ', $names),
            'self: '.($self ? $self->getName().' '.$self->getType().' extra='.$self->getExtra()['maestro']['answer'] : 'none'),
            'install path: '.$fs->findShortestPath(getcwd(), $im->getInstallPath($self), true),
            'requires: '.implode(',', array_map(function ($link) { return $link->getTarget().' '.$link->getPrettyConstraint().' '.get_class($link->getConstraint()); }, $self->getRequires())),
            'installed versions: '.\Composer\InstalledVersions::getPrettyVersion('maestro-test/api-plugin'),
            'process: '.trim($output),
            'locked: '.var_export($this->composer->getLocker()->isLocked(), true),
            'running command: '.var_export(Composer::getRunningCommand(), true),
        ]);
    }
}
