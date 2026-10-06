<?php

namespace MaestroTest\DownloadEvents;

use Composer\Composer;
use Composer\EventDispatcher\EventSubscriberInterface;
use Composer\IO\IOInterface;
use Composer\Package\PackageInterface;
use Composer\Plugin\PluginEvents;
use Composer\Plugin\PluginInterface;
use Composer\Plugin\PostFileDownloadEvent;
use Composer\Plugin\PreFileDownloadEvent;
use Composer\Script\Event;
use Composer\Script\ScriptEvents;

class Plugin implements PluginInterface, EventSubscriberInterface
{
    /** @var IOInterface */
    private $io;

    /** @var array<string, int> */
    private $metadata = ['pre' => 0, 'post' => 0];

    public function activate(Composer $composer, IOInterface $io): void
    {
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
            PluginEvents::PRE_FILE_DOWNLOAD => 'preDownload',
            PluginEvents::POST_FILE_DOWNLOAD => 'postDownload',
            ScriptEvents::POST_INSTALL_CMD => 'report',
            ScriptEvents::POST_UPDATE_CMD => 'report',
        ];
    }

    public function preDownload(PreFileDownloadEvent $event): void
    {
        if ($event->getType() !== 'package') {
            $context = $event->getContext();
            if (is_array($context) && isset($context['repository'])) {
                $this->metadata['pre']++;
            }

            return;
        }

        $package = $event->getContext();
        $this->io->write(sprintf(
            'pre-file-download %s %s %s cache-key=%s downloader=%s',
            $package instanceof PackageInterface ? $package->getName() : 'none',
            $event->getProcessedUrl(),
            json_encode($event->getTransportOptions()),
            var_export($event->getCustomCacheKey(), true),
            get_class($event->getHttpDownloader())
        ));
        $event->setTransportOptions(['http' => ['header' => ['X-Maestro-Test: 1']]]);
        $event->setCustomCacheKey('maestro-test-'.$package->getName());
    }

    public function postDownload(PostFileDownloadEvent $event): void
    {
        if ($event->getType() !== 'package') {
            $context = $event->getContext();
            if (is_array($context) && isset($context['response'])) {
                $this->metadata['post']++;
            }

            return;
        }

        $package = $event->getContext();
        $this->io->write(sprintf(
            'post-file-download %s %s checksum=%s',
            $package->getName(),
            $event->getUrl(),
            var_export($event->getChecksum(), true)
        ));
    }

    public function report(Event $event): void
    {
        $this->io->write('metadata events seen: '.var_export($this->metadata['pre'] > 0, true));
    }
}
