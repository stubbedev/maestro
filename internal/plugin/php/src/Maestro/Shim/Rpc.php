<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

/**
 * The IPC channel to maestro (docs/PLUGINS.md §6): framing, the message
 * envelope and the re-entrant call stack.
 *
 * Exactly one side runs at any moment. While PHP waits for the reply to
 * one of its calls it serves maestro's calls, to any depth, so calls nest
 * both ways and the conversation is strictly stack-shaped: a reply always
 * answers the innermost outstanding call of the other side.
 */
final class Rpc
{
    const PROTOCOL = 1;

    /** The largest frame either side accepts (§6.1). */
    const MAX_FRAME = 1073741824;

    /** @var resource|null maestro → PHP */
    private static $in;

    /** @var resource|null PHP → maestro */
    private static $out;

    /** @var int the last call id PHP allocated */
    private static $last = 0;

    /** @var list<int> PHP's outstanding calls, innermost last */
    private static $pending = [];

    /** @var bool whether maestro is gone */
    private static $lost = false;

    /** @var int how many messages PHP received */
    private static $received = 0;

    /**
     * Opens the channel MAESTRO_IPC names: "fd:3,4" (inherited pipes) or
     * "tcp:127.0.0.1:<port>" (loopback socket).
     */
    public static function connect(string $spec): void
    {
        if (preg_match('{^fd:(\d+),(\d+)$}', $spec, $m) === 1) {
            $in = fopen('php://fd/'.$m[1], 'rb');
            $out = fopen('php://fd/'.$m[2], 'wb');
        } elseif (preg_match('{^tcp:(.+:\d+)$}', $spec, $m) === 1) {
            $in = $out = stream_socket_client('tcp://'.$m[1], $errno, $error, 30);
        } else {
            throw new ProtocolException('maestro shim: invalid MAESTRO_IPC '.json_encode($spec));
        }
        if ($in === false || $out === false) {
            throw new ProtocolException('maestro shim: cannot open the channel '.$spec);
        }

        self::$in = $in;
        self::$out = $out;
    }

    /**
     * The handshake: the first frame, which carries the token of a socket
     * channel.
     */
    public static function hello(?string $token): void
    {
        self::call('hello', [
            'proto' => self::PROTOCOL,
            'token' => $token,
            'phpVersion' => PHP_VERSION,
            'phpBinary' => PHP_BINARY,
            'sapi' => PHP_SAPI,
            'pid' => getmypid(),
        ]);
    }

    /**
     * Calls maestro and returns its reply, serving maestro's calls in the
     * meantime. An error maestro returns is thrown.
     *
     * @param mixed $args
     * @return mixed
     */
    /**
     * The number of the message PHP is reading or last read: the values
     * of one message share it.
     */
    public static function received(): int
    {
        return self::$received;
    }

    public static function call(string $method, $args = null)
    {
        if (self::$lost) {
            throw new ProtocolException('maestro shim: maestro is gone');
        }

        $id = ++self::$last;
        self::$pending[] = $id;
        try {
            self::send(['k' => 'call', 'id' => $id, 'm' => $method], 'a', $args);

            return self::await($id);
        } finally {
            array_pop(self::$pending);
        }
    }

    /**
     * Serves maestro's calls until the process ends (a `shutdown` call,
     * exit() from plugin code, a fatal error).
     */
    public static function serve(): void
    {
        while (true) {
            $msg = self::receive();
            if ($msg['k'] !== 'call') {
                self::violation('maestro shim: unexpected '.$msg['k'].' '.json_encode($msg['id']).' with no call outstanding');
            }
            self::handle($msg);
        }
    }

    /**
     * Flushes what PHP wrote to its standard output and error streams, so
     * it lands before anything maestro writes next. An open ob_start()
     * buffer stays as it is, as in Composer.
     */
    public static function flushOutput(): void
    {
        if (defined('STDOUT')) {
            @fflush(STDOUT);
        }
        if (defined('STDERR')) {
            @fflush(STDERR);
        }
    }

    /**
     * @return mixed
     */
    private static function await(int $id)
    {
        while (true) {
            $msg = self::receive();
            switch ($msg['k']) {
                case 'call':
                    self::handle($msg);
                    break;
                case 'ret':
                    self::expectReply($msg, $id);
                    if (array_key_exists('exit', $msg)) {
                        // An Application with auto-exit ended (§6.2).
                        self::flushOutput();
                        exit((int) $msg['exit']);
                    }

                    return isset($msg['v']) ? $msg['v'] : null;
                case 'err':
                    self::expectReply($msg, $id);

                    throw Exceptions::fromWire($msg['x']);
                default:
                    self::violation('maestro shim: unknown message kind '.json_encode($msg['k']));
            }
        }
    }

    private static function expectReply(array $msg, int $id): void
    {
        if ($msg['id'] !== $id) {
            self::violation('maestro shim: reply '.json_encode($msg['id']).' does not answer the innermost call '.$id);
        }
    }

    /**
     * Ends the process after a protocol violation (see receive()).
     */
    private static function violation(string $message): void
    {
        self::$lost = true;
        if (defined('STDERR')) {
            fwrite(STDERR, $message.PHP_EOL);
        }
        exit(70);
    }

    /**
     * Runs one call from maestro and sends its reply.
     */
    private static function handle(array $msg): void
    {
        try {
            $value = Server::dispatch((string) $msg['m'], isset($msg['a']) ? $msg['a'] : null);
            $reply = ['k' => 'ret', 'id' => $msg['id']];
            $key = 'v';
        } catch (\Throwable $e) {
            $value = Exceptions::toWire($e);
            $reply = ['k' => 'err', 'id' => $msg['id']];
            $key = 'x';
        }

        try {
            self::send($reply, $key, $value);
        } catch (\InvalidArgumentException $e) {
            // The return value cannot cross (a resource): report that.
            self::send(['k' => 'err', 'id' => $msg['id']], 'x', Exceptions::toWire($e));
        }
    }

    /**
     * Sends a message: the sync block first (its objects are defined
     * before the payload refers to them), then the payload under $key.
     *
     * @param mixed $value
     */
    private static function send(array $msg, string $key, $value): void
    {
        try {
            $sync = Sync::outgoing();
            if ($sync !== null) {
                $msg['s'] = Codec::encode($sync);
            }
            if ($value !== null) {
                $msg[$key] = Codec::encode($value);
            }
            $json = Codec::json($msg);
        } catch (\Throwable $e) {
            // Nothing was sent: what the message would have carried stays
            // to be sent with the next one.
            Handles::rollback();

            throw $e;
        }
        Handles::commit();
        Sync::commit();

        self::flushOutput();
        self::write(pack('N', strlen($json)).$json);
    }

    /**
     * Reads the next message, applies its sync block and decodes its
     * payload.
     *
     * @return array<string, mixed>
     */
    private static function receive(): array
    {
        try {
            return self::receiveMessage();
        } catch (ProtocolException $e) {
            // The channel cannot be trusted any more, and plugin code must
            // not catch this and go on: the process ends, as maestro ends
            // it when it finds a violation.
            self::$lost = true;
            if (defined('STDERR')) {
                fwrite(STDERR, $e->getMessage().PHP_EOL);
            }
            exit(70);
        }
    }

    /**
     * @return array<string, mixed>
     */
    private static function receiveMessage(): array
    {
        $header = self::read(4);
        $n = unpack('N', $header)[1];
        if ($n < 1 || $n > self::MAX_FRAME) {
            throw new ProtocolException('maestro shim: invalid frame length '.$n);
        }

        $json = self::read($n);
        ++self::$received;
        $msg = Codec::parse($json);
        if (!is_array($msg) || !isset($msg['k'], $msg['id'])) {
            throw new ProtocolException('maestro shim: invalid message');
        }

        // Tags only exist where the text has an escaped NUL.
        $tags = strpos($json, '\\u0000') !== false;

        if (isset($msg['s'])) {
            // The registrations first: the rest of the block (mirror
            // updates) may name the objects they rebind.
            if (isset($msg['s']['reg'])) {
                Sync::apply(['reg' => $msg['s']['reg']]);
                unset($msg['s']['reg']);
            }
            Sync::apply($tags ? Codec::decode($msg['s']) : $msg['s']);
        }
        if ($tags) {
            foreach (['a', 'v', 'x'] as $key) {
                if (isset($msg[$key])) {
                    $msg[$key] = Codec::decode($msg[$key]);
                }
            }
        }

        return $msg;
    }

    private static function read(int $n): string
    {
        $buf = '';
        while (strlen($buf) < $n) {
            $chunk = fread(self::$in, $n - strlen($buf));
            if ($chunk === false || ($chunk === '' && feof(self::$in))) {
                self::lost();
            }
            $buf .= $chunk;
        }

        return $buf;
    }

    private static function write(string $frame): void
    {
        $len = strlen($frame);
        for ($off = 0; $off < $len; $off += $n) {
            $n = fwrite(self::$out, $off === 0 ? $frame : substr($frame, $off));
            if ($n === false || $n === 0) {
                self::lost();
            }
        }
        fflush(self::$out);
    }

    /**
     * maestro closed the channel: it is gone, so this process ends.
     *
     * @return never
     */
    private static function lost(): void
    {
        self::$lost = true;
        exit(1);
    }
}
