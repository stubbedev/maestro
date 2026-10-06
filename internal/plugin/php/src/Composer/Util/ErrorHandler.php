<?php

/*
 * maestro's plugin shim: Composer\Util\ErrorHandler, reimplemented with
 * Composer 2.10.3's behaviour (docs/PLUGINS.md §5.2 step 8): the same
 * \ErrorExceptions, the same notices shown at the same verbosity. Composer
 * has one ErrorHandler for its code and plugin code alike: its
 * $hasShownDeprecationNotice is the process's (a static maestro keeps in
 * step with its own, internal/util's TriggerDeprecation). How a notice or
 * warning looks is maestro's (docs/PORTING.md "The contract", #13):
 * maestro renders it (`ui.diagnostic`, internal/ui), without Composer's
 * source paths and PHP stacks. Locations in the shim's Composer classes
 * are Composer's (Traces::composerLocation()).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Util;

use Composer\IO\IOInterface;
use Maestro\Shim\Rpc;
use Maestro\Shim\Sync;
use Maestro\Shim\Traces;

/**
 * Turns PHP errors into \ErrorException, and reports deprecation notices
 * through the IO.
 */
class ErrorHandler
{
    /** @var ?IOInterface */
    private static $io;

    /**
     * @throws \ErrorException
     */
    public static function handle(int $level, string $message, string $file, int $line): bool
    {
        $isDeprecationNotice = $level === E_DEPRECATED || $level === E_USER_DEPRECATED;

        // error code is not included in error_reporting
        if (!$isDeprecationNotice && 0 === (error_reporting() & $level)) {
            return true;
        }

        if (filter_var(ini_get('xdebug.scream'), FILTER_VALIDATE_BOOLEAN)) {
            $message .= "\n\nWarning: You have xdebug.scream enabled, the warning above may be".
            "\na legitimately suppressed error that you were not supposed to see.";
        }

        $raisedIn = $file;
        list($file, $line) = Traces::composerLocation($file, $line);

        if (!$isDeprecationNotice) {
            // ignore some newly introduced warnings in new php versions until dependencies
            // can be fixed as we do not want to abort execution for those
            if (in_array($level, [E_WARNING, E_USER_WARNING], true) && strpos($message, 'should either be used or intentionally ignored by casting it as (void)') !== false) {
                // where plugin code raised it; a location in the shim (one
                // of Composer's files) tells the user nothing
                $where = $file === $raisedIn ? ' in '.$file.':'.$line : '';
                self::outputWarning('warning', 'Ignored new PHP warning but it should be reported and fixed: '.$message.$where, true);

                return true;
            }

            throw new \ErrorException($message, 0, $level, $file, $line);
        }

        if (self::$io !== null) {
            $shown = Sync::getStatic('hasShownDeprecationNotice');
            if ($shown > 0 && !self::$io->isVerbose()) {
                if ($shown === 1) {
                    self::outputWarning('note', 'More deprecation notices were hidden, run again with `-v` to show them.');
                    Sync::setStatic('hasShownDeprecationNotice', 2);
                }

                return true;
            }
            Sync::setStatic('hasShownDeprecationNotice', 1);
            self::outputWarning('deprecation', $message);
        }

        return true;
    }

    public static function register(?IOInterface $io = null): void
    {
        set_error_handler([__CLASS__, 'handle']);
        error_reporting(E_ALL);
        self::$io = $io;
    }

    /**
     * Reports a diagnostic of $kind (deprecation, note or warning) on the
     * IO's error output, as maestro renders it; without an IO, a warning
     * goes to STDERR, undecorated, as maestro's plain rendering has it.
     */
    private static function outputWarning(string $kind, string $message, bool $outputEvenWithoutIO = false): void
    {
        if (self::$io !== null) {
            Rpc::call('ui.diagnostic', [self::$io, $kind, $message]);

            return;
        }

        if ($outputEvenWithoutIO) {
            if (defined('STDERR') && is_resource(STDERR)) {
                fwrite(STDERR, 'Warning: '.$message.PHP_EOL);
            } else {
                echo 'Warning: '.$message.PHP_EOL;
            }
        }
    }
}
