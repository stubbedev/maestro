<?php
// Generates random PHP-ish sources from a pool of fragments that exercise
// every scanner state php_strip_whitespace() and PhpFileCleaner go through
// (open/close tags, strings, interpolation, heredoc/nowdoc, comments, casts,
// numbers, attributes, class keywords), and records for each one the md5 of
// php_strip_whitespace() and the result of PhpFileParser::findClasses().
//
// Writes <outdir>/random.bin (the cases: 4-byte big-endian length + bytes)
// and <outdir>/random.golden (one line per case, see record() in
// common.php).
//
// Run: php tools/oracle/classmap/random.php [seed] [count] [outdir]
// Defaults write internal/classmap/testdata/oracle.
require __DIR__.'/common.php';

$seed = (int) ($argv[1] ?? 1);
$count = (int) ($argv[2] ?? 3000);
$outDir = $argv[3] ?? dirname(__DIR__, 3).'/internal/classmap/testdata/oracle';

$fragments = [
    '<?php ', "<?php\n", "<?php\r\n", '<?php', '<?PHP ', '<?', '<?=', '?>', "?>\n", "?>\r\n", '<', '<?xml ',
    "#!/usr/bin/env php\n", '#!',
    ' ', '  ', "\t", "\n", "\r", "\r\n", "\n\n", "\x0b", "\x0c", "\0",
    '"', "'", '`', '\\', '\\\\', '\\"', "\\'", '\\$', '\\{', '\\u{41}', '\\u{', '\\u{110000}', '\\u{zz}', '\\n',
    '$a', '$', '${', '{$', '{', '}', '$a->b', '$a?->b', '->', '?->', '$a[', '[', ']', '$a[0]', '$a[b]', '$a[$b]', '(', ')',
    '<<<EOT', "<<<EOT\n", "<<<'EOT'\n", "<<<\"EOT\"\n", "<<< EOT\r\n", "<<<\tEOT\r", "<<<'EOT'\r\n", '<<<', "<<<A\n",
    'EOT', "\nEOT", "\n  EOT", "\n\tEOT", "\n \tEOT", "EOT;\n", "\nEOT\n", "\nEOTX", "\nA", "\n A;", 'EOT,',
    '/*', '*/', '/**', '/** ', '//', '#', '#[', '#[Attr]', '/* class Foo */', '// class Bar',
    '(int)', '( string )', '(binary)', '(unset)', "(\tbool\t)", '(integer', 'yield', 'from', 'yield from', "yield\nfrom", "yield /**/ from", 'yield #c' . "\n" . 'from',
    'public(set)', 'private(set) ', 'protected(SET)',
    '0x1F', '1_000', '08', '0o17', '0b101', '.5', '1.', '1e5', '1e', '1.5e-3', '0_1', '9',
    'class', 'class ', 'Class', 'CLASS ', 'interface ', 'trait ', 'enum ', 'enum Foo: int ', 'enum Bar:string', 'namespace ', 'namespace',
    'Foo', 'Bar\\Baz', '\\Foo', ' Foo ', ':xhp:foo-bar ', 'extends ', 'implements ', 'new class ', '::class', '$class', '->class', '\\class ',
    ';', ',', '=', '==', '=>', '.', '-', '--', '-=', '?', '??', '??=', '<=>', '<<', '<<=', '<=', '<>', '!', '@', '&', '|', '^', '~', '%', '*', '+', '/', '/=',
    '::', '...', '===', '!==', '**', '**=', '>>', '>>=', '>=', '&&', '||', '&$', '& $', '&...', '&=', '|=', '^=', '%=', '++', '+=', '*=', '.=',
    '"$a"', '"{$a}"', '"${a}"', '"$a->b"', '"$a[0]"', "'it\\'s'", '__halt_compiler();', '?>x<?php ', "EOT\n?>", "EOT;\r\n", '`$a`',
    "\x80", "\xff", "\xe2\x86\x91", 'x', 'abc', '_', '0', 'function f() {}', 'if (1) {', '} else {',
];
$tails = [
    ' class A {}', "\nnamespace N;\nclass B {}", ' namespace N { class C {} }', ' interface I {}', ' enum E: int {}',
    ' trait T {}', "\n<?php class D {}", "\n?>\n<?php class F {}", '',
];

mt_srand($seed);
$bin = '';
$golden = '';
$tmp = tempnam(sys_get_temp_dir(), 'cmrand');
for ($i = 0; $i < $count; $i++) {
    $n = mt_rand(1, 30);
    $src = mt_rand(0, 3) === 0 ? '' : '<?php ';
    for ($j = 0; $j < $n; $j++) {
        $src .= $fragments[mt_rand(0, count($fragments) - 1)];
    }
    $src .= $tails[mt_rand(0, count($tails) - 1)];
    $bin .= pack('N', strlen($src)).$src;
    file_put_contents($tmp, $src);
    $golden .= record($tmp, $tmp)."\n";
}
unlink($tmp);

file_put_contents($outDir.'/random.bin', $bin);
file_put_contents($outDir.'/random.golden', $golden);
