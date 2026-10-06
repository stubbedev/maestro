<?php

/*
 * maestro's plugin shim: Composer\Util\Http\Response, reimplemented with
 * Composer 2.10.3's behaviour (docs/PLUGINS.md §4.8): a PHP value; the
 * responses of maestro's HttpDownloader cross as their data.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Util\Http;

use Composer\Json\JsonFile;
use Composer\Pcre\Preg;
use Composer\Util\Url;
use Seld\JsonLint\ParsingException;

class Response
{
    private $request;
    private $code;
    private $headers;
    private $body;

    public function __construct(array $request, ?int $code, array $headers, ?string $body)
    {
        if (!isset($request['url'])) {
            throw new \LogicException('url key missing from request array');
        }
        $this->request = $request;
        $this->code = (int) $code;
        $this->headers = $headers;
        $this->body = $body;
    }

    public function getStatusCode(): int
    {
        return $this->code;
    }

    public function getStatusMessage(): ?string
    {
        $value = null;
        foreach ($this->headers as $header) {
            if (Preg::isMatch('{^HTTP/\S+ \d+}i', $header)) {
                // a redirect's headers come before the final response's
                $value = $header;
            }
        }

        return $value;
    }

    public function getHeaders(): array
    {
        return $this->headers;
    }

    public function getHeader(string $name): ?string
    {
        return self::findHeaderValue($this->headers, $name);
    }

    public function getBody(): ?string
    {
        return $this->body;
    }

    public function decodeJson()
    {
        try {
            return JsonFile::parseJson($this->body, $this->request['url']);
        } catch (ParsingException $e) {
            throw new ParsingException('"'.Url::sanitize($this->request['url']).'" does not contain valid JSON');
        }
    }

    public function collect(): void
    {
        unset($this->request, $this->code, $this->headers, $this->body);
    }

    public static function findHeaderValue(array $headers, string $name): ?string
    {
        $value = null;
        foreach ($headers as $header) {
            if (Preg::isMatch('{^'.preg_quote($name).':\s*(.+?)\s*$}i', $header, $match)) {
                $value = $match[1];
            }
        }

        return $value;
    }
}
