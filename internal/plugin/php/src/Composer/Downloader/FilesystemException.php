<?php

/*
 * maestro's plugin shim: Composer\Downloader\FilesystemException, Composer
 * 2.10.3's.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Downloader;

class FilesystemException extends \Exception
{
    public function __construct(string $message = '', int $code = 0, ?\Exception $previous = null)
    {
        parent::__construct("Filesystem exception: \n".$message, $code, $previous);
    }
}
