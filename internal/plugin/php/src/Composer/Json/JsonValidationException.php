<?php

/*
 * maestro's plugin shim: Composer\Json\JsonValidationException, Composer
 * 2.10.3's. One maestro throws into PHP carries its errors
 * (Maestro\Shim\Exceptions).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Json;

class JsonValidationException extends \Exception
{
    protected $errors;

    public function __construct(string $message, array $errors = [], ?\Exception $previous = null)
    {
        $this->errors = $errors;
        parent::__construct((string) $message, 0, $previous);
    }

    public function getErrors(): array
    {
        return $this->errors;
    }
}
