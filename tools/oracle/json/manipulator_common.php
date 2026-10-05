<?php
// Shared helpers of the JsonManipulator oracles: the typed value encoding
// the Go replay (internal/json/manipulator_test.go) decodes.
//
//   null, true/false, ints, UTF-8 strings   as JSON
//   {"f": "1.5"}                            a float (PHP's shortest repr)
//   {"b": "<base64>"}                       a string that is not UTF-8
//   {"a": [[key, value], ...]}              a PHP array, keys and order kept
//   {"o": [[key, value], ...]}              a stdClass or ArrayObject
//   {"x": "Class", "m": "message"}          an exception

// Composer's ErrorHandler (registered by its Application): warnings and
// notices become ErrorExceptions, deprecations are ignored.
error_reporting(E_ALL);
set_error_handler(static function (int $level, string $message, string $file, int $line): bool {
    if ($level === E_DEPRECATED || $level === E_USER_DEPRECATED || 0 === (error_reporting() & $level)) {
        return true;
    }
    throw new \ErrorException($message, 0, $level, $file, $line);
});

function enc($v)
{
    if (is_array($v)) {
        $pairs = [];
        foreach ($v as $k => $item) {
            $pairs[] = [$k, enc($item)];
        }

        return ['a' => $pairs];
    }
    if ($v instanceof \stdClass || $v instanceof \ArrayObject) {
        $pairs = [];
        foreach ((array) $v as $k => $item) {
            $pairs[] = [$k, enc($item)];
        }

        return ['o' => $pairs];
    }
    if (is_float($v)) {
        return ['f' => json_encode($v)];
    }
    if (is_string($v) && !preg_match('//u', $v)) {
        return ['b' => base64_encode($v)];
    }
    if ($v === null || is_bool($v) || is_int($v) || is_string($v)) {
        return $v;
    }
    throw new \LogicException('cannot encode '.get_debug_type($v));
}

function encException(\Throwable $e): array
{
    $class = get_class($e);

    // TypeErrors name the calling file; keep the golden independent of it.
    $message = preg_replace('{, called in .* on line \d+$}', '', $e->getMessage());

    return ['x' => substr($class, strrpos('\\'.$class, '\\')), 'm' => $message];
}

function writeJson(string $path, $data): void
{
    $json = json_encode($data, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR)."\n";
    // Goldens over 1 MB are committed gzipped (docs/PORTING.md).
    file_put_contents($path, substr($path, -3) === '.gz' ? gzencode($json, 9) : $json);
}
