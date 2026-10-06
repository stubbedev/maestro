<?php

/*
 * maestro's plugin shim: Composer\Filter\PlatformRequirementFilter\IgnoreAllPlatformRequirementFilter,
 * reimplemented with Composer 2.10.3's behaviour. Given to maestro, it is
 * maestro's filter of the same kind.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Filter\PlatformRequirementFilter;

use Composer\Repository\PlatformRepository;

final class IgnoreAllPlatformRequirementFilter implements PlatformRequirementFilterInterface
{
    public function isIgnored(string $req): bool
    {
        return PlatformRepository::isPlatformPackage($req);
    }

    public function isUpperBoundIgnored(string $req): bool
    {
        return $this->isIgnored($req);
    }
}
