<?php

/*
 * maestro's plugin shim: Composer\Json\JsonFile (docs/PLUGINS.md §4.10):
 * reading, writing, validating and encoding are maestro's (json.*), with
 * Composer's byte-identical encoder and its errors. Each instance has a
 * maestro peer, which remembers the indentation read() found, as Composer's
 * does.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Json;

class JsonFile
{
    private $path;
    private $httpDownloader;
    private $io;
    private $indent = self::INDENT_DEFAULT;
    private $peer;

    /**
     * maestro's JsonFile for this one.
     *
     * @return object
     */
    private function peer()
    {
        if ($this->peer === null) {
            $this->peer = \Maestro\Shim\Rpc::call('json.new', [$this->path, $this->io]);
        }

        return $this->peer;
    }

    public const AUTH_SCHEMA = 3;
    public const COMPOSER_SCHEMA_PATH = '/home/stubbe/git/private/maestro/.ref/composer/src/Composer/Json/../../../res/composer-schema.json';
    public const INDENT_DEFAULT = '    ';
    public const JSON_PRETTY_PRINT = 128;
    public const JSON_UNESCAPED_SLASHES = 64;
    public const JSON_UNESCAPED_UNICODE = 256;
    public const LAX_SCHEMA = 1;
    public const LOCK_SCHEMA = 4;
    public const LOCK_SCHEMA_PATH = '/home/stubbe/git/private/maestro/.ref/composer/src/Composer/Json/../../../res/composer-lock-schema.json';
    public const STRICT_SCHEMA = 2;

    public function __construct(string $path, ?\Composer\Util\HttpDownloader $httpDownloader = null, ?\Composer\IO\IOInterface $io = null)
    {
        $this->path = $path;

        if (null === $httpDownloader && \Composer\Pcre\Preg::isMatch('{^https?://}i', $path)) {
            throw new \InvalidArgumentException('http urls require a HttpDownloader instance to be passed');
        }
        $this->httpDownloader = $httpDownloader;
        $this->io = $io;
    }

    public static function detectIndenting(?string $json): string
    {
        return \Maestro\Shim\Rpc::call('json.detectIndenting', [$json]);
    }

    public static function encode($data, int $options = 448, string $indent = self::INDENT_DEFAULT): string
    {
        return \Maestro\Shim\Rpc::call('json.encode', [self::prepare($data), $options, $indent]);
    }

    /**
     * What json_encode() sees of a value: JsonSerializable objects as
     * their jsonSerialize(), other objects as their public properties,
     * which maestro's encoder then writes as json_encode() does.
     *
     * @param mixed $data
     * @return mixed
     */
    private static function prepare($data)
    {
        if ($data instanceof \JsonSerializable) {
            return self::prepare($data->jsonSerialize());
        }
        if (is_array($data)) {
            foreach ($data as $key => $value) {
                if (is_array($value) || is_object($value)) {
                    $data[$key] = self::prepare($value);
                }
            }

            return $data;
        }
        if (is_object($data)) {
            $object = new \stdClass();
            foreach (get_object_vars($data) as $key => $value) {
                $object->$key = is_array($value) || is_object($value) ? self::prepare($value) : $value;
            }

            return $object;
        }

        return $data;
    }

    public function exists(): bool
    {
        return is_file($this->path);
    }

    public function getPath(): string
    {
        return $this->path;
    }

    public static function parseJson(?string $json, ?string $file = null)
    {
        return \Maestro\Shim\Rpc::call('json.parseJson', [$json, $file]);
    }

    public function read()
    {
        return \Maestro\Shim\Rpc::call('json.read', [$this->peer()]);
    }

    public static function validateJsonSchema(string $source, $data, int $schema, ?string $schemaFile = null): bool
    {
        return \Maestro\Shim\Rpc::call('json.validateJsonSchema', [$source, $data, $schema, $schemaFile]);
    }

    public function validateSchema(int $schema = self::STRICT_SCHEMA, ?string $schemaFile = null): bool
    {
        return \Maestro\Shim\Rpc::call('json.validateSchema', [$this->peer(), $schema, $schemaFile]);
    }

    protected static function validateSyntax(string $json, ?string $file = null): bool
    {
        return \Maestro\Shim\Rpc::call('json.validateSyntax', [$json, $file]);
    }

    public function write(array $hash, int $options = 448)
    {
        \Maestro\Shim\Rpc::call('json.write', [$this->peer(), self::prepare($hash), $options]);
    }
}
