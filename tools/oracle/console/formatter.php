<?php
// Generates internal/console/testdata/oracle/formatter.json: OutputFormatter
// rendering (decorated and not, with and without wrapping), escaping,
// Helper::width/length/removeDecoration and strip_tags results.
// Run: php tools/oracle/console/formatter.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Symfony\Component\Console\Formatter\OutputFormatter;
use Symfony\Component\Console\Formatter\OutputFormatterStyle;
use Symfony\Component\Console\Helper\Helper;
use Composer\Console\HtmlOutputFormatter;

error_reporting(E_ALL & ~E_WARNING);
putenv('COLORTERM');
putenv('TERMINAL_EMULATOR');
putenv('KONSOLE_VERSION');
unset($_SERVER['IDEA_INITIAL_DIRECTORY']);

$messages = [
    '', 'plain text', '<info>info</info>', '<comment>comment</comment>', '<error>error</error>',
    '<question>q</question>', '<warning>w</warning>', '<highlight>h</highlight>',
    '<info>a<comment>b</comment>c</info>', '<info>a</>b', '</>', '</info>', '<info>unclosed',
    '<INFO>upper</INFO>', '<Info>mixed</info>', '<fg=red>r</fg=red>', '<fg=red>r</>',
    '<fg=red;bg=blue>rb</>', '<bg=yellow;options=bold>yb</>', '<options=bold,underscore>bu</>',
    '<fg=green;options=bold;bg=white>x</>', '<fg=#ff0000>hex</>', '<fg=#f00>short hex</>',
    '<fg=#123456;bg=#abcdef>hex2</>', '<fg=bright-red>br</>', '<fg=gray>g</>', '<fg=default>d</>',
    '<fg=RED>case</>', '<FG=red>keycase</>', '<href=https://example.com>link</>',
    '<href=https://example.com/?a=1&b=2;fg=red>link2</>', '<href=http://x/\<y\>>esc</>',
    'a \<info>escaped\</info> b', '\<info>x</info>', 'x \\', 'trailing \\\\', '<info>foo\\</info>',
    '<>', '< info>', '<info >sp</info >', '<1abc>', '<a\b>', "<a\\\nb>x", '<a b c>x</a b c>',
    '<unknown>x</unknown>', '<fg=red;foo=bar>x</>', '<=x>y</>', '<fg=red;>x</>', '<fg=red;;bg=blue>x</>',
    "multi\nline <info>info\nmore</info> end", "<info>tab\tchar</info>", "\x1b[31malready\x1b[0m",
    '<info>héllo wörld</info>', '<comment>日本語テキスト</comment>', "<info>\0nul</info>",
    '<info>a</info><comment>b</comment><error>c</error>', '<info><info>nested</info></info>',
    '<info>x</comment>', '<fg=red>a<fg=blue>b</>c</>', '<options=reverse;fg=cyan>rev</>',
    'Some <info>text</info> with <comment>several</comment> <error>styles</error> and a long tail that will need wrapping somewhere',
    'Pretty long text that will be wrapped at word boundaries hopefully nicely enough',
    'averyveryveryverylongwordwithoutanyspacesatallthatmustbecut ok',
    "  leading spaces and <info>styled   text</info>   trailing  ",
    "line1\nline2 that is longer than the width\n\nline4\n",
    '<info>héllo wörld ünïcödé çharacters everywhere</info> and more',
    "a<info>b</info>c<info>d</info>e<info>f</info>g",
    'xx<info>é</info>', 'aé b', 'éé éé <comment>ééé</comment> é', '日本語 テキスト <info>折り返し</info> です',
    '<error>  padded  </error>', "<info>a\n\nb</info>\n", '<fg=blue;options=bold>bold blue text that wraps around</> tail',
    '<info>x</info> <bg=red>  </> <comment>y</comment>', "\\<info>not a tag\\</info>", '<info>\\</info>',
];

$decorations = [false, true];
$widths = [0, 1, 2, 3, 5, 10, 20];

$out = ['format' => [], 'escape' => [], 'width' => [], 'striptags' => [], 'html' => []];
foreach ($messages as $m) {
    foreach ($decorations as $d) {
        foreach ($widths as $w) {
            $f = new OutputFormatter($d, ['warning' => new OutputFormatterStyle('black', 'yellow'), 'highlight' => new OutputFormatterStyle('red')]);
            $case = ['message' => $m, 'decorated' => $d, 'width' => $w];
            try {
                $case['output'] = $f->formatAndWrap($m, $w);
            } catch (\Throwable $e) {
                $case['error'] = $e->getMessage();
                $case['class'] = get_class($e);
            }
            $out['format'][] = $case;
        }
    }
    $html = new HtmlOutputFormatter(['warning' => new OutputFormatterStyle('black', 'yellow')]);
    try {
        $out['html'][] = ['message' => $m, 'output' => $html->format($m)];
    } catch (\Throwable $e) {
        $out['html'][] = ['message' => $m, 'error' => $e->getMessage()];
    }
}

$escapes = ['', '<', '>', '<<', '>>', '<>', 'a<b', 'a<<b', '\\<', '\\\\<', 'x\\', 'x\\\\', "x\0\\", '<info>', 'a <b> c', '\\<a>', '<a\\>', 'foo<bar>baz</bar>'];
foreach ($escapes as $e) {
    $out['escape'][] = ['input' => $e, 'output' => OutputFormatter::escape($e), 'trailing' => OutputFormatter::escapeTrailingBackslash($e)];
}

$f = new OutputFormatter(true);
$widthInputs = ['', 'abc', 'héllo', '日本語', "a\nbc", "ab\r\ncd", "tab\there", "\x1b[31mred\x1b[0m", "e\u{0301}", "\u{200B}zw", "\xff\xfe", 'emoji 😀', 'ｆｕｌｌ', "a\x07b", '<info>styled</info>', "\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\"];
foreach ($widthInputs as $s) {
    $out['width'][] = ['input' => $s, 'width' => Helper::width($s), 'length' => Helper::length($s), 'removeDecoration' => Helper::removeDecoration($f, $s), 'substr' => Helper::substr($s, 1, 3)];
}

$stripInputs = ['', 'plain', '<b>bold</b>', 'a < b', 'a <b', 'x > y', '<a href="x>y">t</a>', "<a href='x>y'>t</a>", '<!-- c -->after', '<!-- a > b -->x', '<?php echo 1; ?>after', '<? x ?>y', '<?xml version="1.0"?>z', '<!DOCTYPE html>d', '<script>alert(1)</script>', "a\0b", '<<a>>', '<a<b>>c', '<p>"quoted"</p>', 'it\'s <i>"x"</i>', "<\n>", '< x', '<1>', 'trail<', '<span style="color:red;">x</span>', "\x08\x08<info>y</info>"];
foreach ($stripInputs as $s) {
    $out['striptags'][] = ['input' => $s, 'output' => strip_tags($s)];
}

file_put_contents(dirname(__DIR__, 3).'/internal/console/testdata/oracle/formatter.json', json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE)."\n");
