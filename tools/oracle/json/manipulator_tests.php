<?php
// Generates internal/json/testdata/manipulator/tests.json: every test of
// .ref/composer/tests/Composer/Test/Json/JsonManipulatorTest.php, run with
// every data set of its data provider, recorded as the steps the Go port
// replays: the manipulators created, every method call with its arguments
// and result, and every assertion with its expected value. An assertion's
// actual value must be the result of the call just before it (the harness
// checks), so the replay asserts the test's own expected values against
// the Go results.
//
// Run: php tools/oracle/json/manipulator_tests.php
namespace RealJson {
    $root = dirname(__DIR__, 3);
    require $root.'/.ref/composer/vendor/autoload.php';
    // The real JsonManipulator, renamed so the recording proxy can take its
    // name.
    $src = file_get_contents($root.'/.ref/composer/src/Composer/Json/JsonManipulator.php');
    $src = str_replace('namespace Composer\Json;', "namespace RealJson;\nuse Composer\\Json\\JsonFile;", $src);
    eval(substr($src, strlen('<?php')));
}

namespace Composer\Json {
    class JsonManipulator
    {
        /** @var \RealJson\JsonManipulator */
        private $real;

        public function __construct(string $contents)
        {
            \Rec::$steps[] = ['op' => 'new', 'contents' => \enc($contents)];
            try {
                $this->real = new \RealJson\JsonManipulator($contents);
            } catch (\Throwable $e) {
                \Rec::$steps[count(\Rec::$steps) - 1]['result'] = \encException($e);
                throw $e;
            }
        }

        public function __call(string $name, array $args)
        {
            $step = ['op' => 'call', 'method' => $name, 'args' => array_map('enc', $args)];
            try {
                $result = $this->real->$name(...$args);
            } catch (\Throwable $e) {
                $step['result'] = \encException($e);
                \Rec::$steps[] = $step;
                throw $e;
            }
            $step['result'] = \enc($result);
            \Rec::$steps[] = $step;
            \Rec::$last = $result;
            \Rec::$hasLast = true;

            return $result;
        }
    }
}

namespace Composer\Test {
    abstract class TestCase
    {
        private static function record(string $kind, $expected, $actual): void
        {
            if (!\Rec::$hasLast || \Rec::$last !== $actual) {
                throw new \LogicException('assertion on a value that is not the last call result');
            }
            \Rec::$steps[] = ['op' => 'assert', 'kind' => $kind, 'expected' => \enc($expected)];
            \Rec::$hasLast = false;
        }

        public static function assertTrue($actual): void
        {
            self::record('same', true, $actual);
        }

        public static function assertFalse($actual): void
        {
            self::record('same', false, $actual);
        }

        public static function assertEquals($expected, $actual): void
        {
            if (gettype($expected) !== gettype($actual) || !is_scalar($expected)) {
                throw new \LogicException('assertEquals on values of different or non-scalar types');
            }
            self::record('same', $expected, $actual);
        }

        public static function assertSame($expected, $actual): void
        {
            self::record('same', $expected, $actual);
        }

        public static function assertJsonStringEqualsJsonString($expected, $actual): void
        {
            self::record('json', $expected, $actual);
        }
    }
}

namespace {
    require __DIR__.'/manipulator_common.php';

    class Rec
    {
        /** @var list<array<string, mixed>> */
        public static $steps = [];
        /** @var mixed */
        public static $last;
        /** @var bool */
        public static $hasLast = false;
    }

    $root = dirname(__DIR__, 3);
    require $root.'/.ref/composer/tests/Composer/Test/Json/JsonManipulatorTest.php';

    $class = new ReflectionClass(Composer\Test\Json\JsonManipulatorTest::class);
    $out = [];
    foreach ($class->getMethods(ReflectionMethod::IS_PUBLIC) as $method) {
        if (strpos($method->name, 'test') !== 0) {
            continue;
        }
        $sets = [[]];
        if (preg_match('{@dataProvider\s+(\w+)}', (string) $method->getDocComment(), $m)) {
            $sets = $class->getMethod($m[1])->invoke(null);
        }
        $cases = [];
        foreach ($sets as $key => $args) {
            Rec::$steps = [];
            Rec::$hasLast = false;
            $test = $class->newInstanceWithoutConstructor();
            try {
                $method->invokeArgs($test, $args);
                $end = null;
            } catch (Throwable $e) {
                $end = encException($e);
            }
            $cases[] = ['name' => (string) $key, 'steps' => Rec::$steps, 'end' => $end];
        }
        $out[] = ['test' => $method->name, 'cases' => $cases];
    }

    writeJson($root.'/internal/json/testdata/manipulator/tests.json', $out);
    fprintf(STDERR, "%d tests, %d cases\n", count($out), array_sum(array_map(fn ($t) => count($t['cases']), $out)));
}
