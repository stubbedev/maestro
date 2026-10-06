<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * Exceptions crossing the channel (docs/PLUGINS.md §5.10, D12). A PHP
 * exception goes to maestro with its handle, so when maestro hands it back
 * the very same object is rethrown; an error raised by maestro becomes an
 * instance of the PHP class it names.
 */
final class Exceptions
{
    /**
     * The "x" of an "err" message for a PHP throwable.
     *
     * @return array<string, mixed>
     */
    public static function toWire(\Throwable $e): array
    {
        $class = get_class($e);
        $classes = array_values(array_unique(array_merge([$class], array_values(class_parents($e)), array_values(class_implements($e)))));

        // The frames down to the call maestro made, which maestro
        // completes with Composer's (docs/PLUGINS.md §5.12).
        $trace = Traces::toMaestro($e);
        // thrown in a bundled library: Composer's vendor/ file
        list($file, $line) = Traces::composerLocation($e->getFile(), $e->getLine());

        $previous = $e->getPrevious();

        return [
            'class' => $class,
            'classes' => $classes,
            'message' => $e->getMessage(),
            'code' => $e->getCode(),
            'file' => $file,
            'line' => $line,
            'trace' => $trace,
            'previous' => $previous !== null ? self::toWire($previous) : null,
            'h' => Handles::handleOf($e),
            'extra' => new \stdClass(),
        ];
    }

    /**
     * The throwable to throw for the decoded "x" of an "err" message.
     */
    public static function fromWire(array $x): \Throwable
    {
        if (isset($x['h']) && Handles::has((int) $x['h'])) {
            $original = Handles::get((int) $x['h']);
            if ($original instanceof \Throwable) {
                // Composer's frames maestro added as it went up, then
                // the stack it goes on in.
                if (isset($x['trace']) && is_array($x['trace'])) {
                    Traces::throwInto($original, $x['trace']);
                }

                return $original;
            }
        }

        $previous = isset($x['previous']) && is_array($x['previous']) ? self::fromWire($x['previous']) : null;
        $class = isset($x['class']) ? (string) $x['class'] : 'RuntimeException';
        $message = isset($x['message']) ? (string) $x['message'] : '';
        $code = isset($x['code']) ? $x['code'] : 0;

        $e = self::create($class, $message, $code, $previous);
        // The class's own properties (a TransportException's response, a
        // JsonValidationException's errors).
        if (isset($x['props']['scope'], $x['props']['values']) && is_a($e, (string) $x['props']['scope'])) {
            Remote::fill($e, (string) $x['props']['scope'], $x['props']['values']);
        }
        // maestro's throw site: the Composer file and line that throw it,
        // and the frames of Composer's stack down to there, then the stack
        // it is thrown into (docs/PLUGINS.md §5.12).
        if (isset($x['file']) && is_string($x['file']) && $x['file'] !== '') {
            self::locate($e, $x['file'], isset($x['line']) ? (int) $x['line'] : 0);
        }
        Traces::throwInto($e, isset($x['trace']) && is_array($x['trace']) ? $x['trace'] : []);

        return $e;
    }

    /**
     * $e thrown at Composer's line in Composer's file the shim file stands
     * for, so that Symfony's "In <file> line <n>:" heading (and -v's "at"
     * line) names Composer's throw site, as for maestro's errors.
     *
     * @template T of \Throwable
     * @param T $e
     * @return T
     */
    public static function at(\Throwable $e, int $line): \Throwable
    {
        self::locate($e, Traces::composerFile($e->getFile()), $line);

        return $e;
    }

    /**
     * Sets the file and line of a throwable.
     */
    private static function locate(\Throwable $e, string $file, int $line): void
    {
        $scope = $e instanceof \Exception ? \Exception::class : \Error::class;
        foreach (['file' => $file, 'line' => $line] as $name => $value) {
            $property = new \ReflectionProperty($scope, $name);
            if (PHP_VERSION_ID < 80100) {
                $property->setAccessible(true);
            }
            $property->setValue($e, $value);
        }
    }

    /**
     * An instance of $class with the given message, code and previous,
     * without running its constructor (Composer's exception classes take
     * other arguments, and stubs throw). An unknown or unusable class
     * gives a \RuntimeException.
     *
     * @param mixed $code
     */
    public static function create(string $class, string $message, $code, ?\Throwable $previous): \Throwable
    {
        if (class_exists($class) && is_subclass_of($class, \Throwable::class)) {
            $r = new \ReflectionClass($class);
            if (!$r->isAbstract()) {
                try {
                    $e = $r->newInstanceWithoutConstructor();
                    self::fill($e, $message, $code, $previous);

                    return $e;
                } catch (\Throwable $ignored) {
                }
            }
        }

        return new \RuntimeException($message, is_int($code) ? $code : 0, $previous);
    }

    /**
     * Sets the message, code and previous of a throwable built without its
     * constructor.
     *
     * @param mixed $code
     */
    private static function fill(\Throwable $e, string $message, $code, ?\Throwable $previous): void
    {
        // Closures cannot be bound to an internal class's scope; reflection
        // can reach its protected and private properties.
        $scope = $e instanceof \Exception ? \Exception::class : \Error::class;
        foreach (['message' => $message, 'code' => $code, 'previous' => $previous] as $name => $value) {
            $property = new \ReflectionProperty($scope, $name);
            if (PHP_VERSION_ID < 80100) {
                $property->setAccessible(true);
            }
            $property->setValue($e, $value);
        }
    }
}
