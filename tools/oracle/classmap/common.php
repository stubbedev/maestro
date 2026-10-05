<?php
// Shared helpers of the classmap oracle scripts.
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\ClassMapGenerator\PhpFileParser;

error_reporting(E_ALL & ~E_WARNING & ~E_NOTICE & ~E_DEPRECATED);

/**
 * One golden line for a file: the md5 of php_strip_whitespace(), a space,
 * then either "!" and the hex of the exception message (with $base removed
 * from it, to keep goldens independent of the checkout location), or the
 * hex of the found classes joined by NUL bytes.
 */
function record(string $path, string $base): string
{
    $line = md5((string) @php_strip_whitespace($path)).' ';
    try {
        return $line.bin2hex(implode("\0", PhpFileParser::findClasses($path)));
    } catch (\RuntimeException $e) {
        return $line.'!'.bin2hex(str_replace($base, '', firstLine($e->getMessage())));
    }
}

/**
 * The exception message without the "may be helpful" part, which holds
 * whatever error_get_last() returned (possibly stale global state).
 */
function firstLine(string $message): string
{
    $pos = strpos($message, PHP_EOL.'The following message may be helpful:');

    return $pos === false ? $message : substr($message, 0, $pos);
}
