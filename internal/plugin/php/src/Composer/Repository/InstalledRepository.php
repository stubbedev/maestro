<?php

/*
 * maestro's plugin shim: Composer\Repository\InstalledRepository,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.6).
 * See CompositeRepository.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Repository;

use Composer\Package\Link;
use Composer\Package\RootPackageInterface;
use Composer\Package\Version\VersionParser;
use Composer\Semver\Constraint\Constraint;
use Composer\Semver\Constraint\ConstraintInterface;
use Composer\Semver\Constraint\MatchAllConstraint;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class InstalledRepository extends CompositeRepository
{
    public function findPackagesWithReplacersAndProviders(string $name, $constraint = null): array
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.findPackagesWithReplacersAndProviders', [$this, $name, $constraint]);
        }

        $name = strtolower($name);
        if ($constraint !== null && !$constraint instanceof ConstraintInterface) {
            $constraint = (new VersionParser())->parseConstraints($constraint);
        }

        $matches = [];
        foreach ($this->getRepositories() as $repository) {
            foreach ($repository->getPackages() as $candidate) {
                if ($candidate->getName() === $name) {
                    if ($constraint === null || $constraint->matches(new Constraint('==', $candidate->getVersion()))) {
                        $matches[] = $candidate;
                    }
                    continue;
                }

                foreach (array_merge($candidate->getProvides(), $candidate->getReplaces()) as $link) {
                    if ($link->getTarget() === $name && ($constraint === null || $constraint->matches($link->getConstraint()))) {
                        $matches[] = $candidate;
                        continue 2;
                    }
                }
            }
        }

        return $matches;
    }

    public function getDependents($needle, ?ConstraintInterface $constraint = null, bool $invert = false, bool $recurse = true, ?array $packagesFound = null): array
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getDependents', [$this, $needle, $constraint, $invert, $recurse, $packagesFound]);
        }

        $needles = array_map('strtolower', (array) $needle);
        $results = [];
        if ($packagesFound === null) {
            $packagesFound = $needles;
        }

        $rootPackage = null;
        foreach ($this->getPackages() as $package) {
            if ($package instanceof RootPackageInterface) {
                $rootPackage = $package;
                break;
            }
        }

        foreach ($this->getPackages() as $package) {
            $links = $package->getRequires();
            $packagesInTree = $packagesFound;

            if (!$invert) {
                $links += $package->getReplaces();
                foreach ($package->getReplaces() as $link) {
                    foreach ($needles as $needle) {
                        if ($link->getSource() !== $needle) {
                            continue;
                        }
                        if ($constraint !== null && $link->getConstraint()->matches($constraint) !== true) {
                            continue;
                        }
                        if (in_array($link->getTarget(), $packagesInTree)) {
                            $results[] = [$package, $link, false];
                            continue;
                        }
                        $packagesInTree[] = $link->getTarget();
                        $dependents = $recurse ? $this->getDependents($link->getTarget(), null, false, true, $packagesInTree) : [];
                        $results[] = [$package, $link, $dependents];
                        $needles[] = $link->getTarget();
                    }
                }
                unset($needle);
            }

            if ($package instanceof RootPackageInterface) {
                $links += $package->getDevRequires();
            }

            foreach ($links as $link) {
                foreach ($needles as $needle) {
                    if ($link->getTarget() !== $needle) {
                        continue;
                    }
                    if ($constraint !== null && $link->getConstraint()->matches($constraint) !== !$invert) {
                        continue;
                    }
                    if (in_array($link->getSource(), $packagesInTree)) {
                        $results[] = [$package, $link, false];
                        continue;
                    }
                    $packagesInTree[] = $link->getSource();
                    $dependents = $recurse ? $this->getDependents($link->getSource(), null, false, true, $packagesInTree) : [];
                    $results[] = [$package, $link, $dependents];
                }
            }

            if ($invert && in_array($package->getName(), $needles, true)) {
                foreach ($package->getConflicts() as $link) {
                    foreach ($this->findPackages($link->getTarget()) as $found) {
                        if ($link->getConstraint()->matches(new Constraint('=', $found->getVersion())) === $invert) {
                            $results[] = [$package, $link, false];
                        }
                    }
                }
            }

            foreach ($package->getConflicts() as $link) {
                if (!in_array($link->getTarget(), $needles, true)) {
                    continue;
                }
                foreach ($this->findPackages($link->getTarget()) as $found) {
                    if ($link->getConstraint()->matches(new Constraint('=', $found->getVersion())) === $invert) {
                        $results[] = [$package, $link, false];
                    }
                }
            }

            if ($invert && $constraint && in_array($package->getName(), $needles, true) && $constraint->matches(new Constraint('=', $package->getVersion()))) {
                foreach ($package->getRequires() as $link) {
                    if (PlatformRepository::isPlatformPackage($link->getTarget())) {
                        if ($this->findPackage($link->getTarget(), $link->getConstraint())) {
                            continue;
                        }
                        $platformPackage = $this->findPackage($link->getTarget(), '*');
                        $description = $platformPackage ? 'but '.$platformPackage->getPrettyVersion().' is installed' : 'but it is missing';
                        $results[] = [$package, new Link($package->getName(), $link->getTarget(), new MatchAllConstraint, Link::TYPE_REQUIRE, $link->getPrettyConstraint().' '.$description), false];
                        continue;
                    }

                    foreach ($this->getPackages() as $installed) {
                        if (!in_array($link->getTarget(), $installed->getNames())) {
                            continue;
                        }

                        $version = new Constraint('=', $installed->getVersion());
                        if ($link->getTarget() !== $installed->getName()) {
                            foreach (array_merge($installed->getReplaces(), $installed->getProvides()) as $provided) {
                                if ($provided->getTarget() === $link->getTarget()) {
                                    $version = $provided->getConstraint();
                                    break;
                                }
                            }
                        }

                        if (!$link->getConstraint()->matches($version)) {
                            if ($rootPackage) {
                                foreach (array_merge($rootPackage->getRequires(), $rootPackage->getDevRequires()) as $rootRequire) {
                                    if (in_array($rootRequire->getTarget(), $installed->getNames()) && !$rootRequire->getConstraint()->matches($link->getConstraint())) {
                                        $results[] = [$package, $link, false];
                                        $results[] = [$rootPackage, $rootRequire, false];
                                        continue 3;
                                    }
                                }

                                $results[] = [$package, $link, false];
                                $results[] = [$rootPackage, new Link($rootPackage->getName(), $link->getTarget(), new MatchAllConstraint, Link::TYPE_DOES_NOT_REQUIRE, 'but '.$installed->getPrettyVersion().' is installed'), false];
                            } else {
                                $results[] = [$package, $link, false];
                            }
                        }

                        continue 2;
                    }
                }
            }
        }

        ksort($results);

        return $results;
    }

    public function getRepoName(): string
    {
        if (Remote::owned($this)) {
            return Rpc::call('repo.getRepoName', [$this]);
        }

        $names = [];
        foreach ($this->getRepositories() as $repository) {
            $names[] = $repository->getRepoName();
        }

        return 'installed repo ('.implode(', ', $names).')';
    }

    public function addRepository(RepositoryInterface $repository): void
    {
        if (Remote::owned($this)) {
            Rpc::call('repo.addRepository', [$this, $repository]);

            return;
        }

        if (
            $repository instanceof LockArrayRepository
            || $repository instanceof InstalledRepositoryInterface
            || $repository instanceof RootPackageRepository
            || $repository instanceof PlatformRepository
        ) {
            parent::addRepository($repository);

            return;
        }

        throw new \LogicException('An InstalledRepository can not contain a repository of type '.get_class($repository).' ('.$repository->getRepoName().')');
    }
}
