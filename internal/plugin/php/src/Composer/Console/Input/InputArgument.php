<?php

/*
 * maestro's plugin shim: Composer\Console\Input\InputArgument, with
 * Composer 2.10.3's behaviour: Symfony's InputArgument with suggested values
 * (the backport of symfony/console 6.1's), for completion.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Console\Input;

use Symfony\Component\Console\Exception\LogicException;

class InputArgument extends \Symfony\Component\Console\Input\InputArgument
{
    private $suggestedValues;

    public function __construct(string $name, ?int $mode = null, string $description = '', $default = null, $suggestedValues = [])
    {
        parent::__construct($name, $mode, $description, $default);

        $this->suggestedValues = $suggestedValues;
    }

    public function complete(\Symfony\Component\Console\Completion\CompletionInput $input, \Symfony\Component\Console\Completion\CompletionSuggestions $suggestions): void
    {
        $values = $this->suggestedValues;
        if ($values instanceof \Closure && !\is_array($values = $values($input, $suggestions))) {
            throw new LogicException(sprintf('Closure for option "%s" must return an array. Got "%s".', $this->getName(), get_debug_type($values)));
        }
        if ([] !== $values) {
            $suggestions->suggestValues($values);
        }
    }
}
