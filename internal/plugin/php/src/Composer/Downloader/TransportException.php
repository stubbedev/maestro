<?php

/*
 * maestro's plugin shim: Composer\Downloader\TransportException, Composer
 * 2.10.3's. One maestro throws into PHP carries its response's headers,
 * body and status code (Maestro\Shim\Exceptions); the transfer info curl
 * gives Composer is not kept (getResponseInfo() is []).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Downloader;

class TransportException extends \RuntimeException
{
    protected $headers;
    protected $response;
    protected $responseInfo = [];
    protected $statusCode;

    public function __construct(string $message = '', int $code = 400, ?\Throwable $previous = null)
    {
        parent::__construct($message, $code, $previous);
    }

    public function getHeaders(): ?array
    {
        return $this->headers;
    }

    public function getResponse(): ?string
    {
        return $this->response;
    }

    public function getResponseInfo(): array
    {
        return $this->responseInfo;
    }

    public function getStatusCode(): ?int
    {
        return $this->statusCode;
    }

    public function setHeaders(array $headers): void
    {
        $this->headers = $headers;
    }

    public function setResponse(?string $response): void
    {
        $this->response = $response;
    }

    public function setResponseInfo(array $responseInfo): void
    {
        $this->responseInfo = $responseInfo;
    }

    public function setStatusCode($statusCode): void
    {
        $this->statusCode = $statusCode;
    }
}
