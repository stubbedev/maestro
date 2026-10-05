<?php

/*
 * Reflects Composer's public API for the plugin shim's presence parity
 * (docs/PLUGINS.md D7, §9.2). As plugins-survey/tools/apiindex.php does,
 * it loads every class of Composer 2.10.3's src/ and records, per class:
 * kind, flags, parent, interfaces, traits, public and protected constants
 * and properties with their values, and every public or protected method
 * declared by the class with its exact signature. Values and defaults are
 * rendered as PHP source, so tools/shimgen writes stubs from this output
 * and the parity test compares the shim's reflection with it verbatim.
 *
 * Usage:
 *   php reflect.php ref <composer checkout>          Composer's src/
 *   php reflect.php shim <extracted shim root>      the shim's Composer classes
 *
 * Writes JSON to stdout.
 */

error_reporting(-1);
ini_set('display_errors', 'stderr');

$mode = $argv[1] ?? '';
$root = rtrim($argv[2] ?? '', '/');

$classes = [];
if ($mode === 'ref') {
    require $root.'/vendor/autoload.php';
    $src = $root.'/src/';
    $it = new RecursiveIteratorIterator(new RecursiveDirectoryIterator($src, FilesystemIterator::SKIP_DOTS));
    foreach ($it as $file) {
        $path = (string) $file;
        if (substr($path, -4) !== '.php' || $path === $src.'bootstrap.php') {
            continue;
        }
        $class = str_replace(['/', '.php'], ['\\', ''], substr($path, strlen($src)));
        // Compiler builds the phar and PHPStan holds static analysis rules:
        // neither is API (docs/PLUGINS.md D7).
        if ($class === 'Composer\\Compiler' || strpos($class, 'Composer\\PHPStan\\') === 0) {
            continue;
        }
        $classes[] = $class;
    }
} elseif ($mode === 'shim') {
    require $root.'/src/Maestro/Shim/Autoloader.php';
    \Maestro\Shim\Autoloader::register($root);
    $index = require $root.'/autoload.php';
    foreach ($index['classmap'] as $class => $file) {
        if (strpos($class, 'Composer\\') === 0 && (strpos($file, 'src/') === 0 || strpos($file, 'stubs/') === 0)) {
            $classes[] = $class;
        }
    }
} else {
    fwrite(STDERR, "usage: php reflect.php ref|shim <root>\n");
    exit(2);
}

sort($classes);

$out = [];
foreach ($classes as $class) {
    if (!class_exists($class) && !interface_exists($class) && !trait_exists($class)) {
        fwrite(STDERR, "not loadable: $class\n");
        exit(1);
    }
    $out[$class] = reflectClass(new ReflectionClass($class));
}

echo json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRESERVE_ZERO_FRACTION | JSON_THROW_ON_ERROR), "\n";

function reflectClass(ReflectionClass $r): array
{
    $parent = $r->getParentClass();
    $interfaces = $r->getInterfaceNames();
    sort($interfaces);

    $c = [
        'kind' => $r->isInterface() ? 'interface' : ($r->isTrait() ? 'trait' : 'class'),
        'abstract' => !$r->isInterface() && !$r->isTrait() && $r->isAbstract(),
        'final' => $r->isFinal(),
        'parent' => $parent ? $parent->getName() : null,
        'interfaces' => $interfaces,
        'directInterfaces' => directInterfaces($r),
        'traits' => $r->getTraitNames(),
        'constants' => [],
        'properties' => [],
        'methods' => [],
    ];

    foreach ($r->getReflectionConstants() as $const) {
        if ($const->getDeclaringClass()->getName() !== $r->getName() || $const->isPrivate()) {
            continue;
        }
        $c['constants'][$const->getName()] = [
            'visibility' => $const->isProtected() ? 'protected' : 'public',
            'value' => export($const->getValue()),
        ];
    }
    ksort($c['constants']);

    $defaults = $r->getDefaultProperties();
    foreach ($r->getProperties() as $prop) {
        if ($prop->getDeclaringClass()->getName() !== $r->getName() || $prop->isPrivate() || fromTrait($r, $prop->getName(), 'property')) {
            continue;
        }
        $c['properties'][$prop->getName()] = [
            'visibility' => $prop->isProtected() ? 'protected' : 'public',
            'static' => $prop->isStatic(),
            'type' => typeString($prop->getType()),
            'default' => array_key_exists($prop->getName(), $defaults) ? export($defaults[$prop->getName()]) : null,
        ];
    }
    ksort($c['properties']);

    foreach ($r->getMethods() as $m) {
        if ($m->getDeclaringClass()->getName() !== $r->getName() || $m->isPrivate() || fromTrait($r, $m->getName(), 'method')) {
            continue;
        }
        $c['methods'][$m->getName()] = reflectMethod($m, $r);
    }
    ksort($c['methods']);

    // Maps stay JSON objects when empty.
    foreach (['constants', 'properties', 'methods'] as $k) {
        $c[$k] = (object) $c[$k];
    }

    return $c;
}

/**
 * The interfaces a class or interface declares itself: all of them minus
 * those its parent and its other interfaces already bring.
 */
function directInterfaces(ReflectionClass $r): array
{
    $all = $r->getInterfaceNames();
    $inherited = [];
    if ($parent = $r->getParentClass()) {
        $inherited = $parent->getInterfaceNames();
    }
    foreach ($all as $i) {
        $inherited = array_merge($inherited, (new ReflectionClass($i))->getInterfaceNames());
    }
    foreach ($r->getTraitNames() as $t) {
        $inherited = array_merge($inherited, (new ReflectionClass($t))->getInterfaceNames());
    }
    $direct = array_values(array_diff($all, $inherited));
    sort($direct);

    return $direct;
}

/**
 * Whether a member comes from one of the class's traits rather than its
 * own body.
 */
function fromTrait(ReflectionClass $r, string $name, string $kind): bool
{
    foreach ($r->getTraits() as $t) {
        if ($kind === 'method' ? $t->hasMethod($name) : $t->hasProperty($name)) {
            if ($kind === 'method') {
                return $r->getMethod($name)->getFileName() !== $r->getFileName();
            }

            return true;
        }
    }

    return false;
}

function reflectMethod(ReflectionMethod $m, ReflectionClass $r): array
{
    $params = [];
    foreach ($m->getParameters() as $p) {
        $params[] = [
            'name' => $p->getName(),
            'type' => typeString($p->getType()),
            'byRef' => $p->isPassedByReference(),
            'variadic' => $p->isVariadic(),
            'optional' => $p->isOptional(),
            'default' => defaultString($p),
        ];
    }

    $attributes = [];
    if (PHP_VERSION_ID >= 80000) {
        foreach ($m->getAttributes() as $a) {
            $attributes[] = $a->getName();
        }
    }

    return [
        'visibility' => $m->isProtected() ? 'protected' : 'public',
        'static' => $m->isStatic(),
        'abstract' => $m->isAbstract() && !$r->isInterface(),
        'final' => $m->isFinal(),
        'byRef' => $m->returnsReference(),
        'returnType' => typeString($m->hasReturnType() ? $m->getReturnType() : null),
        'attributes' => $attributes,
        'params' => $params,
    ];
}

/**
 * A type as it is written in source: classes fully qualified, nullable
 * named types with "?".
 */
function typeString(?ReflectionType $t): ?string
{
    if ($t === null) {
        return null;
    }
    if ($t instanceof ReflectionNamedType) {
        $name = $t->getName();
        $s = $t->isBuiltin() || in_array(strtolower($name), ['self', 'parent', 'static'], true) ? $name : '\\'.$name;
        if ($t->allowsNull() && $name !== 'mixed' && $name !== 'null') {
            $s = '?'.$s;
        }

        return $s;
    }

    $sep = $t instanceof ReflectionUnionType ? '|' : '&';
    $parts = [];
    foreach ($t->getTypes() as $sub) {
        $parts[] = typeString($sub);
    }

    return implode($sep, $parts);
}

function defaultString(ReflectionParameter $p): ?string
{
    if (!$p->isDefaultValueAvailable()) {
        return null;
    }
    if ($p->isDefaultValueConstant()) {
        $name = $p->getDefaultValueConstantName();
        $pos = strpos($name, '::');
        if ($pos !== false && in_array(strtolower(substr($name, 0, $pos)), ['self', 'parent', 'static'], true)) {
            return $name;
        }

        // An unqualified global constant inside a namespace reports the
        // namespaced name it was looked up as first.
        if ($pos === false && ($ns = strrpos($name, '\\')) !== false && !defined($name) && defined(substr($name, $ns + 1))) {
            $name = substr($name, $ns + 1);
        }

        return '\\'.$name;
    }

    return export($p->getDefaultValue());
}

/**
 * A constant value as PHP source.
 */
function export($v): string
{
    if (is_array($v)) {
        if ($v === []) {
            return '[]';
        }
        $isList = array_keys($v) === range(0, count($v) - 1);
        $parts = [];
        foreach ($v as $k => $item) {
            $parts[] = ($isList ? '' : var_export($k, true).' => ').export($item);
        }

        return '['.implode(', ', $parts).']';
    }
    if ($v === null) {
        return 'null';
    }
    if (is_float($v)) {
        $s = var_export($v, true);

        return $s;
    }

    return var_export($v, true);
}
