<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

use Symfony\Component\Console\Input\InputArgument;
use Symfony\Component\Console\Input\InputDefinition;
use Symfony\Component\Console\Input\InputOption;

/**
 * Input definitions crossing the channel as values (docs/PLUGINS.md §5.7):
 * a list of arguments and a list of options with their constructors'
 * parameters.
 */
final class Definitions
{
    /**
     * @return array<string, list<array<string, mixed>>>
     */
    public static function describe(InputDefinition $definition): array
    {
        $arguments = [];
        foreach ($definition->getArguments() as $argument) {
            $arguments[] = [
                'name' => $argument->getName(),
                'mode' => ($argument->isRequired() ? InputArgument::REQUIRED : InputArgument::OPTIONAL) | ($argument->isArray() ? InputArgument::IS_ARRAY : 0),
                'description' => $argument->getDescription(),
                'default' => $argument->isRequired() ? null : $argument->getDefault(),
            ];
        }

        $options = [];
        foreach ($definition->getOptions() as $option) {
            $mode = Remote::read($option, InputOption::class, ['mode'])['mode'];
            $options[] = [
                'name' => $option->getName(),
                'shortcut' => $option->getShortcut(),
                'mode' => $mode,
                'description' => $option->getDescription(),
                'default' => ($mode & InputOption::VALUE_NONE) !== 0 ? null : $option->getDefault(),
            ];
        }

        return ['arguments' => $arguments, 'options' => $options];
    }

    /**
     * @param array<string, mixed> $description
     */
    public static function build(array $description): InputDefinition
    {
        $items = [];
        foreach ($description['arguments'] as $a) {
            $items[] = new InputArgument($a['name'], $a['mode'], $a['description'], $a['default']);
        }
        foreach ($description['options'] as $o) {
            $items[] = new InputOption($o['name'], $o['shortcut'], $o['mode'], $o['description'], ($o['mode'] & InputOption::VALUE_NONE) !== 0 ? null : $o['default']);
        }

        return new InputDefinition($items);
    }
}
