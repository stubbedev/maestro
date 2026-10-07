<?php

/*
 * Machine-specific strings out of a PCRE capture (preg_collect.php): the
 * paths and names a capture logs on the developer's machine are replaced
 * with neutral placeholders of the same kind, so the committed corpus names
 * no one's machine. internal/php's TestPregCorpusNamesNoMachine checks the
 * result.
 */

declare(strict_types=1);

/** The placeholders the machine's strings become. */
const ANON_WORKDIR = '/tmp/preg-capture-1000';
const ANON_ROOT = '/home/user/src/project';
const ANON_HOME = '/home/user';
const ANON_USER = 'user';

/**
 * The replacements for a capture made in $workdir (null: none) from the
 * repository at $root, with home directory $home, for anonymize(). Each
 * path is replaced as it is and in its preg_quote()d forms, as patterns
 * built from paths hold those. strtr() tries the longest key first, so the
 * repository inside the home directory becomes ANON_ROOT, not ANON_HOME
 * plus the rest.
 *
 * @return array<string, string>
 */
function anonymizer(?string $workdir, string $root, string $home): array
{
    $paths = [$root => ANON_ROOT, $home => ANON_HOME];
    if ($workdir !== null) {
        $paths[$workdir] = ANON_WORKDIR;
    }

    $map = [];
    foreach ($paths as $from => $to) {
        $from = (string) $from;
        if ($from === '' || $from === '/') {
            continue;
        }
        $map[$from] = $to;
        foreach ([null, '/', '#', '{'] as $delimiter) {
            $map[preg_quote($from, $delimiter)] = preg_quote($to, $delimiter);
        }
    }

    return $map;
}

/**
 * $s with the machine's strings replaced: the paths in $map, a string that
 * is only the user name, and Nix store hashes (zeroed, keeping the
 * package's name).
 *
 * @param array<string, string> $map
 */
function anonymize(string $s, array $map, string $user): string
{
    if ($user !== '' && $s === $user) {
        return ANON_USER;
    }
    $s = strtr($s, $map);

    return (string) preg_replace('{/nix/store/[0-9a-z]{32}-}', '/nix/store/'.str_repeat('0', 32).'-', $s);
}
