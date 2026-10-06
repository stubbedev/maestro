<?php

/*
 * maestro's plugin shim: Composer\Plugin\PreFileDownloadEvent
 * (docs/PLUGINS.md §4.4, §5.8), a mirror of maestro's: the URL, cache key
 * and transport options its setters set are what maestro downloads with.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Plugin;

use Composer\EventDispatcher\Event;
use Composer\Util\HttpDownloader;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class PreFileDownloadEvent extends Event
{
    private $httpDownloader;
    private $processedUrl;
    private $customCacheKey;
    private $type;
    private $context;
    private $transportOptions = [];

    public function __construct(string $name, HttpDownloader $httpDownloader, string $processedUrl, string $type, $context = null)
    {
        parent::__construct($name);
        $this->httpDownloader = $httpDownloader;
        $this->processedUrl = $processedUrl;
        $this->type = $type;
        $this->context = $context;
    }

    public function getHttpDownloader(): HttpDownloader
    {
        return $this->httpDownloader;
    }

    public function getProcessedUrl(): string
    {
        return $this->processedUrl;
    }

    public function setProcessedUrl(string $processedUrl): void
    {
        if (Remote::owned($this)) {
            Rpc::call('event.setProcessedUrl', [$this, $processedUrl]);

            return;
        }
        $this->processedUrl = $processedUrl;
    }

    public function getCustomCacheKey(): ?string
    {
        return $this->customCacheKey;
    }

    public function setCustomCacheKey(?string $customCacheKey): void
    {
        if (Remote::owned($this)) {
            Rpc::call('event.setCustomCacheKey', [$this, $customCacheKey]);

            return;
        }
        $this->customCacheKey = $customCacheKey;
    }

    public function getType(): string
    {
        return $this->type;
    }

    public function getContext()
    {
        return $this->context;
    }

    public function getTransportOptions(): array
    {
        return $this->transportOptions;
    }

    public function setTransportOptions(array $options): void
    {
        if (Remote::owned($this)) {
            Rpc::call('event.setTransportOptions', [$this, $options]);

            return;
        }
        $this->transportOptions = $options;
    }
}
