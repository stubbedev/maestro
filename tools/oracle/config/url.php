<?php
// Generates internal/config/testdata/oracle/url.json:
// filter_var($url, FILTER_VALIDATE_URL) and Config::prohibitUrlByConfig over
// thousands of generated URLs, with secure-http on and off, secure-svn-domains
// and ssl repository options, recording the exception or the warnings written.
//
// Run: php tools/oracle/config/url.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Config;
use Composer\IO\NullIO;

set_error_handler(static function (int $level, string $message, string $file, int $line): bool {
    if ($level === E_DEPRECATED || $level === E_USER_DEPRECATED || 0 === (error_reporting() & $level)) {
        return true;
    }
    throw new \ErrorException($message, 0, $level, $file, $line);
});

mt_srand(20261005);

class RecordingIO extends NullIO
{
    /** @var list<string> */
    public $errors = [];

    public function writeError($messages, bool $newline = true, int $verbosity = self::NORMAL): void
    {
        $this->errors[] = (string) $messages;
    }
}

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }

$schemes = ['http', 'https', 'HTTP', 'Https', 'git', 'ftp', 'svn', 'ssh', 'file', 'mailto', 'news', 'hg', 'git+ssh', 'svn+ssh', 's3', 'x-y.z', '', '1abc'];
$userinfos = ['', 'user@', 'user:pass@', 'us%20er@', 'u%zz@', 'u:p:q@', "\u{1F49B}@", 'a b@', 'u!$&\'()*+,;=@', '@', ':@', 'user:@'];
$hosts = ['example.org', 'packagist.org', 'repo.packagist.org', 'localhost', '127.0.0.1', '10.1.0.1', '192.168.0.1', '1.2.3.4',
    '[::1]', '[2001:db8::1]', '[::ffff:1.2.3.4]', '[1:2:3:4:5:6:7:8]', '[1::2::3]', '[::1', 'ex_ample.org', '-bad.org', 'bad-.org',
    'a..b', '.lead', 'trail.', 'UPPER.ORG', 'xn--bcher-kva.example', 'host.xz', str_repeat('a', 63).'.com', str_repeat('a', 64).'.com',
    implode('.', array_fill(0, 60, 'abcd')), 'will.not.resolve', '', 'üñí.org', 'my.satis', '5.6.7.8', '256.1.1.1', '01.2.3.4'];
$ports = ['', ':80', ':8080', ':0', ':65535', ':65536', ':abc', ':'];
$paths = ['', '/', '/satis', '/trunk', '/git.git', '/path/to/repo.git/', '/a b', '/%41', '/ü', '/a?x=1', '/a#frag', '/a?x=1#f', '//double'];

$urls = [
    'https://packagist.org', 'git@github.com:composer/composer.git', 'hg://user:pass@my.satis/satis', '\\myserver\myplace.git',
    'file://myserver.localhost/mygit.git', 'file://example.org/mygit.git', 'git:Department/Repo.git',
    'ssh://[user@]host.xz[:port]/path/to/repo.git/', 'http://packagist.org', 'http://10.1.0.1/satis', 'http://127.0.0.1/satis',
    "http://\u{1F49B}@example.org", 'svn://localhost/trunk', 'svn://will.not.resolve/trunk', 'svn://192.168.0.1/trunk',
    'svn://1.2.3.4/trunk', 'git://5.6.7.8/git.git', '', 'http://', 'http:///x', 'http:example.org', '//example.org/x',
    'mailto:user@example.org', 'news:comp.lang', 'file:///etc/passwd', 'http://user:pa ss@x.org', "http://x.org/\x00",
    'HTTP://EXAMPLE.ORG', 'ftp://ftp.example.org/f', 'https://x.org:443/', ' http://x.org', 'http://x.org ',
];
for ($i = 0; $i < 3000; $i++) {
    $scheme = pick($schemes);
    $url = ($scheme === '' ? '' : $scheme.(mt_rand(0, 9) ? '://' : pick([':', ':/', ':///']))).pick($userinfos).pick($hosts).pick($ports).pick($paths);
    $urls[] = $url;
}

$cases = [];
foreach ($urls as $i => $url) {
    $case = ['url' => $url, 'valid' => filter_var($url, FILTER_VALIDATE_URL) !== false];

    $config = new Config(false);
    $merge = [];
    if (mt_rand(0, 2) === 0) {
        $merge['secure-http'] = false;
    }
    if (mt_rand(0, 3) === 0) {
        $merge['secure-svn-domains'] = pick([['localhost'], ['will.not.resolve', '1.2.3.4'], ['example.org']]);
    }
    if (mt_rand(0, 4) === 0) {
        $merge['disable-tls'] = true;
    }
    $config->merge(['config' => $merge]);
    $case['merge'] = (object) $merge;

    $options = pick([[], ['ssl' => ['verify_peer' => false]], ['ssl' => ['verify_peer_name' => 0]], ['ssl' => ['verify_peer' => false, 'verify_peer_name' => false]], ['ssl' => ['verify_peer' => true, 'verify_peer_name' => '']], ['ssl' => ['verify_peer' => null]]]);
    $case['options'] = $options;
    $withIO = mt_rand(0, 3) !== 0;
    $case['io'] = $withIO;

    $runs = [];
    $io = $withIO ? new RecordingIO() : null;
    // twice, as warnings are only given once per host
    for ($run = 0; $run < 2; $run++) {
        try {
            $config->prohibitUrlByConfig($url, $io, $options);
            $runs[] = ['ok' => true];
        } catch (\Throwable $e) {
            $runs[] = ['e' => [get_class($e), $e->getMessage(), $e->getCode()]];
        }
    }
    $case['runs'] = $runs;
    $case['warnings'] = $io ? $io->errors : [];
    $cases[] = $case;
}

$out = dirname(__DIR__, 3).'/internal/config/testdata/oracle/url.json';
@mkdir(dirname($out), 0777, true);
file_put_contents($out, json_encode($cases, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR | JSON_INVALID_UTF8_SUBSTITUTE));
echo count($cases), " cases\n";
