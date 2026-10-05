<?php
// Generates the schema validation goldens in
// internal/json/jsonschema/testdata/oracle/ by running the real
// justinrainbow/json-schema 6.10.0 and Composer 2.10.3's JsonFile (from
// .ref/composer) over a generated corpus of composer.json, auth.json and
// composer.lock documents:
//
// schema.json holds one record per document, with:
//
//   errors / e      JsonSchema\Validator::validate($data, $schema) with
//                   json_decode($doc) data and the schema objects JsonFile
//                   builds ("composer", "lock", "auth", "strict"), plus the
//                   repository schema; every error with its pointer,
//                   constraint name, var_export()ed params and context.
//   jsonfile        JsonFile::validateJsonSchema('file.json', $data, $mode)
//                   with json_decode($doc, true) data, as Composer calls it:
//                   true, or the exception message and its errors.
//
// The corpus: for every property path the schemas define (following $ref,
// items, anyOf/oneOf/allOf, additionalProperties and patternProperties),
// documents setting it to values of every JSON type and to strings that
// exercise the patterns, enums and formats; Composer's own composer.json,
// composer.lock and test fixtures; and random mutations of those
// (replaced, deleted and added properties).
//
// Run: php tools/oracle/json/schema.php
$root = dirname(__DIR__, 3);
require $root.'/.ref/composer/vendor/autoload.php';

use Composer\Json\JsonFile;
use Composer\Json\JsonValidationException;
use JsonSchema\Validator;

error_reporting(E_ALL & ~E_DEPRECATED & ~E_WARNING & ~E_NOTICE);
mt_srand(20261005);

$resDir = $root.'/.ref/composer/res';
$composerSchemaFile = 'file://'.$resDir.'/composer-schema.json';
$lockSchemaFile = 'file://'.$resDir.'/composer-lock-schema.json';
$repositorySchemaFile = 'file://'.$resDir.'/composer-repository-schema.json';
$composerSchema = json_decode(file_get_contents($resDir.'/composer-schema.json'));
$lockSchema = json_decode(file_get_contents($resDir.'/composer-lock-schema.json'));
$repositorySchema = json_decode(file_get_contents($resDir.'/composer-repository-schema.json'));

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }

// Values of every JSON type, and strings for the patterns, enums and formats.
function palette(): array
{
    return [
        '', 'foo', 'Foo', 'vendor/package', 'Vendor/Package', 'vendor/-pack__age', 'vendor/pack--age', 'a/b/c',
        '1.0.0', 'v2.0.4-p', 'dev-main', '1.0.x-dev', 'invalid version', '^1.0', '*', 'stable', 'RC', 'devz',
        'https://example.org', 'https://example.org/path?q=1#frag', 'http://example.org', 'ftp://h:99999/x',
        'example.org', 'mailto:a@b.co', 'urn:isbn:123', 'git@github.com:a/b.git', 'file:///tmp/x', '//x',
        'a@b.co', 'john.doe@example.com', 'not an email', 'é@ü.de', '"q"@x.org', 'a@[127.0.0.1]',
        'MIT', '(MIT or GPL-2.0)', 'library', 'project', 'composer-plugin', 'auto', 'dist', 'source', 'proxy',
        'lib-foo', 'php', 'ext-json', '0', '1', '1.5', '-1', 'true', 'null', 'stash', 'ignore', 'report', 'fail',
        'all', 'update', 'install', 'audit', 'block', 'composer', 'vcs', 'path', 'package', 'artifact', 'pear',
        'ssh', 'git', 'https', 'tar', 'zip', 'symlink', 'full', 'php-only', 'prompt', 'critical', 'high',
        'GHSA-xxxx', 'CVE-2024-1', "multi\nline", 'ünïcödé', '{$vendor-dir}/bin', 'src/', 'Foo\\Bar\\',
        0, 1, -1, 2, 300, 1.5, -0.5, 1e20, true, false, null,
        [], ['a'], ['a', 'b'], [1, 2], [true], [null], [[]], [new stdClass()],
        new stdClass(), (object) ['a' => 'b'], (object) ['a' => 1], (object) ['a' => ['b']],
        (object) ['type' => 'composer', 'url' => 'https://example.org'], (object) ['type' => 'vcs'],
        (object) ['name' => 'a/b', 'email' => 'a@b.co'], (object) ['psr-4' => (object) ['A\\' => 'src/']],
        (object) ['url' => 'https://x', 'type' => 'url'], (object) ['block' => true, 'audit' => 'report'],
        (object) ['foo/bar' => '^1.0'], (object) ['0' => 'x'], (object) ['' => 'x'],
    ];
}

function deepCopy($v)
{
    return unserialize(serialize($v));
}

// Resolves a local or cross-file $ref of Composer's schemas.
function resolveRef($ref, $current)
{
    global $composerSchema, $lockSchema, $repositorySchema;
    [$file, $fragment] = array_pad(explode('#', $ref, 2), 2, '');
    $doc = $current;
    if ($file !== '') {
        $doc = strpos($file, 'lock') !== false ? $lockSchema : (strpos($file, 'repository') !== false ? $repositorySchema : $composerSchema);
    }
    $node = $doc;
    foreach (array_filter(explode('/', $fragment), 'strlen') as $part) {
        $node = is_array($node) ? $node[(int) $part] : $node->{str_replace(['~1', '~0'], ['/', '~'], $part)};
    }

    return [$node, $doc];
}

// Collects property paths (lists of string keys and int indexes) of a schema.
function collectPaths($schema, $doc, array $prefix, int $depth, array &$out, array $seen = [])
{
    if ($depth > 6 || !is_object($schema)) {
        return;
    }
    if (isset($schema->{'$ref'}) && is_string($schema->{'$ref'})) {
        if (in_array($schema->{'$ref'}, $seen, true)) {
            return;
        }
        $seen[] = $schema->{'$ref'};
        [$target, $doc] = resolveRef($schema->{'$ref'}, $doc);
        collectPaths($target, $doc, $prefix, $depth, $out, $seen);

        return;
    }
    foreach (['anyOf', 'oneOf', 'allOf'] as $of) {
        if (isset($schema->$of)) {
            foreach ($schema->$of as $sub) {
                collectPaths($sub, $doc, $prefix, $depth, $out, $seen);
            }
        }
    }
    if (isset($schema->properties)) {
        foreach ($schema->properties as $name => $sub) {
            $path = array_merge($prefix, [(string) $name]);
            $out[json_encode($path)] = $path;
            collectPaths($sub, $doc, $path, $depth + 1, $out, $seen);
        }
    }
    if (isset($schema->patternProperties)) {
        foreach ($schema->patternProperties as $pattern => $sub) {
            foreach (['ignore-x', 'ignorefoo', 'x'] as $name) {
                $path = array_merge($prefix, [$name]);
                $out[json_encode($path)] = $path;
                collectPaths($sub, $doc, $path, $depth + 1, $out, $seen);
            }
        }
    }
    if (isset($schema->additionalProperties) && is_object($schema->additionalProperties)) {
        $path = array_merge($prefix, ['x-extra']);
        $out[json_encode($path)] = $path;
        collectPaths($schema->additionalProperties, $doc, $path, $depth + 1, $out, $seen);
    }
    if (isset($schema->items) && is_object($schema->items)) {
        $path = array_merge($prefix, [0]);
        $out[json_encode($path)] = $path;
        collectPaths($schema->items, $doc, $path, $depth + 1, $out, $seen);
    }
}

// Sets $path in the json_decode()d (object) document $doc.
function setPath($doc, array $path, $value)
{
    if ($path === []) {
        return $value;
    }
    $key = array_shift($path);
    if (is_int($key)) {
        if (!is_array($doc)) {
            $doc = [];
        }
        $doc[$key] = setPath($doc[$key] ?? null, $path, $value);
        ksort($doc);

        return array_values($doc);
    }
    if (!is_object($doc)) {
        $doc = new stdClass();
    }
    $doc->$key = setPath($doc->$key ?? null, $path, $value);

    return $doc;
}

// Every path to a value in a json_decode()d document.
function nodePaths($doc, array $prefix = []): array
{
    $paths = [$prefix];
    if (is_object($doc) || is_array($doc)) {
        foreach ($doc as $k => $v) {
            $paths = array_merge($paths, nodePaths($v, array_merge($prefix, [is_array($doc) ? (int) $k : (string) $k])));
        }
    }

    return $paths;
}

function deletePath($doc, array $path)
{
    $key = array_shift($path);
    if ($path === []) {
        if (is_object($doc)) {
            unset($doc->$key);
        } elseif (is_array($doc)) {
            unset($doc[$key]);
            $doc = array_values($doc);
        }

        return $doc;
    }
    if (is_object($doc) && isset($doc->$key)) {
        $doc->$key = deletePath($doc->$key, $path);
    } elseif (is_array($doc) && isset($doc[$key])) {
        $doc[$key] = deletePath($doc[$key], $path);
    }

    return $doc;
}

function mutate($doc, int $count)
{
    $palette = palette();
    for ($i = 0; $i < $count; $i++) {
        $paths = nodePaths($doc);
        $path = pick($paths);
        switch (mt_rand(0, 3)) {
            case 0:
            case 1:
                $doc = setPath($doc, $path, deepCopy(pick($palette)));
                break;
            case 2:
                if ($path !== []) {
                    $doc = deletePath($doc, $path);
                }
                break;
            default:
                $doc = setPath($doc, array_merge($path, [pick(['extra', 'x-extra', 'name', 'type', 'url', '0'])]), deepCopy(pick($palette)));
        }
    }

    return $doc;
}

$minimal = (object) ['name' => 'vendor/package', 'description' => 'description'];

$docs = []; // [schema kind, json]
$add = static function (string $kind, $doc) use (&$docs) {
    $json = json_encode($doc, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRESERVE_ZERO_FRACTION);
    if ($json !== false) {
        $docs[$kind."\0".$json] = [$kind, $json];
    }
};

// Schema-driven documents.
$composerPaths = [];
collectPaths($composerSchema, $composerSchema, [], 0, $composerPaths);
$palette = palette();
foreach ($composerPaths as $path) {
    $values = $palette;
    shuffle($values);
    foreach (array_slice($values, 0, 5) as $value) {
        $add(pick(['composer', 'composer', 'strict']), setPath(deepCopy($minimal), $path, deepCopy($value)));
    }
}
$configPaths = [];
collectPaths($composerSchema->properties->config, $composerSchema, [], 0, $configPaths);
foreach ($configPaths as $path) {
    $values = $palette;
    shuffle($values);
    foreach (array_slice($values, 0, 4) as $value) {
        $add('auth', setPath(new stdClass(), $path, deepCopy($value)));
    }
}
$lockPaths = [];
collectPaths($lockSchema, $lockSchema, [], 0, $lockPaths);
$lockBase = json_decode(file_get_contents($root.'/.ref/composer/composer.lock'));
$lockBase->packages = array_slice($lockBase->packages, 0, 1);
$lockBase->{'packages-dev'} = [];
shuffle($lockPaths);
foreach (array_slice($lockPaths, 0, 250) as $path) {
    $values = $palette;
    shuffle($values);
    foreach (array_slice($values, 0, 2) as $value) {
        $add('lock', setPath(deepCopy($lockBase), $path, deepCopy($value)));
    }
}
$repoPaths = [];
collectPaths($repositorySchema, $repositorySchema, [], 0, $repoPaths);
foreach ($repoPaths as $path) {
    $values = $palette;
    shuffle($values);
    foreach (array_slice($values, 0, 3) as $value) {
        $add('repository', setPath(new stdClass(), $path, deepCopy($value)));
    }
}

// Real documents and their mutations.
$real = [json_decode(file_get_contents($root.'/.ref/composer/composer.json'))];
foreach (new RecursiveIteratorIterator(new RecursiveDirectoryIterator($root.'/.ref/composer/tests')) as $file) {
    if ($file->getFilename() === 'composer.json') {
        $decoded = json_decode(file_get_contents($file->getPathname()));
        if (is_object($decoded)) {
            $real[] = $decoded;
        }
    }
}
foreach ($real as $doc) {
    $add('composer', $doc);
    $add('strict', $doc);
    for ($i = 0; $i < 8; $i++) {
        $add(pick(['composer', 'composer', 'strict']), mutate(deepCopy($doc), mt_rand(1, 3)));
    }
}
$add('lock', $lockBase);
for ($i = 0; $i < 80; $i++) {
    $add('lock', mutate(deepCopy($lockBase), mt_rand(1, 3)));
}
$authBase = json_decode('{"http-basic": {"example.org": {"username": "u", "password": "p"}}, "github-oauth": {"github.com": "abc"}, "gitlab-token": {"gitlab.com": "t"}, "bearer": {"x.org": "t"}, "custom-headers": {"x.org": ["X-A: b"]}, "client-certificate": {"x.org": {"local_cert": "/a"}}, "forgejo-token": {"codeberg.org": {"username": "u", "token": "t"}}}');
$add('auth', $authBase);
for ($i = 0; $i < 80; $i++) {
    $add('auth', mutate(deepCopy($authBase), mt_rand(1, 3)));
}
// Documents of other types than objects.
foreach ([[], ['a'], 'str', 1, 1.5, true, null] as $value) {
    foreach (['composer', 'strict', 'auth', 'lock'] as $kind) {
        $add($kind, $value);
    }
}

function schemaFor(string $kind)
{
    global $composerSchemaFile, $lockSchemaFile, $repositorySchemaFile, $resDir;
    $draft = 'https://json-schema.org/draft-04/schema#';
    switch ($kind) {
        case 'composer':
            return (object) ['$ref' => $composerSchemaFile, '$schema' => $draft];
        case 'lock':
            return (object) ['$ref' => $lockSchemaFile, '$schema' => $draft];
        case 'auth':
            return (object) ['$ref' => $composerSchemaFile.'#/properties/config', '$schema' => $draft];
        case 'repository':
            return (object) ['$ref' => $repositorySchemaFile, '$schema' => $draft];
        case 'strict':
            $schema = json_decode(file_get_contents($resDir.'/composer-schema.json'));
            $schema->additionalProperties = false;
            $schema->required = ['name', 'description'];

            return $schema;
    }
}

$modes = ['composer' => JsonFile::LAX_SCHEMA, 'strict' => JsonFile::STRICT_SCHEMA, 'auth' => JsonFile::AUTH_SCHEMA, 'lock' => JsonFile::LOCK_SCHEMA];

$cases = [];
foreach ($docs as [$kind, $json]) {
    $validator = new Validator();
    $data = json_decode($json);
    try {
        $validator->validate($data, schemaFor($kind));
        $errors = [];
        foreach ($validator->getErrors() as $error) {
            $errors[] = [$error['property'], $error['pointer'], $error['message'], $error['constraint']['name'], var_export($error['constraint']['params'], true), $error['context']];
        }
        $case = ['schema' => $kind, 'doc' => $json, 'errors' => $errors];
    } catch (\Throwable $e) {
        $case = ['schema' => $kind, 'doc' => $json, 'e' => $e->getMessage()];
    }

    if (isset($modes[$kind])) {
        try {
            JsonFile::validateJsonSchema('file.json', json_decode($json, true), $modes[$kind]);
            $result = true;
        } catch (JsonValidationException $e) {
            $result = ['message' => $e->getMessage(), 'errors' => $e->getErrors()];
        } catch (\Throwable $e) {
            $result = ['e' => $e->getMessage()];
        }
        $case['jsonfile'] = $result;
    }
    $cases[] = $case;
}

$out = $root.'/internal/json/jsonschema/testdata/oracle';
@mkdir($out, 0777, true);
$lines = [];
foreach ($cases as $case) {
    $lines[] = json_encode($case, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
}
file_put_contents($out.'/schema.json', "[\n".implode(",\n", $lines)."\n]\n");
fprintf(STDERR, "%d cases\n", count($cases));
