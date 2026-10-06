<?php

/*
 * maestro's plugin shim: Composer\Util\HttpDownloader (docs/PLUGINS.md
 * §4.8): a proxy of maestro's (http.*); one created in PHP is maestro's
 * too. Responses are PHP values; add() and addCopy() return promises that
 * settle when maestro's do, while its event loop runs.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Util;

use Composer\Config;
use Composer\IO\IOInterface;
use Composer\Util\Http\Response;
use Maestro\Shim\Promises;
use Maestro\Shim\Rpc;
use React\Promise\PromiseInterface;

class HttpDownloader
{
    public function __construct(IOInterface $io, Config $config, array $options = [], bool $disableTls = false)
    {
        Rpc::call('http.new', [$this, $io, $config, $options, $disableTls]);
    }

    /**
     * A response maestro sent ([url, code, headers, body]).
     *
     * @param array<string, mixed> $r
     */
    private static function response(array $r): Response
    {
        return new Response(['url' => $r['url']], $r['code'], $r['headers'], $r['body']);
    }

    public function get(string $url, array $options = [])
    {
        if ('' === $url) {
            throw new \InvalidArgumentException('$url must not be an empty string');
        }

        return self::response(Rpc::call('http.get', [$this, $url, $options]));
    }

    public function add(string $url, array $options = [])
    {
        if ('' === $url) {
            throw new \InvalidArgumentException('$url must not be an empty string');
        }

        return self::responsePromise(Rpc::call('http.add', [$this, $url, $options]));
    }

    public function copy(string $url, string $to, array $options = [])
    {
        if ('' === $url) {
            throw new \InvalidArgumentException('$url must not be an empty string');
        }

        return self::response(Rpc::call('http.copy', [$this, $url, $to, $options]));
    }

    public function addCopy(string $url, string $to, array $options = [])
    {
        if ('' === $url) {
            throw new \InvalidArgumentException('$url must not be an empty string');
        }

        return self::responsePromise(Rpc::call('http.addCopy', [$this, $url, $to, $options]));
    }

    /**
     * @param array<string, mixed> $p
     */
    private static function responsePromise(array $p): PromiseInterface
    {
        return Promises::fromMaestro($p)->then(static function ($r) {
            return self::response($r);
        });
    }

    public function getOptions()
    {
        return Rpc::call('http.getOptions', [$this]);
    }

    public function setOptions(array $options)
    {
        Rpc::call('http.setOptions', [$this, $options]);
    }

    public function wait(?int $index = null)
    {
        Rpc::call('http.wait', [$this, $index]);
    }

    public function enableAsync(): void
    {
        Rpc::call('http.enableAsync', [$this]);
    }

    public function countActiveJobs(?int $index = null): int
    {
        return Rpc::call('http.countActiveJobs', [$this, $index]);
    }

    public static function outputWarnings(IOInterface $io, string $url, $data): bool
    {
        return Rpc::call('http.outputWarnings', [$io, $url, $data]);
    }

    public static function getExceptionHints(\Throwable $e): ?array
    {
        return Rpc::call('http.getExceptionHints', [$e]);
    }

    public static function isCurlEnabled(): bool
    {
        return \extension_loaded('curl') && \function_exists('curl_multi_exec') && \function_exists('curl_multi_init');
    }
}
