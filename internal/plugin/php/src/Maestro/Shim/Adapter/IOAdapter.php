<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Composer\IO\BaseIO;
use Composer\IO\ConsoleIO;
use Composer\IO\NullIO;
use Maestro\Shim\MirrorAdapter;
use Maestro\Shim\Remote;
use Symfony\Component\Console\Helper\HelperSet;
use Symfony\Component\Console\Helper\QuestionHelper;

/**
 * maestro's IO as PHP mirrors it (docs/PLUGINS.md §5.9): a ConsoleIO (or
 * BufferIO, NullIO) whose flags are fields maestro keeps current:
 * interactive, decorated, verbose, veryVerbose, debug.
 */
final class IOAdapter implements MirrorAdapter
{
    public function base(): string
    {
        return BaseIO::class;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        if (!class_exists($class) || !is_a($class, BaseIO::class, true)) {
            $class = ConsoleIO::class;
        }
        $io = (new \ReflectionClass($class))->newInstanceWithoutConstructor();
        $this->apply($io, $snapshot);

        return $io;
    }

    public function snapshot($object): array
    {
        return [];
    }

    public function fields($object, array $names): array
    {
        return [];
    }

    public function apply($object, array $fields): void
    {
        if ($object instanceof ConsoleIO) {
            // Composer's protected members (docs/PLUGINS.md §5.12): the
            // run's input and output, and the helper set Application::doRun
            // gives its ConsoleIO.
            $members = [];
            foreach (['input', 'output'] as $name) {
                if (array_key_exists($name, $fields)) {
                    $members[$name] = $fields[$name];
                    unset($fields[$name]);
                }
            }
            $state = Remote::read($object, ConsoleIO::class, ['maestroState', 'helperSet']);
            if ($members !== [] && $state['helperSet'] === null) {
                $members['helperSet'] = new HelperSet([new QuestionHelper()]);
            }
            Remote::fill($object, ConsoleIO::class, ['maestroState' => $fields + $state['maestroState']] + $members);
        }
    }

    /**
     * A flag of a mirrored ConsoleIO.
     *
     * @return bool
     */
    public static function state(ConsoleIO $io, string $key)
    {
        if (!Remote::owned($io)) {
            Remote::unsupported(get_class($io), 'is'.ucfirst($key));
        }
        $state = Remote::read($io, ConsoleIO::class, ['maestroState'])['maestroState'];

        return !empty($state[$key]);
    }
}
