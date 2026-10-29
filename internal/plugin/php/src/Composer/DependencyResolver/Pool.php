<?php

/*
 * maestro's plugin shim: Composer\DependencyResolver\Pool (docs/PLUGINS.md
 * §4.12; tier 6). A PHP-local container with Composer's behaviour: `new
 * Pool($packages)` (php-http/discovery's pre-RepositorySet path) and the
 * pools RepositorySet::createPool*() build in maestro, which come back as
 * their packages (each with the id maestro's pool gave it) and what the
 * pool builder and its filters removed, with a PolicyRemovalReason for
 * every version a dependency policy took out.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\DependencyResolver;

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
    private $policyRemovedVersions = [];

    public function __construct(array $packages = [], array $unacceptableFixedOrLockedPackages = [], array $removedVersions = [], array $removedVersionsByPackage = [], array $policyRemovedVersions = [])
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
        $this->policyRemovedVersions = $policyRemovedVersions;
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

    public function getAllPolicyRemovedVersions(): array
    {
        return $this->policyRemovedVersions;
    }

    public function getAllRemovedVersions(): array
    {
        return $this->removedVersions;
    }

    public function getAllRemovedVersionsByPackage(): array
    {
        return $this->removedVersionsByPackage;
    }

    public function getPackages(): array
    {
        return $this->packages;
    }

    /**
     * Versions of a package that dependency policies removed from the pool, with the reason for each
     *
     * @return array<string, PolicyRemovalReason> normalized version => reason, in ascending version order
     */
    public function getPolicyRemovedVersions(string $packageName, ?ConstraintInterface $constraint = null): array
    {
        $result = [];
        foreach (isset($this->policyRemovedVersions[$packageName]) ? $this->policyRemovedVersions[$packageName] : [] as $version => $reason) {
            if ($constraint === null || $constraint->matches(new Constraint('==', (string) $version))) {
                $result[$version] = $reason;
            }
        }
        uksort($result, static function (string $a, string $b): int {
            return version_compare($a, $b);
        });

        return $result;
    }

    public function getPolicyRemovalReason(string $packageName, string $version): ?PolicyRemovalReason
    {
        return isset($this->policyRemovedVersions[$packageName][$version]) ? $this->policyRemovedVersions[$packageName][$version] : null;
    }

    /**
     * Records a removal under every name of the package, but a replacer never hides the removal of the package itself
     *
     * @internal
     * @param array<string, array<string, PolicyRemovalReason>> $policyRemovedVersions
     */
    public static function recordPolicyRemoval(array &$policyRemovedVersions, \Composer\Package\BasePackage $package, PolicyRemovalReason $reason): void
    {
        foreach ($package->getNames(false) as $name) {
            $existing = isset($policyRemovedVersions[$name][$package->getVersion()]) ? $policyRemovedVersions[$name][$package->getVersion()] : null;
            if ($existing === null || ($existing->getPackageName() !== $name && $package->getName() === $name)) {
                $policyRemovedVersions[$name][$package->getVersion()] = $reason;
            }
        }
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

    public function getUnacceptableFixedOrLockedPackages(): array
    {
        return $this->unacceptableFixedOrLockedPackages;
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
}
