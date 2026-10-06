<?php

/*
 * maestro's plugin shim: Composer\Console\Input\InputOption, with Composer
 * 2.10.3's behaviour: Symfony's InputOption with suggested values (the
 * backport of symfony/console 6.1's), for completion.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Console\Input;

use Symfony\Component\Console\Exception\LogicException;

class InputOption extends \Symfony\Component\Console\Input\InputOption
{
    private $suggestedValues;

    public function __construct(string $name, $shortcut = null, ?int $mode = null, string $description = '', $default = null, $suggestedValues = [])
    {
        parent::__construct($name, $shortcut, $mode, $description, $default);

        $this->suggestedValues = $suggestedValues;

        if ([] !== $suggestedValues && !$this->acceptValue()) {
            throw new LogicException('Cannot set suggested values if the option does not accept a value.');
        }
    }

    public function complete(\Symfony\Component\Console\Completion\CompletionInput $input, \Symfony\Component\Console\Completion\CompletionSuggestions $suggestions): void
    {
        $values = $this->suggestedValues;
        if ($values instanceof \Closure && !\is_array($values = $values($input, $suggestions))) {
            throw new LogicException(sprintf('Closure for argument "%s" must return an array. Got "%s".', $this->getName(), get_debug_type($values)));
        }
        if ([] !== $values) {
            $suggestions->suggestValues($values);
        }
    }
}
