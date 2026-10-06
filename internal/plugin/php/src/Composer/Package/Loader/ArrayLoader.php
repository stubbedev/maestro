<?php

/*
 * maestro's plugin shim: Composer\Package\Loader\ArrayLoader
 * (docs/PLUGINS.md §4.5): loading is maestro's (loader.*); the packages it
 * returns are maestro's, as PHP mirrors them.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Package\Loader;

use Composer\Package\Version\VersionParser;
use Maestro\Shim\Rpc;

class ArrayLoader implements LoaderInterface
{
    protected $versionParser;
    protected $loadOptions;

    public function __construct(?VersionParser $parser = null, bool $loadOptions = false)
    {
        if (!$parser) {
            $parser = new VersionParser;
        }
        $this->versionParser = $parser;
        $this->loadOptions = $loadOptions;
    }

    public function load(array $config, string $class = 'Composer\\Package\\CompletePackage'): \Composer\Package\BasePackage
    {
        if ($class !== 'Composer\\Package\\CompletePackage' && $class !== 'Composer\\Package\\RootPackage') {
            trigger_error('The $class arg is deprecated, please reach out to Composer maintainers ASAP if you still need this.', E_USER_DEPRECATED);
        }

        return Rpc::call('loader.load', [$config, $class, $this->loadOptions]);
    }

    public function loadPackages(array $versions): array
    {
        return Rpc::call('loader.loadPackages', [$versions, $this->loadOptions]);
    }

    public function parseLinks(string $source, string $sourceVersion, string $description, array $links): array
    {
        return Rpc::call('loader.parseLinks', [$source, $sourceVersion, $description, $links]);
    }

    public function getBranchAlias(array $config): ?string
    {
        return Rpc::call('loader.getBranchAlias', [$config]);
    }
}
