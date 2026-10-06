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

    protected function get(string $originUrl, string $fileUrl, array $additionalOptions = [], ?string $fileName = null, bool $progress = true)
    {
        Remote::unsupported(self::class, 'get');
    }

    protected function getRemoteContents(string $originUrl, string $fileUrl, $context, ?array &$responseHeaders = null, ?int $maxFileSize = null)
    {
        Remote::unsupported(self::class, 'getRemoteContents');
    }

    protected function callbackGet(int $notificationCode, int $severity, ?string $message, int $messageCode, int $bytesTransferred, int $bytesMax)
    {
        Remote::unsupported(self::class, 'callbackGet');
    }

    protected function promptAuthAndRetry($httpStatus, ?string $reason = null, array $headers = [])
    {
        Remote::unsupported(self::class, 'promptAuthAndRetry');
    }

    protected function getOptionsForUrl(string $originUrl, array $additionalOptions)
    {
        Remote::unsupported(self::class, 'getOptionsForUrl');
    }
}
