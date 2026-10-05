<?php

/*
 * Builds internal/php/testdata/preg/corpus.json: every regex pattern Composer
 * 2.10.3 (src/, vendor/composer/*, vendor/seld/*) uses, each with a set of
 * relevant subjects. Feed the result to preg_golden.php.
 *
 * Pattern sources:
 *   1. static: token_get_all() over the reference sources, evaluating the
 *      first argument of every preg_*() / Preg::*() / Regex::*() call when it
 *      is a constant expression (literals, class constants, static props);
 *   2. dynamic: optional logs of real calls (pattern, subject, call site)
 *      captured while running Composer's own PHPUnit suites with logging
 *      wrappers around preg_* (see preg_capture.sh);
 *   3. manual: preg_manual.php, for patterns built at runtime and for
 *      hand-written tricky subjects.
 *
 * Usage (from the repo root, inside `nix develop`):
 *   php tools/oracle/php/preg_collect.php [capture-log ...]
 *
 * Without capture logs the dynamic subjects already present in the committed
 * corpus.json are kept, so the corpus can be regenerated deterministically.
 */

declare(strict_types=1);

error_reporting(E_ALL);
ini_set('memory_limit', '-1');

$root = dirname(__DIR__, 3);
$ref = $root.'/.ref/composer';
$out = $root.'/internal/php/testdata/preg/corpus.json';

require $ref.'/vendor/autoload.php';

const MAX_SUBJECTS = 16;
// patterns only seen at runtime (built from data) get fewer subjects, and at
// most MAX_VARIANTS distinct patterns are kept per call site
const MAX_DYNAMIC_SUBJECTS = 8;
const MAX_VARIANTS = 4;
const MAX_SUBJECT_LEN = 512;
const MAX_LONG_SUBJECTS = 1;
const MAX_LONG_SUBJECT_LEN = 1024;
// subjects with more matches than this are "heavy"; keep only a few
const MAX_MATCHES = 24;
const MAX_HEAVY_SUBJECTS = 1;

/** @return list<string> */
function sourceFiles(string $ref): array
{
    $dirs = [$ref.'/src'];
    foreach (glob($ref.'/vendor/composer/*', GLOB_ONLYDIR) as $d) {
        $dirs[] = $d;
    }
    foreach (glob($ref.'/vendor/seld/*', GLOB_ONLYDIR) as $d) {
        $dirs[] = $d;
    }
    $files = [];
    foreach ($dirs as $dir) {
        $it = new RecursiveIteratorIterator(new RecursiveDirectoryIterator($dir, FilesystemIterator::SKIP_DOTS));
        foreach ($it as $f) {
            $p = $f->getPathname();
            if (substr($p, -4) !== '.php') {
                continue;
            }
            // the Preg wrapper itself only forwards its arguments
            if (strpos($p, '/vendor/composer/pcre/src/') !== false || strpos($p, '/PHPStan/') !== false) {
                continue;
            }
            if (preg_match('{/(tests?|Tests)/}', substr($p, strlen($ref)))) {
                continue;
            }
            $files[] = $p;
        }
    }
    foreach (glob($ref.'/vendor/composer/*.php') as $p) {
        $files[] = $p;
    }
    sort($files);

    return $files;
}

/**
 * @return array{list<array{string,string}>, list<array{string,string}>} [[pattern, source]], [[source, expression]]
 */
function staticPatterns(string $ref): array
{
    $found = [];
    $unresolved = [];
    foreach (sourceFiles($ref) as $file) {
        $rel = substr($file, strlen($ref) + 1);
        $tokens = token_get_all((string) file_get_contents($file));
        $ns = '';
        $uses = [];
        $class = null;
        $n = count($tokens);
        for ($i = 0; $i < $n; $i++) {
            $t = $tokens[$i];
            if (!is_array($t)) {
                continue;
            }
            if ($t[0] === T_NAMESPACE) {
                $ns = '';
                for ($j = $i + 1; $j < $n && $tokens[$j] !== ';' && $tokens[$j] !== '{'; $j++) {
                    if (is_array($tokens[$j]) && in_array($tokens[$j][0], [T_STRING, T_NAME_QUALIFIED], true)) {
                        $ns .= $tokens[$j][1];
                    }
                }
                continue;
            }
            if ($t[0] === T_USE && $class === null) {
                $name = '';
                $alias = null;
                for ($j = $i + 1; $j < $n && $tokens[$j] !== ';'; $j++) {
                    if (is_array($tokens[$j]) && in_array($tokens[$j][0], [T_STRING, T_NAME_QUALIFIED, T_NAME_FULLY_QUALIFIED], true)) {
                        if ($name !== '' && is_array($tokens[$j - 2] ?? null) && $tokens[$j - 2][0] === T_AS) {
                            $alias = $tokens[$j][1];
                        } elseif ($name === '') {
                            $name = ltrim($tokens[$j][1], '\\');
                        }
                    }
                }
                if ($name !== '') {
                    $parts = explode('\\', $name);
                    $uses[$alias ?? end($parts)] = $name;
                }
                continue;
            }
            if (in_array($t[0], [T_CLASS, T_TRAIT, T_INTERFACE, T_ENUM], true)) {
                $prev = prevToken($tokens, $i);
                if ($prev !== null && is_array($prev) && $prev[0] === T_DOUBLE_COLON) {
                    continue;
                }
                $nt = nextToken($tokens, $i);
                if ($nt !== null && is_array($nt[1]) && $nt[1][0] === T_STRING) {
                    $class = ($ns !== '' ? $ns.'\\' : '').$nt[1][1];
                }
                continue;
            }

            $isCall = false;
            if ($t[0] === T_STRING && preg_match('{^preg_(match|match_all|replace|replace_callback|replace_callback_array|split|grep)$}', $t[1])) {
                $prev = prevToken($tokens, $i);
                $isCall = !($prev !== null && is_array($prev) && in_array($prev[0], [T_FUNCTION, T_OBJECT_OPERATOR, T_DOUBLE_COLON, T_NULLSAFE_OBJECT_OPERATOR], true));
            } elseif (($t[0] === T_STRING || $t[0] === T_NAME_FULLY_QUALIFIED || $t[0] === T_NAME_QUALIFIED) && preg_match('{(^|\\\\)(Preg|Regex)$}', $t[1])) {
                $nt = nextToken($tokens, $i);
                if ($nt !== null && is_array($nt[1]) && $nt[1][0] === T_DOUBLE_COLON) {
                    $mt = nextToken($tokens, $nt[0]);
                    if ($mt !== null && is_array($mt[1]) && $mt[1][0] === T_STRING && $mt[1][1] !== 'class' && strtoupper($mt[1][1]) !== $mt[1][1]) {
                        $i = $mt[0];
                        $isCall = true;
                    }
                }
            } elseif ($t[0] === T_NAME_FULLY_QUALIFIED && preg_match('{^\\\\preg_(match|match_all|replace|replace_callback|replace_callback_array|split|grep)$}', $t[1])) {
                $isCall = true;
            }
            if (!$isCall) {
                continue;
            }
            $open = nextToken($tokens, $i);
            if ($open === null || $open[1] !== '(') {
                continue;
            }
            $line = $t[2];
            // collect first argument
            $depth = 0;
            $arg = [];
            for ($j = $open[0] + 1; $j < $n; $j++) {
                $tok = $tokens[$j];
                $s = is_array($tok) ? $tok[1] : $tok;
                if ($depth === 0 && ($s === ',' || $s === ')')) {
                    break;
                }
                if (in_array($s, ['(', '[', '{'], true) || (is_array($tok) && in_array($tok[0], [T_CURLY_OPEN, T_DOLLAR_OPEN_CURLY_BRACES], true))) {
                    $depth++;
                } elseif (in_array($s, [')', ']', '}'], true)) {
                    $depth--;
                }
                $arg[] = $tok;
            }
            $source = $rel.':'.$line;
            $res = evaluateArg($arg, $ns, $uses, $class);
            if ($res === null) {
                $res = evaluateVariable($tokens, $i, $arg, $ns, $uses, $class);
            }
            if ($res === null) {
                $unresolved[] = [$source, trim(implode('', array_map(static fn ($x) => is_array($x) ? $x[1] : $x, $arg)))];
                continue;
            }
            foreach ($res as $p) {
                $found[] = [$p, $source];
            }
        }
    }

    return [$found, $unresolved];
}

/**
 * For a bare `$var` argument, evaluates the constant assignments `$var = ...;`
 * found earlier in the same function.
 *
 * @return list<string>|null
 */
function evaluateVariable(array $tokens, int $callAt, array $arg, string $ns, array $uses, ?string $class): ?array
{
    $sig = array_values(array_filter($arg, static fn ($t) => !(is_array($t) && $t[0] === T_WHITESPACE)));
    if (count($sig) !== 1 || !is_array($sig[0]) || $sig[0][0] !== T_VARIABLE) {
        return null;
    }
    $var = $sig[0][1];
    $res = [];
    for ($j = $callAt - 1; $j >= 0; $j--) {
        $t = $tokens[$j];
        if (is_array($t) && $t[0] === T_FUNCTION) {
            break;
        }
        if (!is_array($t) || $t[0] !== T_VARIABLE || $t[1] !== $var) {
            continue;
        }
        $eq = nextToken($tokens, $j);
        if ($eq === null || $eq[1] !== '=') {
            continue;
        }
        $rhs = [];
        $depth = 0;
        for ($k = $eq[0] + 1, $n = count($tokens); $k < $n; $k++) {
            $s = is_array($tokens[$k]) ? $tokens[$k][1] : $tokens[$k];
            if ($depth === 0 && $s === ';') {
                break;
            }
            if (in_array($s, ['(', '[', '{'], true)) {
                $depth++;
            } elseif (in_array($s, [')', ']', '}'], true)) {
                $depth--;
            }
            $rhs[] = $tokens[$k];
        }
        $v = evaluateArg($rhs, $ns, $uses, $class);
        if ($v === null) {
            return null;
        }
        array_push($res, ...$v);
    }

    return $res === [] ? null : array_reverse($res);
}

/** @return array{int, array|string}|null */
function nextToken(array $tokens, int $i): ?array
{
    for ($j = $i + 1, $n = count($tokens); $j < $n; $j++) {
        if (is_array($tokens[$j]) && in_array($tokens[$j][0], [T_WHITESPACE, T_COMMENT, T_DOC_COMMENT], true)) {
            continue;
        }

        return [$j, $tokens[$j]];
    }

    return null;
}

/** @return array|string|null */
function prevToken(array $tokens, int $i)
{
    for ($j = $i - 1; $j >= 0; $j--) {
        if (is_array($tokens[$j]) && in_array($tokens[$j][0], [T_WHITESPACE, T_COMMENT, T_DOC_COMMENT], true)) {
            continue;
        }

        return $tokens[$j];
    }

    return null;
}

/**
 * Evaluates a constant pattern expression (or an array literal of them, for
 * preg_replace([..]) and replaceCallbackArray). Returns null when the
 * expression depends on runtime values.
 *
 * @return list<string>|null
 */
function evaluateArg(array $arg, string $ns, array $uses, ?string $class): ?array
{
    $code = '';
    $isArray = false;
    $prevSig = null;
    foreach ($arg as $k => $tok) {
        if (!is_array($tok)) {
            if (!in_array($tok, ['.', '(', ')', '[', ']', ',', '=', '>'], true)) {
                return null;
            }
            if ($tok === '[' && trim($code) === '') {
                $isArray = true;
            }
            $code .= $tok;
            $prevSig = $tok;
            continue;
        }
        [$id, $text] = $tok;
        switch ($id) {
            case T_WHITESPACE:
            case T_COMMENT:
                $code .= ' ';
                continue 2;
            case T_CONSTANT_ENCAPSED_STRING:
            case T_LNUMBER:
            case T_DOUBLE_COLON:
            case T_DOUBLE_ARROW:
                $code .= $text;
                break;
            case T_ARRAY:
                if (trim($code) === '') {
                    $isArray = true;
                }
                $code .= $text;
                break;
            case T_VARIABLE:
                if (!(is_array($prevSig) && $prevSig[0] === T_DOUBLE_COLON)) {
                    return null;
                }
                $code .= $text;
                break;
            case T_STRING:
            case T_NAME_QUALIFIED:
            case T_NAME_FULLY_QUALIFIED:
            case T_STATIC:
                $next = nextToken($arg, $k);
                $isClass = $next !== null && is_array($next[1]) && $next[1][0] === T_DOUBLE_COLON;
                if (!$isClass) {
                    if (is_array($prevSig) && $prevSig[0] === T_DOUBLE_COLON) {
                        $code .= $text; // class constant name
                    } elseif (defined($text) || defined(ltrim($text, '\\'))) {
                        $code .= '\\'.ltrim($text, '\\');
                    } else {
                        return null; // function call or unknown constant
                    }
                    break;
                }
                $lower = strtolower($text);
                if ($lower === 'self' || $lower === 'static') {
                    if ($class === null) {
                        return null;
                    }
                    $code .= '\\'.$class;
                } elseif ($id === T_NAME_FULLY_QUALIFIED) {
                    $code .= $text;
                } else {
                    $first = explode('\\', $text)[0];
                    if (isset($uses[$first])) {
                        $code .= '\\'.$uses[$first].substr($text, strlen($first));
                    } else {
                        $code .= '\\'.($ns !== '' ? $ns.'\\' : '').$text;
                    }
                }
                break;
            default:
                return null;
        }
        $prevSig = $tok;
    }
    if (trim($code) === '') {
        return null;
    }
    try {
        $scope = $class !== null && class_exists($class) ? $class : null;
        $fn = static function () use ($code) {
            return eval('return '.$code.';');
        };
        if ($scope !== null) {
            $fn = Closure::bind($fn, null, $scope);
        }
        $value = $fn();
    } catch (Throwable $e) {
        return null;
    }
    if (is_string($value)) {
        return [$value];
    }
    if (is_array($value)) {
        $res = [];
        foreach ($value as $k => $v) {
            if (is_string($k) && !is_string($v)) {
                $res[] = $k; // pattern => callback
            } elseif (is_string($v)) {
                $res[] = $v;
            } else {
                return null;
            }
        }

        return $res;
    }

    return null;
}

/** @return list<array{string,string,string}> [pattern, subject, source] */
function readCaptureLog(string $file): array
{
    $res = [];
    $prefix = null;
    $fh = fopen($file, 'r');
    while (($line = fgets($fh)) !== false) {
        $e = unserialize(base64_decode(trim($line)));
        if (!is_array($e)) {
            continue;
        }
        [$fn, $pattern, $subject, $src] = $e;
        if (!preg_match('{^.*?/((?:src/Composer|vendor/composer|vendor/seld)/.*)$}', $src, $m)) {
            continue;
        }
        $res[] = [$pattern, $subject, $m[1]];
    }

    return $res;
}

function encodeStr(string $s)
{
    return preg_match('//u', $s) ? $s : ['base64' => base64_encode($s)];
}

function decodeStr($v): string
{
    return is_array($v) ? base64_decode($v['base64']) : $v;
}

function truncateSubject(string $s, int $max): string
{
    if (strlen($s) <= $max) {
        return $s;
    }
    $cut = substr($s, 0, $max);
    $nl = strrpos($cut, "\n");
    if ($nl !== false && $nl > $max / 2) {
        return substr($cut, 0, $nl + 1);
    }
    // do not split a UTF-8 sequence
    while ($cut !== '' && (ord($cut[strlen($cut) - 1]) & 0xC0) === 0x80) {
        $cut = substr($cut, 0, -1);
    }
    if ($cut !== '' && ord($cut[strlen($cut) - 1]) >= 0xC0) {
        $cut = substr($cut, 0, -1);
    }

    return $cut;
}

function sourceSortKey(string $source): string
{
    if (!preg_match('{^(.*):(\d+)$}', $source, $m)) {
        return $source;
    }

    return $m[1].':'.str_pad($m[2], 6, '0', STR_PAD_LEFT);
}

/**
 * Picks a diverse, deterministic subset: manual subjects first, then logged
 * ones grouped by their match signature, round-robin over signatures.
 *
 * @param list<string> $manual
 * @param list<string> $logged
 * @return list<string>
 */
function pickSubjects(string $pattern, array $manual, array $logged, int $max): array
{
    $picked = [];
    foreach ($manual as $s) {
        $picked[$s] = true;
    }
    $long = 0;
    $heavy = [];
    $heavyPicked = 0;
    $cands = [];
    foreach ($logged as $s) {
        if (strlen($s) > MAX_SUBJECT_LEN) {
            $s = truncateSubject($s, MAX_LONG_SUBJECT_LEN);
            $cands[$s] = 'long';
        } else {
            $cands[$s] ??= 'short';
        }
    }
    $keys = array_map('strval', array_keys($cands));
    usort($keys, static fn ($a, $b) => [strlen($a), $a] <=> [strlen($b), $b]);
    $bySig = [];
    foreach ($keys as $s) {
        if (isset($picked[$s])) {
            continue;
        }
        $m = null;
        $r = @preg_match_all($pattern, $s, $m, PREG_SET_ORDER | PREG_UNMATCHED_AS_NULL);
        if ($r !== false && $r > MAX_MATCHES) {
            $heavy[$s] = true;
        }
        $sig = $r === false ? 'err' : $r.':'.md5(serialize(array_map(static fn ($set) => array_map(static fn ($v) => $v === null ? null : strlen($v), $set), array_slice($m, 0, 2))));
        $bySig[$sig][] = $s;
    }
    ksort($bySig, SORT_STRING);
    $budget = $max - count($picked);
    while ($budget > 0 && $bySig !== []) {
        foreach ($bySig as $sig => &$list) {
            if ($budget <= 0) {
                break;
            }
            $s = array_shift($list);
            if ($list === []) {
                unset($bySig[$sig]);
            }
            if (isset($heavy[$s])) {
                if ($heavyPicked >= MAX_HEAVY_SUBJECTS) {
                    continue;
                }
                $heavyPicked++;
            }
            if ($cands[$s] === 'long') {
                if ($long >= MAX_LONG_SUBJECTS) {
                    continue;
                }
                $long++;
            }
            $picked[$s] = true;
            $budget--;
        }
        unset($list);
    }

    return array_map('strval', array_keys($picked));
}

// ---------------------------------------------------------------------------

[$static, $unresolved] = staticPatterns($ref);
$manual = require __DIR__.'/preg_manual.php';

/** @var array<string, array{sources: array<string, true>, manual: list<string>, logged: list<string>, origin: array<string,true>}> $patterns */
$patterns = [];
$add = static function (string $p, ?string $source, string $origin) use (&$patterns): void {
    $patterns[$p] ??= ['sources' => [], 'manual' => [], 'logged' => [], 'origin' => []];
    if ($source !== null) {
        $patterns[$p]['sources'][$source] = true;
    }
    $patterns[$p]['origin'][$origin] = true;
};

foreach ($static as [$p, $source]) {
    $add($p, $source, 'static');
}

$logs = array_slice($argv, 1);
if ($logs !== []) {
    foreach ($logs as $log) {
        foreach (readCaptureLog($log) as [$p, $subject, $source]) {
            if (!isset($patterns[$p]) || !isset($patterns[$p]['origin']['static'])) {
                $add($p, $source, 'dynamic');
            }
            $patterns[$p]['logged'][] = $subject;
        }
    }
} elseif (is_file($out)) {
    // reuse previously captured subjects
    $prev = json_decode((string) file_get_contents($out), true);
    foreach ($prev['patterns'] as $e) {
        $p = decodeStr($e['pattern']);
        if ($e['origin'] === 'static' && !isset($patterns[$p])) {
            continue; // no longer in the sources
        }
        if (!isset($patterns[$p]['origin']['static'])) {
            foreach ($e['sources'] as $source) {
                $add($p, $source, $e['origin']);
            }
        }
        foreach ($e['logged'] ?? [] as $s) {
            $patterns[$p]['logged'][] = decodeStr($s);
        }
    }
}

foreach ($manual['patterns'] as $p => $info) {
    $p = (string) $p;
    $add($p, null, 'manual');
    if (!isset($patterns[$p]['origin']['static'])) {
        foreach ($info['sources'] as $source) {
            $add($p, $source, 'manual');
        }
    }
    foreach ($info['subjects'] as $s) {
        $patterns[$p]['manual'][] = $s;
    }
}

// generic edge-case subjects every pattern is run against
$generic = $manual['generic'];
foreach ($manual['families'] as $family) {
    foreach ($patterns as $p => &$info) {
        if (preg_grep($family['sources'], array_map('strval', array_keys($info['sources'])))) {
            foreach ($family['subjects'] as $s) {
                $info['family'][] = $s;
            }
        }
    }
    unset($info);
}

// thin out runtime-built pattern variants: keep MAX_VARIANTS per call site,
// spread over the variants sorted by length
$bySite = [];
foreach ($patterns as $p => $info) {
    if (isset($info['origin']['dynamic']) && !isset($info['origin']['static']) && !isset($info['origin']['manual'])) {
        $sources = array_map('strval', array_keys($info['sources']));
        usort($sources, static fn ($a, $b) => strcmp(sourceSortKey($a), sourceSortKey($b)));
        $bySite[$sources[0]][] = (string) $p;
    }
}
ksort($bySite);
foreach ($bySite as $variants) {
    if (count($variants) <= MAX_VARIANTS) {
        continue;
    }
    usort($variants, static fn ($a, $b) => [strlen($a), $a] <=> [strlen($b), $b]);
    $keep = [];
    for ($k = 0; $k < MAX_VARIANTS; $k++) {
        $keep[(int) round($k * (count($variants) - 1) / (MAX_VARIANTS - 1))] = true;
    }
    foreach ($variants as $idx => $p) {
        if (!isset($keep[$idx])) {
            unset($patterns[$p]);
        }
    }
}

$entries = [];
foreach ($patterns as $p => $info) {
    $p = (string) $p;
    $sources = array_map('strval', array_keys($info['sources']));
    usort($sources, static fn ($a, $b) => strcmp(sourceSortKey($a), sourceSortKey($b)));
    $manualSubjects = array_values(array_unique(array_merge($info['manual'], $generic)));
    $logged = array_values(array_unique($info['logged']));
    // when reusing a committed corpus, its "logged" list already holds the
    // picked family subjects, so the pick is reproduced exactly
    $family = $logs === [] ? [] : array_values(array_unique($info['family'] ?? []));
    $origin = isset($info['origin']['static']) ? 'static' : (isset($info['origin']['manual']) ? 'manual' : 'dynamic');
    $subjects = pickSubjects($p, $manualSubjects, array_merge($logged, $family), $origin === 'dynamic' ? MAX_DYNAMIC_SUBJECTS : MAX_SUBJECTS);
    $keptLogged = array_values(array_intersect($subjects, array_map(static fn ($s) => strlen($s) > MAX_SUBJECT_LEN ? truncateSubject($s, MAX_LONG_SUBJECT_LEN) : $s, array_merge($logged, $family))));
    $entries[] = [
        'sortkey' => $sources === [] ? "\xff".$p : sourceSortKey($sources[0]),
        'data' => [
            'pattern' => encodeStr($p),
            'origin' => $origin,
            'sources' => $sources,
            'subjects' => array_map('encodeStr', $subjects),
            // non-manual subjects that made it in, so a rerun without logs is stable
            'logged' => array_map('encodeStr', array_values(array_diff($keptLogged, $manualSubjects))),
        ],
    ];
}
usort($entries, static fn ($a, $b) => strcmp($a['sortkey'], $b['sortkey']) ?: strcmp(json_encode($a['data']['pattern']), json_encode($b['data']['pattern'])));

file_put_contents($out, json_encode(['patterns' => array_column($entries, 'data')], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR)."\n");

$counts = array_count_values(array_map(static fn ($e) => $e['data']['origin'], $entries));
ksort($counts);
fwrite(STDERR, sprintf("%d patterns (%s), %d subjects\n", count($entries), json_encode($counts), array_sum(array_map(static fn ($e) => count($e['data']['subjects']), $entries))));
$covered = [];
foreach ($entries as $e) {
    foreach ($e['data']['sources'] as $s) {
        $covered[$s] = true;
    }
}
foreach ($unresolved as [$source, $expr]) {
    if (!isset($covered[$source])) {
        fwrite(STDERR, "unresolved: $source: $expr\n");
    }
}
