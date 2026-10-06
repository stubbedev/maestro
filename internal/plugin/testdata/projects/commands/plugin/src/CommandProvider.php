<?php

namespace MaestroTest\Commands;

use Composer\Plugin\Capability\CommandProvider as CommandProviderCapability;

class CommandProvider implements CommandProviderCapability
{
    public function __construct(array $args)
    {
        if (!$args['composer'] instanceof \Composer\Composer || !$args['io'] instanceof \Composer\IO\IOInterface || !$args['plugin'] instanceof \Composer\Plugin\PluginInterface) {
            throw new \LogicException('unexpected capability arguments');
        }
    }

    public function getCommands()
    {
        return [new HelloCommand(), new NestedCommand()];
    }
}
