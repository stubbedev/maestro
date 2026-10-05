<?php

/*
 * maestro's plugin shim: Composer\DependencyResolver\Operation\UpdateOperation,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.7).
 * maestro's operations are data mirrors (Maestro\Shim\Adapter\
 * OperationAdapter); one created in PHP is plain PHP.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\DependencyResolver\Operation;

class UpdateOperation extends \Composer\DependencyResolver\Operation\SolverOperation
{
    protected const TYPE = 'update';

    protected $initialPackage;
    protected $targetPackage;

    public function __construct(\Composer\Package\PackageInterface $initial, \Composer\Package\PackageInterface $target)
    {
        $this->initialPackage = $initial;
        $this->targetPackage = $target;
    }

    public static function format(\Composer\Package\PackageInterface $initialPackage, \Composer\Package\PackageInterface $targetPackage, bool $lock = false): string
    {
        $fromVersion = $initialPackage->getFullPrettyVersion();
        $toVersion = $targetPackage->getFullPrettyVersion();

        if ($fromVersion === $toVersion && $initialPackage->getSourceReference() !== $targetPackage->getSourceReference()) {
            $fromVersion = $initialPackage->getFullPrettyVersion(true, \Composer\Package\PackageInterface::DISPLAY_SOURCE_REF);
            $toVersion = $targetPackage->getFullPrettyVersion(true, \Composer\Package\PackageInterface::DISPLAY_SOURCE_REF);
        } elseif ($fromVersion === $toVersion && $initialPackage->getDistReference() !== $targetPackage->getDistReference()) {
            $fromVersion = $initialPackage->getFullPrettyVersion(true, \Composer\Package\PackageInterface::DISPLAY_DIST_REF);
            $toVersion = $targetPackage->getFullPrettyVersion(true, \Composer\Package\PackageInterface::DISPLAY_DIST_REF);
        }

        $actionName = \Composer\Package\Version\VersionParser::isUpgrade($initialPackage->getVersion(), $targetPackage->getVersion()) ? 'Upgrading' : 'Downgrading';

        return $actionName.' <info>'.$initialPackage->getPrettyName().'</info> (<comment>'.$fromVersion.'</comment> => <comment>'.$toVersion.'</comment>)';
    }

    public function getInitialPackage(): \Composer\Package\PackageInterface
    {
        return $this->initialPackage;
    }

    public function getTargetPackage(): \Composer\Package\PackageInterface
    {
        return $this->targetPackage;
    }

    public function show($lock): string
    {
        return self::format($this->initialPackage, $this->targetPackage, $lock);
    }
}
