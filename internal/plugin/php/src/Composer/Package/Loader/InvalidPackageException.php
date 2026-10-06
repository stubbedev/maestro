<?php

/*
 * maestro's plugin shim: Composer\Package\Loader\InvalidPackageException,
 * Composer 2.10.3's.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Package\Loader;

class InvalidPackageException extends \Exception
{
    private $errors;
    private $warnings;
    private $data;

    public function __construct(array $errors, array $warnings, array $data)
    {
        $this->errors = $errors;
        $this->warnings = $warnings;
        $this->data = $data;
        parent::__construct("Invalid package information: \n".implode("\n", array_merge($errors, $warnings)));
    }

    public function getData(): array
    {
        return $this->data;
    }

    public function getErrors(): array
    {
        return $this->errors;
    }

    public function getWarnings(): array
    {
        return $this->warnings;
    }
}
