<?php
// Generates the differential goldens in internal/semver/testdata/oracle/
// by running the real composer/semver 3.4.4 (from .ref/composer/vendor)
// over a large generated corpus:
//
//   versions.json     VersionParser: normalize, normalizeBranch,
//                     parseNumericAliasPrefix, parseStability,
//                     normalizeStability, normalizeDefaultBranch
//   fullversions.json normalize($version, $fullVersion)
//   constraints.json  parseConstraints: string and pretty forms, bounds,
//                     Intervals::get and compactConstraint, a matches()
//                     matrix and CompilingMatcher::match over many
//                     operator/version pairs, Semver::satisfies
//   pairs.json        Intervals::isSubsetOf / haveIntersections and
//                     matches() between parsed constraints
//   versioncompare.json  version_compare() over every pair of hand-picked
//                     forms
//   compare.json      version_compare() with and without operators,
//                     Comparator::compare, Constraint::versionCompare
//   sort.json         Semver::sort / rsort / satisfiedBy
//   looseequals.json  PHP semantics semver relies on: $a == $b on strings
//   increment.json    and $numericString + $int, as a string
//
// versions, constraints and compare are over 1 MB and so written gzipped
// (<name>.json.gz, docs/PORTING.md).
//
// Exceptions are recorded as {"e": [class, message]}. Every string in the
// corpus is valid UTF-8 so that it survives JSON.
//
// Run: php tools/oracle/semver/oracle.php
$root = dirname(__DIR__, 3);
require $root.'/.ref/composer/vendor/autoload.php';

use Composer\Semver\Comparator;
use Composer\Semver\CompilingMatcher;
use Composer\Semver\Constraint\Constraint;
use Composer\Semver\Constraint\ConstraintInterface;
use Composer\Semver\Intervals;
use Composer\Semver\Semver;
use Composer\Semver\VersionParser;

mt_srand(20261005);

$warnings = [];
set_error_handler(function ($no, $str) use (&$warnings) {
    $warnings[$str] = ($warnings[$str] ?? 0) + 1;

    return true;
});

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }

function chance(int $percent): bool { return mt_rand(1, 100) <= $percent; }

/** Runs $f, returning its result or the exception it throws. */
function attempt(callable $f)
{
    try {
        return $f();
    } catch (\Throwable $e) {
        return ['e' => [get_class($e), $e->getMessage()]];
    }
}

// ---------------------------------------------------------------------------
// Corpus

$numbers = ['0', '1', '2', '3', '00', '01', '007', '10', '11', '99', '100', '2010', '9999', '12345', '99999', '100000',
    '123456', '201903', '20100102', '9999999', '2023013100000', '9223372036854775806', '9223372036854775807',
    '9223372036854775808', '99999999999999999999', '18446744073709551616'];
$small = ['0', '1', '2', '3', '5', '10', '00', '01'];
$seps = ['.', '.', '.', '.', '-', '_', ':', '', '+', '..', ' '];
$modifiers = ['', '', '', '-dev', '.dev', 'dev', '-DEV', '_dev', '-beta', 'beta1', '-b2', '-B', '-RC1', 'rc.2', 'RC',
    '-alpha.3.1', '-a', 'a1', '-patch5', '-pl3', 'p1', '-stable', '-STABLE', 'stable', '-BETA', '-foo', '-SNAPSHOT',
    '-beta.2-dev', '-rc1-dev', 'b.5', '-alpha-2.1-3', '-p', '-beta-dev', '-dev-dev', '-0', '-1', '.x-dev', '.X-dev',
    '.*-dev', '.x', '.*', '.X', '.x.x', '.*.*', '-x-dev', '.x-DEV', '-x', 'x-dev'];
$suffixes = ['', '', '', '', '+build', '+build.1-2', '+foo bar', '+', '#ref', '#trunk/@123', '#', '@dev', '@stable',
    '@RC', '@beta', '@DEV', '@foo', ' as 1.0', ' as  1.0.0', 'as 1.0', ' as dev-foo', '@dev as 1.0.x-dev', ' as ^2.0',
    ' as 1.0 ', "\n", "\r", "\0", "\u{a0}", ' ', "\t", "\v", "\f", ',', '|', '-', '.', '/'];
$branchNames = ['master', 'trunk', 'default', 'dev-master', 'DEV-foo', 'dev-feature/x', 'dev-1.0.0-dev<1.0.5-dev',
    'feature-dev', 'foo bar-dev', 'xsd2php-dev', '3.next-dev', 'dev-foo bar', 'dev-', 'dev', 'Dev-x', 'feature',
    'foobar', 'dev-041.003', 'v', 'x', 'X', '*', 'v*', 'x.X.x.*', 'release-1.0', '1.0-beta-dev', 'a.b.c-dev',
    "foo\n1.0-dev", "1.0\n-dev", "x\n-dev", "dev-foo\n", "1.0.x-dev\n", "\u{85}1.0", "1.0\u{a0}"];

function version(): string
{
    global $numbers, $small, $seps, $modifiers, $suffixes, $branchNames;
    if (chance(12)) {
        return pick($branchNames).(chance(30) ? pick($suffixes) : '');
    }
    $s = chance(15) ? pick(['v', 'V', ' ', "\t", 'vv', '=']) : '';
    $s .= chance(25) ? pick($numbers) : pick($small);
    $n = mt_rand(0, 5);
    for ($i = 0; $i < $n; $i++) {
        $s .= (chance(85) ? '.' : pick($seps)).(chance(10) ? pick($numbers) : (chance(8) ? pick(['x', 'X', '*']) : pick($small)));
    }
    $s .= pick($modifiers);
    if (chance(30)) {
        $s .= pick($suffixes);
    }

    return $s;
}

function dateVersion(): string
{
    $s = pick(['2010', '2023', '1999', '0000', 'v2010']);
    $n = mt_rand(0, 8);
    for ($i = 0; $i < $n; $i++) {
        $s .= pick(['', '.', '-', ':', '_']).pick(['01', '1', '02', '123', '1234', '20', '5', '30', '0']);
    }

    return $s.pick(['', '', '-dev', '-p1', '-beta2', '.x-dev', '-rc', 'dev', '.0', '-stable']);
}

function cleanVersion(): string
{
    $s = chance(10) ? 'v' : '';
    $s .= pick(['0', '1', '1', '2', '3', '10', '00']);
    $n = mt_rand(0, 3);
    for ($i = 0; $i < $n; $i++) {
        $s .= '.'.pick(['0', '0', '1', '2', '5', '10', '01']);
    }

    return $s.pick(['', '', '', '', '-dev', '-beta', '-beta2', '-RC1', 'rc1', '-alpha.1', '-patch1', 'p2', '-stable',
        '.x-dev', '.*', '.x', '@dev', '@beta', '@stable', '+meta', '-b1-dev']);
}

$operators = ['', '', '=', '==', '<', '<=', '>', '>=', '!=', '<>', '~', '~>', '^', '=>', '=<', '!', '>>', '==='];
$spaces = ['', '', '', '', '', '', '', '', ' ', ' ', '  ', "\t", "\n"];

function single(): string
{
    global $operators, $spaces, $branchNames;
    switch (mt_rand(0, 9)) {
        case 0:
            return pick(['*', 'x', 'X', 'v*', '*.*', 'x.X.x.*', '*-dev', '@dev', '@stable', '', 'v', '^', '~', 'dev-*']);
        case 1:
            $v = mt_rand(0, 3).(chance(70) ? '.'.mt_rand(0, 3) : '').(chance(40) ? '.'.mt_rand(0, 3) : '');

            return pick(['', 'v', '^', '~', '>=', '<']).$v.pick(['.*', '.x', '.X', '.*.*', '.x.x.x', '-dev', '.x-dev', '.*-dev']);
        case 2:
            return cleanVersion().pick([' - ', ' - ', ' - ', ' - ', '  -  ', '-', ' -', "\t-\t"]).(chance(80) ? cleanVersion() : version());
        case 3:
            return pick(['1', '1.2', '1.2.3', 'v2', '0.1', '2.x-dev']).pick([' - ', ' - ', ' - ', ' -  ', '  - ']).pick(['2', '2.0', '2.0.1', '3.0-dev', '3.x-dev', '2.0.1.2', '0', '0.0', '2.*']);
        case 4:
            return pick(['dev-master', 'dev-foo', '1.0.x-dev', '2.x-dev', 'dev-foo#abc', '1.0.x-dev#trunk/@1', 'foo-dev', '1.0#abc', 'dev-foo@dev', 'master']);
        default:
            $v = chance(70) ? cleanVersion() : (chance(15) ? dateVersion() : version());

            return pick($operators).pick($spaces).$v;
    }
}

function constraintString(): string
{
    $s = single();
    $n = chance(45) ? mt_rand(1, 2) : 0;
    for ($i = 0; $i < $n; $i++) {
        $s .= (chance(85) ? pick([',', ', ', ' , ', ' ', '  ', ' ,', ' || ', '||', ' | ', '|', ' || ', ' ||  ']) : pick([' ||| ', ',,', ' as ', "\n|| ", ' - ', "\t", ' ,, ', ', -'])).single();
    }
    if (chance(5)) {
        $s = pick([' ', "\t", '||', ',', '|']).$s;
    }
    if (chance(5)) {
        $s .= pick([' ', "\n", '||', ',', ' as 1.0']);
    }

    return $s;
}

// Versions and operators the parsed constraints are matched against.
$probeVersions = ['0.0.0.0-dev', '0.0.0.0', '0.1.0.0', '0.9.9.9', '1.0.0.0-dev', '1.0.0.0-alpha1', '1.0.0.0-beta2',
    '1.0.0.0-RC1', '1.0.0.0', '1.0.0.0-patch1', '1.0.1.0', '1.2.0.0', '1.2.3.0', '1.9999999.9999999.9999999-dev', '2.0.0.0-dev',
    '2.0.0.0', '2.1.0.0', '3.0.0.0', '3.5.0.0', '10.0.0.0', '20100102', '9999999-dev', 'dev-master', 'dev-foo',
    'dev-bar', '1.0', '2', '1.0.0-beta'];
$probeOps = [Constraint::OP_EQ, Constraint::OP_NE, Constraint::OP_LT, Constraint::OP_LE, Constraint::OP_GT, Constraint::OP_GE];
$opStrings = ['==', '<', '<=', '>', '>=', '!='];

function intervalsData(array $set): array
{
    $numeric = [];
    foreach ($set['numeric'] as $k => $interval) {
        $numeric[] = [$k, (string) $interval->getStart(), (string) $interval->getEnd()];
    }
    $names = [];
    foreach ($set['branches']['names'] as $k => $name) {
        $names[] = [$k, $name];
    }

    return [$numeric, $names, $set['branches']['exclude']];
}

function constraintData(ConstraintInterface $c): array
{
    global $probeVersions, $probeOps, $opStrings;
    $matches = '';
    $compiled = '';
    foreach ($probeVersions as $v) {
        foreach ($probeOps as $op) {
            $matches .= $c->matches(new Constraint($opStrings[$op], $v)) ? '1' : '0';
            $compiled .= CompilingMatcher::match($c, $op, $v) ? '1' : '0';
        }
    }
    $compact = Intervals::compactConstraint($c);

    return [
        's' => (string) $c,
        'p' => $c->getPrettyString(),
        'lb' => (string) $c->getLowerBound(),
        'ub' => (string) $c->getUpperBound(),
        'm' => $matches,
        'cm' => $compiled,
        'iv' => intervalsData(Intervals::get($c)),
        'cc' => (string) $compact,
        'ccp' => $compact->getPrettyString(),
    ];
}

$parser = new VersionParser();

// ---------------------------------------------------------------------------
// versions.json

$versionInputs = ['', ' ', 'a', '1.0.0-meh', '1.0.0.0.0', ' as ', ' as 1.2', '^', '~1', '1.*', '0', '00', '99999',
    '100000', '20100102.0.3.4', '2023013.0.0', '202301311.0.0', '1.0.0RC1dev', '1.0.0.RC.15-dev', 'dev-master as 1.0.0'];
for ($i = 0; $i < 6000; $i++) {
    $versionInputs[] = chance(15) ? dateVersion() : version();
}
$versionInputs = array_values(array_unique($versionInputs));

$fullVersions = [];
foreach ($versionInputs as $v) {
    if (chance(20)) {
        $fullVersions[] = [$v, $v.pick([' as ', '  as  ', ' as', '@dev as ', '@RC  as ', '@foo as ', 'as ']).version()];
        $fullVersions[] = [$v, version().pick([' as ', '  as ', ' as  ', '@dev as ']).$v.pick(['', '@dev', '@stable', '@RC', '@foo', ' ', "\n"])];
    }
}
foreach (['^2.0', '~2.0', '>2.0', '<2.0', '1.0.0+foo', '<2.0@dev', '', 'foo'] as $v) {
    $fullVersions[] = [$v, $v.' as 1.0'];
    $fullVersions[] = [$v, '1.0 as '.$v];
    $fullVersions[] = [$v, '1.0 as  '.$v.'@dev'];
}

$versionsOut = [];
foreach ($versionInputs as $v) {
    $prefix = $parser->parseNumericAliasPrefix($v);
    $versionsOut[] = [
        'v' => $v,
        'n' => attempt(function () use ($parser, $v) { return $parser->normalize($v); }),
        'b' => $parser->normalizeBranch($v),
        'a' => $prefix === false ? null : $prefix,
        's' => VersionParser::parseStability($v),
        'ns' => attempt(function () use ($v) { return VersionParser::normalizeStability($v); }),
        'd' => $parser->normalizeDefaultBranch($v),
    ];
}
$fullOut = [];
foreach ($fullVersions as [$v, $full]) {
    $fullOut[] = [$v, $full, attempt(function () use ($parser, $v, $full) { return $parser->normalize($v, $full); })];
}

// ---------------------------------------------------------------------------
// constraints.json

$constraintInputs = ['', '*', '1.0@dev', '>=1.0@beta', '1.0.x-dev#abcd123', '~>1.2', '^1.', '~1.', '1.2.', '1.2..dev',
    '~2.5.9|~2.6,>=2.6.2', '>2.0,,<=3.0', '>2.0 ||| <=3.0', ',^1@dev', '^1@dev ||', '^1.*-beta-dev', '*-dev',
    '^1.0 || ^2.0 !=2.0.1 || ^3.0 || ^4.0', '>2.0@stable || 0@dev', '1.0 - 2.0.x-dev', '00.x', '^00.1', '~0.x-dev',
    '1.x - 2.*', 'dev-foo as 1.0 || 2.0 as 1.5', '^99999999999999999999', '~9223372036854775807.1',
    '9223372036854775807.*', '^0.9223372036854775807', '1.0 - 99999999999999999999'];
for ($i = 0; $i < 6000; $i++) {
    $constraintInputs[] = constraintString();
}
$constraintInputs = array_values(array_unique($constraintInputs));

$constraintsOut = [];
$parsed = [];
foreach ($constraintInputs as $s) {
    $c = attempt(function () use ($parser, $s) { return $parser->parseConstraints($s); });
    if (is_array($c)) {
        $constraintsOut[] = ['c' => $s, 'r' => $c];
        continue;
    }
    $parsed[$s] = $c;
    $row = ['c' => $s, 'r' => constraintData($c)];
    $sat = [];
    foreach (['1.0.0', '2.0', 'dev-master', 'v1.2.3-beta', 'foo', '1.0.x-dev'] as $v) {
        $sat[] = attempt(function () use ($v, $s) { return Semver::satisfies($v, $s); });
    }
    $row['sat'] = $sat;
    $constraintsOut[] = $row;
}

// ---------------------------------------------------------------------------
// pairs.json

$valid = array_map('strval', array_keys($parsed));
$pairsOut = [];
for ($i = 0; $i < 6000; $i++) {
    $a = pick($valid);
    $b = pick($valid);
    $ca = $parsed[$a];
    $cb = $parsed[$b];
    $pairsOut[] = [$a, $b, Intervals::isSubsetOf($ca, $cb), Intervals::haveIntersections($ca, $cb), $ca->matches($cb)];
}

// ---------------------------------------------------------------------------
// compare.json

$rawVersions = ['', '0', '1', '1.0', '1.0.0', '1.0-', '1.0.', '1.0..', '-1', '1.0#', '#', '#1', '1-dev', '1.0-dev',
    '1.0dev', '1.0a', '1.0alpha', '1.0b', '1.0beta', '1.0RC', '1.0rc', '1.0pl', '1.0p', '1.0#1', '1.0-patch', '1.0+',
    '1.0_1', '1.0+1', '1.0 1', 'dev-master', 'dev-foo', '1.0.0.0-dev', '1.0.0.0', '1.0.0.0-beta2', '1.0.0.0-patch1',
    '9223372036854775807', '9223372036854775808', '99999999999999999999.1', '1.0.0.0-RC1', 'a', 'b', 'abc', 'dev',
    'alpha', 'beta', 'RC', 'rc', 'pl', 'p', 'x', '1.x', '1.*', ".1", '1..1', '1-1', '1_1', '1:1', "1\0" . '5', "1.0\0",
    "\u{e9}", '1.0é', '01', '1.01', '1.10', '1.9', '2010.01.02', '1.0.0-alpha1', '1.0.0-beta1', '1.0.0-RC2', 'v1.0', '.', '..', 'A', 'Z', '#.#', '.#', '#.', '1.#.2',
    'p.1', '1.p', '-', '_', '+', '1+', 'dev.dev', 'a.1', '1.a', '1..', '..1', '1.0.0.', 'é.1', "\n", ' 1', '1 '];
// Every pair of the hand-picked forms, before random ones join them.
$vcPairsOut = [];
foreach ($rawVersions as $a) {
    foreach ($rawVersions as $b) {
        $vcPairsOut[] = [$a, $b, version_compare($a, $b)];
    }
}
for ($i = 0; $i < 300; $i++) {
    $rawVersions[] = version();
}
$rawVersions = array_values(array_unique($rawVersions));
$vcOps = ['<', 'lt', '<=', 'le', '>', 'gt', '>=', 'ge', '==', '=', 'eq', '!=', '<>', 'ne', '', 'LT', '=>', '==='];
$compareOut = [];
for ($i = 0; $i < 6000; $i++) {
    $a = pick($rawVersions);
    $b = chance(10) ? $a : pick($rawVersions);
    $op = pick($vcOps);
    $constraint = new Constraint('==', '1');
    $compareOut[] = [
        $a, $b, $op,
        version_compare($a, $b),
        attempt(function () use ($a, $b, $op) { return version_compare($a, $b, $op); }),
        attempt(function () use ($a, $b, $op) { return Comparator::compare($a, $op, $b); }),
        attempt(function () use ($constraint, $a, $b, $op) { return $constraint->versionCompare($a, $b, $op); }),
        attempt(function () use ($constraint, $a, $b, $op) { return $constraint->versionCompare($a, $b, $op, true); }),
    ];
}

// ---------------------------------------------------------------------------
// sort.json

$sortable = [];
foreach ($versionInputs as $v) {
    if (!is_array(attempt(function () use ($parser, $v) { return $parser->normalize($v); }))) {
        $sortable[] = $v;
    }
}
$devHeavy = ['dev-foo', 'dev-master', 'dev-bar', 'master', 'dev-trunk', '1.0', '1.0.0', '2.0-dev', '1.x-dev', '1.0-beta',
    '1.0-alpha', 'dev-default', '9999999-dev', '0.1', '1.0.0.0'];
$sortOut = [];
for ($i = 0; $i < 400; $i++) {
    $n = pick([0, 1, 2, 3, 5, 6, 10, 16, 17, 20, 40, 100, 1100]);
    if ($n > 100 && chance(80)) {
        $n = 30;
    }
    $list = [];
    $pool = chance(40) ? $devHeavy : $sortable;
    for ($j = 0; $j < $n; $j++) {
        $list[] = pick($pool);
    }
    $constraint = pick($valid);
    $sortOut[] = [
        $list,
        Semver::sort($list),
        Semver::rsort($list),
        $constraint,
        attempt(function () use ($list, $constraint) { return Semver::satisfiedBy($list, $constraint); }),
    ];
}

// ---------------------------------------------------------------------------
// php.json

$numericStrings = ['0', '1', '-1', '00', '1.0', '1.00', '1e3', '1000', '1E3', ' 1', '1 ', ' 1 ', "\t1\n", '1x', '.5',
    '0.5', '5.', '+1', '-0', '0.0', '1e', '1e+', '1e-2', '0.01', '9223372036854775807', '9223372036854775808',
    '-9223372036854775808', '-9223372036854775809', '99999999999999999999', '100000000000000000000', '1e999', '-1e999',
    "1\0", '201001.02', '201001.020', 'abc', '', ' ', '0x1A', '1_000', '١', '1.5e3', '1500', '00000000000000000000001',
    '9223372036854775807.0', '9.2233720368547758E+18'];
$looseOut = [];
foreach ($numericStrings as $a) {
    foreach ($numericStrings as $b) {
        $looseOut[] = [$a, $b, $a == $b];
    }
}
$addOut = [];
foreach (['0', '1', '9', '00', '007', '99', '9223372036854775806', '9223372036854775807', '9223372036854775808',
    '99999999999999999999', '18446744073709551615', '123456789012345678901234567890', '-1', '-2', '-3', '-9', '10',
    '99999999999999', '999999999999999', '9999999999999999', '1e3', '0.5', '-0.5', '1.5', '-9223372036854775808',
    '-9223372036854775809', '12345678901234567', '4503599627370497'] as $s) {
    foreach ([-2, -1, 1, 2] as $inc) {
        $r = $s + $inc;
        $addOut[] = [$s, $inc, (string) $r, $r < 0];
    }
}

// ---------------------------------------------------------------------------

$dir = $root.'/internal/semver/testdata/oracle';
if (!is_dir($dir)) {
    mkdir($dir, 0777, true);
}

/** Writes one JSON value per line of a top-level array, to keep diffs readable. */
function write(string $file, array $rows)
{
    $lines = [];
    foreach ($rows as $row) {
        $lines[] = json_encode($row, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR);
    }
    $json = "[\n".implode(",\n", $lines)."\n]\n";
    // Goldens over 1 MB are committed gzipped (docs/PORTING.md).
    file_put_contents($file, substr($file, -3) === '.gz' ? gzencode($json, 9) : $json);
}

write($dir.'/versions.json.gz', $versionsOut);
write($dir.'/fullversions.json', $fullOut);
write($dir.'/constraints.json.gz', $constraintsOut);
write($dir.'/pairs.json', $pairsOut);
write($dir.'/compare.json.gz', $compareOut);
write($dir.'/versioncompare.json', $vcPairsOut);
write($dir.'/sort.json', $sortOut);
write($dir.'/looseequals.json', $looseOut);
write($dir.'/increment.json', $addOut);

fprintf(STDERR, "versions: %d (+%d full), constraints: %d (%d valid), pairs: %d, compare: %d, sort: %d, php: %d+%d\n",
    count($versionsOut), count($fullOut), count($constraintsOut), count($parsed), count($pairsOut), count($compareOut),
    count($sortOut), count($looseOut), count($addOut));
foreach ($warnings as $w => $n) {
    fprintf(STDERR, "warning x%d: %s\n", $n, $w);
}
