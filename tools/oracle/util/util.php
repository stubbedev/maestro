<?php
// Generates internal/util/testdata/oracle/util.json.gz from the Composer
// sources in .ref/composer:
//   paths     - Filesystem::normalizePath, isAbsolutePath, trimTrailingSlash,
//               isLocalPath and getPlatformPath (POSIX and Windows)
//   shortest  - Filesystem::findShortestPath and findShortestPathCode over
//               pairs of POSIX, Windows-style, UNC and scheme paths, with
//               every flag combination
//   urls      - parse_url, Url::sanitize, stripCredentials, getOrigin,
//               updateDistReference and isAllowedRedirect
//   noproxy   - NoProxyPattern::test, and the filter_var validators behind it
//   expand    - Platform::expandPath (POSIX and Windows)
//   mirror    - ComposerMirror::processUrl and processGitUrl
// The Windows variants run in a child php with PHP_WINDOWS_VERSION_BUILD
// defined, which is all Platform::isWindows() checks; PHP's own dirname()
// stays the POSIX one there, so findShortestPath is only recorded for POSIX.
// Run: php tools/oracle/util/util.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Config;
use Composer\Pcre\Preg;
use Composer\Util\ComposerMirror;
use Composer\Util\Filesystem;
use Composer\Util\NoProxyPattern;
use Composer\Util\Platform;
use Composer\Util\Url;

$env = ['HOME' => '/home/oracle', 'USERPROFILE' => 'C:\\Users\\oracle', 'FOO' => 'bar', 'EMPTY' => '', 'ZERO' => '0', 'A_B1' => 'x/y', '_' => 'underscore'];
foreach ($env as $k => $v) {
    Platform::putEnv($k, $v);
}
Platform::clearEnv('UNSET_VAR');

$expandInputs = [
    '', '~', '~/', '~/foo', '~\\foo', '~foo', '/~/foo', '$HOME', '$HOME/foo', '%HOME%', '%HOME%/foo', '%HOME/foo',
    '$FOO', '$FOO/bar', '%FOO%', '%FOO%%FOO%', '$FOO$FOO', '$UNSET_VAR/x', '%UNSET_VAR%/x', '$EMPTY/x', '$ZERO/x',
    '$A_B1/z', '$A-B1', '$', '%', '%%', '$/x', '%/x%', "\$FOO\nrest", "%FOO%\n/x", "\$FOO/x\ny", 'x$FOO', ' $FOO',
    '$foo', '%foo%', '$FOO_', '$FOOé', '$1', '%1%', '$_', '~/$FOO',
];

if (($argv[1] ?? '') === '--windows') {
    define('PHP_WINDOWS_VERSION_BUILD', 1);
    $in = json_decode(stream_get_contents(STDIN), true);
    $out = ['local' => [], 'platform' => [], 'expand' => []];
    foreach ($in['paths'] as $p) {
        $out['local'][] = Filesystem::isLocalPath($p);
        $out['platform'][] = Filesystem::getPlatformPath($p);
    }
    foreach ($expandInputs as $p) {
        $out['expand'][] = Platform::expandPath($p);
    }
    echo json_encode($out, JSON_THROW_ON_ERROR);
    exit(0);
}

mt_srand(20261005);

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }

$fs = new Filesystem();

// Paths.
$prefixes = ['', '/', '//', '///', '\\\\', '\\', 'C:', 'c:', 'C:/', 'c:\\', 'd:/', 'C://', 'z:', '1:', 'ab:', 'ab:/', 'file://', 'file:///', 'FILE:///', 'file:///c:/', 'file:///C/', 'file://c:/', 'phar://', 'phar://c:/', 'phar://c:', 'http://', 'https://x/', 'git+ssh://', 'a.b://', 'vfs://', '..', '../', './', '.', '/..', '/../', 'c:..'];
$segments = ['a', 'b', 'foo', 'Foo', 'bar_vendor', 'vendor', '..', '.', '', '...', '.git', 'a b', 'a:b', 'C:', 'é', 'x\\y', '-', '_', '%x%', 'a.b', '~', "\n", 'a\n'];
$paths = ['', '/', '//', '///', '\\', '\\\\', 'c:', 'C:', 'c:\\', 'C:/', 'c:/', '/\\/', "/\n", "//\n", "/\n\n", "c:/\n", 'a/', 'a\\', 'a//', '/a/', '/a//'];
foreach ($prefixes as $p) {
    $paths[] = $p;
    foreach ($segments as $s) {
        $paths[] = $p.$s;
        $paths[] = $p.$s.'/';
    }
}
for ($i = 0; $i < 3000; $i++) {
    $p = pick($prefixes);
    $n = mt_rand(1, 6);
    for ($j = 0; $j < $n; $j++) {
        $p .= pick($segments).pick(['/', '/', '/', '\\', '//', '/./', '/../']);
    }
    if (mt_rand(0, 1)) {
        $p = rtrim($p, '/\\');
    }
    $paths[] = $p;
}
$paths = array_values(array_unique($paths));

$cmd = escapeshellarg(PHP_BINARY).' '.escapeshellarg(__FILE__).' --windows';
$proc = proc_open($cmd, [['pipe', 'r'], ['pipe', 'w']], $pipes);
fwrite($pipes[0], json_encode(['paths' => $paths]));
fclose($pipes[0]);
$win = json_decode(stream_get_contents($pipes[1]), true);
fclose($pipes[1]);
if (proc_close($proc) !== 0 || !is_array($win)) {
    fwrite(STDERR, "windows child failed\n");
    exit(1);
}

$out = ['paths' => [], 'shortest' => [], 'urls' => [], 'noproxy' => [], 'filter' => [], 'expand' => [], 'mirror' => []];
foreach ($paths as $i => $p) {
    $out['paths'][] = [
        'in' => $p,
        'normalized' => $fs->normalizePath($p),
        'absolute' => $fs->isAbsolutePath($p),
        'trimmed' => Filesystem::trimTrailingSlash($p),
        'local' => Filesystem::isLocalPath($p),
        'localWindows' => $win['local'][$i],
        'platform' => Filesystem::getPlatformPath($p),
        'platformWindows' => $win['platform'][$i],
    ];
}

// findShortestPath(Code) loop forever when the common path reaches "."
// without being "/" or a drive root (e.g. to = "c:foo"); such pairs are
// skipped, mirroring the loop with a bound.
function terminates(Filesystem $fs, string $from, string $to, bool $code): bool
{
    if (!$fs->isAbsolutePath($from) || !$fs->isAbsolutePath($to)) {
        return true;
    }
    $from = $fs->normalizePath($from);
    $commonPath = $fs->normalizePath($to);
    for ($i = 0; $i < 200; $i++) {
        if (strpos($from.'/', $commonPath.'/') === 0 || '/' === $commonPath || Preg::isMatch('{^[A-Z]:/?$}i', $commonPath) || ($code && '.' === $commonPath)) {
            return true;
        }
        $commonPath = strtr(\dirname($commonPath), '\\', '/');
    }

    return false;
}

$absPrefixes = ['/', '/', '/', 'C:/', 'c:\\', 'd:/', 'C:', '\\\\', '//', 'file://', 'phar://c:/', 'a:', '/..', 'x'];
$absSegments = ['foo', 'bar', 'baz', 'bar_vendor', 'vendor', 'src', 'lib', 'bin', 'run', 'tmp', 'Temp', 'test', '..', '.', '', 'a b', "it's", 'back\\slash', "nul\0"];
$mkpath = function () use ($absPrefixes, $absSegments): string {
    $p = pick($absPrefixes);
    $n = mt_rand(0, 5);
    for ($j = 0; $j < $n; $j++) {
        $p .= ($j ? pick(['/', '/', '\\', '//']) : '').pick($absSegments);
    }
    if (mt_rand(0, 4) === 0) {
        $p .= '/';
    }

    return $p;
};
$pairs = [];
for ($i = 0; $i < 2000; $i++) {
    $from = $mkpath();
    // Related pairs share a prefix more often than random ones.
    $to = mt_rand(0, 2) ? $from.pick(['/', '/../', '/./', '\\']).pick($absSegments).(mt_rand(0, 1) ? '/'.pick($absSegments) : '') : $mkpath();
    if (mt_rand(0, 3) === 0) {
        [$from, $to] = [$to, $from];
    }
    $pairs[] = [$from, $to];
}
$pairs[] = ['/foo', 'relative'];
$pairs[] = ['relative', '/foo'];
foreach ($pairs as [$from, $to]) {
    foreach ([false, true] as $directories) {
        foreach ([false, true] as $preferRelative) {
            $case = ['from' => $from, 'to' => $to, 'directories' => $directories, 'preferRelative' => $preferRelative];
            if (terminates($fs, $from, $to, false)) {
                try {
                    $case['path'] = $fs->findShortestPath($from, $to, $directories, $preferRelative);
                } catch (\InvalidArgumentException $e) {
                    $case['pathError'] = $e->getMessage();
                }
            }
            if (terminates($fs, $from, $to, true)) {
                foreach ([false, true] as $static) {
                    try {
                        $case[$static ? 'codeStatic' : 'code'] = $fs->findShortestPathCode($from, $to, $directories, $static, $preferRelative);
                    } catch (\InvalidArgumentException $e) {
                        $case['codeError'] = $e->getMessage();
                    }
                }
            }
            $out['shortest'][] = $case;
        }
    }
}

// URLs.
$schemes = ['https://', 'http://', 'HTTPS://', 'git+ssh://', 'svn+ssh://', 'ssh://', 'file://', 'file:///', 'ftp://', 'phar://', '//', 'a.b-c+d://', '1a://', '-a://', 'mailto:', 'zlib:', ':', '', 'git@', 'c:'];
$users = ['', 'foo', 'foo:bar', ':tok', 'x-token-auth:secret', 'oauth2:abc', 'private-token:xyz', 'gitlab-ci-token:realtoken', 'ghp_1234567890abcdefghijklmnopqrstuvwxyzAB', 'github_pat_1234567890abcdefghijkl_1234567890', 'abcdef0123456789', 'abcdefghijkl', 'abcdefghijk', 'ghx_abc', 'gho_a.b-c', 'user@corp', 'u:p@ss', 'a b:c', "u\tv:w", 'ü:ö', 'FOO:BAR'];
$hosts = ['example.org', 'github.com', 'www.github.com', 'api.github.com', 'codeload.github.com', 'foo.github.com', 'repo.packagist.org', 'gitlab.com', 'www.gitlab.com', 'bitbucket.org', 'www.bitbucket.org', 'mygithub.com', 'mygitlab.com', 'gitlab.example.com', 'gitlab.example.co', 'localhost', '127.0.0.1', '[::1]', '[2001:db8::1]', '0', 'ex_ample.org', 'EXAMPLE.ORG', 'a', ''];
$ports = ['', ':80', ':443', ':8080', ':0', ':65535', ':65536', ':123456', ':-1', ':+80', ': 80', ':80a', ':'];
$tails = ['', '/', '/foo/bar', '/foo/bar@2x.png', '/repo.git', '/foo/bar/zipball/abcd', '/foo/bar/tarball/abcd', '/foo/bar/archive/abcd.zip', '/foo/bar/archive/abcd.tar.gz', '/repos/foo/bar/tarball', '/repos/foo/bar/zipball/abcd', '/api/v3/repos/foo/bar/tarball/abcd', '/foo/bar/get/abcd.zip', '/foo/bar/get/abcd.tar.bz2', '/api/v4/projects/foo%2Fbar/repository/archive.zip?sha=abcd', '/api/v3/projects/foo%2Fbar/repository/archive.tar.gz?sha=abcd', '?access_token=abc', '/x?foo=bar&access_token=abc&y=z', '#frag', '?q#f', '/a?b=c@d', "/new\nline", '/ZIPBALL/x', '/foo/bar/ZipBall/abcd'];
$urls = ['', '@', 'a@b', ':@', 'foo:bar@', 'tried https://foo:bar@example.org/a and https://baz:qux@example.com/b', "fatal: unable to access 'https://gitlab-ci-token:realtoken@example.org/g/r.git/'", 'git@github.com:acme/repo.git', 'http://[::1', 'http://]', 'http:///x', 'http://:80', 'http://@/', '/foo/bar', 'example.org/foo', 'data://text/plain;base64,Zm9v', 'a.com:80', 'a.com:80/x', 'a.com:123456/x', 'x:/y', ':80', '://x', 'http:', 'http:/x', 'file:///c:/x', 'file:///c/x', 'file:////x', 'https://user:pa@ss@example.org/repo.git', 'https://example.org/@scope/repo.git'];
foreach ($schemes as $s) {
    foreach ($hosts as $h) {
        $urls[] = $s.$h;
    }
}
for ($i = 0; $i < 4000; $i++) {
    $u = pick($schemes);
    $user = pick($users);
    if ($user !== '' || mt_rand(0, 4) === 0) {
        $u .= $user.'@';
    }
    $u .= pick($hosts).pick($ports).pick($tails);
    if (mt_rand(0, 9) === 0) {
        $u = pick(['Error: ', 'Failed to execute git clone -- ', "'"]).$u.pick(['', ' /cache', "'", ' and '.pick($schemes).pick($users).'@'.pick($hosts)]);
    }
    $urls[] = $u;
}
$urls = array_values(array_unique($urls));
$config = new Config(false);
$config->merge(['config' => ['github-domains' => ['mygithub.com'], 'gitlab-domains' => ['mygitlab.com', 'gitlab.example.com:443/gitlab', 'gitlab.example.co.uk/gitlab', 'a:b:80', '']]]);
$gitlabDomains = $config->get('gitlab-domains');
$githubDomains = $config->get('github-domains');
foreach ($urls as $u) {
    $parsed = parse_url($u);
    $case = [
        'in' => $u,
        'parsed' => $parsed === false ? false : (object) $parsed,
        'sanitize' => Url::sanitize($u),
        'strip' => Url::stripCredentials($u),
        'redirect' => Url::isAllowedRedirect($u),
    ];
    if ($u !== '') {
        $case['origin'] = Url::getOrigin($config, $u);
        $case['ref'] = pick(['newref', '$1x', '\\1', '${1}', '65', 'a/b', '\\\\$1']);
        $case['dist'] = Url::updateDistReference($config, $u, $case['ref']);
    }
    $out['urls'][] = $case;
}
$out['gitlabDomains'] = $gitlabDomains;
$out['githubDomains'] = $githubDomains;
$usernames = array_merge($users, ['', 'a', 'ghp_x', 'ghp_', 'gh_x', 'github_pat_', 'github_pat_x', 'abcdef01234', 'abcdef012345', 'ABCDEF012345', "abcdef012345\n", 'é123456789ab', 'x-oauth-basic', 'X-Token-Auth']);
$out['usernames'] = [];
foreach (array_values(array_unique($usernames)) as $user) {
    $out['usernames'][] = ['in' => $user, 'out' => Url::sanitizeUsername($user)];
}

// NoProxyPattern.
$rules = ['*', '', ',', 'foobar.com', '.barbaz.net', 'FOOBAR.COM', 'foobar.com:80', 'foobar.com:443', 'foobar.com:0', 'foobar.com:65536', 'foobar.com:x', 'foobar.com:8080:1', '192.168.1.1', '192.168.1.1:80', '192.168.1.0/24', '192.168.1.0/33', '192.168.1.0/x', '192.168.1.0/24/8', '10.0.0.0/30', '10.0.0.0/0', '0.0.0.0/0', '2001:db8::52:0:1', '[2001:db8::52:0:1]', '[2001:db8::52:0:1]:443', '[::1]', '[::]', '[:]', '2002:db8:a::45/121', '::/0', '::ffff:10.0.0.0/120', '::ffff:10.0.0.1', '01.2.3.4', '1.2.3', '1.2.3.4.5', '256.1.1.1', 'localhost', '.localhost', 'local', 'x/8', '[::1]x', '[::1]:80:90', '1::2:3:4:5:6:7', '1:2:3:4:5:6:7:8:9', ':::', '1:::2', 'fe80::1%eth0', ' foo ', 'a,b c', '.com', '.', 'example.org:+80', 'example.org: 80', 'example.org:080', 'example.org:-0', '1.2.3.4/032', '1.2.3.4/+8', '1.2.3.4/ 8', '[1.2.3.4]', '::1.2.3.4', '::ffff:1.2.3.4:80'];
$targets = ['foobar.com', 'www.foobar.com', 'foofoobar.com', 'barbaz.net', 'www.barbaz.net', 'barbarbaz.net', 'barbaz.com', 'foobar.com.', 'FooBar.Com', '192.168.1.1', '192.168.1.4', '192.168.1.200', '10.0.0.2', '10.0.0.4', '[2001:db8:0:0:0:52:0:1]', '[2001:db8:0:0:0:52:0:2]', '[::FFFF:C0A8:0101]', '[::FFFF:C0A8:0104]', '[2002:db8:a:0:0:0:0:7f]', '[2002:db8:a:0:0:0:0:ff]', '[::FFFF:0A00:0002]', '[::FFFF:0A00:0004]', '[::1]', '[::]', 'localhost', 'sub.localhost', 'example.org', '0', '1.2.3.4', '01.2.3.4', 'a.com'];
$urlForms = ['http://%s', 'https://%s', 'http://%s:80', 'https://%s:443', 'http://%s:8080', 'ftp://%s', 'HTTP://%s', '%s', '//%s', 'http://user:pass@%s/x'];
$patterns = [];
foreach ($rules as $r) {
    $patterns[] = $r;
}
for ($i = 0; $i < 300; $i++) {
    $patterns[] = implode(pick([',', ', ', ' ', "\t", ",,", " ,\n"]), [pick($rules), pick($rules), pick($rules)]);
}
foreach ($patterns as $pattern) {
    $matcher = new NoProxyPattern($pattern);
    $case = ['pattern' => $pattern, 'urls' => [], 'out' => []];
    foreach ($targets as $t) {
        $url = sprintf(pick($urlForms), $t);
        $case['urls'][] = $url;
        $case['out'][] = $matcher->test($url);
    }
    $out['noproxy'][] = $case;
}
$filterInputs = array_merge($rules, $targets, ['', ' 1', '1 ', '+1', '-0', '+0', '0', '00', '65535', '65536', '-1', '9223372036854775807', '9223372036854775808', '-9223372036854775808', '1e3', '0x10', "\t8\n", '128', '129', '::', '::1', '1::', '1:2:3:4:5:6:7:8', '1:2:3:4:5:6:7::', '::2:3:4:5:6:7:8', '1:2:3:4:5:6:1.2.3.4', '1:2:3:4:5:6:7:1.2.3.4', '::1.2.3.4', ':1.2.3.4', '1.2.3.4', '0.0.0.0', '255.255.255.255', '1.2.3.04', 'g::1', '12345::', '1:2', 'ABCD:ef01::']);
foreach (array_values(array_unique($filterInputs)) as $s) {
    $out['filter'][] = [
        'in' => $s,
        'ip' => filter_var($s, FILTER_VALIDATE_IP) !== false,
        'port' => filter_var($s, FILTER_VALIDATE_INT, ['options' => ['min_range' => 1, 'max_range' => 65535]]),
        'prefix' => filter_var($s, FILTER_VALIDATE_INT, ['options' => ['min_range' => 0, 'max_range' => 128]]),
    ];
}

// Platform::expandPath.
foreach ($expandInputs as $i => $p) {
    $out['expand'][] = ['in' => $p, 'posix' => Platform::expandPath($p), 'windows' => $win['expand'][$i]];
}
$out['env'] = $env;

// ComposerMirror.
$mirrors = ['https://mirror/%package%/%version%/%reference%.%type%', '%package%%version%%reference%%type%%prettyVersion%', 'https://m/%normalizedUrl%/%package%.%type%', 'plain'];
$refs = [null, '', '0', 'abcdef0123', 'ABCDEF', '%reference%', 'not-hex', 'g123', "abc\n"];
$versions = ['1.0.0', 'dev-main', 'dev-feature/x', '1.0.0.0', ''];
$names = ['vendor/package', '%version%', ''];
foreach ($mirrors as $m) {
    foreach ($refs as $ref) {
        foreach ($versions as $v) {
            foreach ([null, 'zip'] as $type) {
                foreach ([null, 'v1.0'] as $pretty) {
                    $name = pick($names);
                    $out['mirror'][] = ['kind' => 'url', 'mirror' => $m, 'name' => $name, 'version' => $v, 'reference' => $ref, 'type' => $type, 'pretty' => $pretty, 'out' => ComposerMirror::processUrl($m, $name, $v, $ref, $type, $pretty)];
                }
            }
        }
    }
}
$gitUrls = ['https://github.com/acme/repo.git', 'http://github.com/acme/repo', 'git://github.com/acme/repo.git', 'git@github.com:acme/repo.git', 'git@github.com:acme/sub/repo.git', 'https://github.com/acme/repo.git.git', "https://github.com/acme/repo.git\n", 'https://bitbucket.org/acme/repo.git', 'https://bitbucket.org/acme/repo/', 'https://bitbucket.org/acme/repo.git/', 'https://gitlab.com/acme/repo.git', '/local/path/', 'ssh://user@host:22/~/repo', 'C:\\repos\\x', 'üñí', ''];
foreach ($mirrors as $m) {
    foreach ($gitUrls as $u) {
        foreach ([null, 'git', 'hg'] as $type) {
            $out['mirror'][] = ['kind' => 'git', 'mirror' => $m, 'name' => 'vendor/package', 'url' => $u, 'type' => $type, 'out' => ComposerMirror::processGitUrl($m, 'vendor/package', $u, $type)];
        }
    }
}

$file = dirname(__DIR__, 3).'/internal/util/testdata/oracle/util.json.gz';
@mkdir(dirname($file), 0777, true);
file_put_contents($file, gzencode(json_encode($out, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR)."\n", 9));
fwrite(STDERR, sprintf("paths %d, shortest %d, urls %d, noproxy %d, filter %d, expand %d, mirror %d\n", count($out['paths']), count($out['shortest']), count($out['urls']), array_sum(array_map(fn ($c) => count($c['urls']), $out['noproxy'])), count($out['filter']), count($out['expand']), count($out['mirror'])));
