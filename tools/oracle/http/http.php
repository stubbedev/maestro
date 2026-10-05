<?php
// Generates internal/util/http/testdata/oracle/http.json from the Composer
// sources in .ref/composer:
//   proxies   - ProxyItem parsing: errors, statuses, context options and
//               curl credentials
//   redirects - CurlDownloader::handleRedirect's target URL (or error) for
//               pairs of request URLs and Location headers
//   headers   - Response::findHeaderValue, getStatusMessage and
//               RemoteFilesystem::findStatusCode
//   auth      - AuthHelper::addAuthenticationOptions: the options and the
//               debug output, for many credential kinds
//   github    - GitHub::getRateLimit, getSsoUrl, isRateLimited, requiresSso
//   forgejo   - ForgejoUrl::tryFrom
//   bitbucket - AuthHelper::isPublicBitBucketDownload
//   warnings  - HttpDownloader::outputWarnings
//   prompts   - AuthHelper::promptAuthIfNeeded for a non-interactive IO and
//               hosts that are not GitHub, GitLab or Bitbucket
// and internal/cache/testdata/oracle/cache.json:
//   cachekeys - the cache file name sanitising of Composer\Cache
// Run: php tools/oracle/http/http.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Config;
use Composer\Downloader\TransportException;
use Composer\IO\BufferIO;
use Composer\IO\NullIO;
use Composer\Pcre\Preg;
use Composer\Util\AuthHelper;
use Composer\Util\ForgejoUrl;
use Composer\Util\GitHub;
use Composer\Util\Http\CurlDownloader;
use Composer\Util\Http\ProxyItem;
use Composer\Util\Http\Response;
use Composer\Util\RemoteFilesystem;
use Symfony\Component\Console\Output\OutputInterface;

date_default_timezone_set('UTC');

function prop(object $o, string $name)
{
    $p = new ReflectionProperty($o, $name);
    $p->setAccessible(true);

    return $p->getValue($o);
}

$out = [];

// proxies
$proxyInputs = [
    'http://proxy.com:8888', 'HTTP://proxy.com:8888', 'proxy.com:80', 'http://proxy.com', 'https://proxy.com',
    'http://user@proxy.com:6180', 'http://user:p%40ss@proxy.com:6180', 'http://:pass@proxy.com', 'http://user:@proxy.com',
    'http://@proxy.com', 'https://u%3Ax:p%2Fy@proxy.com:8443/path?q', 'socks5://proxy.com:1080', 'socks5://proxy.com',
    'localhost', 'scheme://localhost', 'http://localhost:0', 'http://localhost:65536', "http://user\rname@localhost:80",
    "http://a\tb", '', 'http://', 'http://[::1]:3128', 'http://[::1]', 'HTTPS://Proxy.Example.ORG', 'proxy.com',
    '//proxy.com:3128', 'http://proxy.com:abc', 'http://user:pa:ss@proxy.com', 'http://us%zzer:p@proxy.com',
];
foreach ($proxyInputs as $in) {
    $case = ['in' => $in];
    try {
        $item = new ProxyItem($in, 'https_proxy');
        $http = $item->toRequestProxy('http');
        $https = $item->toRequestProxy('https');
        $case['status'] = $http->getStatus();
        $case['httpContext'] = $http->getContextOptions();
        $case['httpsContext'] = $https->getContextOptions();
        $case['url'] = prop($http, 'url');
        $case['auth'] = prop($http, 'auth');
    } catch (\RuntimeException $e) {
        $case['error'] = $e->getMessage();
    }
    $out['proxies'][] = $case;
}

// redirects
$curl = new CurlDownloader(new NullIO(), new Config(false));
$handleRedirect = new ReflectionMethod($curl, 'handleRedirect');
$handleRedirect->setAccessible(true);
$redirectUrls = [
    'https://example.org/a/b?c=1', 'https://example.org:8443/a/b', 'https://user:pass@example.org/a/', 'http://example.org',
    'https://example.org/', 'https://example.org/dir/file.json?x=/y', 'https://repo.example.org/p2/vendor/pkg.json',
    'http://[::1]:8080/a/b', 'https://example.org/a#frag',
];
$locations = [
    'https://other.org/x', 'HTTP://other.org/x', '//other.org/x', '//other.org', '/x/y', '/x?q=1', '/', 'x', 'x/y?z',
    '../up', '?q=1', 'file:///etc/passwd', 'ftp://example.org/x', 'data:text/plain,foo', '0', '', ' /spaced',
    '/$1', '/\\1', 'mailto:x@y',
];
foreach ($redirectUrls as $url) {
    foreach ($locations as $location) {
        $case = ['url' => $url, 'location' => $location];
        $headers = ['HTTP/1.1 302 Found'];
        if ($location !== '') {
            $headers[] = 'Location: '.$location;
        }
        try {
            $case['target'] = $handleRedirect->invoke($curl, ['url' => $url, 'attributes' => ['redirects' => 0]], new Response(['url' => $url], 302, $headers, ''));
        } catch (TransportException $e) {
            $case['error'] = $e->getMessage();
        }
        $out['redirects'][] = $case;
    }
}

// headers
$headerSets = [
    ['HTTP/1.1 200 OK', 'Content-Type: application/json', 'content-length: 12'],
    ['HTTP/1.1 302 Found', 'Location: /a', 'HTTP/2 200 ', 'content-type:   text/html; charset=utf-8  ', 'Content-Type: text/plain'],
    ['HTTP/2 404 ', 'x-ratelimit-remaining: 0', 'X-RateLimit-Limit:60', 'Location:', 'Location: ', 'Etag: "abc"'],
    ['http/1.0 500 Internal', 'Content-Type:x', 'Set-Cookie: a=1', 'Set-Cookie: b=2'],
    ['', 'HTTP/ 200', 'HTTP/1.1 abc', 'Location:  two  words  '],
    [],
];
$names = ['content-type', 'Content-Type', 'location', 'set-cookie', 'etag', 'x-ratelimit-limit', 'missing', 'content.type', 'Location '];
foreach ($headerSets as $headers) {
    $response = new Response(['url' => 'https://example.org'], 200, $headers, '');
    $values = [];
    foreach ($names as $name) {
        $values[$name] = Response::findHeaderValue($headers, $name);
    }
    $out['headers'][] = [
        'headers' => $headers,
        'values' => $values,
        'statusMessage' => $response->getStatusMessage(),
        'statusCode' => RemoteFilesystem::findStatusCode($headers),
    ];
}

// cache keys
$cache = [];
foreach (['a-z0-9._', 'a-z0-9.', 'a-z0-9_.-'] as $allowlist) {
    foreach (['provider-vendor/package.json', 'packages.json', 'p2/Vendor/Pkg~dev.json', 'https---repo.packagist.org/x', 'ünïcödé.zip', "tab\there", '..', '', 'A-Z_a.z'] as $file) {
        $cache['cachekeys'][] = ['allowlist' => $allowlist, 'file' => $file, 'key' => Preg::replace('{[^'.$allowlist.']}i', '-', $file)];
    }
}

// auth
$authCases = [
    ['example.org', 'https://example.org/p.json', 'user', 'pass'],
    ['example.org', 'https://example.org/p.json', 'tok', 'bearer'],
    ['example.org', 'https://example.org/p.json', '["A: 1","B: 2"]', 'custom-headers'],
    ['example.org', 'https://example.org/p.json', '{"a":"A: 1"}', 'custom-headers'],
    ['example.org', 'https://example.org/p.json', 'not json', 'custom-headers'],
    ['github.com', 'https://api.github.com/repos/a/b', 'ghp_abcdefghijklmnopqrstuvwxyz0123456789', 'x-oauth-basic'],
    ['github.com', 'https://codeload.github.com/a/b/zip/c', 'ghp_abcdefghijklmnopqrstuvwxyz0123456789', 'x-oauth-basic'],
    ['api.github.com', 'https://api.github.com/repos/a/b', 'ghp_abcdefghijklmnopqrstuvwxyz0123456789', 'x-oauth-basic', 'github.com'],
    ['gitlab.com', 'https://gitlab.com/api/v4/x', 'glpat-xyz', 'oauth2'],
    ['gitlab.com', 'https://gitlab.com/api/v4/x', 'glpat-xyz', 'private-token'],
    ['gitlab.com', 'https://gitlab.com/api/v4/x', 'glpat-xyz', 'gitlab-ci-token'],
    ['gitlab.example.org', 'https://gitlab.example.org/x', 'glpat-xyz', 'private-token'],
    ['bitbucket.org', 'https://bitbucket.org/a/b/get/c.zip', 'x-token-auth', 'access'],
    ['bitbucket.org', 'https://bitbucket.org/a/b/downloads/c.zip', 'x-token-auth', 'access'],
    ['bitbucket.org', 'https://bitbucket.org/site/oauth2/access_token', 'x-token-auth', 'access'],
    ['api.bitbucket.org', 'https://api.bitbucket.org/2.0/x', 'x-token-auth', 'access', 'bitbucket.org'],
    ['bitbucket.org', 'https://bitbucket.org/a/b', 'consumer', 'secret'],
    ['example.org', 'https://example.org/p.json', 'client-certificate', '{"local_cert":"/c.pem","local_pk":"/k.pem"}'],
    ['example.org', 'https://example.org/p.json', 'averyveryverylongusername', 'p'],
    ['example.org', 'https://example.org/p.json', 'x-token-auth', ''],
    ['other.org', 'https://other.org/p.json', null, null, 'none'],
];
foreach ($authCases as $c) {
    $io = new BufferIO('', OutputInterface::VERBOSITY_DEBUG);
    $config = new Config(false);
    $config->merge(['config' => ['gitlab-domains' => ['gitlab.com', 'gitlab.example.org']]]);
    $authOrigin = $c[4] ?? $c[0];
    if ($authOrigin !== 'none') {
        $io->setAuthentication($authOrigin, $c[2], $c[3]);
    }
    $helper = new AuthHelper($io, $config);
    $options = $helper->addAuthenticationOptions(['http' => ['header' => ['Accept: x']]], $c[0], $c[1]);
    // a second call shows the message is only displayed once
    $helper->addAuthenticationOptions([], $c[0], $c[1]);
    $out['auth'][] = [
        'origin' => $c[0], 'url' => $c[1], 'authOrigin' => $authOrigin, 'username' => $c[2], 'password' => $c[3],
        'options' => $options, 'output' => $io->getOutput(),
    ];
}

// github
$github = new GitHub(new NullIO(), new Config(false));
$githubHeaders = [
    ['X-RateLimit-Limit: 5000', 'X-RateLimit-Remaining: 0', 'X-RateLimit-Reset: 1700000000'],
    ['x-ratelimit-limit: abc', 'x-ratelimit-reset: -5', 'x-ratelimit-remaining:  0'],
    ['x-ratelimit-remaining: 01', 'X-GitHub-SSO: required; url=https://github.com/orgs/a/sso?x=1'],
    ['  X-GitHub-SSO: required; url=https://a/b; other', 'x-github-sso: partial-results; organizations=1'],
    ['X-GitHub-SSO: required', 'X-RateLimit-Limit: 60'],
    [],
];
foreach ($githubHeaders as $headers) {
    $out['github'][] = [
        'headers' => $headers,
        'rateLimit' => $github->getRateLimit($headers),
        'sso' => $github->getSsoUrl($headers),
        'rateLimited' => $github->isRateLimited($headers),
        'requiresSso' => $github->requiresSso($headers),
    ];
}

// forgejo
foreach ([
    'git@codeberg.org:acme/repo.git', 'https://codeberg.org/acme/repo', 'https://codeberg.org/acme/repo.git',
    'https://codeberg.org/acme/repo/', 'git://CodeBerg.org/Acme/Repo.git', 'git@codeberg.org:/acme/repo',
    'https://codeberg.org/acme', 'https://example.org', 'ssh://git@codeberg.org/acme/repo.git',
    'https://codeberg.org:3000/acme/repo.git.git', 'https://codeberg.org/acme/re.po',
] as $url) {
    $f = ForgejoUrl::tryFrom($url);
    $out['forgejo'][] = ['url' => $url, 'parsed' => $f === null ? null : [
        'owner' => $f->owner, 'repository' => $f->repository, 'originUrl' => $f->originUrl, 'apiUrl' => $f->apiUrl,
        'ssh' => $f->generateSshUrl(),
    ]];
}

// bitbucket
$helper = new AuthHelper(new NullIO(), new Config(false));
foreach ([
    'https://bitbucket.org/user/repo/downloads/whatever', 'https://bitbucket.org/user/repo/get/x.zip',
    'https://bbuseruploads.s3.amazonaws.com/x/downloads/y', 'https://bitbucket.org/user',
    'https://api.bitbucket.org/2.0/repositories/a/b', 'https://notbitbucket.org.evil.com/a/b/downloads/c',
    'https://bitbucket.org//repo/downloads', 'https://bitbucket.org/a/b/Downloads/c',
] as $url) {
    $out['bitbucket'][] = ['url' => $url, 'public' => $helper->isPublicBitBucketDownload($url)];
}

// warnings
$warningData = [
    [],
    ['warning' => 'plain'],
    ['warning' => 'colored '.chr(27).'[31mred'.chr(27).'[0m', 'info' => 'info msg', 'info-versions' => '^2.5'],
    ['warning' => 'old', 'warning-versions' => '<1.0 || >=2.10'],
    ['warnings' => [['message' => 'a', 'versions' => '*'], ['message' => 'b', 'versions' => '2.10.3'], ['message' => 'c', 'versions' => '>2.10.3']]],
    ['infos' => [['message' => '<comment>x</comment>', 'versions' => '>=2.0']], 'info' => ''],
];
foreach ($warningData as $data) {
    foreach ([false, true] as $decorated) {
        $io = new BufferIO('', OutputInterface::VERBOSITY_NORMAL, new \Symfony\Component\Console\Formatter\OutputFormatter($decorated));
        $wrote = @\Composer\Util\HttpDownloader::outputWarnings($io, 'https://user:secret@repo.example.org', $data);
        $out['warnings'][] = ['data' => $data, 'decorated' => $decorated, 'wrote' => $wrote, 'output' => $io->getOutput()];
    }
}

// prompts
$config = new Config(false);
$config->merge(['config' => ['github-domains' => [], 'gitlab-domains' => []]]);
foreach ([
    ['https://example.org/p.json', 'example.org', 401, 'HTTP/1.1 401 Unauthorized'],
    ['https://u:p@example.org/p.json', 'example.org', 403, 'HTTP/2 403 '],
    ['https://example.org/p.json', 'example.org', 404, 'HTTP/1.1 404 Not Found'],
    ['https://example.org/p.json', 'example.org', 500, null],
    ['https://example.org/p.json?access_token=abc', 'example.org', 407, 'HTTP/1.1 407 Proxy'],
] as $c) {
    $helper = new AuthHelper(new NullIO(), $config);
    $case = ['url' => $c[0], 'origin' => $c[1], 'status' => $c[2], 'reason' => $c[3]];
    try {
        $case['result'] = $helper->promptAuthIfNeeded($c[0], $c[1], $c[2], $c[3]);
    } catch (TransportException $e) {
        $case['error'] = $e->getMessage();
        $case['code'] = $e->getCode();
    }
    $out['prompts'][] = $case;
}

foreach (['internal/util/http/testdata/oracle/http.json' => $out, 'internal/cache/testdata/oracle/cache.json' => $cache] as $path => $data) {
    $file = dirname(__DIR__, 3).'/'.$path;
    @mkdir(dirname($file), 0777, true);
    file_put_contents($file, json_encode($data, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_INVALID_UTF8_SUBSTITUTE)."\n");
}
