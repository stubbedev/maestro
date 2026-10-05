<?php

/*
 * maestro's plugin shim: Composer\Package\Version\VersionParser,
 * reimplemented with Composer 2.10.3's behaviour (docs/PLUGINS.md §4.5) on the
 * vendored composer/semver parser.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Package\Version;

class VersionParser extends \Composer\Semver\VersionParser
{
    private static $constraints = [];

    public const DEFAULT_BRANCH_ALIAS = '9999999-dev';

    public static function isUpgrade(string $normalizedFrom, string $normalizedTo): bool
    {
        if ($normalizedFrom === $normalizedTo) {
            return true;
        }

        if (in_array($normalizedFrom, ['dev-master', 'dev-trunk', 'dev-default'], true)) {
            $normalizedFrom = VersionParser::DEFAULT_BRANCH_ALIAS;
        }
        if (in_array($normalizedTo, ['dev-master', 'dev-trunk', 'dev-default'], true)) {
            $normalizedTo = VersionParser::DEFAULT_BRANCH_ALIAS;
        }

        if (strpos($normalizedFrom, 'dev-') === 0 || strpos($normalizedTo, 'dev-') === 0) {
            return true;
        }

        $sorted = \Composer\Semver\Semver::sort([$normalizedTo, $normalizedFrom]);

        return $sorted[0] === $normalizedFrom;
    }

    public function parseConstraints($constraints): \Composer\Semver\Constraint\ConstraintInterface
    {
        if (!isset(self::$constraints[$constraints])) {
            self::$constraints[$constraints] = parent::parseConstraints($constraints);
        }

        return self::$constraints[$constraints];
    }

    public function parseNameVersionPairs(array $pairs): array
    {
        $pairs = array_values($pairs);
        $result = [];

        for ($i = 0, $count = count($pairs); $i < $count; $i++) {
            $pair = \Composer\Pcre\Preg::replace('{^([^=: ]+)[=: ](.*)$}', '$1 $2', trim($pairs[$i]));
            if (false === strpos($pair, ' ') && isset($pairs[$i + 1]) && false === strpos($pairs[$i + 1], '/') && !\Composer\Pcre\Preg::isMatch('{(?<=[a-z0-9_/-])\*|\*(?=[a-z0-9_/-])}i', $pairs[$i + 1]) && !\Composer\Repository\PlatformRepository::isPlatformPackage($pairs[$i + 1])) {
                $pair .= ' '.$pairs[$i + 1];
                $i++;
            }

            if (strpos($pair, ' ')) {
                [$name, $version] = explode(' ', $pair, 2);
                $result[] = ['name' => $name, 'version' => $version];
            } else {
                $result[] = ['name' => $pair];
            }
        }

        return $result;
    }
}
