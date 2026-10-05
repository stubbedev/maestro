<?php
// Generates internal/semver/testdata/providers.json: the data of every
// PHPUnit data provider in .ref/semver/tests, which the Go ports of those
// tests iterate over, so that no case is retyped by hand.
//
// Values are JSON scalars, except:
//   {"a": [[key, value], ...]}        a PHP array, keys and order kept
//   {"c": [operator, version]}        a Constraint
//   {"m": [conjunctive, [values]]}    a MultiConstraint
//   {"all": true} / {"none": true}    MatchAllConstraint / MatchNoneConstraint
//   {"bound": [version, inclusive]}   a Bound
//
// Run: php tools/oracle/semver/providers.php
namespace PHPUnit\Framework {
    abstract class TestCase {}
}

namespace {
    $root = dirname(__DIR__, 3);
    require $root.'/.ref/composer/vendor/autoload.php';

    use Composer\Semver\Constraint\Bound;
    use Composer\Semver\Constraint\Constraint;
    use Composer\Semver\Constraint\MatchAllConstraint;
    use Composer\Semver\Constraint\MatchNoneConstraint;
    use Composer\Semver\Constraint\MultiConstraint;

    function encode($v)
    {
        if (is_array($v)) {
            $pairs = [];
            foreach ($v as $k => $item) {
                $pairs[] = [$k, encode($item)];
            }

            return ['a' => $pairs];
        }
        if ($v instanceof Constraint) {
            return ['c' => [$v->getOperator(), $v->getVersion()]];
        }
        if ($v instanceof MultiConstraint) {
            return ['m' => [$v->isConjunctive(), array_map('encode', $v->getConstraints())]];
        }
        if ($v instanceof MatchAllConstraint) {
            return ['all' => true];
        }
        if ($v instanceof MatchNoneConstraint) {
            return ['none' => true];
        }
        if ($v instanceof Bound) {
            return ['bound' => [$v->getVersion(), $v->isInclusive()]];
        }
        if (is_object($v)) {
            throw new \RuntimeException('cannot encode '.get_class($v));
        }

        return $v;
    }

    $tests = $root.'/.ref/semver/tests';
    $out = [];
    foreach (['ComparatorTest', 'CompilingMatcherTest', 'IntervalsTest', 'SemverTest', 'SubsetsTest', 'VersionParserTest',
        'Constraint/ConstraintTest', 'Constraint/MatchAllConstraintTest', 'Constraint/MatchNoneConstraintTest',
        'Constraint/MultiConstraintTest'] as $file) {
        require $tests.'/'.$file.'.php';
        $class = 'Composer\\Semver\\'.str_replace('/', '\\', $file);
        $short = basename($file);
        foreach ((new ReflectionClass($class))->getMethods(ReflectionMethod::IS_STATIC) as $method) {
            if ($method->isPublic() && strpos($method->getName(), 'test') !== 0) {
                $out[$short.'::'.$method->getName()] = encode($method->invoke(null));
            }
        }
    }
    ksort($out);

    // One provider per line keeps the file small and diffable.
    $lines = [];
    foreach ($out as $name => $data) {
        $lines[] = json_encode($name).': '.json_encode($data, JSON_UNESCAPED_SLASHES | JSON_THROW_ON_ERROR);
    }
    file_put_contents($root.'/internal/semver/testdata/providers.json', "{\n".implode(",\n", $lines)."\n}\n");
    fprintf(STDERR, "%d providers\n", count($out));
}
