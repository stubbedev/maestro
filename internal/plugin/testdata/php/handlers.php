<?php

/*
 * PHP-side handlers of internal/plugin's runtime tests, loaded through
 * boot's `require`. They exercise the protocol from PHP: echoing values,
 * nesting calls, throwing, exiting, changing env and cwd, mirrors.
 */

use Maestro\Shim\Mirrors;
use Maestro\Shim\Rpc;
use Maestro\Shim\Server;
use Maestro\Shim\Sync;

final class MaestroTestStore
{
    /** @var \Throwable|null */
    public static $exception;

    /** @var array<string, object> */
    public static $objects = [];
}

class MaestroTestException extends \DomainException
{
}

/**
 * A data mirror for the tests: one Go-owned box with a value and a label.
 */
class MaestroTestBox
{
    private $value;
    private $label;

    public function __construct($value = null, $label = '')
    {
        $this->value = $value;
        $this->label = $label;
    }

    public function getValue()
    {
        return $this->value;
    }

    public function setValue($value)
    {
        $this->value = $value;
        Mirrors::touch($this, 'value');
    }

    public function getLabel()
    {
        return $this->label;
    }

    public function setLabel($label)
    {
        $this->label = $label;
        Mirrors::touch($this, 'label');
    }
}

class MaestroTestSubBox extends MaestroTestBox
{
}

final class MaestroTestBoxAdapter implements \Maestro\Shim\MirrorAdapter
{
    public function base(): string
    {
        return MaestroTestBox::class;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        $box = (new \ReflectionClass(class_exists($class) ? $class : MaestroTestBox::class))->newInstanceWithoutConstructor();
        $this->apply($box, $snapshot);

        return $box;
    }

    public function snapshot($object): array
    {
        return $this->fields($object, ['value', 'label']);
    }

    public function fields($object, array $names): array
    {
        $read = function (array $names) {
            $out = [];
            foreach ($names as $name) {
                $out[$name] = $this->$name;
            }

            return $out;
        };

        return \Closure::bind($read, $object, MaestroTestBox::class)($names);
    }

    public function apply($object, array $fields): void
    {
        $write = function (array $fields) {
            foreach ($fields as $name => $value) {
                $this->$name = $value;
            }
        };
        \Closure::bind($write, $object, MaestroTestBox::class)($fields);
    }
}

Mirrors::register(new MaestroTestBoxAdapter());

Server::register('test.echo', function ($a) {
    return $a;
});

// Calls maestro's method $a['m'] with $a['a'] and returns its result.
Server::register('test.call', function ($a) {
    return Rpc::call($a['m'], isset($a['a']) ? $a['a'] : null);
});

// The re-entrancy torture: PHP and Go call each other alternately, each
// level adding its mark, down to depth 0, where maestro's handler may
// throw.
Server::register('test.down', function ($a) {
    $n = $a['n'];
    $trail = $a['trail'].'p'.$n;
    if ($n === 0) {
        if (!empty($a['throw'])) {
            throw new MaestroTestException('bottom '.$trail, 42);
        }

        return $trail;
    }

    return Rpc::call('test.down', ['n' => $n - 1, 'trail' => $trail, 'throw' => !empty($a['throw'])]);
});

Server::register('test.throw', function ($a) {
    throw new MaestroTestException($a['message'], $a['code']);
});

Server::register('test.storeAndThrow', function ($a) {
    MaestroTestStore::$exception = new MaestroTestException('stored', 7);

    throw MaestroTestStore::$exception;
});

// Exception identity (D12): maestro's handler calls test.storeAndThrow and
// returns its exception as its error; PHP must catch the same object.
Server::register('test.identity', function ($a) {
    try {
        Rpc::call('test.bounce');
    } catch (\Throwable $e) {
        return [
            'same' => $e === MaestroTestStore::$exception,
            'class' => get_class($e),
            'previousSame' => $e->getPrevious() === MaestroTestStore::$exception,
            'message' => $e->getMessage(),
        ];
    }

    return ['same' => false, 'class' => null];
});

// Calls maestro's method that fails and reports what PHP caught.
Server::register('test.catch', function ($a) {
    try {
        Rpc::call($a['m'], isset($a['a']) ? $a['a'] : null);
    } catch (\Throwable $e) {
        $previous = $e->getPrevious();

        return [
            'class' => get_class($e),
            'message' => $e->getMessage(),
            'code' => $e->getCode(),
            'previous' => $previous === null ? null : get_class($previous).': '.$previous->getMessage(),
        ];
    }

    return null;
});

Server::register('test.exit', function ($a) {
    echo 'exiting', PHP_EOL;
    exit($a['code']);
});

Server::register('test.fatal', function ($a) {
    ini_set('memory_limit', '16M');
    $s = str_repeat('x', 64 * 1024 * 1024);

    return strlen($s);
});

Server::register('test.registerShutdown', function ($a) {
    register_shutdown_function(function () use ($a) {
        echo $a['echo'];
        if (isset($a['callGo'])) {
            echo Rpc::call($a['callGo']);
        }
        if (isset($a['exit'])) {
            exit($a['exit']);
        }
    });
});

Server::register('test.putenv', function ($a) {
    foreach ($a['set'] as $name => $value) {
        putenv($name.'='.$value);
    }
    foreach (isset($a['unset']) ? $a['unset'] : [] as $name) {
        putenv($name);
    }
});

Server::register('test.getenv', function ($a) {
    $out = [];
    foreach ($a as $name) {
        $out[$name] = [getenv($name), isset($_SERVER[$name]) ? $_SERVER[$name] : false];
    }

    return $out;
});

Server::register('test.chdir', function ($a) {
    chdir($a);
});

Server::register('test.getcwd', function () {
    return getcwd();
});

Server::register('test.statics', function ($a) {
    $before = [
        'runningCommand' => Sync::getStatic('runningCommand'),
        'processTimeout' => Sync::getStatic('processTimeout'),
    ];
    foreach (isset($a['set']) ? $a['set'] : [] as $name => $value) {
        Sync::setStatic($name, $value);
    }

    return $before;
});

// Keeps an object PHP received, by name.
Server::register('test.keep', function ($a) {
    MaestroTestStore::$objects[$a['name']] = $a['object'];

    return true;
});

Server::register('test.kept', function ($a) {
    return MaestroTestStore::$objects[$a];
});

Server::register('test.same', function ($a) {
    return MaestroTestStore::$objects[$a['name']] === $a['object'];
});

Server::register('test.boxSet', function ($a) {
    $box = MaestroTestStore::$objects[$a['name']];
    if (array_key_exists('value', $a)) {
        $box->setValue($a['value']);
    }
    if (array_key_exists('label', $a)) {
        $box->setLabel($a['label']);
    }
    if (isset($a['callGo'])) {
        return Rpc::call($a['callGo'], $box);
    }

    return null;
});

Server::register('test.boxGet', function ($a) {
    $box = MaestroTestStore::$objects[$a];

    return ['class' => get_class($box), 'value' => $box->getValue(), 'label' => $box->getLabel()];
});

// A PHP-born mirror crosses to maestro, which adopts it (reg).
Server::register('test.newBox', function ($a) {
    $class = $a['class'];
    $box = new $class($a['value'], $a['label']);
    MaestroTestStore::$objects[$a['name']] = $box;
    $result = Rpc::call('test.adopt', $box);

    return [
        'result' => $result,
        'handle' => \Maestro\Shim\Handles::lookup($box),
    ];
});

Server::register('test.closure', function ($a) {
    $f = function () {
    };
    MaestroTestStore::$objects['closure'] = $f;

    return [$f, $f, new \ArrayObject([])];
});

Server::register('test.unsupported', function () {
    return (new \Composer\Util\Filesystem())->normalizePath('/a');
});

Server::register('test.classes', function ($a) {
    $out = [];
    foreach ($a as $class) {
        $out[$class] = class_exists($class) || interface_exists($class);
    }

    return $out;
});

Server::register('test.errorHandler', function () {
    // A warning becomes an \ErrorException under Composer's ErrorHandler.
    try {
        $x = [];
        $y = $x['missing'];
    } catch (\ErrorException $e) {
        return $e->getMessage();
    }

    return null;
});

Server::register('test.server', function () {
    return [
        'argv' => $_SERVER['argv'],
        'argc' => $_SERVER['argc'],
        'globalArgv' => $GLOBALS['argv'],
        'script' => $_SERVER['SCRIPT_NAME'],
        'ipc' => getenv('MAESTRO_IPC'),
        'ipcServer' => isset($_SERVER['MAESTRO_IPC']),
        'composerBinary' => getenv('COMPOSER_BINARY'),
        'locale' => setlocale(LC_ALL, 0),
        'errorReporting' => error_reporting() === E_ALL,
        'memoryLimit' => ini_get('memory_limit'),
        'polyfill' => function_exists('str_contains') && class_exists('Symfony\\Polyfill\\Php80\\Php80'),
        'autoloadFiles' => isset($GLOBALS['__composer_autoload_files']['a4a119a56e50fbb293281d9a48007e0e']),
    ];
});

// exit() while a grandchild still holds the channel's fds open: maestro
// must notice the exit without waiting for EOF.
Server::register('test.exitLeavingGrandchild', function ($a) {
    exec('sleep '.(int) $a['seconds'].' > /dev/null 2>&1 &');
    exit($a['code']);
});

// Sends a value that cannot cross (a resource) with pending sync state:
// the call fails in PHP, and the state still reaches maestro later.
Server::register('test.unsendable', function ($a) {
    putenv($a['name'].'=pending');
    try {
        Rpc::call('test.never', fopen('php://memory', 'r'));
    } catch (\InvalidArgumentException $e) {
        return $e->getMessage();
    }

    return null;
});
