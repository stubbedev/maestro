<?php
// The per-PHP-version php_strip_whitespace() and findClasses() oracle (see
// versions.sh, which runs it).
//
//   php versions.php cases <file> [seed] [count]
//       writes the cases, as random.bin does (4-byte big-endian length +
//       bytes): hand-written sources around every scanner difference
//       between PHP 7.2 and 8.5 (internal/classmap/phpversion.go lists
//       them), then random ones from a pool of fragments that adds those
//       differences to random.php's (seed 8 and 2500 of them by default).
//   php versions.php record <file> <composer>
//       prints, for each case of the file, what the running PHP makes of
//       it: the md5 of php_strip_whitespace(), "/", then the result of
//       PhpFileParser::findClasses() of the class-map-generator vendored
//       in the Composer checkout <composer> (.ref/composer): the hex of
//       the classes joined by NUL bytes, or "!" and the hex of the
//       exception message (with the case's path removed).
//
// Runs on every PHP from 7.2 on (so does Composer's vendor directory).
error_reporting(E_ALL & ~E_WARNING & ~E_NOTICE & ~E_DEPRECATED);

if (($argv[1] ?? '') === 'record') {
    require $argv[3].'/vendor/autoload.php';
    $bin = file_get_contents($argv[2]);
    $tmp = tempnam(sys_get_temp_dir(), 'cmver');
    for ($o = 0; $o < strlen($bin); $o += 4 + $n) {
        $n = unpack('N', substr($bin, $o, 4))[1];
        file_put_contents($tmp, substr($bin, $o + 4, $n));
        echo md5((string) @php_strip_whitespace($tmp)), '/';
        try {
            echo bin2hex(implode("\0", \Composer\ClassMapGenerator\PhpFileParser::findClasses($tmp))), "\n";
        } catch (\RuntimeException $e) {
            // without the "may be helpful" part, which holds whatever
            // error_get_last() returned (see firstLine() in common.php)
            $message = explode(PHP_EOL.'The following message may be helpful:', $e->getMessage())[0];
            echo '!', bin2hex(str_replace($tmp, '', $message)), "\n";
        }
    }
    unlink($tmp);
    exit(0);
}
if (($argv[1] ?? '') !== 'cases' || !isset($argv[2])) {
    fwrite(STDERR, "usage: php versions.php cases <file> [seed] [count] | record <file> <composer>\n");
    exit(1);
}

// Hand-written cases, grouped by the difference they exercise.
$cases = [
    // 7.3: flexible heredoc and nowdoc
    "<?php \$x = <<<EOT\nEOT;\nclass A {}",
    "<?php \$x = <<<EOT\nEOT\n;class A {}",
    "<?php \$x = <<<EOT\na\nEOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  a\n  EOT;\nclass A {}",
    "<?php \$x = <<<'EOT'\n  a\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n\ta\n\tEOT;\nclass A {}",
    "<?php \$x = [<<<EOT\na\nEOT, 1];\nclass A {}",
    "<?php \$x = [<<<'EOT'\na\nEOT, 1];\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT . 'b';\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOTX\nEOT;\nclass A {}",
    "<?php \$x = <<<EOT\na\n EOT\nEOT;\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT;",
    "<?php \$x = <<<EOT\na\nEOT",
    "<?php \$x = <<<EOT\r\na\r\nEOT;\r\nclass A {}",
    "<?php \$x = <<<EOT\ra\rEOT;\rclass A {}",
    "<?php \$x = <<<\"EOT\"\na \$b {\$c} \${d}\nEOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  a {\$b} c\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$b}\n EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  a\n \tEOT;\nclass A {}",
    "<?php \$x = <<<A\n  {\$x(<<<B\n    b\n    B)}\n  A;\nclass C {}",
    "<?php \$x = <<<EOT\nclass B {}\nEOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  class B {}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  EOT;\nclass A {}",
    "<?php \$x = <<<'EOT'\n  EOT;\nclass A {}",
    "<?php \$x = <<<'EOT'\n  a\n  EOTb\n  EOT;\nclass A {}",
    // 7.3: heredoc scan-ahead errors (which errors stop it changes with
    // the version)
    "<?php \$x = <<<EOT\n  {\$a[0_8]}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a[08]}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a /* x\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a)}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a]}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a[\"\\u{zz}\"]}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a[\"\\u{110000}\"]}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a(<<<B\n \t B\n)}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a #[\n]}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a[0o8]}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a[1_0_8]}\n  EOT;\nclass A {}",
    "<?php \$x = <<<EOT\n  {\$a[0x_1]}\n  EOT;\nclass A {}",
    // the token zend_strip() copies after a closing marker
    "<?php \$x = <<<EOT\na\nEOT\\Foo;\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT?->b;\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT??=1;\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT|>f(...);\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT#[x\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT.1_0;\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT.0o1;\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT//c\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT#c\r\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT\x01;\nclass A {}",
    "<?php \$x = <<<EOT\na\nEOT\x7f\x01;\nclass A {}",
    // 7.4: unexpected characters
    "<?php \$a \x01 \$b; class A {}",
    "<?php \$a\x01\$b; class A {}",
    "<?php a\x7fb; class A {}",
    "<?php \$a\x00\$b; class A {}",
    "<?php \$a \x0b\x0c \$b; class A {}",
    "<?php class\x01A {}",
    "<?php \"\$a[\x01]\"; class A {}",
    "<?php \"\$a[b\x02]\"; class A {}",
    "<?php \x01",
    "<?php ?>\x01<?php \x01",
    // 7.4: numbers
    "<?php 1_000; 0x1_F; 0b1_0; 1_0.5_0e1_0; class A {}",
    "<?php 08; 0_8; 1__0; 1_; class A {}",
    // 8.0: attributes
    "<?php #[Attr] class A {}",
    "<?php\n#[Attr]\nclass A {}",
    "<?php #[Attr(1)] final class A {} #[B] class B {}",
    "<?php #[\nAttr] class A {}",
    "<?php function f(#[A] \$a, #[B] \$b) {} class A {}",
    "<?php #[A]?>\nclass B {}",
    "<?php #[A]?>\n<?php class B {}",
    // 8.0: line comments and their newline
    "<?php // c\nclass A {}",
    "<?php \$a//c\n\$b; class A {}",
    "<?php \$a # c\r\n\$b; class A {}",
    "<?php \$a #c\r\$b; class A {}",
    "<?php \$a #c\n\n\$b; class A {}",
    "<?php class//c\nA {}",
    "<?php class #c\r\nA {}",
    "<?php //c?>x<?php class A {}",
    "<?php #c\n?>x",
    "<?php /* c */\nclass A {}",
    "<?php /** c */class A {}",
    "<?php // c",
    "<?php #",
    // 8.0: shebang
    "#!/usr/bin/env php\n<?php class A {}",
    "#!/usr/bin/env php\r\n<?php class A {}",
    "#!php",
    "#!\n",
    "#!/usr/bin/env php\nclass A {}",
    // 8.0: names
    "<?php new \\Foo\\Bar; namespace\\Baz(); A\\B::c(); class A {}",
    "<?php \\ Foo; Foo \\ Bar; class A {}",
    // 8.0: nullsafe
    "<?php \$a?->b?->c; \$a ?-> b; \"\$a?->b\"; class A {}",
    "<?php \$a = <<<EOT\n  \$a?->b\n  EOT;\nclass A {}",
    // 8.1: octal
    "<?php 0o17; 0O17; 0o_1; class A {}",
    // 8.1: enums, readonly, & (token types only)
    "<?php enum Foo: string {} enum /* c */ Bar {} class A {}",
    "<?php readonly class A {} readonly (1); fn(&\$a, & ...\$b) => 1;",
    // 8.2: comments after ->
    "<?php \$a-> /* c */ b; class A {}",
    "<?php \$a->/*c*/b; class A {}",
    "<?php \$a->//c\nb; class A {}",
    "<?php \$a->#c\nb; class A {}",
    "<?php \$a->#[x\nb; class A {}",
    "<?php \$a->/*c*/yield\n\nfrom; class A {}",
    "<?php \$a?->/*c*/b; class A {}",
    "<?php \$a->\n/*c*/\nb; class A {}",
    "<?php \"\$a->b\"; \$a->/**/class; class A {}",
    // 8.3: yield from
    "<?php function f() { yield /* c */ from g(); } class A {}",
    "<?php function f() { yield\n\tfrom g(); } class A {}",
    "<?php function f() { yield // c\nfrom g(); } class A {}",
    "<?php function f() { yield #c\nfrom g(); } class A {}",
    "<?php function f() { yield /**/from g(); } class A {}",
    "<?php function f() { yield/**/ /**/ from g(); } class A {}",
    "<?php function f() { yield\n\nfromage; } class A {}",
    // 8.4: asymmetric visibility, property hooks
    "<?php class A { public(set) int \$a; PRIVATE(SET) int \$b; protected(set) string \$c; }",
    "<?php class A { public private(set) int \$a { get => 1; set { \$this->a = \$value; } } }",
    "<?php class A { public (set) int \$a; }",
    "<?php class A { public string \$a = __PROPERTY__; }",
    // 8.5: (void) and |>
    "<?php ( void ) f(); (\tvoid\t)g(); (VOID)h(); class A {}",
    "<?php \$a |> f(...) | > g(...); class A {}",
    "<?php (  int  ) \$a; (\tstring\t)\$b; ( real ) \$c; ( unset ) \$d; class A {}",
    // open tags
    "<?php",
    "<?php class A {}?>x<?php",
    "<?phpx class A {}",
    "x<?phpx class A {}",
    "x<?php\tclass A {}",
    "<?= 1 ?><? class A {}",
    "<?xml version=\"1.0\"?>\n<?php class A {}",
    "<? class A {}",
];

// Random cases.
$fragments = [
    '<?php ', "<?php\n", "<?php\r\n", '<?php', '<?PHP ', '<?', '<?=', '?>', "?>\n", "?>\r\n", '<', '<?xml ',
    "#!/usr/bin/env php\n", '#!',
    ' ', '  ', "\t", "\n", "\r", "\r\n", "\n\n", "\x0b", "\x0c", "\0", "\x01", "\x7f",
    '"', "'", '`', '\\', '\\\\', '\\"', "\\'", '\\$', '\\{', '\\u{41}', '\\u{', '\\u{110000}', '\\u{zz}', '\\n',
    '$a', '$', '${', '{$', '{', '}', '$a->b', '$a?->b', '->', '?->', '$a[', '[', ']', '$a[0]', '$a[b]', '$a[$b]', '(', ')',
    '<<<EOT', "<<<EOT\n", "<<<'EOT'\n", "<<<\"EOT\"\n", "<<< EOT\r\n", "<<<\tEOT\r", "<<<'EOT'\r\n", '<<<', "<<<A\n",
    'EOT', "\nEOT", "\n  EOT", "\n\tEOT", "\n \tEOT", "EOT;\n", "\nEOT\n", "\nEOT;\n", "\nEOT,", "\nEOT)", "\nEOTX", "\nA", "\n A;", 'EOT,',
    '/*', '*/', '/**', '/** ', '//', '#', '#[', '#[Attr]', '#[Attr] ', '/* class Foo */', '// class Bar', "// c\n", "# c\r\n", "#c\r",
    '(int)', '( string )', '(binary)', '(unset)', "(\tbool\t)", '(integer', '(void)', '( void )', '(real)',
    'yield', 'from', 'yield from', "yield\nfrom", 'yield /**/ from', 'yield #c' . "\n" . 'from', "yield //c\nfrom",
    'public(set)', 'private(set) ', 'protected(SET)', 'public (set)',
    '0x1F', '1_000', '08', '0_8', '0o17', '0o8', '0b101', '0b1_1', '0x_1', '.5', '1.', '1e5', '1e', '1.5e-3', '0_1', '9', '1_0.5',
    'class', 'class ', 'Class', 'CLASS ', 'interface ', 'trait ', 'enum ', 'enum Foo: int ', 'enum Bar:string', 'namespace ', 'namespace',
    'Foo', 'Bar\\Baz', '\\Foo', ' Foo ', 'namespace\\Foo', ':xhp:foo-bar ', 'extends ', 'implements ', 'new class ', '::class', '$class', '->class', '\\class ',
    ';', ',', '=', '==', '=>', '.', '-', '--', '-=', '?', '??', '??=', '<=>', '<<', '<<=', '<=', '<>', '!', '@', '&', '|', '|>', '^', '~', '%', '*', '+', '/', '/=',
    '::', '...', '===', '!==', '**', '**=', '>>', '>>=', '>=', '&&', '||', '&$', '& $', '&...', '&=', '|=', '^=', '%=', '++', '+=', '*=', '.=',
    '"$a"', '"{$a}"', '"${a}"', '"$a->b"', '"$a?->b"', '"$a[0]"', '"$a[0_8]"', "'it\\'s'", '__halt_compiler();', '?>x<?php ', "EOT\n?>", "EOT;\r\n", '`$a`',
    "\x80", "\xff", "\xe2\x86\x91", 'x', 'abc', '_', '0', 'function f() {}', 'if (1) {', '} else {',
];
$tails = [
    ' class A {}', "\nnamespace N;\nclass B {}", ' namespace N { class C {} }', ' interface I {}', ' enum E: int {}',
    ' trait T {}', "\n<?php class D {}", "\n?>\n<?php class F {}", '',
];

mt_srand((int) ($argv[3] ?? 8));
for ($i = 0; $i < (int) ($argv[4] ?? 2500); $i++) {
    $n = mt_rand(1, 30);
    $src = mt_rand(0, 3) === 0 ? '' : '<?php ';
    for ($j = 0; $j < $n; $j++) {
        $src .= $fragments[mt_rand(0, count($fragments) - 1)];
    }
    $cases[] = $src.$tails[mt_rand(0, count($tails) - 1)];
}

$bin = '';
foreach ($cases as $src) {
    $bin .= pack('N', strlen($src)).$src;
}
file_put_contents($argv[2], $bin);
