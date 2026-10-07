<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Composer\Package\AliasPackage;
use Composer\Package\BasePackage;
use Composer\Package\CompleteAliasPackage;
use Composer\Package\CompletePackage;
use Composer\Package\Package;
use Composer\Package\RootAliasPackage;
use Composer\Package\RootPackage;
use Maestro\Shim\MirrorAdapter;
use Maestro\Shim\Remote;

/**
 * The package family's mirrors (docs/PLUGINS.md §5.3, §6.4): maestro's
 * packages fill Composer's protected properties, by name. The release
 * date crosses as an ISO 8601 string.
 */
final class PackageAdapter implements MirrorAdapter
{
    const BASE = ['id', 'name', 'prettyName'];

    const PACKAGE = [
        'type', 'targetDir', 'installationSource', 'sourceType', 'sourceUrl', 'sourceReference', 'sourceMirrors',
        'distType', 'distUrl', 'distReference', 'distSha1Checksum', 'distMirrors', 'version', 'prettyVersion',
        'releaseDate', 'extra', 'binaries', 'dev', 'stability', 'notificationUrl', 'requires', 'conflicts',
        'provides', 'replaces', 'devRequires', 'suggests', 'autoload', 'devAutoload', 'includePaths',
        'isDefaultBranch', 'transportOptions', 'phpExt',
    ];

    const COMPLETE = [
        'repositories', 'license', 'keywords', 'authors', 'description', 'homepage', 'scripts', 'support',
        'funding', 'abandoned', 'archiveName', 'archiveExcludes',
    ];

    const ROOT = ['minimumStability', 'preferStable', 'stabilityFlags', 'config', 'references', 'aliases'];

    const ALIAS = [
        'version', 'prettyVersion', 'dev', 'rootPackageAlias', 'stability', 'hasSelfVersionRequires', 'aliasOf',
        'requires', 'devRequires', 'conflicts', 'provides', 'replaces',
    ];

    public function base(): string
    {
        return BasePackage::class;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        if (!class_exists($class) || !is_a($class, BasePackage::class, true)) {
            $class = Package::class;
        }
        $package = (new \ReflectionClass($class))->newInstanceWithoutConstructor();
        $this->apply($package, $snapshot);

        return $package;
    }

    public function snapshot($object): array
    {
        $fields = $this->fields($object, self::names($object));
        $fields['class'] = self::composerClass($object);

        return $fields;
    }

    /**
     * The nearest Composer class of a package: what maestro builds for a
     * package created in PHP (a plugin's subclass keeps its own class in
     * PHP).
     *
     * @param object $object
     */
    private static function composerClass($object): string
    {
        foreach ([RootAliasPackage::class, CompleteAliasPackage::class, AliasPackage::class, RootPackage::class, CompletePackage::class] as $class) {
            if ($object instanceof $class) {
                return $class;
            }
        }

        return Package::class;
    }

    public function fields($object, array $names): array
    {
        $fields = Remote::read($object, BasePackage::class, $names);
        if (array_key_exists('releaseDate', $fields) && $fields['releaseDate'] instanceof \DateTimeInterface) {
            $fields['releaseDate'] = $fields['releaseDate']->format('Y-m-d\TH:i:s.uP');
        }

        return $fields;
    }

    public function apply($object, array $fields): void
    {
        // A snapshot of the core fields only (docs/PLUGINS.md §5.3): the
        // getters of the rest fetch them first (LazyPackages).
        if (isset($fields['lazy'])) {
            unset($fields['lazy']);
            \Maestro\Shim\LazyPackages::$pending[spl_object_id($object)] = $object;
        } else {
            unset(\Maestro\Shim\LazyPackages::$pending[spl_object_id($object)]);
        }
        if (isset($fields['releaseDate'])) {
            $fields['releaseDate'] = new \DateTime($fields['releaseDate']);
        }
        Remote::fill($object, BasePackage::class, $fields);
    }

    /**
     * The property names of a package's class.
     *
     * @param object $object
     * @return list<string>
     */
    private static function names($object): array
    {
        if ($object instanceof AliasPackage) {
            return array_merge(self::BASE, self::ALIAS);
        }
        $names = array_merge(self::BASE, self::PACKAGE);
        if ($object instanceof CompletePackage) {
            $names = array_merge($names, self::COMPLETE);
        }
        if ($object instanceof RootPackage) {
            $names = array_merge($names, self::ROOT);
        }

        return $names;
    }
}
