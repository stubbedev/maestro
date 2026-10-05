<?php

/*
 * Differential oracle for internal/php's ports of strip_tags, levenshtein,
 * stripcslashes, escapeshellarg, basename, sprintf, string == and +:
 * writes internal/php/testdata/oracle/funcs.json.
 *
 * Usage (from the repo root, inside the devenv shell):
 *   php tools/oracle/php/funcs.php
 *
 * Strings that are not valid UTF-8 are written as {"base64": "..."}; other
 * PHP values travel as serialize() strings. A thrown error is written as
 * {"error": message, "class": class}.
 */

declare(strict_types=1);

error_reporting(E_ALL);
// Warnings (non-numeric values, ...) are expected.
set_error_handler(static fn (): bool => true);

$root = dirname(__DIR__, 3);
$file = $root.'/internal/php/testdata/oracle/funcs.json';

function enc(string $s)
{
    return preg_match('//u', $s) ? $s : ['base64' => base64_encode($s)];
}

/** Runs $fn, returning enc() of its string result or the error it threw. */
function attempt(callable $fn)
{
    try {
        $r = $fn();
    } catch (\Throwable $e) {
        return ['error' => $e->getMessage(), 'class' => get_class($e)];
    }

    return is_string($r) ? enc($r) : enc(serialize($r));
}

$cases = [];

// --- strip_tags ---------------------------------------------------------------

$stripInputs = ['', 'plain', '<b>bold</b>', 'a < b', 'a <b', 'x > y', '<a href="x>y">t</a>', "<a href='x>y'>t</a>", '<!-- c -->after', '<!-- a > b -->x',
    '<?php echo 1; ?>after', '<? x ?>y', '<?xml version="1.0"?>z', '<!DOCTYPE html>d', '<!doctype html>d', '<script>alert(1)</script>', "a\0b", '<<a>>', '<a<b>>c',
    '<p>"quoted"</p>', 'it\'s <i>"x"</i>', "<\n>", '< x', '<1>', 'trail<', '<span style="color:red;">x</span>', "\x08\x08<info>y</info>", '<?php echo "?>"; ?>z',
    '<?php f("a>b"); ?>q', '<?php (1>0); ?>r', '<!-- x -- y -->w', '<!--->v', '<!x>y', "<a title='it\\'s'>q</a>", '<a b="c\'d">e</a>', '>>', '<<', '<?xml?><b>x</b>',
    "<\t>tab", '<a>"</a>"b', "<br/>\xff\xfe", '<foo <bar> baz>q'];
foreach ($stripInputs as $s) {
    $cases[] = ['fn' => 'strip_tags', 'args' => [enc($s)], 'result' => attempt(static fn () => strip_tags($s))];
}

// --- levenshtein --------------------------------------------------------------

$words = ['', 'a', 'ab', 'abc', 'kitten', 'sitting', 'flaw', 'lawn', 'list', 'lsit', 'cache:clear', 'cache:clean', 'about', "caf\u{e9}", 'cafe', 'AbC'];
foreach ($words as $a) {
    foreach ($words as $b) {
        $cases[] = ['fn' => 'levenshtein', 'args' => [enc($a), enc($b)], 'result' => attempt(static fn () => serialize(levenshtein($a, $b)))];
    }
}

// --- stripcslashes ------------------------------------------------------------

$slashed = ['', 'plain', '\\n\\t\\r\\a\\v\\b\\f', '\\x41\\x4a\\x4G\\xg', '\\x', '\\x1', '\\x123', '\\101\\60\\7\\777\\0', '\\8\\9', '\\\\', '\\"\\\'', 'trail\\',
    '\\e\\z', '\\0x', "\\\xff", '\\400', '\\1234'];
foreach ($slashed as $s) {
    $cases[] = ['fn' => 'stripcslashes', 'args' => [enc($s)], 'result' => attempt(static fn () => stripcslashes($s))];
}

// --- escapeshellarg -----------------------------------------------------------

// escapeshellarg skips bytes that mblen() rejects in PHP 8's default LC_CTYPE,
// C.UTF-8 (glibc: up to 6-byte sequences, no overlongs or surrogates).
$shellArgs = ['', 'a', 'a b', "it's", "''", "a\nb", '$HOME', "\\'", "nul\0byte", "\xff\xfe", '"q"', "\xc3\xa9", "caf\xc3\xa9's", "a\xffb", "\xc0\xaf", "\xc1\xbf",
    "\xc2\x80", "\xe0\x80\x80", "\xe0\xa0\x80", "\xed\x9f\xbf", "\xed\xa0\x80", "\xee\x80\x80", "\xef\xbf\xbf", "\xf0\x8f\xbf\xbf", "\xf0\x90\x80\x80",
    "\xf4\x90\x80\x80", "\xf7\xbf\xbf\xbf", "\xf8\x87\xbf\xbf\xbf", "\xf8\x88\x80\x80\x80", "\xfb\xbf\xbf\xbf\xbf", "\xfc\x83\xbf\xbf\xbf\xbf",
    "\xfc\x84\x80\x80\x80\x80", "\xfd\xbf\xbf\xbf\xbf\xbf", "\xfe", "\xff", "a\xe2\x82", "\xe2\x82'x", "\x80'", "\xe2'\x82\xac"];
foreach ($shellArgs as $s) {
    $cases[] = ['fn' => 'escapeshellarg', 'args' => [enc($s)], 'result' => attempt(static fn () => escapeshellarg($s))];
}

// --- basename -----------------------------------------------------------------

$paths = ['', '/', '//', 'a', 'a/', 'a//', '/a', '/a/b', '/a/b/', 'a/b.php', 'a/.php', '.php', 'dir/file.tar.gz', 'c:\\x\\y', '/a/b/.', '..', 'x.php/'];
foreach ($paths as $p) {
    foreach (['', '.php', '.gz', 'b', 'file.tar.gz', '/'] as $suffix) {
        $cases[] = ['fn' => 'basename', 'args' => [enc($p), enc($suffix)], 'result' => attempt(static fn () => basename($p, $suffix))];
    }
}

// --- sprintf ------------------------------------------------------------------

$formats = ['plain', '%s', '%5s|', '%-5s|', "%'*10s", '%05s', '%.3s', '%5.1s|', '%-5.1s|', '%d', '%5d|', '%-5d|', '%05d', '%+d', '%+05d', '%-+5d|', '% d', '%ld',
    '%u', '%x', '%X', '%o', '%b', '%c', '%e', '%.2e', '%.0e', '%E', '%f', '%.3f', '%.0f', '%F', '%10.4f|', '%-10.2f|', '%+.1f', '%010.2f', '%g', '%G', '%.0g',
    '%.3g', '%%', '%1$s %1$s', '%2$s %1$s', '%0$s', '%', '%y', "%'", '%s %s', '%3$s', 'a%sb%sc', '%-\'x8s|', '%.60f'];
$args = ['abc', '', 42, -42, 0, 3.14159, -0.5, 1e20, 1.5e-7, 123456789.0, 0.1 + 0.2, '12abc', ' 7', true, false, null, INF, -INF, NAN, PHP_INT_MIN, PHP_INT_MAX, 255, 'é'];
foreach ($formats as $f) {
    foreach ($args as $a) {
        foreach ([[$a], [$a, 'second']] as $list) {
            $cases[] = ['fn' => 'sprintf', 'args' => [enc($f), enc(serialize($list))], 'result' => attempt(static fn () => sprintf($f, ...$list))];
        }
    }
}

// --- string == string ---------------------------------------------------------

$numeric = ['', '0', '1', '-1', '+1', '01', '1.0', '1.', '.5', '0.5', '5e-1', '1e3', '1000', '1E3', ' 1', '1 ', "1\n", '1x', 'abc', 'ABC', '0x1A', '26',
    '9223372036854775807', '9223372036854775808', '9223372036854775808.0', '9.2233720368547758E+18', '-9223372036854775808', '-9223372036854775809',
    '99999999999999999999', '100000000000000000000', '1e1000', '2e1000', '-1e1000', 'INF', '0.0', '-0', '-0.0', '00', '1.00000000000000001', '1.0000000000000001'];
foreach ($numeric as $a) {
    foreach ($numeric as $b) {
        $cases[] = ['fn' => '==', 'args' => [enc($a), enc($b)], 'result' => attempt(static fn () => serialize($a == $b))];
    }
}

// --- + ------------------------------------------------------------------------

$operands = [0, 1, -1, PHP_INT_MAX, PHP_INT_MIN, 2, 1.5, -0.0, 1e308, INF, null, true, false, '', '1', ' 2 ', '3.5', '1e3', '9223372036854775807', '9223372036854775808',
    '12abc', 'abc', '0x10', [], [1, 2], ['a' => 1, 0 => 'x'], [5 => 'y', 'a' => 2]];
foreach ($operands as $a) {
    foreach ($operands as $b) {
        $cases[] = ['fn' => '+', 'args' => [enc(serialize($a)), enc(serialize($b))], 'result' => attempt(static fn () => $a + $b)];
    }
}

$flags = JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR;
file_put_contents($file, "[\n".implode(",\n", array_map(static fn ($c) => json_encode($c, $flags), $cases))."\n]\n");
fwrite(STDERR, sprintf("funcs: %d cases\n", count($cases)));
