<?php

/*
 * maestro's plugin shim: Composer\Config\JsonConfigSource (docs/PLUGINS.md
 * §4.2): a proxy of maestro's (cfgsrc.*), whose edits are Composer's,
 * byte for byte; one created in PHP is maestro's too.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Config;

use Composer\Json\JsonFile;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class JsonConfigSource implements ConfigSourceInterface
{
    public function __construct(JsonFile $file, bool $authConfig = false)
    {
        $io = Remote::read($file, JsonFile::class, ['io'])['io'];
        Rpc::call('cfgsrc.new', [$this, $file->getPath(), $io, $authConfig]);
    }

    public function getName(): string
    {
        return Rpc::call('cfgsrc.getName', [$this]);
    }

    public function addRepository(string $name, $config, bool $append = true): void
    {
        Rpc::call('cfgsrc.addRepository', [$this, $name, $config, $append]);
    }

    public function insertRepository(string $name, $config, string $referenceName, int $offset = 0): void
    {
        Rpc::call('cfgsrc.insertRepository', [$this, $name, $config, $referenceName, $offset]);
    }

    public function setRepositoryUrl(string $name, string $url): void
    {
        Rpc::call('cfgsrc.setRepositoryUrl', [$this, $name, $url]);
    }

    public function removeRepository(string $name): void
    {
        Rpc::call('cfgsrc.removeRepository', [$this, $name]);
    }

    public function addConfigSetting(string $name, $value): void
    {
        Rpc::call('cfgsrc.addConfigSetting', [$this, $name, $value]);
    }

    public function removeConfigSetting(string $name): void
    {
        Rpc::call('cfgsrc.removeConfigSetting', [$this, $name]);
    }

    public function addProperty(string $name, $value): void
    {
        Rpc::call('cfgsrc.addProperty', [$this, $name, $value]);
    }

    public function removeProperty(string $name): void
    {
        Rpc::call('cfgsrc.removeProperty', [$this, $name]);
    }

    public function addLink(string $type, string $name, string $value): void
    {
        Rpc::call('cfgsrc.addLink', [$this, $type, $name, $value]);
    }

    public function removeLink(string $type, string $name): void
    {
        Rpc::call('cfgsrc.removeLink', [$this, $type, $name]);
    }

    /**
     * @internal
     * @param array<mixed> $config
     */
    public static function restoreEmptyObjects(array &$config): void
    {
        if (isset($config['config']['policy']) && is_array($config['config']['policy'])) {
            foreach ($config['config']['policy'] as $listName => $listValue) {
                if ($listValue === []) {
                    $config['config']['policy'][$listName] = new \stdClass;
                }
            }
            if ($config['config']['policy'] === []) {
                $config['config']['policy'] = new \stdClass;
            }
        }
        foreach (['platform', 'http-basic', 'bearer', 'gitlab-token', 'gitlab-oauth', 'github-oauth', 'custom-headers', 'forgejo-token', 'preferred-install'] as $prop) {
            if (isset($config['config'][$prop]) && $config['config'][$prop] === []) {
                $config['config'][$prop] = new \stdClass;
            }
        }
        foreach (['psr-0', 'psr-4'] as $prop) {
            foreach (['autoload', 'autoload-dev'] as $section) {
                if (isset($config[$section][$prop]) && $config[$section][$prop] === []) {
                    $config[$section][$prop] = new \stdClass;
                }
            }
        }
        foreach (['require', 'require-dev', 'conflict', 'provide', 'replace', 'suggest', 'config', 'autoload', 'autoload-dev', 'scripts', 'scripts-descriptions', 'scripts-aliases', 'support'] as $prop) {
            if (isset($config[$prop]) && $config[$prop] === []) {
                $config[$prop] = new \stdClass;
            }
        }
    }
}
