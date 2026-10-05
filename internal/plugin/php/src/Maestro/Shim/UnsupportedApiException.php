<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Thrown by every member of Composer's API the shim does not implement
 * (docs/PLUGINS.md D7), and for a call to maestro that it has no handler
 * for. The message names the exact member.
 */
class UnsupportedApiException extends \LogicException
{
}
