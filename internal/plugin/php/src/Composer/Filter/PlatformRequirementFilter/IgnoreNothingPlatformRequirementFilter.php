<?php

/*
 * maestro's plugin shim: Composer\Filter\PlatformRequirementFilter\IgnoreNothingPlatformRequirementFilter,
 * reimplemented with Composer 2.10.3's behaviour.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Filter\PlatformRequirementFilter;

final class IgnoreNothingPlatformRequirementFilter implements PlatformRequirementFilterInterface
{
    public function isIgnored(string $req): bool
    {
        return false;
    }

    public function isUpperBoundIgnored(string $req): bool
    {
        return false;
    }
}
