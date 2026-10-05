<?php
// Generates internal/repository/testdata/providers.json: the data of the
// PHPUnit data providers of .ref/composer/tests/Composer/Test/Repository
// that the Go ports iterate over (PlatformRepositoryTest's are long), so
// that no case is retyped by hand.
//
// Values are JSON scalars, except:
//   {"a": [[key, value], ...]}   a PHP array, keys and order kept
//   {"rb": version}              a ResourceBundleStub (its get('Version'))
//   {"imagick": versionString}   an ImagickStub
//
// Run: php tools/oracle/repository/providers.php
namespace PHPUnit\Framework {
    abstract class TestCase {}
}

namespace {
    $root = dirname(__DIR__, 3);
    require $root.'/.ref/composer/vendor/autoload.php';
    require $root.'/.ref/composer/tests/Composer/Test/TestCase.php';

    use Composer\Test\Repository\ImagickStub;
    use Composer\Test\Repository\ResourceBundleStub;

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
        if ($v instanceof ResourceBundleStub) {
            return ['rb' => ResourceBundleStub::STUB_VERSION];
        }
        if ($v instanceof ImagickStub) {
            return ['imagick' => (fn () => $this->versionString)->call($v)];
        }
        if (is_object($v)) {
            throw new \RuntimeException('cannot encode '.get_class($v));
        }

        return $v;
    }

    $tests = $root.'/.ref/composer/tests/Composer/Test';
    $out = [];
    foreach (['Repository/PlatformRepositoryTest'] as $file) {
        require $tests.'/'.$file.'.php';
        $class = 'Composer\\Test\\'.str_replace('/', '\\', $file);
        $short = basename($file);
        foreach ((new ReflectionClass($class))->getMethods(ReflectionMethod::IS_STATIC) as $method) {
            if ($method->isPublic() && $method->getDeclaringClass()->getName() === $class && strpos($method->getName(), 'provide') === 0) {
                $out[$short.'::'.$method->getName()] = encode($method->invoke(null));
            }
        }
    }
    ksort($out);

    $lines = [];
    foreach ($out as $name => $data) {
        $lines[] = json_encode($name).': '.json_encode($data, JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE | JSON_THROW_ON_ERROR);
    }
    file_put_contents($root.'/internal/repository/testdata/providers.json', "{\n".implode(",\n", $lines)."\n}\n");
    fprintf(STDERR, "%d providers\n", count($out));
}
