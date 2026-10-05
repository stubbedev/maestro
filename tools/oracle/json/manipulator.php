<?php
// Generates internal/json/testdata/manipulator/oracle.json.gz: the real
// Composer\Json\JsonManipulator run over thousands of generated
// composer.json documents (varied indentation, newlines, spacing, one-line
// nodes, escapes, assoc and list repositories, empty nodes, a few invalid
// and a few large documents) with sequences of operations on varied
// arguments. Each step records the result (or exception) and the contents
// afterwards when they changed; internal/json/manipulator_replay_test.go
// replays them.
//
// Run: php tools/oracle/json/manipulator.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';
require __DIR__.'/manipulator_common.php';

use Composer\Json\JsonManipulator;

mt_srand(20261005);

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }
function chance(int $percent): bool { return mt_rand(1, 100) <= $percent; }

const PACKAGES = ['php', 'php-64bit', 'ext-json', 'ext-mbstring', 'lib-icu', 'hhvm', 'composer-plugin-api', 'composer',
    'vendor/a', 'vendor/b', 'Vendor/B', 'foo/bar', 'foo/bar-baz', 'x/y.z', 'a/b', 'monolog/monolog', 'symfony/console',
    'psr/log', 'zeta/z', 'alpha/a1', 'alpha/a10', 'alpha/a2', 'foo/*', 'ext-foo', '0', 'foo bar/baz', 'é/ü'];
const CONSTRAINTS = ['*', '^1.0', '~2.0', '>=7.4', '^1.0 || ^2.0', 'dev-main', '1.0.*@dev', '$1', '\\', '\\1', 'a"b', '^8.1', ''];
const LINK_TYPES = ['require', 'require-dev', 'conflict', 'provide', 'replace', 'suggest', 'new-type'];
const MAIN_KEYS = ['name', 'description', 'require', 'require-dev', 'repositories', 'config', 'extra', 'scripts', 'suggest',
    'autoload', 'autoload-dev', 'minimum-stability', 'homepage', 'license', 'keywords', 'authors', 'foo', '0', '', 'a.b',
    'prefer-stable', 'support', 'policy', 'with space', 'é'];
const REPO_NAMES = ['', 'packagist.org', 'packagist', 'foo', 'bar', 'baz', 'my-repo', '0', '1', 'a/b', 'Foo', '1.5'];
const URLS = ['https://example.org', 'https://repo.packagist.org', 'http://x/y', '../path', 'file:///tmp/$1', 'https://a\\b'];
const CONFIG_NAMES = ['sort-packages', 'platform.php', 'platform.ext-foo', 'preferred-install', 'preferred-install.foo/*',
    'github-oauth.github.com', 'http-basic.example.org', 'allow-plugins', 'allow-plugins.foo/bar', 'policy',
    'policy.advisories', 'policy.advisories.block', 'policy.custom.ignore', 'policy.malware.block-scope', 'policy.a.b.c',
    'process-timeout', 'vendor-dir', 'foo.bar', 'a.b.c', '0', 'bin-dir'];
const PROPERTY_NAMES = ['extra.foo', 'extra.foo.bar', 'extra.branch-alias', 'extra.branch-alias.dev-main', 'scripts.test',
    'scripts.post-install-cmd', 'suggest.x/y', 'suggest.vendor/a', 'homepage', 'minimum-stability', 'name', 'autoload.psr-4',
    'autoload-dev.classmap', 'autoload.psr-4.Foo\\', 'description', 'extra', 'scripts', 'license'];
const SUB_NODES = ['require', 'require-dev', 'config', 'extra', 'scripts', 'suggest', 'autoload', 'repositories', 'foo', 'name'];
const SUB_NAMES = ['vendor/a', 'foo/bar', 'foo', 'bar', 'foo.bar', 'branch-alias.dev-main', 'psr-4', 'platform.php', '0',
    'test', 'packagist.org', 'a/b', 'Foo\\Bar\\', 'x.y.z'];
const LIST_NODES = ['repositories', 'keywords', 'license', 'authors', 'scripts', 'require', 'missing', 'name'];

function str(): string
{
    return pick(['foo', 'bar', 'Some ñ text', '€', 'a/b', '$1', '\\', '"q"', '', 'line'."\n".'break', 'tab'."\t", '</script>',
        '${1}', '\\\\1', '0', '1.0', 'x', "\u{1F600}"]);
}

function value(int $depth = 0)
{
    $r = mt_rand(0, $depth > 1 ? 6 : 11);
    switch ($r) {
        case 0: case 1: return str();
        case 2: return pick([0, 1, -5, 42, 9000, PHP_INT_MAX]);
        case 3: return pick([1.5, 0.1, -0.0, 1e20, 3.0, 2.5e-5]);
        case 4: return pick([true, false]);
        case 5: return null;
        case 6: return pick(PACKAGES);
        case 7: case 8:
            $n = mt_rand(0, 3);
            $out = [];
            for ($i = 0; $i < $n; $i++) {
                $out[] = value($depth + 1);
            }

            return $out;
        case 9: case 10:
            $n = mt_rand(0, 3);
            $out = [];
            for ($i = 0; $i < $n; $i++) {
                $out[pick(array_merge(SUB_NAMES, PACKAGES, ['type', 'url', 'name']))] = value($depth + 1);
            }

            return $out;
        default:
            $o = new stdClass();
            if (chance(50)) {
                $o->{pick(['a', 'foo', '0', 'url'])} = value($depth + 1);
            }

            return chance(20) ? new ArrayObject((array) $o) : $o;
    }
}

function repoConfig()
{
    if (chance(15)) {
        return false;
    }
    $c = ['type' => pick(['composer', 'vcs', 'path', 'package']), 'url' => pick(URLS)];
    if (chance(20)) {
        $c = ['url' => pick(URLS)] + $c;
    }
    if (chance(15)) {
        $c['name'] = pick(REPO_NAMES);
    }
    if (chance(15)) {
        $c['options'] = ['symlink' => chance(50)];
    }
    if (chance(10)) {
        $c['package'] = ['name' => 'a/b', 'version' => '1.0', 'require' => ['php' => '>=7']];
    }

    return $c;
}

/**
 * A random composer.json-like document as a PHP value (objects as stdClass).
 */
function document()
{
    $doc = new stdClass();
    $keys = MAIN_KEYS;
    shuffle($keys);
    foreach (array_slice($keys, 0, mt_rand(0, 8)) as $key) {
        switch ($key) {
            case 'require': case 'require-dev': case 'suggest':
                $o = new stdClass();
                for ($i = mt_rand(0, 4); $i > 0; $i--) {
                    $o->{pick(PACKAGES)} = pick(CONSTRAINTS);
                }
                $doc->$key = $o;
                break;
            case 'repositories':
                if (chance(50)) {
                    $list = [];
                    for ($i = mt_rand(0, 4); $i > 0; $i--) {
                        $list[] = chance(15) ? (object) [pick(REPO_NAMES) => false] : (object) repoConfig();
                    }
                    $doc->$key = $list;
                } else {
                    $o = new stdClass();
                    for ($i = mt_rand(0, 4); $i > 0; $i--) {
                        $c = repoConfig();
                        $o->{pick(REPO_NAMES)} = is_array($c) ? (object) $c : $c;
                    }
                    $doc->$key = $o;
                }
                break;
            case 'config':
                $o = new stdClass();
                foreach (array_slice(CONFIG_NAMES, mt_rand(0, 15), mt_rand(0, 5)) as $name) {
                    $bits = explode('.', $name);
                    $cur = $o;
                    foreach (array_slice($bits, 0, -1) as $bit) {
                        if (!isset($cur->$bit) || !$cur->$bit instanceof stdClass) {
                            $cur->$bit = new stdClass();
                        }
                        $cur = $cur->$bit;
                    }
                    $cur->{end($bits)} = chance(60) ? value(1) : new stdClass();
                }
                $doc->$key = $o;
                break;
            case 'extra': case 'scripts': case 'autoload': case 'autoload-dev': case 'policy':
                $o = new stdClass();
                for ($i = mt_rand(0, 3); $i > 0; $i--) {
                    $o->{pick(SUB_NAMES)} = chance(30) ? (object) [pick(SUB_NAMES) => value(2)] : value(1);
                }
                $doc->$key = $o;
                break;
            case 'keywords': case 'license': case 'authors':
                $list = [];
                for ($i = mt_rand(0, 3); $i > 0; $i--) {
                    $list[] = $key === 'authors' ? (object) ['name' => str()] : str();
                }
                $doc->$key = $list;
                break;
            default:
                $doc->$key = value(1);
        }
    }

    return $doc;
}

/**
 * Serializes a value as JSON in one of many formatting styles.
 */
class Writer
{
    public $indent;
    public $nl;
    public $colon;
    public $oneLineDepth;
    public $escapeSlashes;
    public $escapeUnicode;
    public $emptyObject;
    public $listSpace;

    public function __construct()
    {
        $this->indent = pick(['    ', '    ', '  ', "\t", '   ', '        ']);
        $this->nl = chance(20) ? "\r\n" : "\n";
        $this->colon = pick([': ', ': ', ':', ' : ', ":\t"]);
        $this->oneLineDepth = pick([99, 99, 99, 2, 1, 0]);
        $this->escapeSlashes = chance(15);
        $this->escapeUnicode = chance(15);
        $this->emptyObject = pick(['{}', '{}', '{ }', null]);
        $this->listSpace = pick([' ', '', ' ']);
    }

    public function scalar($v): string
    {
        $flags = JSON_PRESERVE_ZERO_FRACTION;
        if (!$this->escapeSlashes) {
            $flags |= JSON_UNESCAPED_SLASHES;
        }
        if (!$this->escapeUnicode) {
            $flags |= JSON_UNESCAPED_UNICODE;
        }

        return json_encode($v, $flags);
    }

    public function write($v, int $depth = 0): string
    {
        if ($v instanceof ArrayObject) {
            $v = (object) (array) $v;
        }
        if ($v instanceof stdClass) {
            $props = (array) $v;
            if (!$props) {
                return $this->emptyObject ?? '{'.$this->nl.str_repeat($this->indent, $depth).'}';
            }
            $parts = [];
            foreach ($props as $k => $item) {
                $parts[] = $this->scalar((string) $k).$this->colon.$this->write($item, $depth + 1);
            }
            if ($depth >= $this->oneLineDepth) {
                return '{'.$this->listSpace.implode(','.$this->listSpace, $parts).$this->listSpace.'}';
            }
            $pad = str_repeat($this->indent, $depth + 1);

            return '{'.$this->nl.$pad.implode(','.$this->nl.$pad, $parts).$this->nl.str_repeat($this->indent, $depth).'}';
        }
        if (is_array($v)) {
            if (!$v) {
                return '[]';
            }
            $parts = [];
            foreach ($v as $item) {
                $parts[] = $this->write($item, $depth + 1);
            }
            if ($depth >= $this->oneLineDepth || chance(50)) {
                return '['.implode(','.$this->listSpace, $parts).']';
            }
            $pad = str_repeat($this->indent, $depth + 1);

            return '['.$this->nl.$pad.implode(','.$this->nl.$pad, $parts).$this->nl.str_repeat($this->indent, $depth).']';
        }

        return $this->scalar($v);
    }
}

function documentText(): string
{
    $w = new Writer();
    $text = $w->write(document());
    if (chance(10)) {
        $text = pick(["\n", "  ", "\r\n", "\n\n"]).$text;
    }
    if (chance(30)) {
        $text .= pick(["\n", "\r\n", "  \n", "\n\n"]);
    }
    if (chance(3)) {
        $text = pick(['', '{}', '{ }', "{\n}", '[]', '{"a": 1,}', '{"a" 1}', "{\n    \"a\": 'x'\n}", '{"a": {"b": 1}']);
    }
    if (chance(2)) {
        // un-regexable content: a value the DEFINES grammar rejects
        $text = substr($text, 0, -1).(strlen($text) > 2 ? ',' : '').'"z": 01}';
    }

    return $text;
}

function largeDocument(): string
{
    $doc = new stdClass();
    $doc->name = 'big/project';
    $doc->description = str_repeat('A long description ñ \\u00f1. ', 50);
    $doc->require = new stdClass();
    for ($i = 0; $i < 400; $i++) {
        $doc->require->{"vendor$i/package-$i"} = '^'.($i % 7).'.'.($i % 13);
    }
    $doc->repositories = [];
    for ($i = 0; $i < 150; $i++) {
        $doc->repositories[] = (object) ['type' => 'vcs', 'url' => "https://github.com/vendor$i/package-$i"];
    }
    $doc->extra = (object) ['data' => array_fill(0, 300, 'x\\"y')];
    $doc->config = (object) ['platform' => (object) ['php' => '8.1']];

    return (new Writer())->write($doc);
}

/**
 * A key of the array at $path in the current document 60% of the time (so
 * removals and lookups hit), else one from $pool.
 */
function key_($cur, array $path, array $pool)
{
    foreach ($path as $bit) {
        $cur = is_array($cur) ? ($cur[$bit] ?? null) : null;
    }
    if (is_array($cur) && $cur && chance(60)) {
        $k = (string) pick(array_keys($cur));
        if (is_array($cur[$k] ?? null) && isset($cur[$k]['name']) && is_string($cur[$k]['name']) && chance(50)) {
            return $cur[$k]['name'];
        }

        return $k;
    }

    return pick($pool);
}

function operation($cur): array
{
    switch (mt_rand(0, 21)) {
        case 0: case 1: case 2: return ['addLink', [pick(LINK_TYPES), pick(PACKAGES), pick(CONSTRAINTS), chance(30)]];
        case 3: return ['addRepository', [pick(REPO_NAMES), repoConfig(), chance(70)]];
        case 4: return ['setRepositoryUrl', [key_($cur, ['repositories'], REPO_NAMES), pick(URLS)]];
        case 5: return ['insertRepository', [pick(REPO_NAMES), repoConfig(), key_($cur, ['repositories'], REPO_NAMES), pick([0, 0, 1, -1, 2])]];
        case 6: return ['removeRepository', [key_($cur, ['repositories'], REPO_NAMES)]];
        case 7: case 8: return ['addConfigSetting', [pick(CONFIG_NAMES), value(1)]];
        case 9: return ['removeConfigSetting', [chance(50) ? key_($cur, ['config'], CONFIG_NAMES) : pick(CONFIG_NAMES)]];
        case 10: return ['addProperty', [pick(PROPERTY_NAMES), value(1)]];
        case 11: return ['removeProperty', [chance(40) ? key_($cur, [], MAIN_KEYS) : pick(PROPERTY_NAMES)]];
        case 12: return ['addSubNode', [$node = key_($cur, [], SUB_NODES), key_($cur, [$node], SUB_NAMES), value(1), chance(70)]];
        case 13: return ['removeSubNode', [$node = key_($cur, [], SUB_NODES), key_($cur, [$node], array_merge(SUB_NAMES, PACKAGES))]];
        case 14: return ['addListItem', [pick(LIST_NODES), value(1), chance(70)]];
        case 15: return ['insertListItem', [key_($cur, [], LIST_NODES), value(1), pick([0, 1, 2, 3, -1])]];
        case 16: return ['removeListItem', [key_($cur, [], LIST_NODES), pick([0, 1, 2, 3, -1])]];
        case 17: return ['addMainKey', [pick(MAIN_KEYS), value()]];
        case 18: return ['removeMainKey', [key_($cur, [], MAIN_KEYS)]];
        case 19: return ['changeEmptyMainKeyFromAssocToList', [key_($cur, [], MAIN_KEYS)]];
        case 20: return ['removeMainKeyIfEmpty', [key_($cur, [], MAIN_KEYS)]];
        default: return ['format', [value(), mt_rand(0, 2), chance(30)]];
    }
}

function runCase(string $text, int $ops): array
{
    $steps = [['op' => 'new', 'contents' => enc($text)]];
    try {
        $m = new JsonManipulator($text);
    } catch (Throwable $e) {
        $steps[0]['result'] = encException($e);

        return $steps;
    }
    $contents = $m->getContents();
    for ($i = 0; $i < $ops; $i++) {
        [$method, $args] = operation(json_decode($contents, true));
        $step = ['op' => 'call', 'method' => $method, 'args' => array_map('enc', $args)];
        try {
            $step['result'] = enc($m->$method(...$args));
        } catch (Throwable $e) {
            $step['result'] = encException($e);
        }
        if ($m->getContents() !== $contents) {
            $contents = $m->getContents();
            $step['contents'] = enc($contents);
        }
        $steps[] = $step;
    }

    return $steps;
}

$cases = [];
for ($n = 0; $n < 3000; $n++) {
    $cases[] = runCase(documentText(), mt_rand(1, 4));
}
for ($n = 0; $n < 6; $n++) {
    $cases[] = runCase(largeDocument(), 3);
}

writeJson(dirname(__DIR__, 3).'/internal/json/testdata/manipulator/oracle.json.gz', $cases);
fprintf(STDERR, "%d cases\n", count($cases));
