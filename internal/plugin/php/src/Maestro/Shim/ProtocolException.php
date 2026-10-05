<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * A violation of the IPC protocol (docs/PLUGINS.md §6). The channel
 * cannot be trusted afterwards, so the shim ends the process.
 */
final class ProtocolException extends \RuntimeException
{
}
