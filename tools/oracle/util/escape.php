<?php
// Generates internal/util/testdata/oracle/escape.json:
//   escape   - Composer\Util\ProcessExecutor::escape() on POSIX and on Windows
//              (the Windows branch runs in a child php with
//              PHP_WINDOWS_VERSION_BUILD defined, which is all
//              Platform::isWindows() checks);
//   symfony  - Symfony\Component\Process\Process::escapeArgument() for both
//              platforms (private and keyed on DIRECTORY_SEPARATOR, so its
//              body is copied below verbatim with the platform as a flag);
//   prepare  - Process::prepareWindowsCommandLine() (copied likewise, with a
//              fixed uid, comSpec "cmd" and no pipe files);
//   password - the --password masking regex of ProcessExecutor::outputCommandRun.
// Run: php tools/oracle/util/escape.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Pcre\Preg;
use Composer\Util\ProcessExecutor;

if (($argv[1] ?? '') === '--windows') {
    define('PHP_WINDOWS_VERSION_BUILD', 1);
    $out = [];
    foreach (json_decode(stream_get_contents(STDIN), true) as $arg) {
        $out[] = ProcessExecutor::escape($arg);
    }
    echo json_encode($out);
    exit(0);
}

function symfonyEscape(?string $argument, bool $windows): string
{
    if ('' === $argument || null === $argument) {
        return '""';
    }
    if (!$windows) {
        return "'".str_replace("'", "'\\''", $argument)."'";
    }
    if (str_contains($argument, "\0")) {
        $argument = str_replace("\0", '?', $argument);
    }
    if (!preg_match('/[()%!^"<>&|\s[\]=;*?\'$]/', $argument)) {
        return $argument;
    }
    $argument = preg_replace('/(\\\\+)$/', '$1$1', $argument);

    return '"'.str_replace(['"', '^', '%', '!', "\n"], ['""', '"^^"', '"^%"', '"^!"', '!LF!'], $argument).'"';
}

function prepareWindowsCommandLine(string $cmd, array &$env): string
{
    $uid = 'UID';
    $varCount = 0;
    $varCache = [];
    $cmd = preg_replace_callback(
        '/"(?:(
            [^"%!^]*+
            (?:
                (?: !LF! | "(?:\^[%!^])?+" )
                [^"%!^]*+
            )++
        ) | [^"]*+ )"/x',
        function ($m) use (&$env, &$varCache, &$varCount, $uid) {
            if (!isset($m[1])) {
                return $m[0];
            }
            if (isset($varCache[$m[0]])) {
                return $varCache[$m[0]];
            }
            if (str_contains($value = $m[1], "\0")) {
                $value = str_replace("\0", '?', $value);
            }
            if (false === strpbrk($value, "\"%!\n")) {
                return '"'.$value.'"';
            }

            $value = str_replace(['!LF!', '"^!"', '"^%"', '"^^"', '""'], ["\n", '!', '%', '^', '"'], $value);
            $value = '"'.preg_replace('/(\\\\*)"/', '$1$1\\"', $value).'"';
            $var = $uid.++$varCount;

            $env[$var] = $value;

            return $varCache[$m[0]] = '!'.$var.'!';
        },
        $cmd
    );

    return 'cmd /V:ON /E:ON /D /C ('.str_replace("\n", ' ', $cmd).')';
}

$fixed = [
    '', 'a', 'abc', 'a b c', "a\tb", "a\nb\nc", "a\n", "a\\\n", "a\\\\\n", "\n", "\r\n", 'a,bc', "a'bc", "'", "''",
    'a"bc', 'a\\"bc', 'a\\\\"bc', 'ab\\\\c\\', 'a b c\\\\', 'a b c\\', '\\', '\\\\', 'a "b" c', '"', '""', '"a"',
    '%path%', '%path', '%%path', '%%', '%a%b%', '!path!', '!path', '!!path', '!!', '!a!b!', '%a!b%c!',
    '<>"&|()^', '<> &| ()^', '<>&|()^', '^', '^^', 'a^b', 'a&b', 'a|b', 'a<b', 'a>b', '(a)', 'a=b', 'a;b',
    'a*b', 'a?b', 'a[b]', 'a$b', '$HOME', '${:VAR}', "a\0b", "\0", "a\x0bb", "a\x0cb", "a\rb",
    "a\u{ff02}b", "a\u{02ba}b", "a\u{301d}b", "a\u{301e}b", "a\u{030e}b", "a\u{ff1a}b", "a\u{0589}b",
    "a\u{2236}b", "a\u{ff0f}b", "a\u{2044}b", "a\u{2215}b", "a\u{00b4}b", 'héllo wörld', '日本語',
    'C:\\Program Files\\PHP\\php.exe', 'C:\\path\\', 'C:\\path with space\\', '\\\\server\\share',
    'https://user:pass@example.org/', '--option=value', '-dmemory_limit=-1', 'it\'s "quoted"',
];

mt_srand(20261005);
$alphabet = ['a', 'b', ' ', "\t", "\n", '"', "'", '\\', '\\', '%', '!', '^', '&', '|', '<', '>', '(', ')', ',', '$', '=', ';',
    '*', '?', '[', ']', "\0", 'é', "\u{ff02}", "\u{ff1a}", "\u{ff0f}", "\u{00b4}", "\r", '{', '}', ':', 'L', 'F'];
$corpus = $fixed;
for ($i = 0; $i < 1500; $i++) {
    $len = mt_rand(1, 10);
    $s = '';
    for ($j = 0; $j < $len; $j++) {
        $s .= $alphabet[mt_rand(0, count($alphabet) - 1)];
    }
    $corpus[] = $s;
}
$corpus = array_values(array_unique($corpus));

$proc = proc_open([PHP_BINARY, __FILE__, '--windows'], [['pipe', 'r'], ['pipe', 'w']], $pipes);
fwrite($pipes[0], json_encode($corpus));
fclose($pipes[0]);
$windows = json_decode(stream_get_contents($pipes[1]), true);
fclose($pipes[1]);
if (proc_close($proc) !== 0 || count($windows) !== count($corpus)) {
    fwrite(STDERR, "windows child failed\n");
    exit(1);
}

$out = ['escape' => [], 'symfony' => [], 'prepare' => [], 'password' => []];
foreach ($corpus as $i => $arg) {
    $out['escape'][] = ['input' => $arg, 'posix' => ProcessExecutor::escape($arg), 'windows' => $windows[$i]];
    $out['symfony'][] = ['input' => $arg, 'posix' => symfonyEscape($arg, false), 'windows' => symfonyEscape($arg, true)];
}

// Command lines as Process builds them from argument lists, plus raw ones.
$lines = ['', 'echo foo', '"unterminated', 'a "b" c', '"a""b"', '"a" "a" "b%c"', '"x!LF!y"', '"^%"', '"a"^!"b"'];
for ($i = 0; $i < 400; $i++) {
    $n = mt_rand(1, 4);
    $args = [];
    for ($j = 0; $j < $n; $j++) {
        $args[] = $corpus[mt_rand(0, count($corpus) - 1)];
    }
    $lines[] = implode(' ', array_map(static fn ($a) => symfonyEscape($a, true), $args));
}
for ($i = 0; $i < 200; $i++) {
    $len = mt_rand(1, 12);
    $s = '';
    for ($j = 0; $j < $len; $j++) {
        $s .= $alphabet[mt_rand(0, count($alphabet) - 1)];
    }
    $lines[] = $s;
}
foreach (array_values(array_unique($lines)) as $line) {
    $env = [];
    $cmd = prepareWindowsCommandLine($line, $env);
    $pairs = [];
    foreach ($env as $k => $v) {
        $pairs[] = $k.'='.$v;
    }
    $out['prepare'][] = ['input' => $line, 'output' => $cmd, 'env' => $pairs];
}

$passwords = [
    "svn ls --username 'foo' --password 'bar'  'https://x/'",
    "svn ls --username 'foo' --password 'bar \\'bar'  'https://x/'",
    "svn --password 'a' --password 'b' x",
    "svn --password 'a\\' x",
    "svn --password 'a'",
    "svn --password 'a' ",
    "svn --password '' x",
    "svn --password 'x' y\n--password 'z' w",
    "svn --password ''' x",
    "--password 'é' x",
    'no password here',
    "svn --password x' y",
];
foreach ($passwords as $p) {
    $out['password'][] = ['input' => $p, 'output' => Preg::replace("{--password (.*[^\\\\]\') }", '--password \'***\' ', $p)];
}

$file = dirname(__DIR__, 3).'/internal/util/testdata/oracle/escape.json';
@mkdir(dirname($file), 0777, true);
file_put_contents($file, json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR)."\n");
printf("%d escape, %d prepare, %d password cases\n", count($out['escape']), count($out['prepare']), count($out['password']));
