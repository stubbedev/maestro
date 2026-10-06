<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.6 "Promises"). Not part of
 * Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

use React\Promise\Deferred;
use React\Promise\PromiseInterface;

/**
 * Promises crossing the channel. Composer's installers return
 * React\Promise\PromiseInterface; maestro's are its own (util.Promise),
 * which settle on the goroutine driving its event loop. Each side stands
 * in for the other's pending promise with one of its own, settled when the
 * original settles, so then() callbacks run when they would in Composer's
 * single loop: a callback of a maestro promise runs in PHP while maestro
 * settles it, and maestro's continuations of a PHP promise run while PHP
 * settles it.
 *
 * Rejection reasons cross as exceptions (docs/PLUGINS.md D12): the side
 * that needs one calls the other, which throws it.
 */
final class Promises
{
    /** @var int the last id given to a PHP promise maestro waits on */
    private static $last = 0;

    /**
     * PHP promises maestro waits on, by id: their state, their rejection
     * reason, and whether maestro waits for the settlement (it does once
     * watch() returned pending).
     *
     * @var array<int, array{state: string, reason: \Throwable|null, live: bool}>
     */
    private static $watched = [];

    /**
     * The deferreds standing for pending maestro promises, by maestro's id.
     *
     * @var array<int, Deferred>
     */
    private static $deferreds = [];

    public static function register(): void
    {
        Server::register('promise.settle', [self::class, 'settle']);
        Server::register('promise.reason', [self::class, 'reason']);
    }

    /**
     * Hands a PHP promise to maestro: its id and its state ("fulfilled",
     * "rejected" or "pending"). A pending promise tells maestro when it
     * settles (`promise.settled`); maestro asks for a rejection's reason
     * (`promise.reason`).
     *
     * @return array{id: int, s: string}
     */
    public static function watch(PromiseInterface $promise): array
    {
        $id = ++self::$last;
        self::$watched[$id] = ['state' => 'pending', 'reason' => null, 'live' => false];
        $promise->then(function () use ($id): void {
            self::settled($id, 'fulfilled', null);
        }, function ($reason) use ($id): void {
            self::settled($id, 'rejected', $reason);
        });

        $state = self::$watched[$id]['state'];
        if ($state === 'pending') {
            self::$watched[$id]['live'] = true;
        } elseif ($state === 'fulfilled') {
            unset(self::$watched[$id]);
        }

        return ['id' => $id, 's' => $state];
    }

    /**
     * @param mixed $reason
     */
    private static function settled(int $id, string $state, $reason): void
    {
        if (!$reason instanceof \Throwable && $state === 'rejected') {
            $reason = new \UnexpectedValueException('Promise rejected with a non-throwable reason');
        }
        self::$watched[$id]['state'] = $state;
        self::$watched[$id]['reason'] = $reason;
        if (!self::$watched[$id]['live']) {
            return;
        }
        if ($state === 'fulfilled') {
            unset(self::$watched[$id]);
        }
        Rpc::call('promise.settled', [$id, $state === 'fulfilled']);
    }

    /**
     * `promise.reason`: throws the rejection reason of a watched promise.
     *
     * @param array<string, mixed> $a
     * @return never
     */
    public static function reason(array $a): void
    {
        $id = (int) $a['id'];
        if (!isset(self::$watched[$id]) || self::$watched[$id]['reason'] === null) {
            throw new ProtocolException('maestro shim: promise '.$id.' was not rejected');
        }
        $reason = self::$watched[$id]['reason'];
        unset(self::$watched[$id]);

        throw $reason;
    }

    /**
     * The PHP promise standing for a maestro promise ({id, s}, and the
     * value v of a fulfilled one).
     *
     * @param array<string, mixed> $p
     */
    public static function fromMaestro(array $p): PromiseInterface
    {
        if ($p['s'] === 'fulfilled') {
            return \React\Promise\resolve(isset($p['v']) ? $p['v'] : null);
        }

        $deferred = new Deferred();
        if ($p['s'] === 'rejected') {
            self::reject($deferred, (int) $p['id']);
        } else {
            self::$deferreds[(int) $p['id']] = $deferred;
        }

        return $deferred->promise();
    }

    /**
     * `promise.settle`: a maestro promise settled; its deferred settles
     * the same way, running the then() callbacks.
     *
     * @param array<string, mixed> $a
     */
    public static function settle(array $a): void
    {
        $id = (int) $a['id'];
        if (!isset(self::$deferreds[$id])) {
            throw new ProtocolException('maestro shim: unknown promise '.$id);
        }
        $deferred = self::$deferreds[$id];
        unset(self::$deferreds[$id]);

        if ($a['ok']) {
            $deferred->resolve(isset($a['v']) ? $a['v'] : null);
        } else {
            self::reject($deferred, $id);
        }
    }

    /**
     * Rejects a deferred with the rejection reason of maestro's promise
     * $id, which maestro throws.
     */
    private static function reject(Deferred $deferred, int $id): void
    {
        try {
            Rpc::call('promise.rejection', [$id]);
        } catch (\Throwable $e) {
            $deferred->reject($e);

            return;
        }

        throw new ProtocolException('maestro shim: promise '.$id.' has no rejection reason');
    }
}
