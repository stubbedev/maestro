<?php

/*
 * Writes internal/php/testdata/preg/engine.json: patterns exercising the
 * PCRE2 features the Go engine implements (and the errors it must report),
 * beyond the patterns Composer itself uses, each with tricky subjects. It
 * has the corpus.json format and is turned into goldens by preg_golden.php.
 *
 * Usage (from the repo root, inside the devenv shell):
 *   php tools/oracle/php/preg_engine.php
 */

declare(strict_types=1);

$root = dirname(__DIR__, 3);
$out = $root.'/internal/php/testdata/preg/engine.json';

$lines = ['', 'a', "a\n", "a\nb\n", "\n", "\n\n", "ba\na", "b\na\n", "a\r\nb"];
$empty = ['', 'axb', 'b', 'aab', 'xx', 'a b', "ünï", "a\nb"];
$words = ['', 'abc', 'aaa', 'abab', 'aaab', 'ab ab', 'abcabc', 'xabcx', "a1b2c3", '123,456,789', '1234567'];
$quotes = ['"a"', "'b'", '"c\'', 'x"y"z', '""', "'\"'"];
$unicode = ['', 'abc', 'ÄBC', 'äbc', 'straße', 'STRASSE', 'Kelvin K k', "\u{212A}", 'ſ s S', 'é É e', 'Ωω', '١٢٣', 'x_y', "\u{0301}a", "\u{2160}", "a\u{00A0}b", "a\u{2028}b", '😀x', "\u{85}"];
$bytes = ["\xff\xfe", "a\xe9b", "\xc3\xa9", "\x80", "abc\xc3", "\xe2\x82\xac"];
$classes = ['', 'aZ09_-.', '[]a]^\\', '!@#$%^&*()', " \t\n\r\x0b\x0c", "\x00\x1f\x7f", 'ÀÿĀ', 'abc-def'];
$nested = ['()', '(a)', '((a)(b))', '(()', '(a(b)c)d)', '[[1],[2,[3]]]', '[[]', 'aba', 'abba', 'abcba', 'abcd', ''];

$patterns = [
    // anchors and newlines
    '/^/m' => $lines, '/$/m' => $lines, '/^$/m' => $lines, '/\Z/' => $lines, '/\z/' => $lines,
    '/a$/' => $lines, '/a$/D' => $lines, '/a$/m' => $lines, '/^a/m' => $lines, '/\Aa/m' => $lines,
    '/(?m)^b/' => $lines, '/\G\w/' => $words, '/\Ga/' => $words, '/a\Z/D' => $lines, '/.$/s' => $lines,
    '/^.*$/m' => $lines, '/^.*$/s' => $lines, '/.+/s' => $lines, '/\N+/s' => $lines,
    // empty matches
    '/x*/' => $empty, '/x*?/' => $empty, '/(?:)/' => $empty, '/a|/' => $empty, '/|a/' => $empty,
    '/\b/' => $empty, '/\B/' => $empty, '/(?=a)/' => $empty, '/x*|b/' => $empty, '/(a|)+/' => $empty,
    '/(a?)*?b/' => $empty, '/(?=a)*/' => $empty, '/.?/u' => $empty, '/\b/u' => $empty,
    // quantifiers
    '/a+?/' => $words, '/a*+a/' => $words, '/a++b/' => $words, '/(?>a+)b/' => $words, '/(a+)+b/' => $words,
    '/a{2,3}/' => $words, '/a{2,3}?/' => $words, '/a{,2}/' => $words, '/a{ 2 }/' => $words, '/a{2,}+/' => $words,
    '/(ab){2}/' => $words, '/(ab){0,2}?c/' => $words, '/\d{1,3}(?=(\d{3})+$)/' => $words, '/a{0}b/' => $words,
    '/(?:ab)*+/' => $words, '/(?:a|ab)*c/' => $words, '/[ab]*?b/' => $words, '/a{2}{3}/' => $words, '/a{x/' => ['a{x', 'a'],
    '/a*?$/' => $words, '/(a)*/U' => $words, '/a+?/U' => $words, '/(?U)a+/' => $words,
    // groups and captures
    '/(a)|(b)/' => $words, '/(a)?(b)?(c)?/' => $words, '/(?:(a)|b)*/' => $words, '/(a|b\1)+/' => $words,
    '/(a)(?<n>b)?(c)/' => $words, '/(?<y>\d{2})(?<m>\d)?/' => $words, '/(?|(a)|(b)(c))/' => $words,
    '/(?n)(a)(?<x>b)/' => $words, '/((a)|(b))+/' => $words, '/(a*)*/' => $words, '/(a*)+/' => $words,
    '/(a|ab)(c|bcd)(d*)/' => ['abcd', 'acd', 'abcdd'], '/(?<a>.)(?<a>.)/J' => $words, '/(?J)(?<a>x)|(?<a>a)/' => $words,
    '/(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)/' => ['abcdefghijk', 'abcdefghij'],
    // back references
    '/(a)\1/' => $words, '/(a)\1/i' => ['aA', 'Aa', 'aa'], '/(?<q>[\'"]).*?\k<q>/' => $quotes, '/(\w)\g{-1}/' => $words,
    '/(a)\g1/' => $words, '/(?P<x>a)(?P=x)/' => $words, '/(a)?\1/' => $words, '/(ä)\1/iu' => ['äÄ', 'ää', 'Ää'],
    '/(a)|\1b/' => $words, '/(\w+)\s\1/' => ['the the', 'a ab', 'ab ab'], '/(a)\k{1}/' => $words, '/(a)\10/' => ["a\x08", 'aa'],
    '/(a)\12/' => ["a\n"], '/(k)\1/iu' => ["k\u{212A}", 'kK'],
    // lookaround
    '/(?<=a)b/' => $words, '/(?<!a)b/' => $words, '/(?<=a|bc)d/' => ['ad', 'bcd', 'cd', 'd'], '/(?<=\d{1,3})x/' => ['1x', '1234x', 'x', 'a12x'],
    '/(?<=^|,)\w+/' => $words, '/(?=(\w+))\w/' => $words, '/(?!a)\w/' => $words, '/\w+(?=;)/' => ['ab;', 'a;b;'],
    '/(?<=(a))b/' => $words, '/(?<![\d.])\d+/' => ['1.5 22', 'v1.0'], '/(?<=\R)x/' => ["\r\nx", "\nx", 'x'],
    '/(?<=ab|a)c/' => ['abc', 'ac'], '/(?<=é)x/u' => ['éx', 'ex'], '/(?<=é)x/' => ['éx', 'ex'], '/(?<!^)x/' => ['xx', 'x'],
    '/(?=(a))a(?!(b))/' => $words, '/(?<=(?=a)a)b/' => $words,
    // conditionals
    '/^(a)?(?(1)b|c)$/' => ['ab', 'c', 'b', 'ac'], '/(?(?=a)ab|cd)/' => ['ab', 'cd', 'ad'], '/(?(?!a)cd|ab)/' => ['ab', 'cd', 'ad'],
    '/(?<n>x)?(?(<n>)y|z)/' => ['xy', 'z', 'xz', 'y'], '/(?(DEFINE)(?<d>\d+))(?&d)-(?&d)/' => ['12-345', '1-', 'a-b'],
    '/(?(?<=a)b|c)/' => ['ab', 'c', 'b'], '/(a)?(?(1)b)/' => $words, '/(?(R)a|b(?R)?)/' => ['ba', 'b', 'bba'],
    // recursion and subroutine calls
    '/\((?:[^()]|(?R))*\)/' => $nested, '/(?<p>\[(?:[^\[\]]|(?&p))*\])/' => $nested, '/(a(?1)?b)/' => ['ab', 'aabb', 'aab'],
    '/(?1)(a)/' => ['aa', 'a'], '/(?R)?a/' => ['aa'], '/^((.)(?1)\2|.?)$/' => $nested, '/((a)|b)(?1)/' => ['ab', 'ba', 'bb'],
    '/(?<x>a)(?&x)/' => ['aa'], '/(\d)(?-1)/' => ['12', '1'], '/(?+1)(\d)/' => ['12'], '/(a|(?R)b)/' => ['ab'],
    '/^(?:\((?:(?R)|[^()])*\))+$/' => ['(())()', '(()', ''], '/(?<q>(?<c>[a-z])(?&c))/' => ['ab', 'a'],
    '/(?<x>a+)(?&x)a/' => ['aaaa', 'aa', 'aaa'], '/^(?:(a)|b)(?1)?$/' => ['ab', 'aa', 'b', 'ba'], '/(?<x>a|b)(?&x)/' => ['ab', 'ba'],
    '/(?(R1)x|(a)(?1))/' => ['aa', 'ax'], '/((?(R)a|b))(?1)/' => ['ba', 'bb'], '/(?<e>(?:[^()]++|\((?&e)\))*)/' => $nested,
    '/(a+?)(a*)/' => $words, '/a{20}/' => [str_repeat('a', 25)], '/.+?é/u' => ['abéé', 'é'], '/(|a)+b/' => $words,
    '/(a*)*b/' => $words, '/(a*)+b/' => $words, '/(a|b*)*c/' => ['abc', 'c', 'bbbc'], '/(?:(a)|(b))+\1?/' => $words,
    '/(?i)(É)\1/u' => ['Éé', 'éÉ', 'ÉE'], '/(?<!\S)\w+(?!\S)/' => ['a b', 'a-b c'], '/(?<=\G..)./' => ['abcdef'],
    '/\Ga|b/' => ['aab', 'ba'], '/(?=(a+))a*b\1/' => ['baaabac'], '/^(a\1?){4}$/' => ['aaaaaaaaaa', 'aaaa'],
    // classes
    '/[a-z]+/i' => $classes, '/[^a-z]/' => $classes, '/[\w.-]+/' => $classes, '/[]a]/' => $classes, '/[^]a]/' => $classes,
    '/[a\]b]/' => $classes, '/[\\\\]/' => $classes, '/[[:alpha:]]+/' => $classes, '/[[:^digit:]]+/' => $classes,
    '/[[:punct:]]/' => $classes, '/[\d-]/' => $classes, '/[-\d]/' => $classes, '/[a-]/' => $classes, '/[\x00-\x1f]/' => $classes,
    '/[\x{100}-\x{200}]/u' => $classes, '/[^\x00-\x7F]/' => $classes, '/[^\x00-\x7F]/u' => $classes, '/\h+/' => $classes,
    '/\v/' => $classes, '/\R/' => $classes, '/\s+/' => $classes, '/\S+/' => $classes, '/\W+/' => $classes, '/\D+/' => $classes,
    '/[[:space:]]/' => $classes, '/[[:word:]]+/' => $classes, '/[[:xdigit:]]+/' => $classes, '/[[:upper:]]/i' => $classes,
    '/[a-c-e]/' => $classes, '/[\Qa-c\E]/' => $classes, '/[.-\/]/' => $classes, '/[\]-a]/' => $classes, '/[a-\x{7a}]/' => $classes,
    '/[^\s\d]/' => $classes, '/[\D]/' => $classes, '/[\W\d]/' => $classes,
    // Unicode and case folding
    '/\p{L}+/u' => $unicode, '/\P{L}+/u' => $unicode, '/\pN/u' => $unicode, '/[\p{Lu}\d]/u' => $unicode,
    '/\p{Nd}/u' => $unicode, '/\p{Zs}/u' => $unicode, '/\p{L&}/u' => $unicode, '/\p{Xwd}+/u' => $unicode,
    '/\p{^L}/u' => $unicode, '/straße/iu' => $unicode, '/k/iu' => $unicode, '/[k]/iu' => $unicode, '/s/iu' => $unicode,
    '/ß/iu' => $unicode, '/é/i' => $unicode, '/é/iu' => $unicode, '/É/iu' => $unicode, '/[à-ÿ]/iu' => $unicode, '/\w+/u' => $unicode,
    '/\w+/' => $unicode, '/\bé/u' => $unicode, '/\bé/' => $unicode, '/./u' => $unicode, '/./' => $unicode, '/.{2}/u' => $unicode,
    '/\d+/u' => $unicode, '/\s/u' => $unicode, '/\h/u' => $unicode, '/\v/u' => $unicode, '/\R/u' => $unicode, '/[[:alpha:]]+/u' => $unicode,
    '/[[:punct:]]/u' => $unicode, '/[[:graph:]]+/u' => $unicode, '/[^a]/u' => $unicode, '/[^a]/' => $unicode, '/ω/iu' => $unicode,
    '/\x{e9}/u' => $unicode, '/\xe9/' => $unicode, '/[\xc3]/' => $unicode, '/\p{Lu}/' => $unicode, '/\pL/i' => $unicode,
    // invalid UTF-8 subjects
    '/a/u' => $bytes, '/./s' => $bytes, '/(?<=x)y/u' => $bytes, '/\xff/' => $bytes, '/[^a]+/' => $bytes,
    // extended mode
    "/a b # c\n d/x" => ['abd', 'a b d', 'ab'], '/[ ]/x' => [' ', 'a'], '/a\ b/x' => ['a b', 'ab'], '/(?x) a (?-x) b/' => ['a b', 'ab'],
    '/a b/xx' => ['ab'], '/[a b]/xx' => ['a', ' ', 'b'], '/a#b/x' => ['a', 'a#b'], "/a(?#c)b/" => ['ab'], '/a + b/x' => ['aab'],
    // \K and \Q
    '/a\Kb/' => $words, '/foo\K\d+/' => ['foo123', 'foo'], '/(?=ab\K)/' => ['ab'], '/\Q.*\E+/' => ['.*', '.**', 'a'],
    '/[\Q]\E]/' => [']', 'a'], '/\Qa\E*/' => ['aaa', ''], '/\Q/' => ['', 'a'], '/a\E/' => ['a'],
    // escapes
    '/\x41\x{42}\101\o{103}\cD/' => ["ABACD\x04", 'ABAC'], '/\e\a\f\t/' => ["\x1b\x07\x0c\t"], '/\0/' => ["\0", 'a'],
    '/\012/' => ["\n"], '/\_/' => ['_'], '/\N{U+41}/u' => ['A'], '/\$\^\./' => ['$^.'], '/\//' => ['/'], '#\##' => ['#'],
    // delimiters and modifiers
    '{a}' => ['a'], '(a)' => ['a'], '[a]' => ['a'], '<a>' => ['a'], '{a{1}}' => ['a'], '#a#' => ['a'], '~a~i' => ['A'],
    " \n/a/" => ['a'], '/a/ S' => ['a'], '/a/X' => ['a'], '/a/A' => ['ba', 'ab'], '/b/A' => ['ba', 'ab'], '/a/n' => ['a'],
    // errors at match time
    '/(a+)+$/' => [str_repeat('a', 30).'!', 'aaa'], '/(?:(?R)|a)/' => ['a'], '/(.*?)*x/' => [str_repeat('ab', 20)],
    // invalid patterns
    '/(/' => ['a'], '/)/' => ['a'], '/[a/' => ['a'], '/a**/' => ['a'], '/\x/' => ['a'], '/[z-a]/' => ['a'], '/a{3,2}/' => ['a'],
    '/(?<n>a)(?<n>b)/' => ['ab'], '/\k<x>/' => ['a'], '/(?<=a+)b/' => ['ab'], '/\y/' => ['y'], 'abc' => ['abc'], '/abc' => ['abc'],
    '/a/Q' => ['a'], '{a}}' => ['a'], '/a/e' => ['a'], '/(?P>x)/' => ['a'], '' => ['a'], '/\8/' => ['8'], '/[\d-z]/' => ['a'],
    '/(?<=a\d+)x/' => ['a1x'], '/\x{100}/' => ['a'], '/a{65536}/' => ['a'], '/[[:foo:]]/' => ['a'], '/(?z)/' => ['a'],
    '/\u0041/' => ['A'], '/(*FOO)a/' => ['a'], '/(?(1)a|b|c)/' => ['a'], '/(?#a/' => ['a'], '/\c/' => ['a'], '/\g0/' => ['a'],
    "/\xff/u" => ['a'], '/(?<1a>x)/' => ['x'], '/[[.a.]]/' => ['a'], '/\o{8}/' => ['a'], '/\p{Foo}/' => ['a'], '/a\\' => ['a'],
];

$corpus = [];
foreach ($patterns as $p => $subjects) {
    $p = (string) $p;
    $corpus[] = [
        'pattern' => preg_match('//u', $p) ? $p : ['base64' => base64_encode($p)],
        'origin' => 'engine',
        'subjects' => array_map(static fn (string $s) => preg_match('//u', $s) ? $s : ['base64' => base64_encode($s)], array_values(array_unique($subjects))),
    ];
}
file_put_contents($out, json_encode(['patterns' => $corpus], JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR)."\n");
fwrite(STDERR, count($corpus)." patterns\n");
