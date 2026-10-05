<?php
// Generates internal/spdx/testdata/oracle/spdx.json: SpdxLicenses::validate
// over thousands of generated license strings and lists, and the
// identifier lookups over case variants, from the real composer/spdx-licenses.
// Run: php tools/oracle/spdx/spdx.php
require dirname(__DIR__, 3).'/.ref/spdx-licenses/src/SpdxLicenses.php';

use Composer\Spdx\SpdxLicenses;

error_reporting(E_ALL & ~E_WARNING);
mt_srand(20261005);

$spdx = new SpdxLicenses();
$licenseIds = array_keys(json_decode(file_get_contents(SpdxLicenses::getResourcesDir().'/'.SpdxLicenses::LICENSES_FILE), true));
$exceptionIds = array_keys(json_decode(file_get_contents(SpdxLicenses::getResourcesDir().'/'.SpdxLicenses::EXCEPTIONS_FILE), true));

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }

function randCase(string $s): string {
    switch (mt_rand(0, 5)) {
        case 0: return strtolower($s);
        case 1: return strtoupper($s);
        case 2:
            $out = '';
            for ($i = 0; $i < strlen($s); $i++) {
                $out .= mt_rand(0, 1) ? strtoupper($s[$i]) : strtolower($s[$i]);
            }
            return $out;
        default: return $s;
    }
}

// Ids sharing prefixes with others are the interesting ones for the regex.
$tricky = ['GPL-2.0', 'GPL-2.0+', 'GPL-2.0-only', 'GPL-2.0-or-later', 'MIT', 'MIT-0', 'MIT-CMU', 'BSD-3-Clause',
    'BSD-3-Clause-Clear', 'Apache-2.0', 'LGPL-2.1+', 'AGPL-3.0', 'CC-BY-4.0', '0BSD', 'Unlicense', 'X11', 'Zlib'];
$wsAll = [' ', ' ', ' ', '  ', "\t", "\n", "\r\n", "\v", "\f", "\r", "\x85", "\xa0", "\0", ''];
$idBytes = ['a', 'Z', '0', '9', '.', '-', '_', ':', '+', "\xe9", "\xaa", "\xb2", "\xbc", "\xd7", "\xf7", "\xc3\xa9", '/', '#'];

function idstring(array $idBytes): string {
    $n = mt_rand(0, 4);
    $s = '';
    for ($i = 0; $i < $n; $i++) {
        $s .= mt_rand(0, 3) ? pick(['a', 'b', 'X', '1', '.', '-']) : pick($idBytes);
    }
    return $s;
}

function ws(array $wsAll): string {
    return mt_rand(0, 4) ? ' ' : pick($wsAll);
}

function simple(): string {
    global $licenseIds, $tricky, $idBytes;
    switch (mt_rand(0, 9)) {
        case 0: case 1: case 2: return randCase(pick($licenseIds));
        case 3: case 4: return randCase(pick($tricky));
        case 5: return randCase(pick($licenseIds)).'+';
        case 6: return randCase('LicenseRef-').idstring($idBytes);
        case 7: return randCase('DocumentRef-').idstring($idBytes).(mt_rand(0, 4) ? ':' : '').randCase('LicenseRef-').idstring($idBytes);
        case 8: return pick(['NONE', 'NOASSERTION', 'none', 'proprietary', 'foo', 'MIT2', 'WITH', 'AND', 'OR', '', 'LicenseRef', 'Licenseref-', 'GPL', '+']);
        default: return randCase(pick($tricky)).pick(['', '+', '++', '-', '.0']);
    }
}

function expr(int $depth): string {
    global $exceptionIds, $wsAll;
    $r = mt_rand(0, 9);
    if ($depth > 3) {
        $r = 0;
    }
    switch ($r) {
        case 0: case 1: case 2:
            return simple();
        case 3:
            return simple().ws($wsAll).randCase(pick(['WITH', 'WITH', 'with', 'WITHOUT', 'W1TH'])).ws($wsAll).(mt_rand(0, 5) ? randCase(pick($exceptionIds)) : simple());
        case 4: case 5: case 6:
            return expr($depth + 1).ws($wsAll).randCase(pick(['AND', 'OR', 'AND', 'OR', 'XOR', 'and', 'or', '&&', ','])).ws($wsAll).expr($depth + 1);
        case 7: case 8:
            $open = mt_rand(0, 9) ? '(' : pick(['', '((', ')']);
            $close = mt_rand(0, 9) ? ')' : pick(['', '))', '(']);
            return $open.(mt_rand(0, 2) ? '' : ws($wsAll)).expr($depth + 1).(mt_rand(0, 2) ? '' : ws($wsAll)).$close;
        default:
            return expr($depth + 1).ws($wsAll).expr($depth + 1);
    }
}

function entry(string $s): array {
    return preg_match('//u', $s) ? ['input' => $s] : ['hex' => bin2hex($s)];
}

$validate = [];
$seen = [];
$add = function ($input) use ($spdx, &$validate, &$seen) {
    $key = is_array($input) ? 'a:'.serialize($input) : 's:'.$input;
    if (isset($seen[$key])) {
        return;
    }
    $seen[$key] = true;
    try {
        $valid = $spdx->validate($input);
        $error = null;
    } catch (\Throwable $e) {
        $valid = false;
        $error = $e->getMessage();
    }
    if (is_array($input)) {
        $e = ['list' => array_map('entry', $input)];
    } else {
        $e = entry($input);
    }
    $e['valid'] = $valid;
    if ($error !== null) {
        $e['error'] = $error;
    }
    $validate[] = $e;
};

// Every identifier, plain, with + and WITH an exception.
foreach ($licenseIds as $id) {
    $add($id);
    $add(strtoupper($id).'+');
    $add($id.' WITH '.pick($exceptionIds));
}
foreach ($exceptionIds as $id) {
    $add($id);
    $add('MIT with '.strtolower($id));
}
// Hand-picked edge cases.
foreach ([
    '', ' ', "\n", 'MIT', 'MIT ', ' MIT', "MIT\n", "MIT\n\n", "MIT\r\n", "MIT\r", "(MIT)\n", "NONE\n", "none", "NoAssertion",
    "MIT\nOR Apache-2.0", "MIT\tAND\tApache-2.0", "MIT\vOR\fBSD-3-Clause", "MIT\x85OR Apache-2.0", "MIT\xa0OR Apache-2.0",
    '( MIT )', '(  MIT  OR  Apache-2.0  )', '((MIT))', '(((MIT)))', '()', '(MIT', 'MIT)', ')(', '(MIT) OR (Apache-2.0)',
    '(MIT)OR(Apache-2.0)', 'MIT OR(Apache-2.0)', '(MIT) WITH Classpath-exception-2.0', 'MIT WITH Classpath-exception-2.0 WITH Classpath-exception-2.0',
    'MIT+ WITH Classpath-exception-2.0', 'GPL-2.0+', 'GPL-2.0++', 'GPL-2.0-only+', 'LicenseRef-', 'LicenseRef-a', 'LicenseRef-a+',
    'LicenseRef-a.b-c', "LicenseRef-\xe9", "LicenseRef-\xc3\xa9", "LicenseRef-\xd7", "LicenseRef-\xb2", "LicenseRef-\xbc", "LicenseRef-\xaa",
    'licenseref-x', 'LICENSEREF-X', 'DocumentRef-a:LicenseRef-b', 'DocumentRef-:LicenseRef-b', 'DocumentRef-a:LicenseRef-', 'DocumentRef-a:b',
    'DocumentRef-a.b:LicenseRef-c WITH Classpath-exception-2.0', 'documentref-A:licenseref-B', 'LicenseRef-a:LicenseRef-b',
    'LicenseRef-a OR DocumentRef-x:LicenseRef-y AND MIT', 'MIT AND (Apache-2.0 OR (BSD-3-Clause AND (GPL-2.0-only WITH Classpath-exception-2.0)))',
    'MIT OR NONE', 'NONE OR MIT', 'NONE+', 'NOASSERTION WITH Classpath-exception-2.0', 'MIT WITH', 'WITH MIT', 'MIT AND', 'AND MIT',
    'MIT  AND  Apache-2.0', 'MITAND Apache-2.0', 'MIT ANDApache-2.0', 'MIT OR', 'mit or apache-2.0', 'Mit Or Apache-2.0', "MIT\0",
    "\0MIT", 'K', "MIT\xe2\x80\xa8", 'proprietary', 'MIT OR proprietary', str_repeat('(', 20).'MIT'.str_repeat(')', 20),
    implode(' OR ', array_slice($licenseIds, 0, 60)), '('.implode(' AND ', array_slice($licenseIds, 100, 40)).')',
] as $s) {
    $add($s);
}
// Lists.
foreach ([[], ['MIT'], ['MIT', 'Apache-2.0'], ['MIT', 'foo'], ['(MIT'], ['MIT)', 'x'], ['MIT', ''], [''], ['NONE', 'MIT'],
    ['LGPL-2.0-only', 'GPL-3.0-or-later'], ['MIT AND Apache-2.0', 'BSD-3-Clause'], ["MIT\n", 'X11']] as $l) {
    $add($l);
}
for ($i = 0; $i < 300; $i++) {
    $n = mt_rand(0, 4);
    $l = [];
    for ($j = 0; $j < $n; $j++) {
        $l[] = expr(2);
    }
    $add($l);
}
// Random expressions.
for ($i = 0; count($validate) < 12000 && $i < 100000; $i++) {
    $s = expr(0);
    switch (mt_rand(0, 19)) {
        case 0: $s = ' '.$s; break;
        case 1: $s .= ' '; break;
        case 2: $s .= "\n"; break;
        case 3: $s .= "\n\n"; break;
        case 4: $s = pick(['NONE', 'NOASSERTION']).pick(['', ' ', "\n"]).(mt_rand(0, 1) ? $s : ''); break;
    }
    $add($s);
}

// Lookups.
$lookups = [];
$probe = [];
foreach ($licenseIds as $id) {
    $probe[] = $id;
    $probe[] = randCase($id);
}
foreach ($exceptionIds as $id) {
    $probe[] = $id;
    $probe[] = randCase($id);
}
array_push($probe, '', 'mit ', ' MIT', 'MIT+', 'AGPL-1.0-Illegal', 'Font-exception-2.0-Errorl', 'gpl-3.0', "MIT\n", 'NONE');
foreach (array_values(array_unique($probe)) as $id) {
    $l = $spdx->getLicenseByIdentifier($id);
    $e = $spdx->getExceptionByIdentifier($id);
    $row = ['id' => $id, 'license' => $l, 'exception' => $e];
    if ($l !== null) {
        $row['osi'] = $spdx->isOsiApprovedByIdentifier($id);
        $row['deprecated'] = $spdx->isDeprecatedByIdentifier($id);
    }
    $lookups[] = $row;
}

$names = [];
$nameProbe = [];
foreach ($spdx->getLicenses() as $l) {
    $nameProbe[] = $l[1];
}
foreach (json_decode(file_get_contents(SpdxLicenses::getResourcesDir().'/'.SpdxLicenses::EXCEPTIONS_FILE), true) as $e) {
    $nameProbe[] = $e[0];
}
array_push($nameProbe, '', 'mit license', 'MIT License ', 'null-identifier-name');
foreach (array_values(array_unique($nameProbe)) as $name) {
    $names[] = ['name' => $name, 'id' => $spdx->getIdentifierByName($name)];
}

$licenses = [];
foreach ($spdx->getLicenses() as $key => $l) {
    $licenses[] = [(string) $key, $l[0], $l[1], $l[2], $l[3]];
}

$out = ['validate' => $validate, 'lookup' => $lookups, 'byName' => $names, 'licenses' => $licenses];
file_put_contents(
    dirname(__DIR__, 3).'/internal/spdx/testdata/oracle/spdx.json',
    json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR)."\n"
);
fprintf(STDERR, "validate: %d, lookup: %d, byName: %d\n", count($validate), count($lookups), count($names));
