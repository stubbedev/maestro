<?php
// Generates internal/pkg/testdata/providers.json: the data of the PHPUnit
// data providers in .ref/composer/tests/Composer/Test/Package (and
// Util/PackageSorterTest) that the Go ports of those tests iterate over, so
// that no case is retyped by hand.
//
// Values are JSON scalars, except:
//   {"a": [[key, value], ...]}        a PHP array, keys and order kept
//   {"c": [operator, version]}        a Constraint
//   {"link": [source, target, constraint, description, prettyConstraint]}
//   {"date": "Y-m-d\TH:i:sP"}         a DateTime
//
// Run: php tools/oracle/pkg/providers.php
namespace PHPUnit\Framework {
    abstract class TestCase {}
}

namespace {
    $root = dirname(__DIR__, 3);
    require $root.'/.ref/composer/vendor/autoload.php';
    require $root.'/.ref/composer/tests/Composer/Test/TestCase.php';

    use Composer\Package\Link;
    use Composer\Semver\Constraint\Constraint;

    function encode($v)
    {
        if ($v instanceof \Generator) {
            $v = iterator_to_array($v);
        }
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
        if ($v instanceof Link) {
            return ['link' => [$v->getSource(), $v->getTarget(), encode($v->getConstraint()), $v->getDescription(), $v->getPrettyConstraint()]];
        }
        if ($v instanceof \DateTimeInterface) {
            return ['date' => $v->format(DATE_RFC3339)];
        }
        if (is_object($v)) {
            throw new \RuntimeException('cannot encode '.get_class($v));
        }

        return $v;
    }

    $tests = $root.'/.ref/composer/tests/Composer/Test';
    $out = [];
    foreach (['Package/BasePackageTest', 'Package/CompletePackageTest', 'Package/Dumper/ArrayDumperTest', 'Package/Loader/ArrayLoaderTest',
        'Package/Loader/ValidatingArrayLoaderTest', 'Package/Version/VersionBumperTest', 'Package/Version/VersionParserTest',
        'Package/Version/VersionSelectorTest'] as $file) {
        require $tests.'/'.$file.'.php';
        $class = 'Composer\\Test\\'.str_replace('/', '\\', $file);
        $short = basename($file);
        foreach ((new ReflectionClass($class))->getMethods(ReflectionMethod::IS_STATIC) as $method) {
            if ($method->isPublic() && $method->getDeclaringClass()->getName() === $class && strpos($method->getName(), 'test') !== 0
                && $method->getName() !== 'provideMaliciousPackages') {
                $out[$short.'::'.$method->getName()] = encode($method->invoke(null));
            }
        }
    }
    ksort($out);

    $lines = [];
    foreach ($out as $name => $data) {
        $lines[] = json_encode($name).': '.json_encode($data, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR);
    }
    file_put_contents($root.'/internal/pkg/testdata/providers.json', "{\n".implode(",\n", $lines)."\n}\n");
    fprintf(STDERR, "%d providers\n", count($out));
}
