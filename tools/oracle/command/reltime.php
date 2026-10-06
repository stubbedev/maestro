<?php
// Writes internal/command/testdata/reltime.json: ShowCommand::getRelativeTime
// and the DateTime::diff fields it reads, for release dates as package
// metadata gives them (new \DateTime($time, new \DateTimeZone('UTC'))) and
// "now" in several default time zones. Run from the repository root:
//   php tools/oracle/command/reltime.php
// (Composer 2.10.3's getRelativeTime, with date('Ymd') and
// new \DateTimeImmutable() taken at a fixed moment.)

function relativeTime(DateTimeInterface $releaseDate, DateTimeImmutable $now): string
{
    if ($releaseDate->format('Ymd') === $now->format('Ymd')) {
        return 'today';
    }

    $diff = $releaseDate->diff($now);
    if ($diff->days < 7) {
        return 'this week';
    }

    if ($diff->days < 14) {
        return 'last week';
    }

    if ($diff->m < 1 && $diff->days < 31) {
        return floor($diff->days / 7) . ' weeks ago';
    }

    if ($diff->y < 1) {
        return $diff->m . ' month' . ($diff->m > 1 ? 's' : '') . ' ago';
    }

    return $diff->y . ' year' . ($diff->y > 1 ? 's' : '') . ' ago';
}

mt_srand(20261006);

$zones = ['UTC', 'Europe/Copenhagen', 'America/New_York', 'Asia/Kolkata', 'Pacific/Auckland', 'Pacific/Kiritimati', 'America/St_Johns'];
$offsets = ['+00:00', '', '+05:30', '-08:00', '+14:00', '-11:00', '+01:00'];
// day deltas around the boundaries getRelativeTime cares about
$deltas = [0, 1, 6, 7, 13, 14, 27, 28, 29, 30, 31, 32, 58, 59, 60, 61, 89, 90, 91, 364, 365, 366, 729, 730, 731, 1100, -1, -40];

$cases = [];
foreach (range(1, 4000) as $n) {
    $tz = $zones[mt_rand(0, count($zones) - 1)];
    $offset = $offsets[mt_rand(0, count($offsets) - 1)];
    // month ends and DST transitions are where the borrowing differs
    $y = mt_rand(2015, 2026);
    $m = mt_rand(1, 12);
    $d = mt_rand(0, 3) === 0 ? (int) date('t', gmmktime(0, 0, 0, $m, 1, $y)) - mt_rand(0, 2) : mt_rand(1, 28);
    $release = sprintf('%04d-%02d-%02dT%02d:%02d:%02d%s', $y, $m, $d, mt_rand(0, 23), mt_rand(0, 59), mt_rand(0, 59), $offset);
    $releaseDate = new DateTime($release, new DateTimeZone('UTC'));

    $delta = $deltas[mt_rand(0, count($deltas) - 1)] * 86400 + mt_rand(-86400, 86400);
    $ts = $releaseDate->getTimestamp() + $delta;
    $us = mt_rand(0, 999999);
    $now = DateTimeImmutable::createFromFormat('U.u', sprintf('%d.%06d', $ts, $us))->setTimezone(new DateTimeZone($tz));

    $diff = $releaseDate->diff($now);
    $cases[] = [
        'release' => $release,
        'now' => $ts,
        'us' => $us,
        'tz' => $tz,
        'y' => $diff->y,
        'm' => $diff->m,
        'days' => $diff->days,
        'want' => relativeTime($releaseDate, $now),
    ];
}

file_put_contents(__DIR__.'/../../../internal/command/testdata/reltime.json', json_encode(['php' => PHP_VERSION, 'cases' => $cases], JSON_UNESCAPED_SLASHES)."\n");
