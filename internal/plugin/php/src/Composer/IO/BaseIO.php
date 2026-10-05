<?php

/*
 * maestro's plugin shim: Composer\IO\BaseIO (docs/PLUGINS.md §4.3).
 * maestro's IO is the one IOInterface of a run: its PHP mirror's methods
 * are maestro's (io.*). An IO created in PHP (new NullIO) keeps Composer's
 * behaviour locally.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\IO;

abstract class BaseIO implements \Composer\IO\IOInterface
{
    protected $authentications = [];

    public function alert($message, array $context = []): void
    {
        $this->log(\Psr\Log\LogLevel::ALERT, $message, $context);
    }

    protected function checkAndSetAuthentication(string $repositoryName, string $username, ?string $password = null)
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Rpc::call('io.checkAndSetAuthentication', [$this, $repositoryName, $username, $password]);

            return;
        }

        if ($this->hasAuthentication($repositoryName)) {
            $auth = $this->getAuthentication($repositoryName);
            if ($auth['username'] === $username && $auth['password'] === $password) {
                return;
            }

            $this->writeError(
                sprintf(
                    "<warning>Warning: You should avoid overwriting already defined auth settings for %s.</warning>",
                    $repositoryName
                )
            );
        }
        $this->setAuthentication($repositoryName, $username, $password);
    }

    public function critical($message, array $context = []): void
    {
        $this->log(\Psr\Log\LogLevel::CRITICAL, $message, $context);
    }

    public function debug($message, array $context = []): void
    {
        $this->log(\Psr\Log\LogLevel::DEBUG, $message, $context);
    }

    public function emergency($message, array $context = []): void
    {
        $this->log(\Psr\Log\LogLevel::EMERGENCY, $message, $context);
    }

    public function error($message, array $context = []): void
    {
        $this->log(\Psr\Log\LogLevel::ERROR, $message, $context);
    }

    public function getAuthentication($repositoryName)
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            return \Maestro\Shim\Rpc::call('io.getAuthentication', [$this, $repositoryName]);
        }

        if (isset($this->authentications[$repositoryName])) {
            return $this->authentications[$repositoryName];
        }

        return ['username' => null, 'password' => null];
    }

    public function getAuthentications()
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            return \Maestro\Shim\Rpc::call('io.getAuthentications', [$this]);
        }

        return $this->authentications;
    }

    public function hasAuthentication($repositoryName)
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            return \Maestro\Shim\Rpc::call('io.hasAuthentication', [$this, $repositoryName]);
        }

        return isset($this->authentications[$repositoryName]);
    }

    public function info($message, array $context = []): void
    {
        $this->log(\Psr\Log\LogLevel::INFO, $message, $context);
    }

    public function loadConfiguration(\Composer\Config $config)
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Rpc::call('io.loadConfiguration', [$this, $config]);

            return;
        }

        \Maestro\Shim\Remote::unsupported(static::class, 'loadConfiguration');
    }

    public function log($level, $message, array $context = []): void
    {
        $message = (string) $message;

        if ($context !== []) {
            $json = \Composer\Util\Silencer::call('json_encode', $context, JSON_INVALID_UTF8_IGNORE | JSON_UNESCAPED_SLASHES | JSON_UNESCAPED_UNICODE);
            if ($json !== false) {
                $message .= ' ' . $json;
            }
        }

        if (in_array($level, [\Psr\Log\LogLevel::EMERGENCY, \Psr\Log\LogLevel::ALERT, \Psr\Log\LogLevel::CRITICAL, \Psr\Log\LogLevel::ERROR])) {
            $this->writeError('<error>'.$message.'</error>');
        } elseif ($level === \Psr\Log\LogLevel::WARNING) {
            $this->writeError('<warning>'.$message.'</warning>');
        } elseif ($level === \Psr\Log\LogLevel::NOTICE) {
            $this->writeError('<info>'.$message.'</info>', true, self::VERBOSE);
        } elseif ($level === \Psr\Log\LogLevel::INFO) {
            $this->writeError('<info>'.$message.'</info>', true, self::VERY_VERBOSE);
        } else {
            $this->writeError($message, true, self::DEBUG);
        }
    }

    public function notice($message, array $context = []): void
    {
        $this->log(\Psr\Log\LogLevel::NOTICE, $message, $context);
    }

    public function resetAuthentications()
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Rpc::call('io.resetAuthentications', [$this]);

            return;
        }

        $this->authentications = [];
    }

    public function setAuthentication($repositoryName, $username, $password = null)
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Rpc::call('io.setAuthentication', [$this, $repositoryName, $username, $password]);

            return;
        }

        $this->authentications[$repositoryName] = ['username' => $username, 'password' => $password];
    }

    public function warning($message, array $context = []): void
    {
        $this->log(\Psr\Log\LogLevel::WARNING, $message, $context);
    }

    public function writeErrorRaw($messages, bool $newline = true, int $verbosity = self::NORMAL)
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Rpc::call('io.writeErrorRaw', [$this, $messages, $newline, $verbosity]);

            return;
        }

        $this->writeError($messages, $newline, $verbosity);
    }

    public function writeRaw($messages, bool $newline = true, int $verbosity = self::NORMAL)
    {
        if (\Maestro\Shim\Remote::owned($this)) {
            \Maestro\Shim\Rpc::call('io.writeRaw', [$this, $messages, $newline, $verbosity]);

            return;
        }

        $this->write($messages, $newline, $verbosity);
    }
}
