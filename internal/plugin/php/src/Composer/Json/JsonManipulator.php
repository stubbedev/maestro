<?php

/*
 * maestro's plugin shim: Composer\Json\JsonManipulator (docs/PLUGINS.md
 * §4.10): each instance has a maestro peer holding the contents (the
 * indentation and newline it detected, as Composer's does); every edit is
 * maestro's (json.manipulate), so the result is byte for byte Composer's.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Json;

use Maestro\Shim\Rpc;

class JsonManipulator
{
    /** @var object maestro's JsonManipulator */
    private $peer;

    public function __construct(string $contents)
    {
        $this->peer = Rpc::call('json.newManipulator', [$contents]);
    }

    /**
     * @param list<mixed> $args
     * @return mixed
     */
    private function edit(string $method, array $args)
    {
        return Rpc::call('json.manipulate', [$this->peer, $method, $args]);
    }

    public function getContents(): string
    {
        return $this->edit('getContents', []);
    }

    public function addConfigSetting(string $name, $value): bool
    {
        return $this->edit('addConfigSetting', [$name, $value]);
    }

    public function addLink(string $type, string $package, string $constraint, bool $sortPackages = false): bool
    {
        return $this->edit('addLink', [$type, $package, $constraint, $sortPackages]);
    }

    public function addListItem(string $mainNode, $value, bool $append = true): bool
    {
        return $this->edit('addListItem', [$mainNode, $value, $append]);
    }

    public function addMainKey(string $key, $content): bool
    {
        return $this->edit('addMainKey', [$key, $content]);
    }

    public function addProperty(string $name, $value): bool
    {
        return $this->edit('addProperty', [$name, $value]);
    }

    public function addRepository(string $name, $config, bool $append = true): bool
    {
        return $this->edit('addRepository', [$name, $config, $append]);
    }

    public function addSubNode(string $mainNode, string $name, $value, bool $append = true): bool
    {
        return $this->edit('addSubNode', [$mainNode, $name, $value, $append]);
    }

    public function changeEmptyMainKeyFromAssocToList(string $key): bool
    {
        return $this->edit('changeEmptyMainKeyFromAssocToList', [$key]);
    }

    public function insertListItem(string $mainNode, $value, int $index): bool
    {
        return $this->edit('insertListItem', [$mainNode, $value, $index]);
    }

    public function insertRepository(string $name, $config, string $referenceName, int $offset = 0): bool
    {
        return $this->edit('insertRepository', [$name, $config, $referenceName, $offset]);
    }

    public function removeConfigSetting(string $name): bool
    {
        return $this->edit('removeConfigSetting', [$name]);
    }

    public function removeListItem(string $mainNode, int $nodeIndex): bool
    {
        return $this->edit('removeListItem', [$mainNode, $nodeIndex]);
    }

    public function removeMainKey(string $key): bool
    {
        return $this->edit('removeMainKey', [$key]);
    }

    public function removeMainKeyIfEmpty(string $key): bool
    {
        return $this->edit('removeMainKeyIfEmpty', [$key]);
    }

    public function removeProperty(string $name): bool
    {
        return $this->edit('removeProperty', [$name]);
    }

    public function removeRepository(string $name): bool
    {
        return $this->edit('removeRepository', [$name]);
    }

    public function removeSubNode(string $mainNode, string $name): bool
    {
        return $this->edit('removeSubNode', [$mainNode, $name]);
    }

    public function setRepositoryUrl(string $name, string $url): bool
    {
        return $this->edit('setRepositoryUrl', [$name, $url]);
    }

    public function format($data, int $depth = 0, bool $wasObject = false): string
    {
        return $this->edit('format', [$data, $depth, $wasObject]);
    }

    protected function detectIndenting(): void
    {
        $this->edit('detectIndenting', []);
    }
}
