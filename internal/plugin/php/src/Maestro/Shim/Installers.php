<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.6). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

use Composer\Installer\BinaryInstaller;
use Composer\Installer\BinaryPresenceInterface;
use Composer\Installer\LibraryInstaller;
use Composer\Installer\MetapackageInstaller;
use Composer\Installer\NoopInstaller;
use Composer\Installer\PluginInstaller;
use Composer\Installer\ProjectInstaller;
use React\Promise\PromiseInterface;

/**
 * Custom installers (docs/PLUGINS.md §5.6, D9). The shim's installer base
 * classes each have a peer in maestro: the Go installer that does the
 * work. A PHP subclass's peer calls back into PHP for the methods the
 * subclass overrides (its override set, found by reflection), so the
 * inherited Go code honours them exactly as PHP's virtual dispatch would;
 * the methods it inherits run in Go (`installer.base`), downloads and
 * extraction included.
 */
final class Installers
{
    /**
     * The shim's installer base classes, with the kind maestro knows their
     * peers by.
     */
    private const BASES = [
        LibraryInstaller::class => 'library',
        PluginInstaller::class => 'plugin',
        MetapackageInstaller::class => 'metapackage',
        NoopInstaller::class => 'noop',
        ProjectInstaller::class => 'project',
    ];

    /**
     * The overridable methods of the installer base classes: those of
     * InstallerInterface and BinaryPresenceInterface, LibraryInstaller's
     * protected hooks and PluginInstaller::disablePlugins().
     */
    private const METHODS = [
        'supports', 'isInstalled', 'download', 'prepare', 'install', 'update', 'uninstall', 'cleanup', 'getInstallPath',
        'ensureBinariesPresence', 'getPackageBasePath', 'installCode', 'updateCode', 'removeCode', 'disablePlugins',
    ];

    /**
     * The installers whose peer exists, by spl_object_id (the handle table
     * keeps the objects, so the ids stay unique).
     *
     * @var array<int, true>
     */
    private static $created = [];

    public static function register(): void
    {
        Server::register('installer.call', [self::class, 'call']);
    }

    /**
     * What maestro needs to know of an installer object: the kind of its
     * nearest shim base class ("interface" for a direct InstallerInterface
     * implementation), the methods its class overrides (whose declaring
     * class is not a shim base class), and whether it is a
     * BinaryPresenceInterface.
     *
     * @param object $installer
     * @return array<string, mixed>
     */
    public static function describe($installer): array
    {
        $kind = 'interface';
        for ($class = get_class($installer); is_string($class); $class = get_parent_class($class)) {
            if (isset(self::BASES[$class])) {
                $kind = self::BASES[$class];
                break;
            }
        }

        $overrides = [];
        if ($kind !== 'interface') {
            foreach (self::METHODS as $method) {
                if (!method_exists($installer, $method)) {
                    continue;
                }
                $declaring = (new \ReflectionMethod($installer, $method))->getDeclaringClass()->getName();
                if (!isset(self::BASES[$declaring])) {
                    $overrides[] = $method;
                }
            }
        }

        return [
            'kind' => $kind,
            'overrides' => $overrides,
            'binaryPresence' => $installer instanceof BinaryPresenceInterface,
        ];
    }

    /**
     * Creates the maestro peer of an installer being constructed.
     *
     * @param object $installer
     * @param list<mixed> $params the constructor's params maestro builds the peer from
     */
    public static function create($installer, array $params): void
    {
        Rpc::call('installer.new', [$installer, self::describe($installer), $params]);
        self::$created[spl_object_id($installer)] = true;
    }

    /**
     * Calls the base class implementation of $method ($level: the shim class
     * declaring it) on the installer's peer, as parent::$method() would.
     * Promises come back as React promises.
     *
     * @param object $installer
     * @param list<mixed> $args
     * @return mixed
     */
    public static function base($installer, string $level, string $method, array $args)
    {
        // NoopInstaller has no constructor: its peer is created when first
        // needed.
        if ($installer instanceof NoopInstaller && !isset(self::$created[spl_object_id($installer)])) {
            self::create($installer, []);
        }
        $result = Rpc::call('installer.base', [$installer, $level, $method, $args]);
        if (isset($result['vd']) && $installer instanceof LibraryInstaller) {
            Remote::fill($installer, LibraryInstaller::class, ['vendorDir' => $result['vd']]);
        }
        if (isset($result['p'])) {
            return Promises::fromMaestro($result['p']);
        }

        return $result['v'];
    }

    /**
     * The React promise of a maestro promise result ({id, s}), null for
     * null.
     *
     * @param array<string, mixed>|null $p
     */
    public static function promise($p): ?PromiseInterface
    {
        return $p === null ? null : Promises::fromMaestro($p);
    }

    /**
     * BinaryInstaller's methods on its maestro peer.
     *
     * @param list<mixed> $args
     * @return mixed
     */
    public static function binary(BinaryInstaller $installer, string $method, array $args)
    {
        return Rpc::call('installer.binary', [$installer, $method, $args]);
    }

    /**
     * Creates the maestro peer of a BinaryInstaller being constructed;
     * $overridden tells whether its class overrides the methods
     * LibraryInstaller calls (maestro then calls them in PHP).
     *
     * @param list<mixed> $params
     */
    public static function createBinary(BinaryInstaller $installer, array $params): void
    {
        $overridden = false;
        foreach (['installBinaries', 'removeBinaries'] as $method) {
            if ((new \ReflectionMethod($installer, $method))->getDeclaringClass()->getName() !== BinaryInstaller::class) {
                $overridden = true;
            }
        }
        Rpc::call('installer.newBinary', [$installer, $overridden, $params]);
    }

    /**
     * `installer.call`: maestro calls a method of a PHP installer (one it
     * overrides, or any method of an installer with no shim base). A
     * returned promise goes to maestro as a watched promise.
     *
     * @param array<string, mixed> $a
     * @return array<string, mixed>
     */
    public static function call(array $a): array
    {
        $installer = $a['h'];
        $method = (string) $a['method'];
        $args = array_values($a['args']);
        if (isset($a['vendorDir']) && $installer instanceof LibraryInstaller) {
            Remote::fill($installer, LibraryInstaller::class, ['vendorDir' => $a['vendorDir']]);
        }

        // The protected hooks (getPackageBasePath, installCode, ...) are
        // called from the object's own scope, as the base class calls them.
        $call = function () use ($method, $args) {
            return $this->$method(...$args);
        };
        $result = \Closure::bind($call, $installer, get_class($installer))();

        if ($result instanceof PromiseInterface) {
            return ['p' => Promises::watch($result)];
        }

        return ['v' => $result];
    }
}
