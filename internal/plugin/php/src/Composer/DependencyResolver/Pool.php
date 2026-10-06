<?php

/*
 * maestro's plugin shim: Composer\DependencyResolver\Pool (docs/PLUGINS.md
 * §4.12; phase 6). A PHP-local container with Composer's behaviour: `new
 * Pool($packages)` (php-http/discovery's pre-RepositorySet path) and the
 * pools RepositorySet::createPool*() build in maestro, which come back as
 * their packages (each with the id maestro's pool gave it) and the
 * versions the pool builder removed.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\DependencyResolver;

use Composer\Package\BasePackage;
use Composer\Package\Version\VersionParser;
use Composer\Semver\CompilingMatcher;
use Composer\Semver\Constraint\Constraint;
use Composer\Semver\Constraint\ConstraintInterface;

class Pool implements \Countable, \Stringable
{
    protected $packageByName = [];
    protected $packages = [];
    protected $providerCache = [];
    protected $removedVersions = [];
    protected $removedVersionsByPackage = [];
    protected $unacceptableFixedOrLockedPackages;
    protected $versionParser;
    private $securityRemovedVersions = [];
    private $abandonedRemovedVersions = [];
    private $filterListRemovedVersions = [];

    public function __construct(array $packages = [], array $unacceptableFixedOrLockedPackages = [], array $removedVersions = [], array $removedVersionsByPackage = [], array $securityRemovedVersions = [], array $abandonedRemovedVersions = [], array $filterListRemovedVersions = [])
    {
        $this->versionParser = new VersionParser();
        $id = 1;
        foreach ($packages as $package) {
            $this->packages[] = $package;
            $package->id = $id++;
            foreach ($package->getNames() as $provided) {
                $this->packageByName[$provided][] = $package;
            }
        }
        $this->unacceptableFixedOrLockedPackages = $unacceptableFixedOrLockedPackages;
        $this->removedVersions = $removedVersions;
        $this->removedVersionsByPackage = $removedVersionsByPackage;
        $this->securityRemovedVersions = $securityRemovedVersions;
        $this->abandonedRemovedVersions = $abandonedRemovedVersions;
        $this->filterListRemovedVersions = $filterListRemovedVersions;
    }

    public function __toString(): string
    {
        $out = "Pool:\n";
        foreach ($this->packages as $package) {
            $out .= '- '.str_pad((string) $package->id, 6, ' ', STR_PAD_LEFT).': '.$package->getName()."\n";
        }

        return $out;
    }

    public function count(): int
    {
        return \count($this->packages);
    }

    public function getAllAbandonedRemovedPackageVersions(): array
    {
        return $this->abandonedRemovedVersions;
    }

    public function getAllFilterListRemovedPackageVersions(): array
    {
        return $this->filterListRemovedVersions;
    }

    public function getAllRemovedVersions(): array
    {
        return $this->removedVersions;
    }

    public function getAllRemovedVersionsByPackage(): array
    {
        return $this->removedVersionsByPackage;
    }

    public function getAllSecurityRemovedPackageVersions(): array
    {
        return $this->securityRemovedVersions;
    }

    public function getFilterListEntryForPackageVersion(string $packageName, ?\Composer\Semver\Constraint\ConstraintInterface $constraint): array
    {
        $byList = [];
        $seen = [];
        foreach (isset($this->filterListRemovedVersions[$packageName]) ? $this->filterListRemovedVersions[$packageName] : [] as $version => $entries) {
            if ($constraint === null || !$constraint->matches(new Constraint('==', (string) $version))) {
                continue;
            }
            foreach ($entries as $entry) {
                if (isset($seen[spl_object_id($entry)])) {
                    continue;
                }
                $seen[spl_object_id($entry)] = true;
                $byList[$entry->listName][] = ((bool) $entry->source ? ' reported by '.$entry->source : '')
                    .((bool) $entry->url ? ' (see '.$entry->url.')' : '')
                    .((bool) $entry->reason ? ' reason: '.$entry->reason : '');
            }
        }

        $out = [];
        foreach ($byList as $listName => $parts) {
            $out[$listName] = ($listName === 'malware' ? 'flagged as ' : 'filtered by ').$listName.implode(', ', $parts);
        }

        return $out;
    }

    public function getPackages(): array
    {
        return $this->packages;
    }

    public function getRemovedVersions(string $name, \Composer\Semver\Constraint\ConstraintInterface $constraint): array
    {
        $out = [];
        foreach (isset($this->removedVersions[$name]) ? $this->removedVersions[$name] : [] as $version => $prettyVersion) {
            if ($constraint->matches(new Constraint('==', (string) $version))) {
                $out[$version] = $prettyVersion;
            }
        }

        return $out;
    }

    public function getRemovedVersionsByPackage(int $objectId): array
    {
        return isset($this->removedVersionsByPackage[$objectId]) ? $this->removedVersionsByPackage[$objectId] : [];
    }

    public function getSecurityAdvisoryIdentifiersForPackageVersion(string $packageName, ?\Composer\Semver\Constraint\ConstraintInterface $constraint): array
    {
        foreach (isset($this->securityRemovedVersions[$packageName]) ? $this->securityRemovedVersions[$packageName] : [] as $version => $advisories) {
            if ($constraint !== null && $constraint->matches(new Constraint('==', (string) $version))) {
                $ids = [];
                foreach ($advisories as $advisory) {
                    $ids[] = $advisory->advisoryId;
                }

                return $ids;
            }
        }

        return [];
    }

    public function getUnacceptableFixedOrLockedPackages(): array
    {
        return $this->unacceptableFixedOrLockedPackages;
    }

    public function isAbandonedRemovedPackageVersion(string $packageName, ?\Composer\Semver\Constraint\ConstraintInterface $constraint): bool
    {
        return self::anyVersionMatches(isset($this->abandonedRemovedVersions[$packageName]) ? $this->abandonedRemovedVersions[$packageName] : [], $constraint);
    }

    public function isFilterListRemovedPackageVersion(string $packageName, ?\Composer\Semver\Constraint\ConstraintInterface $constraint): bool
    {
        return self::anyVersionMatches(isset($this->filterListRemovedVersions[$packageName]) ? $this->filterListRemovedVersions[$packageName] : [], $constraint);
    }

    public function isSecurityRemovedPackageVersion(string $packageName, ?\Composer\Semver\Constraint\ConstraintInterface $constraint): bool
    {
        return self::anyVersionMatches(isset($this->securityRemovedVersions[$packageName]) ? $this->securityRemovedVersions[$packageName] : [], $constraint);
    }

    public function isUnacceptableFixedOrLockedPackage(\Composer\Package\BasePackage $package): bool
    {
        return \in_array($package, $this->unacceptableFixedOrLockedPackages, true);
    }

    public function literalToPackage(int $literal): \Composer\Package\BasePackage
    {
        return $this->packageById(abs($literal));
    }

    public function literalToPrettyString(int $literal, array $installedMap): string
    {
        $package = $this->literalToPackage($literal);
        if (isset($installedMap[$package->id])) {
            $verb = $literal > 0 ? 'keep' : 'remove';
        } else {
            $verb = $literal > 0 ? 'install' : 'don\'t install';
        }

        return $verb.' '.$package->getPrettyString();
    }

    public function match(\Composer\Package\BasePackage $candidate, string $name, ?\Composer\Semver\Constraint\ConstraintInterface $constraint = null): bool
    {
        if ($candidate->getName() === $name) {
            return $constraint === null || CompilingMatcher::match($constraint, Constraint::OP_EQ, $candidate->getVersion());
        }

        $provides = $candidate->getProvides();
        $replaces = $candidate->getReplaces();

        // An alias's links are a list (several per target).
        if (isset($replaces[0]) || isset($provides[0])) {
            foreach ([$provides, $replaces] as $links) {
                foreach ($links as $link) {
                    if ($link->getTarget() === $name && ($constraint === null || $constraint->matches($link->getConstraint()))) {
                        return true;
                    }
                }
            }

            return false;
        }

        foreach ([$provides, $replaces] as $links) {
            if (isset($links[$name]) && ($constraint === null || $constraint->matches($links[$name]->getConstraint()))) {
                return true;
            }
        }

        return false;
    }

    public function packageById(int $id): \Composer\Package\BasePackage
    {
        return $this->packages[$id - 1];
    }

    public function whatProvides(string $name, ?\Composer\Semver\Constraint\ConstraintInterface $constraint = null): array
    {
        $key = (string) $constraint;
        if (!isset($this->providerCache[$name][$key])) {
            $matches = [];
            foreach (isset($this->packageByName[$name]) ? $this->packageByName[$name] : [] as $candidate) {
                if ($this->match($candidate, $name, $constraint)) {
                    $matches[] = $candidate;
                }
            }
            $this->providerCache[$name][$key] = $matches;
        }

        return $this->providerCache[$name][$key];
    }

    /**
     * @param array<string, mixed> $versions
     */
    private static function anyVersionMatches(array $versions, ?ConstraintInterface $constraint): bool
    {
        foreach ($versions as $version => $_) {
            if ($constraint !== null && $constraint->matches(new Constraint('==', (string) $version))) {
                return true;
            }
        }

        return false;
    }
}
