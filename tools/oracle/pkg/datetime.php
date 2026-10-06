<?php
// Generates internal/pkg/loader/testdata/oracle/datetime.json: new
// \DateTime($s, new \DateTimeZone('UTC')) on hand-picked strings (the
// relative forms package metadata hits: blank strings, military zone
// letters, bare digits) and on random concatenations of date fragments,
// for TestOracle_DateTime.
//
// Each case is [string, result]: {"e": message} for the exception, else
// {"r": [Y-m-d\TH:i:sP, timestamp, microseconds], "now": [Y-m-d H:i:s,
// us before, us after]}: "now" is the clock around the call (both reads
// in the same second), which the fields the string leaves unset come from.
//
// Run: php tools/oracle/pkg/datetime.php
$out = dirname(__DIR__, 3).'/internal/pkg/loader/testdata/oracle/datetime.json';

mt_srand(20261006);

$cases = ['', ' ', '  ', "\t", "\n", "\v", "\x00", " \x00", "a\x00b", "2012-01-01\x00x", "\r\n2012-01-01\n", " \v ", "\v2012",
    '0', '1', '12', '123', '1234', '12345', '123456', '1234567', '12345678', '123456789', '1234567890', '12345678901', '123456789012',
    '99', '24', '25', '2400', '2460', '0000', '9999', '99999', '000000', '235959', '240000', '246060', '00000000', '20121301',
    '99999999', '20120101', '2012001', '2012366', '2012367', '1700000000', '17000000000', ' 12', '1 ', '0 a', '1a', '1234 a',
    '12345678 a', '12 a', '12345 a', '1234 5678', '1234 5678 9012', '1234 2012-01-01', '2012-01-01 1234', '2012-01-01 1234 5678',
    'now', 'NOW', 'today', 'midnight', 'noon', 'tomorrow', 'yesterday', 'yesterday noon', 'tomorrow 10:00', 'now UTC', 'today Z',
    'ago', 'tomorrow ago', '@5 ago', '@5 tomorrow', 'UTC @5', '@5 UTC', '@1 @2', 'noon noon', 'today today',
    '@', '@1', '@-1', '@0', '@1.5', '@-1.5', '@1.', '@1.1234567', '@--1', '@ 1', '@-0.5', '@1234567890123456789', '@99999999999999999999',
    '@9223372036854775808', '@1700000000.123456',
    '10am', '10 am', '10am UTC', '12am', '12pm', '12:30 a.m.', '1:02:03pm', '10:00:00.5 am', '13am', '10:00:00:123am',
    'Jan 1 2012', '1 January 2012', 'January 2012', '2012 January', 'Jan', 'jan 1', '1 jan', 'Jan-01-2012', '2012-Jan-01', 'IV',
    '1st March 2012', '01/Oct/2000:13:55:36 -0700', 'Jan 1 10:00', 'Jan 1 10:00pm', 'Jan 1 10:00:00 CEST', 'XII 2012',
    '2012-W01', '2012W015', '2012-W53-7', '2026-W40', '2012-W54',
    '12/25', '12/25/2012', '12/25/12', '1/2/70', '13/1/2012', '01.02.2012', '1.2.12', '1-2-2012', '2012-02', '2012-2', '12-1-1', '70-01-01',
    '+2012-01-01', '-2012-01-01', '+12345-01-01', '-12345-01-01', '2012/01/02/', '2012/1/2',
    '20120101T101010', '20120101t101010', '2012-01-01T10:10:10.5+01:00', '2012-1-1T1:2:3', '2012:01:01 10:10:10', '20120101T10:10:10',
    '2012.123', '2012-123', '2012123',
    'T', 't', 'tt', 'T10', 't10', 't1010', 't101010', '10', '10:', '10:00', '10.00', '1.5', '10:00:60', '10:60', '24:00', '25:00', '10:00:00.123',
    'a', 'A', 'j', 'J', 'z', 'Z', 'm', 'n', 'y', 'x', 'ab', ' a ', 'a ', 'a1', 'a b', 'a a a', 'x2012',
    'UTC', 'utc', 'GMT', 'gmt', 'Utc', 'CEST', 'cest', 'EST', 'est', 'IST', 'abcde', 'abcdef', 'abcdefg', '(CEST)', '(UTC', 'UTC)',
    'Europe/Paris', 'europe/paris', 'America/Argentina/Buenos_Aires', 'Europe/Nowhere', 'Etc/GMT-5', 'Local', 'GMT+5', 'GMT-0530',
    '+1', '+01', '+0100', '+01:00', '+1:3', '+01:3', '+1:30', '+053045', '+05:30:45', '+1234567', '-25', '+99', '-', '+', '+:', '--1',
    '2012-01-01 Europe/Paris', '2012-07-01 Europe/Paris', '2012-01-01 Local', '2012-01-01 (CEST)', 'CEST CEST', 'CEST CEST CEST',
    '2012-01-01 a', '2012-01-01 x', '2012-01-01T', '2012-01-01garbage', '2012-01-01 2012-01-01', '10:00 10:00',
    '2012-01-01T10:00:00Z', '2012-01-01T10:00:00 +05', '2012-01-01T10:00:00+0530', '2012-01-01 10:00:00 PST', '2012-01-01 10:00 Europe/Paris',
    "2012-01-01\xc2\xa010:00", "2012-01-01\xe2\x80\xaf10:00", "\xc2\xa0", "2012\xc3\xa9", "\xff",
    '2012-02-30', '2012-00-00', '0000-00-00', '0000-00-00 00:00:00', '9999-12-31 23:59:59', '1970-01-01 00:00:00',
    '2012-01-01,10:00', '2012-01-01.', ',', '.', '..', ';', '2012-01-01;',
    '2012-03-25 02:30:00 Europe/Paris', '2012-10-28 02:30:00 Europe/Paris', '2012-03-11 02:30 America/New_York',
    '2012-11-04 01:30 America/New_York', '2012-11-04 01:30 EST', '2012-11-04 01:30 EDT',
    // relative formats maestro does not evaluate; an earlier error is still PHP's
    '+1 day', '2012-01-01 +1 day', 'next monday', 'first day of next month', 'monday', 'last year', 'back of 7pm', '3 weeks ago',
    'garbage +1 day', '2012-01-01garbage next month', '1 2 day', 'j monday', '10:00 10:00 +1 day',
];

// wall clock times around DST changes (gaps and overlaps)
foreach (['Europe/Paris', 'America/New_York', 'Australia/Sydney', 'Australia/Lord_Howe', 'America/Sao_Paulo', 'Europe/London'] as $zone) {
    foreach ((new \DateTimeZone($zone))->getTransitions(1325376000, 1356998400) as $i => $tr) {
        if ($i === 0) {
            continue;
        }
        for ($d = -10800; $d <= 10800; $d += 1800) {
            $cases[] = gmdate('Y-m-d H:i:s', $tr['ts'] + $d).' '.$zone;
        }
    }
}

$fragments = ['2012', '-', '01', '/', ':', 'T', 't', ' ', '  ', '1', '12', '123', '1234', '20120101', '+', '-05:00', '+0100', 'Z', 'a',
    'j', 'x', 'UTC', 'CEST', 'Europe/Paris', 'am', 'pm', '.', ',', '@', '0', '99', '60', '24', 'Jan', 'march', 'IV', 'garbage', "\t",
    '2012-01-01', '10:00', '10:00:00', '.5', 'noon', 'today', 'tomorrow', 'ago', '(', ')', 'GMT', 'W01', '2', '366', 'st', 'th'];
for ($n = 0; $n < 3000; $n++) {
    $s = '';
    $k = mt_rand(1, 5);
    for ($j = 0; $j < $k; $j++) {
        $s .= $fragments[mt_rand(0, count($fragments) - 1)];
    }
    $cases[] = $s;
}
$cases = array_values(array_unique($cases));

$utc = new \DateTimeZone('UTC');
$result = [];
foreach ($cases as $s) {
    for ($try = 0; ; $try++) {
        $before = new \DateTime('now', $utc);
        try {
            $d = new \DateTime($s, $utc);
            $r = ['r' => [$d->format('Y-m-d\TH:i:sP'), $d->getTimestamp(), (int) $d->format('u')]];
        } catch (\Exception $e) {
            $r = ['e' => $e->getMessage()];
        }
        $after = new \DateTime('now', $utc);
        if ($before->format('U') === $after->format('U') || $try > 100) {
            break;
        }
    }
    if (isset($r['r'])) {
        $r['now'] = [$before->format('Y-m-d H:i:s'), (int) $before->format('u'), (int) $after->format('u')];
    }
    if (isset($r["e"]) && !mb_check_encoding($r["e"], "UTF-8")) {
        $r["e"] = ["hex" => bin2hex($r["e"])];
    }
    $result[] = [mb_check_encoding($s, "UTF-8") ? $s : ["hex" => bin2hex($s)], $r];
}

$json = "[\n";
foreach ($result as $i => $c) {
    $json .= json_encode($c, JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR).($i < count($result) - 1 ? ",\n" : "\n");
}
file_put_contents($out, $json."]\n");
