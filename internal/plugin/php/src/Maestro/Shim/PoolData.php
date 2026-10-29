<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §4.12, §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

use Composer\Advisory\PartialSecurityAdvisory;
use Composer\Advisory\SecurityAdvisory;
use Composer\DependencyResolver\PolicyRemovalReason;
use Composer\FilterList\FilterListEntry;

/**
 * What a pool's builder and its filters removed crosses as values
 * (docs/PLUGINS.md §4.12): RepositorySet::createPool*() rebuilds Composer's
 * advisory, filter list entry and policy removal reason objects from them.
 * None of them has a usable constructor for data maestro holds, so they
 * are built without one and their fields set.
 */
final class PoolData
{
    /**
     * Composer's (Partial)SecurityAdvisory of maestro's advisory.
     *
     * @param array<string, mixed> $a
     * @return PartialSecurityAdvisory|SecurityAdvisory
     */
    public static function advisory(array $a)
    {
        $class = isset($a['title']) ? SecurityAdvisory::class : PartialSecurityAdvisory::class;
        $advisory = (new \ReflectionClass($class))->newInstanceWithoutConstructor();
        $advisory->advisoryId = $a['advisoryId'];
        $advisory->packageName = $a['packageName'];
        $advisory->affectedVersions = $a['affectedVersions'];
        if (isset($a['title'])) {
            $advisory->title = $a['title'];
            $advisory->sources = $a['sources'];
            $advisory->reportedAt = new \DateTimeImmutable($a['reportedAt']);
            $advisory->cve = $a['cve'];
            $advisory->link = $a['link'];
            $advisory->severity = $a['severity'];
        }

        return $advisory;
    }

    /**
     * Composer's FilterListEntry of maestro's entry (its public
     * properties, as its constructor sets them).
     *
     * @param array<string, mixed> $e
     */
    public static function filterListEntry(array $e): FilterListEntry
    {
        $entry = (new \ReflectionClass(FilterListEntry::class))->newInstanceWithoutConstructor();
        foreach (['packageName', 'listName', 'constraint', 'url', 'reason', 'id', 'source'] as $name) {
            $entry->$name = $e[$name];
        }

        return $entry;
    }

    /**
     * Composer's PolicyRemovalReason of maestro's reason: its type, the
     * package and version it explains, and the advisories, filter list
     * entries or cooldown that caused the removal.
     *
     * @param array{type: string, packageName: string, prettyVersion: string, advisories: array, filterListEntries: array, cooldown: array{releaseDate: string, availableIn: string, source: string}|null} $fields
     */
    public static function policyRemovalReason(array $fields): PolicyRemovalReason
    {
        $advisories = [];
        foreach ($fields['advisories'] as $a) {
            $advisories[] = self::advisory($a);
        }
        $filterListEntries = [];
        foreach ($fields['filterListEntries'] as $e) {
            $filterListEntries[] = self::filterListEntry($e);
        }

        /** @var PolicyRemovalReason $reason */
        $reason = (new \ReflectionClass(PolicyRemovalReason::class))->newInstanceWithoutConstructor();
        \Closure::bind(static function (PolicyRemovalReason $reason) use ($fields, $advisories, $filterListEntries): void {
            $reason->type = $fields['type'];
            $reason->packageName = $fields['packageName'];
            $reason->prettyVersion = $fields['prettyVersion'];
            $reason->advisories = $advisories;
            $reason->filterListEntries = $filterListEntries;
            $reason->cooldown = $fields['cooldown'];
        }, null, PolicyRemovalReason::class)($reason);

        return $reason;
    }
}
