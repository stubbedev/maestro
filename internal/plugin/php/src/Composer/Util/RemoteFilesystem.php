<?php

/*
 * maestro's plugin shim: Composer\Util\RemoteFilesystem (docs/PLUGINS.md
 * §4.8): one created in PHP is maestro's (rfs.*), which downloads.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Util;

use Composer\Config;
use Composer\IO\IOInterface;
use Composer\Pcre\Preg;
use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class RemoteFilesystem
{
    public function __construct(IOInterface $io, Config $config, array $options = [], bool $disableTls = false, ?AuthHelper $authHelper = null)
    {
        if ($authHelper !== null) {
            Remote::unsupported(self::class, '__construct');
        }
        Rpc::call('rfs.new', [$this, $io, $config, $options, $disableTls]);
    }

    public function copy(string $originUrl, string $fileUrl, string $fileName, bool $progress = true, array $options = [])
    {
        return Rpc::call('rfs.copy', [$this, $originUrl, $fileUrl, $fileName, $progress, $options]);
    }

    public function getContents(string $originUrl, string $fileUrl, bool $progress = true, array $options = [])
    {
        return Rpc::call('rfs.getContents', [$this, $originUrl, $fileUrl, $progress, $options]);
    }

    public function getOptions()
    {
        return Rpc::call('rfs.getOptions', [$this]);
    }

    public function setOptions(array $options)
    {
        Rpc::call('rfs.setOptions', [$this, $options]);
    }

    public function isTlsDisabled()
    {
        return Rpc::call('rfs.isTlsDisabled', [$this]);
    }

    public function getLastHeaders()
    {
        return Rpc::call('rfs.getLastHeaders', [$this]);
    }

    public static function findStatusCode(array $headers)
    {
        $value = null;
        foreach ($headers as $header) {
            if (Preg::isMatch('{^HTTP/\S+ (\d+)}i', $header, $match)) {
                // a redirect's headers come before the final response's
                $value = (int) $match[1];
            }
        }

        return $value;
    }

    public function findStatusMessage(array $headers)
    {
        $value = null;
        foreach ($headers as $header) {
            if (Preg::isMatch('{^HTTP/\S+ \d+}i', $header)) {
                $value = $header;
            }
        }

        return $value;
    }

    // The protected methods are maestro's (a subclass's parent::get(),
    // ...), but for getRemoteContents(), which reads the stream context it
    // is given in PHP, as Composer does. maestro's copy() and
    // getContents() do not call a subclass's overrides of them.

    protected function get(string $originUrl, string $fileUrl, array $additionalOptions = [], ?string $fileName = null, bool $progress = true)
    {
        return Rpc::call('rfs.get', [$this, $originUrl, $fileUrl, $additionalOptions, $fileName, $progress]);
    }

    protected function getRemoteContents(string $originUrl, string $fileUrl, $context, ?array &$responseHeaders = null, ?int $maxFileSize = null)
    {
        $result = false;

        if (\PHP_VERSION_ID >= 80400) {
            http_clear_last_response_headers();
        }

        try {
            $e = null;
            if ($maxFileSize !== null) {
                $result = file_get_contents($fileUrl, false, $context, 0, $maxFileSize);
            } else {
                // passing `null` to file_get_contents will convert `null` to `0` and return 0 bytes
                $result = file_get_contents($fileUrl, false, $context);
            }
        } catch (\Throwable $e) {
        }

        if ($result !== false && $maxFileSize !== null && Platform::strlen($result) >= $maxFileSize) {
            throw new \Composer\Downloader\MaxFileSizeExceededException('Maximum allowed download size reached. Downloaded ' . Platform::strlen($result) . ' of allowed ' .  $maxFileSize . ' bytes for ' . Rpc::call('rfs.sanitizeUrl', [$fileUrl]));
        }

        // https://www.php.net/manual/en/reserved.variables.httpresponseheader.php
        if (\PHP_VERSION_ID >= 80400) {
            $responseHeaders = http_get_last_response_headers() ?? [];
            http_clear_last_response_headers();
        } else {
            $responseHeaders = $http_response_header ?? [];
        }

        if (null !== $e) {
            throw $e;
        }

        return $result;
    }

    protected function callbackGet(int $notificationCode, int $severity, ?string $message, int $messageCode, int $bytesTransferred, int $bytesMax)
    {
        Rpc::call('rfs.callbackGet', [$this, $notificationCode, $severity, $message, $messageCode, $bytesTransferred, $bytesMax]);
    }

    protected function promptAuthAndRetry($httpStatus, ?string $reason = null, array $headers = [])
    {
        Rpc::call('rfs.promptAuthAndRetry', [$this, $httpStatus, $reason, $headers]);
    }

    protected function getOptionsForUrl(string $originUrl, array $additionalOptions)
    {
        return Rpc::call('rfs.getOptionsForUrl', [$this, $originUrl, $additionalOptions]);
    }
}
