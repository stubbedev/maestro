<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Maestro\Shim\GoConsoleOutput;
use Maestro\Shim\GoOutput;
use Maestro\Shim\PolledMirrorAdapter;
use Symfony\Component\Console\Formatter\OutputFormatter;
use Symfony\Component\Console\Output\ConsoleOutput;
use Symfony\Component\Console\Output\OutputInterface;

/**
 * maestro's outputs as PHP mirrors them (docs/PLUGINS.md §5.9, D11): a
 * vendored ConsoleOutput writing to the inherited stdout and stderr when
 * maestro's output is its console, else a GoOutput writing through
 * maestro; both with maestro's verbosity, decoration and Composer's styles.
 * PHP's setVerbosity() and setDecorated() go back to maestro.
 */
final class OutputAdapter implements PolledMirrorAdapter
{
    const BASE = 'Maestro\Shim\Mirror\Output';

    /** @var array<int, array{int, bool}> the state maestro has, by spl_object_id */
    private $known = [];

    public function base(): string
    {
        return self::BASE;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        $verbosity = (int) $snapshot['verbosity'];
        $decorated = (bool) $snapshot['decorated'];
        $formatter = new OutputFormatter($decorated, \Composer\Factory::createAdditionalStyles());
        switch ($class) {
            case ConsoleOutput::class:
                $output = new ConsoleOutput($verbosity, $decorated, $formatter);
                break;
            case GoConsoleOutput::class:
                $output = new GoConsoleOutput($verbosity, $decorated, $formatter);
                break;
            default:
                $output = new GoOutput($verbosity, $decorated, $formatter);
        }
        $this->known[spl_object_id($output)] = [$verbosity, $decorated];

        return $output;
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
        /** @var OutputInterface $object */
        if (array_key_exists('verbosity', $fields)) {
            $object->setVerbosity((int) $fields['verbosity']);
        }
        if (array_key_exists('decorated', $fields)) {
            $object->setDecorated((bool) $fields['decorated']);
        }
        $this->known[spl_object_id($object)] = [$object->getVerbosity(), $object->isDecorated()];
    }

    public function poll($object): ?array
    {
        /** @var OutputInterface $object */
        $id = spl_object_id($object);
        $state = [$object->getVerbosity(), $object->isDecorated()];
        $known = isset($this->known[$id]) ? $this->known[$id] : $state;
        $this->known[$id] = $state;

        $changed = [];
        if ($state[0] !== $known[0]) {
            $changed['verbosity'] = $state[0];
        }
        if ($state[1] !== $known[1]) {
            $changed['decorated'] = $state[1];
        }

        return $changed === [] ? null : $changed;
    }
}
