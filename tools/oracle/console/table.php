<?php
// Generates internal/console/testdata/oracle/table.json:
//  - "provider", "setTitle", "horizontal": the data providers of Symfony's
//    Tests/Helper/TableTest.php (renderProvider, renderSetTitle,
//    provideRenderHorizontalTests) with their expected output, extracted
//    verbatim so the Go tests run every case;
//  - "oracle": many more tables rendered by the real Table helper.
// Cell values are encoded as: string, null, {"i":int}, {"f":float},
// {"b":bool}, {"sep":true} (TableSeparator), {"cell":value, "rowspan",
// "colspan", "style":{fg,bg,options,align,cellFormat}|null} (TableCell),
// or a JSON array (a row or a nested array).
// Run: php tools/oracle/console/table.php
namespace PHPUnit\Framework {
    if (!class_exists(TestCase::class)) {
        abstract class TestCase
        {
        }
    }
}

namespace {
    require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';
    require dirname(__DIR__, 3).'/.ref/symfony-console/Tests/Helper/TableTest.php';

    use Symfony\Component\Console\Helper\Table;
    use Symfony\Component\Console\Helper\TableCell;
    use Symfony\Component\Console\Helper\TableCellStyle;
    use Symfony\Component\Console\Helper\TableSeparator;
    use Symfony\Component\Console\Helper\TableStyle;
    use Symfony\Component\Console\Output\StreamOutput;
    use Symfony\Component\Console\Tests\Helper\TableTest;

    error_reporting(E_ALL & ~E_WARNING & ~E_DEPRECATED);
    putenv('COLORTERM');
    putenv('TERMINAL_EMULATOR');
    putenv('KONSOLE_VERSION');
    unset($_SERVER['IDEA_INITIAL_DIRECTORY']);

    function enc($v)
    {
        if (null === $v || is_string($v)) {
            return $v;
        }
        if (is_int($v)) {
            return ['i' => $v];
        }
        if (is_float($v)) {
            return ['f' => $v];
        }
        if (is_bool($v)) {
            return ['b' => $v];
        }
        if ($v instanceof TableSeparator) {
            return ['sep' => true, 'rowspan' => $v->getRowspan(), 'colspan' => $v->getColspan()];
        }
        if ($v instanceof TableCell) {
            $style = $v->getStyle();

            return ['cell' => (string) $v, 'rowspan' => $v->getRowspan(), 'colspan' => $v->getColspan(), 'style' => $style ? $style->getOptions() : null];
        }
        if (is_array($v)) {
            if (!array_is_list($v)) {
                throw new \LogicException('non-list array');
            }

            return array_map('enc', $v);
        }
        throw new \LogicException('cannot encode '.get_debug_type($v));
    }

    // Custom styles, registered identically by the Go test.
    Table::setStyleDefinition('oracle-dots', (new TableStyle())->setPaddingChar('.')->setPadType(\STR_PAD_BOTH));
    Table::setStyleDefinition('oracle-colored', (new TableStyle())->setBorderFormat('<comment>%s</comment>')->setCellRowContentFormat('[%s]')->setCellHeaderFormat('<error>%s</error>')->setCellRowFormat('<info>%s</info>'));
    Table::setStyleDefinition('oracle-zero', (new TableStyle())->setHorizontalBorderChars('0')->setVerticalBorderChars('')->setDefaultCrossingChar(''));
    Table::setStyleDefinition('oracle-left', (new TableStyle())->setPadType(\STR_PAD_LEFT)->setHeaderTitleFormat('<info>[%s]</info>')->setFooterTitleFormat('%s'));

    $out = ['provider' => [], 'setTitle' => [], 'horizontal' => [], 'oracle' => []];

    foreach (TableTest::renderProvider() as $name => $c) {
        $out['provider'][] = ['name' => (string) $name, 'headers' => enc($c[0]), 'rows' => enc($c[1]), 'style' => $c[2], 'expected' => $c[3], 'decorated' => $c[4] ?? false];
    }
    foreach (TableTest::renderSetTitle() as $name => $c) {
        $out['setTitle'][] = ['name' => (string) $name, 'headerTitle' => $c[0], 'footerTitle' => $c[1], 'style' => $c[2], 'expected' => $c[3]];
    }
    foreach (TableTest::provideRenderHorizontalTests() as $name => $c) {
        $out['horizontal'][] = ['name' => (string) $name, 'headers' => enc($c[0]), 'rows' => enc($c[1]), 'expected' => $c[2]];
    }

    $books = [
        ['99921-58-10-7', 'Divine Comedy', 'Dante Alighieri'],
        ['9971-5-0210-0', 'A Tale of Two Cities', 'Charles Dickens'],
        new TableSeparator(),
        ['960-425-059-0', 'The Lord of the Rings', 'J. R. R. Tolkien'],
    ];
    $bookHeaders = ['ISBN', 'Title', 'Author'];
    $cs = function (array $o) { return new TableCellStyle($o); };

    $datasets = [
        'books' => [$bookHeaders, $books],
        'no headers' => [[], $books],
        'headers only' => [$bookHeaders, []],
        'empty' => [[], []],
        'multi header rows' => [[['A', 'B'], [new TableCell('AB', ['colspan' => 2])]], [['1', '2']]],
        'unicode' => [['名前', 'Ünïcödé'], [['日本語テキスト', 'ok'], ['😀 emoji', 'ｆｕｌｌ'], ["e\u{0301}x", "\u{200B}zw"], ['■■', 'Ｈｅｌｌｏ']]],
        'scalars' => [['int', 'float', 'bool', 'null'], [[1, 1.5, true, null], [-42, 0.1 + 0.2, false, ''], [PHP_INT_MAX, 1e25, true, '0']]],
        'tags' => [['<info>Info</info>', '<comment>C</comment>'], [['<error>err</error>', '<fg=red;bg=blue>rb</>'], ['<href=https://example.com>link</>', 'a \<b\> c'], ['<info>multi'."\n".'line</info>', 'x']]],
        'colspan' => [['A', 'B', 'C', 'D'], [
            [new TableCell('spans two', ['colspan' => 2]), 'c', 'd'],
            ['a', new TableCell('spans three columns wide text', ['colspan' => 3])],
            [new TableCell('all four', ['colspan' => 4])],
            new TableSeparator(),
            ['a', 'b', new TableCell('cd', ['colspan' => 2])],
        ]],
        'rowspan' => [['A', 'B', 'C'], [
            [new TableCell('r3', ['rowspan' => 3]), 'b1', 'c1'],
            ['b2', 'c2'],
            ['b3', 'c3'],
            ['a4', new TableCell('r2', ['rowspan' => 2]), 'c4'],
            ['a5', 'c5'],
        ]],
        'rowspan colspan' => [['A', 'B', 'C', 'D'], [
            [new TableCell("multi\nline\ncell", ['rowspan' => 2, 'colspan' => 2]), 'c', 'd'],
            ['c2', 'd2'],
            new TableSeparator(),
            ['a', new TableCell("x\ny", ['rowspan' => 3]), new TableCell('wide', ['colspan' => 2])],
        ]],
        'newlines' => [['Head'."\n".'er', 'B'], [["line1\nline2\nline3", 'b'], ["crlf\r\nline", "trail\\\nx"], ['', "\n"]]],
        'separator cells' => [['A', 'B', 'C'], [['a', new TableSeparator(), 'c'], [new TableSeparator(['colspan' => 2]), 'x']]],
        'cell styles' => [['A', 'B', 'C'], [
            [new TableCell('left', ['style' => $cs(['align' => 'left'])]), new TableCell('center', ['style' => $cs(['align' => 'center', 'fg' => 'red'])]), new TableCell('right', ['style' => $cs(['align' => 'right', 'bg' => 'green', 'options' => 'bold'])])],
            [new TableCell('<info>tagged</info>', ['style' => $cs(['align' => 'right'])]), new TableCell('fmt', ['style' => $cs(['cellFormat' => '<comment>%s</comment>'])]), new TableCell('a longer value here', ['style' => $cs(['fg' => 'cyan', 'options' => 'bold,underscore'])])],
            [new TableCell("multi\nline", ['style' => $cs(['align' => 'center'])]), 'x', new TableCell('span', ['colspan' => 1, 'style' => $cs(['align' => 'center'])])],
        ]],
        'long' => [['Description'], [['Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore'], ['short']]],
    ];

    $styles = ['default', 'borderless', 'compact', 'symfony-style-guide', 'box', 'box-double', 'oracle-dots', 'oracle-colored', 'oracle-zero', 'oracle-left'];

    $cases = [];
    foreach ($datasets as $dname => [$headers, $rows]) {
        foreach ($styles as $style) {
            foreach ([false, true] as $decorated) {
                $cases[] = ['name' => "$dname/$style", 'headers' => $headers, 'rows' => $rows, 'style' => $style, 'decorated' => $decorated];
            }
        }
    }
    foreach (['books', 'unicode', 'colspan', 'rowspan', 'tags', 'long', 'cell styles', 'newlines'] as $dname) {
        [$headers, $rows] = $datasets[$dname];
        foreach ([[0 => 5], [0 => 3, 1 => 8], [1 => 10, 2 => 4], [0 => 1, 1 => 1, 2 => 1, 3 => 1]] as $mw) {
            foreach ([false, true] as $decorated) {
                $cases[] = ['name' => "$dname/maxwidth", 'headers' => $headers, 'rows' => $rows, 'style' => 'default', 'decorated' => $decorated, 'maxWidths' => $mw];
            }
        }
        $cases[] = ['name' => "$dname/columnwidths", 'headers' => $headers, 'rows' => $rows, 'style' => 'box', 'columnWidths' => [20, 0, 3, 12]];
        $cases[] = ['name' => "$dname/horizontal", 'headers' => $headers, 'rows' => $rows, 'style' => 'default', 'horizontal' => true];
        $cases[] = ['name' => "$dname/horizontal box-double", 'headers' => $headers, 'rows' => $rows, 'style' => 'box-double', 'horizontal' => true, 'decorated' => true];
        $cases[] = ['name' => "$dname/column styles", 'headers' => $headers, 'rows' => $rows, 'style' => 'default', 'columnStyles' => [1 => 'oracle-dots', 2 => 'box']];
    }
    $titles = [null, '', '0', 'Books', 'A very long title that will certainly not fit in the border of this table', '<info>Styled</info>', '日本語のタイトル', "multi\nline"];
    foreach (['books', 'no headers', 'unicode', 'empty'] as $dname) {
        [$headers, $rows] = $datasets[$dname];
        foreach (['default', 'box-double', 'compact', 'oracle-left'] as $style) {
            foreach ($titles as $ht) {
                foreach ([null, 'Footer', ''] as $ft) {
                    $cases[] = ['name' => "$dname/titles", 'headers' => $headers, 'rows' => $rows, 'style' => $style, 'decorated' => 'box-double' === $style, 'headerTitle' => $ht, 'footerTitle' => $ft];
                }
            }
        }
    }
    $cases[] = ['name' => 'horizontal title header', 'headers' => [new TableCell('Title', ['colspan' => 2]), 'b', 'c'], 'rows' => [['x', 'y'], ['1'], new TableSeparator(), ['p', 'q', 'r']], 'style' => 'default', 'horizontal' => true];
    $cases[] = ['name' => 'invalid cell', 'headers' => ['A', 'B'], 'rows' => [['a', ['nested']]], 'style' => 'default'];

    foreach ($cases as $c) {
        $stream = fopen('php://memory', 'r+');
        $output = new StreamOutput($stream, StreamOutput::VERBOSITY_NORMAL, $c['decorated'] ?? false);
        $table = new Table($output);
        $case = [
            'name' => $c['name'],
            'headers' => enc($c['headers']),
            'rows' => enc($c['rows']),
            'style' => $c['style'],
            'decorated' => $c['decorated'] ?? false,
            'maxWidths' => (object) ($c['maxWidths'] ?? []),
            'columnWidths' => $c['columnWidths'] ?? [],
            'columnStyles' => (object) ($c['columnStyles'] ?? []),
            'horizontal' => $c['horizontal'] ?? false,
            'headerTitle' => $c['headerTitle'] ?? null,
            'footerTitle' => $c['footerTitle'] ?? null,
        ];
        try {
            $table->setHeaders($c['headers'])->setRows($c['rows'])->setStyle($c['style']);
            foreach ($c['maxWidths'] ?? [] as $col => $w) {
                $table->setColumnMaxWidth($col, $w);
            }
            if (isset($c['columnWidths'])) {
                $table->setColumnWidths($c['columnWidths']);
            }
            foreach ($c['columnStyles'] ?? [] as $col => $s) {
                $table->setColumnStyle($col, $s);
            }
            $table->setHorizontal($case['horizontal']);
            $table->setHeaderTitle($case['headerTitle']);
            $table->setFooterTitle($case['footerTitle']);
            $table->render();
            rewind($stream);
            $case['output'] = stream_get_contents($stream);
        } catch (\Throwable $e) {
            $case['error'] = $e->getMessage();
        }
        $out['oracle'][] = $case;
    }

    file_put_contents(dirname(__DIR__, 3).'/internal/console/testdata/oracle/table.json', json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRESERVE_ZERO_FRACTION)."\n");
}
