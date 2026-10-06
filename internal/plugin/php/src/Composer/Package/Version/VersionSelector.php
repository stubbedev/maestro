<?php

/*
 * maestro's plugin shim: Composer\Package\Version\VersionSelector
 * (docs/PLUGINS.md §4.5): one created in PHP is maestro's (selector.*),
 * over a RepositorySet and PlatformRepository of maestro's.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Package\Version;

use Composer\IO\IOInterface;
use Composer\Package\PackageInterface;
use Composer\Repository\PlatformRepository;
use Composer\Repository\RepositorySet;
use Maestro\Shim\Rpc;

class VersionSelector
{
    public function __construct(RepositorySet $repositorySet, ?PlatformRepository $platformRepo = null)
    {
        Rpc::call('selector.new', [$this, $repositorySet, $platformRepo, PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION.'.'.PHP_RELEASE_VERSION]);
    }

    public function findBestCandidate(string $packageName, ?string $targetPackageVersion = null, string $preferredStability = 'stable', $platformRequirementFilter = null, int $repoSetFlags = 0, ?IOInterface $io = null, $showWarnings = true)
    {
        if (!isset(\Composer\Package\BasePackage::STABILITIES[$preferredStability])) {
            // If you get this, maybe you are still relying on the Composer 1.x signature where the 3rd arg was the php version
            throw new \UnexpectedValueException('Expected a valid stability name as 3rd argument, got '.$preferredStability);
        }

        if ($platformRequirementFilter !== null && !$platformRequirementFilter instanceof \Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterInterface) {
            trigger_error('VersionSelector::findBestCandidate with ignored platform reqs as bool|array is deprecated since Composer 2.2, use an instance of PlatformRequirementFilterInterface instead.', E_USER_DEPRECATED);
            $platformRequirementFilter = \Composer\Filter\PlatformRequirementFilter\PlatformRequirementFilterFactory::fromBoolOrList($platformRequirementFilter);
        }
        if ($platformRequirementFilter instanceof \Composer\Filter\PlatformRequirementFilter\IgnoreAllPlatformRequirementFilter) {
            $filter = true;
        } elseif ($platformRequirementFilter === null || $platformRequirementFilter instanceof \Composer\Filter\PlatformRequirementFilter\IgnoreNothingPlatformRequirementFilter) {
            $filter = false;
        } elseif ($platformRequirementFilter instanceof \Composer\Filter\PlatformRequirementFilter\IgnoreListPlatformRequirementFilter) {
            $read = function () {
                return $this->reqList;
            };
            $filter = \Closure::bind($read, $platformRequirementFilter, \Composer\Filter\PlatformRequirementFilter\IgnoreListPlatformRequirementFilter::class)();
        } else {
            // A filter class of the plugin's own: maestro calls its
            // isIgnored() and isUpperBoundIgnored().
            $filter = $platformRequirementFilter;
        }
        if (!is_bool($showWarnings) && !is_callable($showWarnings)) {
            $showWarnings = (bool) $showWarnings;
        }

        return Rpc::call('selector.findBestCandidate', [$this, $packageName, $targetPackageVersion, $preferredStability, $filter, $repoSetFlags, $io, $showWarnings]);
    }

    public function findRecommendedRequireVersion(PackageInterface $package): string
    {
        return Rpc::call('selector.findRecommendedRequireVersion', [$this, $package]);
    }
}
