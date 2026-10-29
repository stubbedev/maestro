<?php
// Generates internal/console/testdata/oracle/programstatus.json from Composer's
// ProgramStatus, its OSC encoders and TerminalStatusPolicy:
//  - "encode": every encoder's payload for a status (state, progress and
//    protocol option), and for a status carrying a message.
//  - "policy": the protocols TerminalStatusPolicy selects for an environment.
// Run: php tools/oracle/console/programstatus.php
require dirname(__DIR__, 3).'/.ref/composer/vendor/autoload.php';

use Composer\Console\Osc7501ProgramStatusEncoder;
use Composer\Console\Osc9ProgressEncoder;
use Composer\Console\ProgramStatus;
use Composer\Console\TerminalStatusPolicy;

$encoder7501 = new Osc7501ProgramStatusEncoder();
$encoder9 = new Osc9ProgressEncoder();

function encodeCase(ProgramStatus $status, ?string $kind, Osc7501ProgramStatusEncoder $encoder7501, Osc9ProgressEncoder $encoder9): array
{
    $status = $kind === null ? $status : $status->withProtocolOption(Osc7501ProgramStatusEncoder::PROTOCOL, 'kind', $kind);

    return [
        'state' => $status->getState(),
        'message' => $status->getMessage(),
        'progress' => $status->getProgress(),
        'kind' => $kind,
        'osc7501' => $encoder7501->encode($status),
        'osc9' => $encoder9->encode($status),
    ];
}

$out = ['encode' => [], 'policy' => []];
$states = [
    ProgramStatus::STATE_IDLE,
    ProgramStatus::STATE_WORKING,
    ProgramStatus::STATE_DONE,
    ProgramStatus::STATE_BLOCKED,
    ProgramStatus::STATE_ERROR,
    ProgramStatus::STATE_CLEAR,
];
$progresses = [null, 0, 42, 100, -1, 101];
$kinds = [null, 'permission', 'question', 'auth', 'foobar', 'permission:msg=injected', false, 42];
foreach ($states as $state) {
    foreach ($progresses as $progress) {
        foreach ($kinds as $kind) {
            $out['encode'][] = encodeCase((new ProgramStatus($state))->withProgress($progress), $kind, $encoder7501, $encoder9);
        }
    }
}

$messages = [
    null,
    '',
    'Installing acme/foo',
    "<info>Approve?</info>\n\0\x7f\xc2\x85\033]2;title\007",
    "multi\nline message",
    str_repeat('a', 2049),
    str_repeat('a', 2046).'éx',
    str_repeat('a', 2047).'é',
    str_repeat('a', 2046).'€',
    str_repeat('a', 2045).'😀',
    'surrounding whitespace  ',
];
foreach ($states as $state) {
    foreach ($messages as $message) {
        $out['encode'][] = encodeCase((new ProgramStatus($state, $message))->withProgress(42), null, $encoder7501, $encoder9);
    }
}
$out['encode'][] = [
    'state' => ProgramStatus::STATE_WORKING,
    'message' => null,
    'progress' => null,
    'kind' => null,
    'start7501' => $encoder7501->encodeStart(),
    'start9' => $encoder9->encodeStart(),
];

$ghostty = ['TERM_PROGRAM' => 'ghostty', 'TERM_PROGRAM_VERSION' => '1.2.0'];
$environments = [
    [],
    ['COMPOSER_TERMINAL_STATUS' => 'auto'],
    ['COMPOSER_TERMINAL_STATUS' => ''],
    ['TERM' => 'xterm-256color'],
    ['WT_SESSION' => 'session-id'],
    ['ConEmuANSI' => 'ON'],
    ['ConEmuANSI' => 'OFF'],
    $ghostty,
    ['TERM_PROGRAM' => 'ghostty', 'TERM_PROGRAM_VERSION' => '1.3.0'],
    ['TERM_PROGRAM' => 'ghostty', 'TERM_PROGRAM_VERSION' => '1.1.3'],
    ['TERM_PROGRAM' => 'ghostty'],
    ['TERM_PROGRAM' => 'ghostty', 'TERM_PROGRAM_VERSION' => 'unknown'],
    ['TERM_PROGRAM' => 'iTerm.app', 'TERM_PROGRAM_VERSION' => '3.6.6'],
    ['TERM_PROGRAM' => 'iTerm.app', 'TERM_PROGRAM_VERSION' => '3.6.5'],
    ['TERM_PROGRAM' => 'Other', 'TERM_PROGRAM_VERSION' => '99.0.0'],
    $ghostty + ['TMUX' => '/socket'],
    $ghostty + ['STY' => 'session'],
    $ghostty + ['TERM' => 'screen-256color'],
    $ghostty + ['TERM' => 'tmux-256color'],
    $ghostty + ['SSH_CONNECTION' => 'connection'],
    $ghostty + ['SSH_TTY' => '/dev/pts/1'],
    $ghostty + ['COMPOSER_TERMINAL_STATUS' => 'off'],
    $ghostty + ['COMPOSER_TERMINAL_STATUS' => '7501'],
    ['COMPOSER_TERMINAL_STATUS' => '9;4'],
    ['COMPOSER_TERMINAL_STATUS' => '7501,9;4'],
    ['COMPOSER_TERMINAL_STATUS' => '7501,9;4', 'TMUX' => '/socket'],
    ['COMPOSER_TERMINAL_STATUS' => '9;4,7501,9;4'],
    ['COMPOSER_TERMINAL_STATUS' => ' 7501, 9;4 '],
    ['COMPOSER_TERMINAL_STATUS' => 'unknown'],
    ['COMPOSER_TERMINAL_STATUS' => '0'],
    ['COMPOSER_TERMINAL_STATUS' => 'false'],
    ['COMPOSER_TERMINAL_STATUS' => 'auto,7501'],
    ['COMPOSER_TERMINAL_STATUS' => 'off,9;4'],
    ['COMPOSER_TERMINAL_STATUS' => '7501,'],
    ['COMPOSER_TERMINAL_STATUS' => '7501,unknown'],
];
foreach ($environments as $environment) {
    $policy = new TerminalStatusPolicy($environment);
    $out['policy'][] = [
        'environment' => (object) $environment,
        'protocols' => array_map(static function ($encoder) {
            return $encoder->getCommand();
        }, $policy->getEncoders()),
    ];
}

file_put_contents(dirname(__DIR__, 3).'/internal/console/testdata/oracle/programstatus.json', json_encode($out, JSON_PRETTY_PRINT | JSON_UNESCAPED_SLASHES | JSON_INVALID_UTF8_SUBSTITUTE)."\n");
