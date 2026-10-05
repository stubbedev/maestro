<?php
// Generates internal/json/jsonlint/testdata/oracle/jsonlint.json.gz: the result
// of Seld\JsonLint\JsonParser::parse (seld/jsonlint 1.12.1, as locked by
// Composer 2.10.3) over thousands of valid, malformed and mutated JSON
// inputs, under every flag combination, and Utf8Validator::validate over
// random byte strings.
//
// Each case records the input, the flags and either the parsed value (its
// var_export, or the md5 of it when long) or the exception: its short class
// name, message and getDetails() (as JSON, invalid UTF-8 substituted;
// key order is significant). Other strings that are not valid
// UTF-8 are stored as {"b64": ...}.
// Run: php tools/oracle/json/jsonlint.php
require dirname(__DIR__, 3).'/.ref/jsonlint/src/Seld/JsonLint/JsonParser.php';
require dirname(__DIR__, 3).'/.ref/jsonlint/src/Seld/JsonLint/Lexer.php';
require dirname(__DIR__, 3).'/.ref/jsonlint/src/Seld/JsonLint/ParsingException.php';
require dirname(__DIR__, 3).'/.ref/jsonlint/src/Seld/JsonLint/DuplicateKeyException.php';
require dirname(__DIR__, 3).'/.ref/jsonlint/src/Seld/JsonLint/InvalidEncodingException.php';
require dirname(__DIR__, 3).'/.ref/jsonlint/src/Seld/JsonLint/Undefined.php';
require dirname(__DIR__, 3).'/.ref/jsonlint/src/Seld/JsonLint/Utf8Validator.php';

use Seld\JsonLint\JsonParser;
use Seld\JsonLint\Utf8Validator;

error_reporting(E_ALL & ~E_DEPRECATED);
mt_srand(20261005);

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }

function s(string $s)
{
    return preg_match('//u', $s) ? $s : ['b64' => base64_encode($s)];
}

function result(callable $fn): array
{
    try {
        $v = $fn();
        $export = var_export($v, true);

        return strlen($export) > 2048 ? ['md5' => md5($export)] : ['value' => s($export)];
    } catch (\Throwable $e) {
        $out = ['class' => (new \ReflectionClass($e))->getShortName(), 'message' => s($e->getMessage())];
        if (method_exists($e, 'getDetails')) {
            $out['details'] = json_decode(json_encode($e->getDetails(), JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE));
        }

        return $out;
    }
}

$root = dirname(__DIR__, 3);
$seeds = [
    '42', '42.3', '0.3', '-42', '-42.3', '-0.3', '-0', '0', '-0.0', '1.0', '1e2', '1E-2', '2e+3',
    '9223372036854775807', '9223372036854775808', '-9223372036854775808', '-9223372036854775809',
    '99999999999999999999999', '1e400', '-1e400', '1e-400', '0.1e1', '123456789012345678901234567890.5',
    'true', 'false', 'null', '""', '[]', '{}', '"string"', '[1,2,3]', '[[],[[]],{}]',
    '{"foo":"bar", "bar":"baz", "":"buz"}', '{"":"foo", "_empty_":"bar"}',
    '"\u00c9v\u00e9nement"', '"http:\/\/foo.com"', '"zo\\\\mg"', '"\u0022"', '"\u0000"', '"\ud83d\udc7b"',
    '"\uD83D"', '"\u1f47d"', '"\u007f"', '"\u0080"', '"\u009F"', '"\u00a0"', '"\uFFFE"', '"\uffff"', '"\uFDD0"',
    '"\u0009\u000a\u000D\u0001\u001f\u0020"', '"\b\f\n\r\t\/\\\\\""', '"a\\\\u0041"', '"\\\\\\\\"',
    '{"1":"a","01":"b","1.5":"c","-1":"d","9223372036854775808":"e"}',
    '{"a":{"b":{"c":[1,{"d":null}]}}}', "{\n    \"a\": 1,\n    \"b\": [\n        true\n    ]\n}\n",
    "\t[ 1 ,\r\n 2 ]\r\n", '"👻"', '"é"', "\"\xc3\xa9\"",
];

// The 65536 \uXXXX escapes, in chunks.
$chunk = [];
for ($cp = 0; $cp < 0x10000; $cp++) {
    $chunk[] = sprintf(mt_rand(0, 1) ? '\\u%04x' : '\\u%04X', $cp);
    if (count($chunk) === 512) {
        $seeds[] = '"'.implode('', $chunk).'"';
        $chunk = [];
    }
}

$docs = [
    file_get_contents($root.'/.ref/composer/composer.json'),
    file_get_contents($root.'/.ref/jsonlint/composer.json'),
    file_get_contents($root.'/.ref/composer/tests/Composer/Test/Json/Fixtures/composer.json'),
    file_get_contents($root.'/.ref/jsonlint/tests/without-comments.json'),
    '{"a":"b", "b":"c", "c": [1, 2.5, -3e2, true, false, null, "x\\"y"], "d": {"e": {}}}',
];
foreach ($docs as $d) {
    $seeds[] = $d;
}
// A slice of a real lock file: its first package.
$lock = json_decode(file_get_contents($root.'/.ref/composer/composer.lock'), true);
$seeds[] = json_encode(['packages' => array_slice($lock['packages'], 0, 2)], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);

$malformed = [
    '', ' ', "\n\n", 'ABCD', 'a', '{', '}', '[', ']', '{"a"}', '{"a":}', '{"a":1,}', '[1,]', '[,1]', '{,}', '{"a" 1}',
    '{"a":1 "b":2}', '[1 2]', "{'a':1}", "{\"a\":'b'}", '{a:1}', '"abc', '{"bar": "foo}', "{\"bar\": \"foo\nbar\"}",
    '{"foo": "bar\z"}', '"\x"', '"\u12"', '"\u12G4"', "\"\t\"", "\"\x01\"", '01', '-', '1.', '.5', '1e', '1e+', '+1',
    '0x10', 'NaN', 'Infinity', 'tru', 'nul', 'falsey', 'true false', '[] []', '{} x', '/* c */ {}', '// c', '{} // c',
    '{"a":1} /*', '*/', '{"a": /* x */ 1}', "\xEF\xBB\xBF{}", "\xEF\xBB{}", '{"a":1}}', '[[[[', ']]]]', '{"a":[}', '{"a":{]}',
    "{\n    \"foo\":\"bar\",\n}", "{\n    \"foo\": 'bar',\n}", "{\n    \"foo\": \"bar\\z\",\n}",
    "{\n    \"#3026386-26 - Drush fatal error after upgrading to 8.6.6, 8.5.9, or 7.62: PHP Fatal error: Uncaught TYPO3\\PharStreamWrapper\\Exception\": \"https://www.drupal.org/files/issues/2019-01-16/d8-3026386-26.patch\"\n}",
    "{\n    \"#3026386-26 - Drush fatal error after upgrading to 8.6.6, 8.5.9, or 7.62: PHP Fatal error\": \"https://www.drupal.org/files/issues/201\\9-01-16/d8-3026386-26.patch\n}",
    '{"', '{"a', '{"a"', '{"a":', '{"a":"', '["', "[\"\n", '"\\', '"\\"', '{"a":"\\', "\"\xff\"", "\xff", "\xc3", "{\"\xc3\":1}",
    '{"a":1,"a":2}', '{"a":null,"a":2}', '{"a":1,"a":null,"a":3}', '{"a":1,"a.1":2,"a":3}', '{"1":1,"1":2}',
    '{"a":{"__duplicates__":[1]},"a":2}', '{"a":{"__duplicates__":"s"},"a":2}', '{"a":{"__duplicates__":{}},"a":2}',
    '{"a":{"__duplicates__":5},"a":2}', '{"a":{"__duplicates__":false},"a":2}', '{"a":{"__duplicates__":true},"a":2}',
    '{"a":{"__duplicates__":null},"a":2}', '{"a":[1],"a":[2],"a":{"b":3}}', '{"":1,"":2}', '{"\u0000a":1}', '{"\u0000":1,"b":2}',
    '{"b":1,"\u0000a":2}', '{"a":1,"\u0000a":2}', '{"a\u0000":1}', '{"x":{"a":1,"a":2}}',
    '["a", "sdfsd"]//test', '[/*"a",*/ "sdfsd"]//', '["a", "sdf//sd"]/**/', '/**/{/*"":*/"g":"foo"}', '{"a":"b"}//, "b":"c"}',
    "/* unterminated", "// only comment\n", "[1, // c\n 2]", "[1, /* c */ 2]", "[1 /* c */, 2] /**/", "{/**/}",
    file_get_contents($root.'/.ref/jsonlint/tests/with-comments.json'),
];

$tokens = [',', ':', '"', "'", '\\', '{', '}', '[', ']', 'x', ' ', "\n", "\r\n", '/*', '//', '*/', "\t", "\0", "\x1f",
    "\xff", "\xc3", "\xe2\x82", 'true', 'nul', '01', '-', '1e', '.5', '\\u', '\\uD83D', '"a":', '""', '[]', '{}', 'null,'];

function mutate(string $s, array $tokens): string
{
    $n = strlen($s);
    switch (mt_rand(0, 5)) {
        case 0: // truncate
            return substr($s, 0, mt_rand(0, $n));
        case 1: // delete a byte
            $i = mt_rand(0, max(0, $n - 1));

            return substr($s, 0, $i).substr($s, $i + 1);
        case 2: // insert a token
            $i = mt_rand(0, $n);

            return substr($s, 0, $i).pick($tokens).substr($s, $i);
        case 3: // replace a byte with a token
            $i = mt_rand(0, max(0, $n - 1));

            return substr($s, 0, $i).pick($tokens).substr($s, $i + 1);
        case 4: // duplicate a slice
            $i = mt_rand(0, $n);
            $j = mt_rand($i, $n);

            return substr($s, 0, $j).substr($s, $i, $j - $i).substr($s, $j);
        default: // swap two bytes
            if ($n < 2) {
                return $s;
            }
            $i = mt_rand(0, $n - 2);

            return substr($s, 0, $i).$s[$i + 1].$s[$i].substr($s, $i + 2);
    }
}

$allFlags = [0, 1, 2, 4, 5, 6, 8, 12, 16, 20, 32, 36, 13, 18, 9, 17, 40, 63];

$cases = [];
$add = static function (string $input, array $flagsList) use (&$cases): void {
    $results = [];
    foreach ($flagsList as $flags) {
        $parser = new JsonParser();
        $results[] = ['flags' => $flags] + result(static function () use ($parser, $input, $flags) {
            return $parser->parse($input, $flags);
        });
    }
    $cases[] = ['input' => s($input), 'results' => $results];
};

foreach (array_merge($seeds, $malformed) as $input) {
    $add($input, strlen($input) > 1000 ? [0, 4] : $allFlags);
}

$bases = array_merge($docs, $malformed, array_slice($seeds, 0, 60));
for ($i = 0; $i < 4000; $i++) {
    $s = pick($bases);
    $rounds = mt_rand(1, 3);
    for ($r = 0; $r < $rounds; $r++) {
        $s = mutate($s, $tokens);
    }
    $add($s, [0, pick($allFlags)]);
}

// Random token soups.
$soup = ['{', '}', '[', ']', ',', ':', '"a"', '"b"', '1', '-2.5e3', 'true', 'false', 'null', ' ', "\n", '//x', "\n", '/*y*/', "'c'", '"\\q"', '""'];
for ($i = 0; $i < 1500; $i++) {
    $s = '';
    $n = mt_rand(1, 14);
    for ($j = 0; $j < $n; $j++) {
        $s .= pick($soup);
    }
    $add($s, [0, pick($allFlags)]);
}

// Utf8Validator over random byte strings biased to near-UTF-8.
$utf8 = [];
$bytes = ["a", "\n", "\xc3\xa9", "\xe2\x82\xac", "\xf0\x9d\x84\x9e", "\xc3", "\xe2\x82", "\xf0\x9d", "\x80", "\xbf",
    "\xc0", "\xc1", "\xf5", "\xff", "\xed\xa0\x80", "\xed\x9f\xbf", "\xe0\x80\x80", "\xe0\xa0\x80", "\xf0\x80\x80\x80",
    "\xf4\x90\x80\x80", "\xf4\x8f\xbf\xbf", "\xf8", "\xfe"];
for ($i = 0; $i < 1500; $i++) {
    $s = '';
    $n = mt_rand(0, 10);
    for ($j = 0; $j < $n; $j++) {
        $s .= pick($bytes);
    }
    $utf8[] = ['input' => s($s)] + result(static function () use ($s) {
        Utf8Validator::validate($s);

        return null;
    });
}

$out = dirname(__DIR__, 3).'/internal/json/jsonlint/testdata/oracle';
@mkdir($out, 0777, true);
file_put_contents($out.'/jsonlint.json.gz', gzencode(json_encode(['parse' => $cases, 'utf8' => $utf8], JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE)."\n", 9));
echo count($cases), " parse cases, ", count($utf8), " utf8 cases\n";
