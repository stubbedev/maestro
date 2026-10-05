<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * The value codec of the IPC channel (docs/PLUGINS.md §6.4).
 *
 * Values cross as JSON. A tag is a JSON object whose first key starts
 * with NUL: binary strings ("\0b"), non-finite floats ("\0f"), arrays
 * whose first key starts with NUL ("\0e", explicit pairs), stdClass
 * ("\0s") and objects ("\0o", by handle). encode() is the pre-pass that
 * turns a PHP value into one json_encode() can write; decode() is the
 * post-pass that resolves the tags json_decode() leaves.
 */
final class Codec
{
    const JSON_FLAGS = JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRESERVE_ZERO_FRACTION;

    /**
     * json_encode/json_decode depth: far beyond anything Composer data
     * nests to, with the tags' extra levels.
     */
    const DEPTH = 65536;

    /**
     * Decoders of value tags other packages define (constraints and links
     * from phase 2 on), by tag key.
     *
     * @var array<string, callable(array): mixed>
     */
    private static $tags = [];

    /**
     * Registers the decoder of a value tag ("\0c" for instance).
     */
    public static function registerTag(string $key, callable $decoder): void
    {
        self::$tags[$key] = $decoder;
    }

    /**
     * The JSON text of a value already passed through encode().
     */
    public static function json($value): string
    {
        $json = json_encode($value, self::JSON_FLAGS, self::DEPTH);
        if ($json === false || json_last_error() !== JSON_ERROR_NONE) {
            throw new ProtocolException('maestro shim: cannot encode a message: '.json_last_error_msg());
        }

        return $json;
    }

    /**
     * Parses JSON text; tags are left for decode().
     *
     * @return mixed
     */
    public static function parse(string $json)
    {
        $value = json_decode($json, true, self::DEPTH, JSON_BIGINT_AS_STRING);
        if (json_last_error() !== JSON_ERROR_NONE) {
            throw new ProtocolException('maestro shim: cannot decode a message: '.json_last_error_msg());
        }

        return $value;
    }

    /**
     * The pre-pass: a value json_encode() writes as §6.4 describes.
     *
     * @param mixed $value
     * @return mixed
     */
    public static function encode($value)
    {
        if (is_array($value)) {
            return self::encodeArray($value);
        }
        if (is_string($value)) {
            return self::encodeString($value);
        }
        if (is_float($value)) {
            if (is_finite($value)) {
                return $value;
            }

            return ["\0f" => is_nan($value) ? 'NAN' : ($value > 0 ? 'INF' : '-INF')];
        }
        if (is_object($value)) {
            if (get_class($value) === 'stdClass') {
                return ["\0s" => self::encodeArray((array) $value)];
            }

            return Handles::encodeObject($value);
        }
        if (is_resource($value)) {
            throw new \InvalidArgumentException('maestro shim: a resource cannot cross to maestro');
        }

        return $value;
    }

    /**
     * @return array<mixed>
     */
    private static function encodeArray(array $array): array
    {
        $out = [];
        $pairs = false;
        $first = true;
        foreach ($array as $key => $item) {
            if (is_string($key) && !$pairs && (($first && $key !== '' && $key[0] === "\0") || preg_match('//u', $key) !== 1)) {
                // A first key starting with NUL would read as a tag, and
                // JSON object keys must be UTF-8.
                $pairs = true;
            }
            $first = false;
            $out[$key] = self::encode($item);
        }

        if (!$pairs) {
            return $out;
        }

        $list = [];
        foreach ($out as $key => $item) {
            $list[] = [is_string($key) ? self::encodeString($key) : $key, $item];
        }

        return ["\0e" => $list];
    }

    /**
     * @return string|array<string, string>
     */
    private static function encodeString(string $value)
    {
        if (preg_match('//u', $value) === 1) {
            return $value;
        }

        return ["\0b" => base64_encode($value)];
    }

    /**
     * The post-pass: resolves the tags of a json_decode()d value.
     *
     * @param mixed $value
     * @return mixed
     */
    public static function decode($value)
    {
        if (!is_array($value)) {
            return $value;
        }

        foreach ($value as $key => $_) {
            if (is_string($key) && $key !== '' && $key[0] === "\0") {
                return self::decodeTag($key, $value);
            }
            break;
        }

        foreach ($value as $key => $item) {
            if (is_array($item)) {
                $value[$key] = self::decode($item);
            }
        }

        return $value;
    }

    /**
     * @return mixed
     */
    private static function decodeTag(string $key, array $tag)
    {
        switch ($key) {
            case "\0o":
                return Handles::decodeObject($tag);
            case "\0b":
                $bytes = base64_decode((string) $tag[$key], true);
                if ($bytes === false) {
                    throw new ProtocolException('maestro shim: invalid binary string');
                }

                return $bytes;
            case "\0f":
                switch ($tag[$key]) {
                    case 'INF':
                        return INF;
                    case '-INF':
                        return -INF;
                    case 'NAN':
                        return NAN;
                }
                throw new ProtocolException('maestro shim: invalid float tag');
            case "\0e":
                $out = [];
                foreach ($tag[$key] as $pair) {
                    $out[self::decode($pair[0])] = self::decode($pair[1]);
                }

                return $out;
            case "\0s":
                return (object) self::decode($tag[$key]);
        }

        if (isset(self::$tags[$key])) {
            return (self::$tags[$key])($tag);
        }

        throw new ProtocolException('maestro shim: unknown value tag '.json_encode(substr($key, 1)));
    }
}
