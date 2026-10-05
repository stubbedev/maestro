<?php
// Generates internal/json/jsonschema/testdata/oracle/generic.json: the errors
// (or exception message) of the real justinrainbow/json-schema 6.10.0
// Validator for hand-written schemas exercising every draft-03/04 keyword
// the default check mode implements (beyond what Composer's own schemas
// use), each over a corpus of generated documents.
//
// Run: php tools/oracle/json/schema_generic.php
$root = dirname(__DIR__, 3);
require $root.'/.ref/composer/vendor/autoload.php';

use JsonSchema\Validator;

error_reporting(E_ALL & ~E_DEPRECATED & ~E_WARNING & ~E_NOTICE);
mt_srand(20261006);

$schemas = [
    '{"type":"object","properties":{"a":{"type":"integer","minimum":2,"maximum":5,"exclusiveMaximum":true},"b":{"type":"string"}},"required":["a","b"],"additionalProperties":false}',
    '{"patternProperties":{"^x-":{"type":"string"},"[":{"type":"string"},"^[0-9]+$":{"type":"integer"}},"additionalProperties":{"type":"number"}}',
    '{"type":"array","items":[{"type":"string"},{"type":"integer"}],"additionalItems":false,"minItems":1,"maxItems":3,"uniqueItems":true}',
    '{"type":"array","items":[{"type":"string"},{"type":"integer"},{"type":"boolean"}]}',
    '{"type":"array","items":{"type":"string"},"additionalItems":{"type":"integer"}}',
    '{"items":{"type":"integer"},"uniqueItems":true}',
    '{"type":["string","null",{"type":"object"}],"minLength":2,"maxLength":4,"pattern":"^a"}',
    '{"dependencies":{"a":["b","c"],"b":"c","c":{"required":["d"]}}}',
    '{"minProperties":2,"maxProperties":3,"properties":{"o":{"minProperties":1,"maxProperties":1,"type":"object"}}}',
    '{"multipleOf":0.5,"minimum":0,"exclusiveMinimum":true}',
    '{"divisibleBy":3,"maximum":10.5}',
    '{"exclusiveMaximum":true,"exclusiveMinimum":false}',
    '{"not":{"type":"string"},"disallow":["integer"]}',
    '{"disallow":"string"}',
    '{"enum":[1,"1",[1],{"a":1},null,true,1.5]}',
    '{"const":{"a":[1,2]}}',
    '{"properties":{"a":{"const":"x"},"b":{"enum":["x"],"required":["q"]}}}',
    '{"anyOf":[{"type":"string"},{"type":"integer","minimum":3}],"oneOf":[{"maximum":10},{"minimum":5}],"allOf":[{"type":["integer","string"]}]}',
    '{"oneOf":[{"type":"object","required":["a"]},{"type":"object","required":["b"]}]}',
    '{"properties":{"d":{"format":"date"},"t":{"format":"time"},"dt":{"format":"date-time"},"u":{"format":"utc-millisec"},"r":{"format":"regex"},"c":{"format":"color"},"s":{"format":"style"},"p":{"format":"phone"},"uri":{"format":"uri"},"ur":{"format":"uri-reference"},"e":{"format":"email"},"ip":{"format":"ipv4"},"ip6":{"format":"ipv6"},"h":{"format":"hostname"},"x":{"format":"unknown"}}}',
    '{"additionalProperties":{"format":"uri"}}',
    '{"additionalProperties":{"format":"date-time"}}',
    '{"additionalProperties":{"format":"email"}}',
    '{"additionalProperties":{"format":"hostname"}}',
    '{"additionalProperties":{"format":"ipv6"}}',
    '{"properties":{"a":{"required":true,"type":"string"},"b":{"requires":"a"},"c":{"required":false}}}',
    '{"definitions":{"a":{"$ref":"#/definitions/b"},"b":{"type":"integer"},"loop":{"$ref":"#/definitions/loop"}},"properties":{"x":{"$ref":"#/definitions/a"},"y":{"$ref":"#/definitions/missing"},"z":{"$ref":"#/definitions/loop"},"w":{"$ref":"#/definitions/a","type":"string"}}}',
    '{"definitions":{"list":{"type":"array","items":{"$ref":"#"}}},"type":["array","integer"],"items":{"$ref":"#/definitions/list/items"}}',
    '{"type":"any"}',
    '{"type":"foo"}',
    '{"properties":{"a":{"type":["foo","string"]}}}',
    '{"type":0}',
    '{"items":true,"additionalProperties":true}',
    '{"properties":{"a b":{"type":"string"},"c/d":{"type":"string"},"e~f":{"type":"string"},"1.5":{"type":"string"},"g.":{"type":"string"},"100":{"type":"string"},"%25":{"type":"string"}}}',
    '{"type":"object","properties":{"$schema":{"type":"integer"}},"additionalProperties":false}',
    '{"patternProperties":{"^a":{}},"additionalProperties":false}',
    '{"minItems":2,"maxItems":"3"}',
    '{"maxLength":3,"minLength":"2"}',
    '{"properties":{"a":{"properties":{"b":{"properties":{"c":{"type":"integer"}}}}}}}',
];

$keys = ['a', 'b', 'c', 'd', 'o', 'w', 'x', 'y', 'z', 'q', 'x-a', '[', '0', '12', '$schema', 'a b', 'c/d', 'e~f', '1.5', 'g.', '100', '%25',
    't', 'dt', 'u', 'r', 's', 'p', 'uri', 'ur', 'e', 'ip', 'ip6', 'h'];
$scalars = [
    '', 'a', 'ab', 'abcde', 'abc', 'ä', 'aé', 'x', 's', '1', '1.5', '7', '-3', '1e3', ' 5', '5 ', 0, 1, 2, 3, 4, 5, 6, 10, 11, 12, -1, 0.5, 1.5, 2.25, 3.0, 9.0, 10.5, 1e20, true, false, null,
    '2020-02-29', '2021-02-29', '2020-13-01', '2020-1-01', '0000-01-01', '23:59:59', '24:00:00', '12:60:00', '1:00:00',
    '2020-01-01T23:59:60Z', '2020-01-01t10:00:00z', '2020-01-01 10:00:00+01:00', '2020-01-01T10:00:00.1234567Z', '2020-01-01T10:00:00.123456Z',
    '2020-12-31T23:59:60Z', '2020-01-01T00:59:60+01:00', '2020-02-30T10:00:00Z', '2020-01-01T10:00:00+0100',
    '1234', '-5', '012', '+5', '^a(', '[a-z]+', 'a~b', '#fff', '#ffff', 'Red', 'rebeccapurple', '#A0b1C2', 'color: red;', 'a:b;c:d', 'nocolon',
    '555 123 4567', '+(555) 123 4567', '5551234567',
    'https://example.org', 'http://a..b/x', 'https://x:0/', 'https://x:00/', 'https://x:65536/', 'https://x/a b', 'https://x/a^b', 'https://[::1]:80/p?q#f',
    'mailto:a@b.co', 'mailto:nope', 'news:comp.lang', 'news:a..b', 'tel:+1 (555) 123', 'tel:abc', 'urn:x', 'data:,x', 'foo:bar',
    '../a/b', '//host/p', '///a', '://x', ':/x', 'a\\b', '#frag', '?q', 'a b',
    'a@b.co', 'a@b', 'é@ü.de', 'a..b@c.de', '"a b"@c.de', 'a@[127.0.0.1]', 'a@[IPv6:::1]', 'x@-a.de',
    '1.2.3.4', '01.2.3.4', '256.1.1.1', '1.2.3', '::1', '::ffff:1.2.3.4', 'fe80::1%eth0', '1:2:3:4:5:6:7:8:9', '1::2::3', '2001:db8::',
    'a.b', '-a.b', 'a_b', 'a.b-', str_repeat('a', 64).'.b', 'localhost',
];

function pick(array $a) { return $a[mt_rand(0, count($a) - 1)]; }

function randomValue(int $depth)
{
    global $keys, $scalars;
    $r = mt_rand(0, 9);
    if ($depth > 2 || $r < 5) {
        return pick($scalars);
    }
    if ($r < 7) {
        $list = [];
        for ($i = mt_rand(0, 4); $i > 0; $i--) {
            $list[] = randomValue($depth + 1);
        }

        return $list;
    }
    $o = new stdClass();
    for ($i = mt_rand(0, 5); $i > 0; $i--) {
        $o->{pick($keys)} = randomValue($depth + 1);
    }

    return $o;
}

$cases = [];
foreach ($schemas as $schemaJson) {
    $docs = [];
    foreach ($scalars as $s) {
        $docs[] = $s;
    }
    foreach ($keys as $k) {
        foreach (array_slice($scalars, mt_rand(0, count($scalars) - 12), 10) as $s) {
            $docs[] = (object) [$k => $s];
        }
    }
    for ($i = 0; $i < 150; $i++) {
        $docs[] = randomValue(0);
    }
    $seen = [];
    foreach ($docs as $doc) {
        $json = json_encode($doc, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_PRESERVE_ZERO_FRACTION);
        if ($json === false || isset($seen[$json])) {
            continue;
        }
        $seen[$json] = true;
        $validator = new Validator();
        $data = json_decode($json);
        $case = ['schema' => $schemaJson, 'doc' => $json];
        try {
            $validator->validate($data, json_decode($schemaJson));
            $errors = [];
            foreach ($validator->getErrors() as $error) {
                $errors[] = [$error['property'], $error['pointer'], $error['message'], $error['constraint']['name'], var_export($error['constraint']['params'], true), $error['context']];
            }
            $case['errors'] = $errors;
        } catch (\Throwable $e) {
            $case['e'] = $e->getMessage();
        }
        $cases[] = $case;
    }
}

$lines = [];
foreach ($cases as $case) {
    $lines[] = json_encode($case, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
}
file_put_contents($root.'/internal/json/jsonschema/testdata/oracle/generic.json', "[\n".implode(",\n", $lines)."\n]\n");
fprintf(STDERR, "%d cases\n", count($cases));
