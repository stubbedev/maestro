<?php
// Generates internal/json/testdata/oracle/jsonfile.json.gz: JsonFile::encode
// (all option combinations Composer uses, custom indents),
// JsonFormatter::format, JsonFile::parseJson (with and without a file
// name, lock merge conflicts, malformed and non-UTF-8 input) and
// JsonFile::detectIndenting, over thousands of generated values and texts.
// Input values are tagged JSON (see serialize_value); strings, results
// and messages are base64 encoded, as they need not be UTF-8.
//
// Run: php tools/oracle/json/jsonfile.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Json\JsonFile;
use Composer\Json\JsonFormatter;

set_error_handler(static function (int $level, string $message, string $file, int $line): bool {
    if ($level === E_DEPRECATED || $level === E_USER_DEPRECATED || 0 === (error_reporting() & $level)) {
        return true;
    }
    throw new \ErrorException($message, 0, $level, $file, $line);
});

mt_srand(20261005);

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }
function chance(int $percent): bool { return mt_rand(1, 100) <= $percent; }

function str(): string
{
    return pick(['', 'a', 'foo/bar', 'a\\b', "quote\"d", "\u{e9}t\u{e9}", "\u{1F600}", "\u{2028}\u{2029}", "tab\tnl\n", "\x7f", '</script>', "'apos'", '&amp;', 'ƌ', '\\u0119', 'https://x.org/a/b', '  lead', 'trail  ', "\x00nul", '0', '1.5', 'Žluťoučký kůň', "\xc3", "\xff\xfe"]);
}

function value(int $depth = 0)
{
    $r = mt_rand(0, $depth > 3 ? 5 : 9);
    switch ($r) {
        case 0: return null;
        case 1: return pick([true, false]);
        case 2: return pick([0, -1, 42, PHP_INT_MAX, PHP_INT_MIN]);
        case 3: return pick([0.0, 1.5, -0.0, 1e100, 0.1, 1e-7, 100.0, 3.14159265358979]);
        case 4:
        case 5: return str();
        case 6:
        case 7:
            $a = [];
            $n = mt_rand(0, 4);
            for ($i = 0; $i < $n; $i++) {
                $a[] = value($depth + 1);
            }

            return $a;
        case 8:
            $a = [];
            $n = mt_rand(0, 4);
            for ($i = 0; $i < $n; $i++) {
                $a[pick(['name', 'require', 'a/b', '0', '5', '', 'é', 'x y', 'k'.$i])] = value($depth + 1);
            }

            return $a;
        default:
            $o = new \stdClass();
            $n = mt_rand(0, 3);
            for ($i = 0; $i < $n; $i++) {
                $o->{pick(['a', 'b', 'é', '1', 'x/y'])} = value($depth + 1);
            }

            return $o;
    }
}

function outcome(callable $fn): array
{
    try {
        $v = $fn();

        return ['v' => base64_encode(is_string($v) ? $v : var_export($v, true))];
    } catch (\Throwable $e) {
        return ['e' => [get_class($e), base64_encode($e->getMessage())]];
    }
}

$flagSets = [
    JSON_UNESCAPED_SLASHES | JSON_PRETTY_PRINT | JSON_UNESCAPED_UNICODE,
    0,
    JSON_PRETTY_PRINT,
    JSON_UNESCAPED_SLASHES,
    JSON_UNESCAPED_UNICODE,
    JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE,
    JSON_PRETTY_PRINT | JSON_UNESCAPED_UNICODE,
    JSON_UNESCAPED_SLASHES | JSON_PRETTY_PRINT | JSON_UNESCAPED_UNICODE | JSON_PRESERVE_ZERO_FRACTION,
    JSON_UNESCAPED_SLASHES | JSON_PRETTY_PRINT | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_IGNORE,
];
$indents = ['    ', "\t", '  ', '        ', '   ', "\t\t", ' '];

$encode = [];
for ($n = 0; $n < 2000; $n++) {
    $v = value();
    // JSON input for the Go side: objects as {"o": ...}, arrays as {"a": [[k, v], ...]}
    $flags = pick($flagSets);
    $indent = chance(60) ? '    ' : pick($indents);
    $encode[] = ['in' => serialize_value($v), 'flags' => $flags, 'indent' => $indent, 'r' => outcome(static function () use ($v, $flags, $indent) { return JsonFile::encode($v, $flags, $indent); })];
}

function serialize_value($v)
{
    if (is_array($v)) {
        $pairs = [];
        foreach ($v as $k => $item) {
            $pairs[] = [$k, serialize_value($item)];
        }

        return ['a' => $pairs];
    }
    if ($v instanceof \stdClass) {
        $pairs = [];
        foreach ((array) $v as $k => $item) {
            $pairs[] = [(string) $k, serialize_value($item)];
        }

        return ['o' => $pairs];
    }
    if (is_float($v)) {
        return ['f' => var_export($v, true)];
    }
    if (is_string($v)) {
        return ['s' => base64_encode($v)];
    }

    return $v;
}

// JsonFormatter::format over compact encodings (escaped or not)
$format = [];
for ($n = 0; $n < 2000; $n++) {
    $v = value();
    $json = json_encode($v, pick([0, JSON_UNESCAPED_SLASHES, JSON_UNESCAPED_UNICODE, JSON_INVALID_UTF8_SUBSTITUTE]));
    if ($json === false) {
        continue;
    }
    if (chance(10)) {
        $json = substr($json, 0, mt_rand(0, strlen($json)));
    }
    $u = chance(50);
    $s = chance(50);
    $format[] = ['json' => base64_encode($json), 'u' => $u, 's' => $s, 'r' => outcome(static function () use ($json, $u, $s) { return JsonFormatter::format($json, $u, $s); })];
}
foreach (['"\\\\\\u0119"', '"\\ud83d\\ude00"', '"\\\\u0119"', '"\\\\\\\\\\u00e9"', '"\\u00E9\\/x"', '"\\u0000"', '"\\uD7FF\\uE000"', ']', '}{', '[[]]', '{"a":{}}', '[1,[2,[3]]]', '"a\\"b"'] as $json) {
    foreach ([[true, true], [true, false], [false, true], [false, false]] as [$u, $s]) {
        $format[] = ['json' => base64_encode($json), 'u' => $u, 's' => $s, 'r' => outcome(static function () use ($json, $u, $s) { return JsonFormatter::format($json, $u, $s); })];
    }
}

// parseJson and detectIndenting
$texts = ['', 'null', '{}', '[]', '{"a": 1}', "{\n\t\"a\": 1\n}", "{\n  \"a\": {\n    \"b\": 2\n  }\n}", '"str"', '1', 'true', '{"a": 1,}', "{\"a\": \"\xff\"}", "\xef\xbb\xbf{}", '{"a":1}{"b":2}', ' ', "{\n    \"content-hash\": \"abc\"\n}"];
foreach (glob(dirname(__DIR__, 3).'/internal/json/testdata/Fixtures/*') as $fixture) {
    $texts[] = file_get_contents($fixture);
}
$mutations = [];
foreach ($texts as $t) {
    $mutations[] = $t;
    for ($i = 0, $max = strlen($t) > 400 ? 5 : 30; $i < $max && strlen($t) > 0; $i++) {
        $m = $t;
        $pos = mt_rand(0, strlen($m) - 1);
        switch (mt_rand(0, 3)) {
            case 0: $m = substr($m, 0, $pos).substr($m, $pos + 1); break;
            case 1: $m = substr($m, 0, $pos).pick([',', '"', '{', '}', '[', "\n", ' ', ':', '\\', "\xc3"]).substr($m, $pos); break;
            case 2: $m = substr($m, 0, $pos); break;
            default: $m = str_replace("\n", "\r\n", $m);
        }
        $mutations[] = $m;
    }
}
$parse = [];
foreach ($mutations as $text) {
    $file = pick([null, 'composer.json', '/path/to/composer.lock', 'x.lock']);
    $parse[] = ['text' => base64_encode($text), 'file' => $file, 'r' => outcome(static function () use ($text, $file) { return var_export(JsonFile::parseJson($text, $file), true); }), 'indent' => base64_encode(JsonFile::detectIndenting($text))];
}

$out = dirname(__DIR__, 3).'/internal/json/testdata/oracle/jsonfile.json.gz';
@mkdir(dirname($out), 0777, true);
file_put_contents($out, gzencode(json_encode(['encode' => $encode, 'format' => $format, 'parse' => $parse], JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR | JSON_INVALID_UTF8_SUBSTITUTE), 9));
echo count($encode), ' ', count($format), ' ', count($parse), "\n";
