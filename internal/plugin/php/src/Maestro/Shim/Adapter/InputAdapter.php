<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim\Adapter;

use Maestro\Shim\Definitions;
use Maestro\Shim\PolledMirrorAdapter;
use Maestro\Shim\Remote;
use Symfony\Component\Console\Input\ArgvInput;
use Symfony\Component\Console\Input\ArrayInput;
use Symfony\Component\Console\Input\Input;
use Symfony\Component\Console\Input\StringInput;

/**
 * maestro's inputs as PHP mirrors them (docs/PLUGINS.md §5.9): a vendored
 * ArgvInput, StringInput or ArrayInput with maestro's tokens or
 * parameters, definition, arguments, options and interactivity. What PHP
 * code changes in it while it stays bound to maestro's definition
 * (setArgument(), setOption(), setInteractive(), the tokens) goes back to
 * maestro; once rebound (a command run in PHP binds it to its own
 * definition) it is PHP's.
 */
final class InputAdapter implements PolledMirrorAdapter
{
    const BASE = 'Maestro\Shim\Mirror\Input';

    /** @var array<int, array<string, mixed>> the state maestro has, by spl_object_id */
    private $known = [];

    public function base(): string
    {
        return self::BASE;
    }

    public function create(int $handle, string $class, array $snapshot)
    {
        if (!in_array($class, [ArgvInput::class, StringInput::class, ArrayInput::class], true)) {
            $class = ArgvInput::class;
        }
        $input = (new \ReflectionClass($class))->newInstanceWithoutConstructor();
        $this->apply($input, $snapshot);

        return $input;
    }

    /**
     * What maestro builds an input created in PHP from (it adopts it when
     * it first crosses): its tokens or parameters and interactivity.
     */
    public function snapshot($object): array
    {
        if ($object instanceof ArrayInput) {
            $d = ['kind' => 'array', 'parameters' => Remote::read($object, ArrayInput::class, ['parameters'])['parameters']];
        } else {
            $d = ['kind' => $object instanceof StringInput ? 'string' : 'argv', 'tokens' => Remote::read($object, ArgvInput::class, ['tokens'])['tokens']];
        }
        $d['interactive'] = $object->isInteractive();

        return $d;
    }

    public function fields($object, array $names): array
    {
        return [];
    }

    public function apply($object, array $fields): void
    {
        $props = [];
        if (isset($fields['definition'])) {
            $props['definition'] = Definitions::build($fields['definition']);
        }
        foreach (['arguments', 'options'] as $name) {
            if (array_key_exists($name, $fields)) {
                $props[$name] = $fields[$name];
            }
        }
        if (array_key_exists('interactive', $fields)) {
            $props['interactive'] = (bool) $fields['interactive'];
        }
        if ($props !== []) {
            Remote::fill($object, Input::class, $props);
        }
        if (array_key_exists('tokens', $fields) && $object instanceof ArgvInput) {
            Remote::fill($object, ArgvInput::class, ['tokens' => $fields['tokens']]);
        }
        if (array_key_exists('parameters', $fields) && $object instanceof ArrayInput) {
            Remote::fill($object, ArrayInput::class, ['parameters' => $fields['parameters']]);
        }
        $this->known[spl_object_id($object)] = self::state($object);
    }

    public function poll($object): ?array
    {
        $id = spl_object_id($object);
        $state = self::state($object);
        $known = isset($this->known[$id]) ? $this->known[$id] : null;
        $this->known[$id] = $state;
        if ($known === null || $known['definition'] !== $state['definition']) {
            return null;
        }

        $changed = [];
        foreach (['arguments', 'options', 'interactive', 'tokens'] as $name) {
            if ($state[$name] !== $known[$name]) {
                $changed[$name] = $state[$name];
            }
        }

        return $changed === [] ? null : $changed;
    }

    /**
     * @return array<string, mixed>
     */
    private static function state(Input $input): array
    {
        $state = Remote::read($input, Input::class, ['definition', 'arguments', 'options', 'interactive']);
        $state['tokens'] = $input instanceof ArgvInput ? Remote::read($input, ArgvInput::class, ['tokens'])['tokens'] : null;

        return $state;
    }
}
