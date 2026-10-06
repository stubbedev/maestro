<?php
// Generates internal/pkg/loader/testdata/oracle/datetime.json: new
// \DateTime($s, new \DateTimeZone('UTC')) on hand-picked strings (the
// relative forms package metadata hits: blank strings, military zone
// letters, bare digits), on timelib's relative formats ("+1 day", "next
// month", weekdays, "first/last day of", "back/front of", "ago", ...)
// after fixed dates and around DST changes, and on random concatenations
// of date fragments, for TestOracle_DateTime.
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
    // relative formats; an earlier error is PHP's
    '+1 day', '2012-01-01 +1 day', 'next monday', 'first day of next month', 'monday', 'last year', 'back of 7pm', '3 weeks ago',
    'garbage +1 day', '2012-01-01garbage next month', '1 2 day', 'j monday', '10:00 10:00 +1 day',
    'last day of feb 2024', 'first day of january 2025', 'last day of next month noon', 'first monday of january 2025',
    'last friday of next month', 'monday next week', 'sunday this week', 'saturday last week', 'next week', 'this week',
    'noon tomorrow', 'tomorrow noon', 'midnight +1 day', 'yesterday 14:00', '+1 day +1 day', '1 day 1 day ago', '+1 days ago ago',
    "next\xc2\xa0month", "+1\xc2\xa0day", "+1\xe2\x80\xafday", "first\xc2\xa0monday\xc2\xa0of", 'NEXT MONTH', 'Next Monday', 'LAST DAY OF',
    '+1 Day', 'FRONT OF 7', '+ 1 day', '+-1 day', '--1 day', '- - 2 days', "+\t1 day", '+1day', '+1 µs', '+1 µsec', '+1 µS', '2 MS',
    'back of 24', 'front of 0', 'back of 0am', 'front of 12pm', 'back of 7 p.m.', 'back of 7pm 10:00', '10:00 back of 7',
    'this', 'next', 'last', 'first', 'twelfth', 'eight', 'next day', 'this day', 'previous year', 'last week ago', 'this week ago',
    'first day of ago', 'weekday ago', '+3 weekdays ago', 'monday ago', 'sunday ago', '1 monday ago', '-1 monday', '0 monday', '+0 weekday',
    'weekdays', 'next weekday', 'last weekday', 'this weekday', '+1 weekdays', '2 weekday', '-7 weekdays', '+15 weekdays',
    '+9999999999999 days', '-9999999999999 days', '+9999999999999 years', '+9999999999999 seconds', '+9999999999999 ms',
    str_repeat('+9999999999999 msec ', 923), '@0 +1 day', '@0 -1 sec', '@1.5 +1 sec', '@1.5 -2 usec', '@1 ago', '+1 day @0',
    '2024-02-29 +1 year', '2024-02-29 -1 year', '2024-01-31 +1 month', '2024-03-31 -1 month', '2024-W01 +1 day', '2024W017 monday',
    'tuesday 2024-01-01', '2024-01-01 tuesday', '2024-01-01 10:00 tuesday', 'tuesday 10:00 2024-01-01', '+1 tuesday 2024-01-01 10:00',
    '2024-01-01 Europe/Copenhagen +1 day', '+1 day 2024-01-01 Europe/Copenhagen', '2024-01-01 CEST +1 day', '2024-01-01 -05:00 next month',
];

// relative formats after fixed dates (weekdays, month ends, leap days,
// the Epoch shortcut)
$bases = ['2024-01-31 10:20:30', '2024-02-29', '2023-12-31 23:59:59', '2026-10-04 12:00', '2026-10-10', '2026-10-06 10:00',
    '1970-01-01', '2000-03-01 00:00:00.5', '1969-12-31 23:59:59.999999', '0000-01-01', '-0001-12-31 12:00'];
$relatives = ['+1 day', '-1 day', '+1 month', '-1 month', 'next month', 'last month', '+1 year', 'next year', '+1 week', '+2 weeks',
    'next week', 'last week', 'this week', 'previous week', 'monday', 'sunday', 'saturday', 'next monday', 'last monday', 'this monday',
    'this sunday', 'previous friday', 'next sunday', 'last sunday', 'sat', 'tue', 'mondays', 'monday next week', 'sunday this week',
    'sunday last week', 'monday this week', 'friday next week', 'first day of', 'last day of', 'first day of next month',
    'last day of previous month', 'first day of this month', 'last day of +1 year', 'first monday of', 'last friday of',
    'first monday of next month', 'third wednesday of', 'last sunday of next month', 'second sunday of', 'twelfth friday of',
    'next sunday of', 'this monday of', 'last monday of this month', 'back of 7', 'front of 7', 'back of 7pm', 'front of 13', 'Back of 7',
    'back of 19 am', 'front of 0', '+1 weekday', '+5 weekdays', '-3 weekdays', '+0 weekdays', '-0 weekday', '-5 weekdays',
    '+6 weekdays', '-6 weekdays', '+4 weekdays', '-4 weekdays', 'weekday', 'next weekday', 'last weekday', '2 days ago', '1 month ago',
    '+1 week 2 days 4 hours 2 seconds', '+1 week 2 days 4 hours 2 seconds ago', 'monday ago', '1 monday ago', '2 mondays',
    '-2 fridays', '3 tuesdays', '+1 hour', '-90 minutes', '+3600 sec', '+1500 ms', '+1500000 usec', '-1 µs', '+1 fortnight',
    '+13 months', '-25 hours', '-1 year -1 month -1 day', '+1 day noon', 'midnight', 'tomorrow', 'yesterday', '+1 days ago',
    '1 sec ago', '-1 week ago', 'next month ago', 'first day of next month ago', 'last day of last month midnight', '+1 sat',
    '+30 days', '-366 days', '+1000000 hours', '-100000 minutes', '+59 seconds', '+1 msec', 'last year', 'this year', 'next fortnight'];
foreach ($bases as $base) {
    foreach ($relatives as $rel) {
        $cases[] = $base.' '.$rel;
    }
}
foreach (['first day of', 'last day of', 'next monday', 'monday', '+1 day', 'first monday of', 'back of 7'] as $rel) {
    foreach ($bases as $base) {
        $cases[] = $rel.' '.$base;
    }
}

// relative formats across DST changes: wall clock arithmetic for days,
// months and weekdays, and for hours, minutes and seconds too (timelib
// 2022 adds them to the wall clock before do_adjust_timezone)
$dstRelatives = ['+1 day', '+24 hours', '+1 hour', '-1 hour', '+30 minutes', '+3600 seconds', '-1 day', '-24 hours', 'tomorrow',
    'next sunday', 'first day of next month', '+1 weekday', '+2 hours ago', '+90 minutes', 'back of 2', 'front of 3'];
foreach (['Europe/Copenhagen', 'America/New_York', 'Australia/Lord_Howe'] as $zone) {
    $tz = new \DateTimeZone($zone);
    foreach ($tz->getTransitions(1704067200, 1735689600) as $i => $tr) {
        if ($i === 0) {
            continue;
        }
        for ($d = -10800; $d <= 10800; $d += 1800) {
            $local = (new \DateTime('@'.($tr['ts'] + $d)))->setTimezone($tz)->format('Y-m-d H:i:s');
            foreach ($dstRelatives as $rel) {
                $cases[] = $local.' '.$zone.' '.$rel;
            }
        }
    }
}

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
    '2012-01-01', '10:00', '10:00:00', '.5', 'noon', 'today', 'tomorrow', 'ago', '(', ')', 'GMT', 'W01', '2', '366', 'st', 'th',
    '+1', '-2', 'day', 'days', 'week', 'month', 'year', 'hour', 'sec', 'ms', 'µs', 'fortnight', 'next', 'last', 'this', 'first',
    'third', 'first day of', 'last day of', 'monday', 'sat', 'sundays', 'weekday', 'weekdays', 'of', 'back of 7', 'front of 7', 'pm'];
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
