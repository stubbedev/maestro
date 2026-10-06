<?php

namespace MaestroTest\StubsPlugin;

use Composer\Plugin\Capability\CommandProvider as CommandProviderCapability;

class CommandProvider implements CommandProviderCapability
{
    public function getCommands()
    {
        return [new BuiltinCommand(), new HelpersCommand(), new IoCommand()];
    }
}
