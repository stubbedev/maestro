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

        $bitbucketOauth = $config->get('bitbucket-oauth');
        $githubOauth = $config->get('github-oauth');
        $gitlabOauth = $config->get('gitlab-oauth');
        $gitlabToken = $config->get('gitlab-token');
        $forgejoToken = $config->get('forgejo-token');
        $httpBasic = $config->get('http-basic');
        $bearerToken = $config->get('bearer');
        $customHeaders = $config->get('custom-headers');
        $clientCertificate = $config->get('client-certificate');

        foreach ($bitbucketOauth as $domain => $cred) {
            $this->checkAndSetAuthentication($domain, $cred['consumer-key'], $cred['consumer-secret']);
        }

        foreach ($githubOauth as $domain => $token) {
            if ($domain !== 'github.com' && !in_array($domain, $config->get('github-domains'), true)) {
                $this->debug($domain.' is not in the configured github-domains, adding it implicitly as authentication is configured for this domain');
                $config->merge(['config' => ['github-domains' => array_merge($config->get('github-domains'), [$domain])]], 'implicit-due-to-auth');
            }

            $this->checkAndSetAuthentication($domain, $token, 'x-oauth-basic');
        }

        foreach ($gitlabOauth as $domain => $token) {
            if ($domain !== 'gitlab.com' && !in_array($domain, $config->get('gitlab-domains'), true)) {
                $this->debug($domain.' is not in the configured gitlab-domains, adding it implicitly as authentication is configured for this domain');
                $config->merge(['config' => ['gitlab-domains' => array_merge($config->get('gitlab-domains'), [$domain])]], 'implicit-due-to-auth');
            }

            $token = is_array($token) ? $token['token'] : $token;
            $this->checkAndSetAuthentication($domain, $token, 'oauth2');
        }

        foreach ($gitlabToken as $domain => $token) {
            if ($domain !== 'gitlab.com' && !in_array($domain, $config->get('gitlab-domains'), true)) {
                $this->debug($domain.' is not in the configured gitlab-domains, adding it implicitly as authentication is configured for this domain');
                $config->merge(['config' => ['gitlab-domains' => array_merge($config->get('gitlab-domains'), [$domain])]], 'implicit-due-to-auth');
            }

            $username = is_array($token) ? $token['username'] : $token;
            $password = is_array($token) ? $token['token'] : 'private-token';
            $this->checkAndSetAuthentication($domain, $username, $password);
        }

        foreach ($forgejoToken as $domain => $cred) {
            if (!in_array($domain, $config->get('forgejo-domains'), true)) {
                $this->debug($domain.' is not in the configured forgejo-domains, adding it implicitly as authentication is configured for this domain');
                $config->merge(['config' => ['forgejo-domains' => array_merge($config->get('forgejo-domains'), [$domain])]], 'implicit-due-to-auth');
            }

            $this->checkAndSetAuthentication($domain, $cred['username'], $cred['token']);
        }

        foreach ($httpBasic as $domain => $cred) {
            $this->checkAndSetAuthentication($domain, $cred['username'], $cred['password']);
        }

        foreach ($bearerToken as $domain => $token) {
            $this->checkAndSetAuthentication($domain, $token, 'bearer');
        }

        foreach ($customHeaders as $domain => $headers) {
            if ($headers !== null) {
                $this->checkAndSetAuthentication($domain, (string) json_encode($headers), 'custom-headers');
            }
        }

        foreach ($clientCertificate as $domain => $cred) {
            $sslOptions = array_filter(
                [
                    'local_cert' => isset($cred['local_cert']) ? $cred['local_cert'] : null,
                    'local_pk' => isset($cred['local_pk']) ? $cred['local_pk'] : null,
                    'passphrase' => isset($cred['passphrase']) ? $cred['passphrase'] : null,
                ],
                static function (?string $value): bool {
                    return $value !== null;
                }
            );
            if (!isset($sslOptions['local_cert'])) {
                $this->writeError(sprintf('<warning>Warning: Client certificate configuration is missing key `local_cert` for %s.</warning>', $domain));
                continue;
            }
            $this->checkAndSetAuthentication($domain, 'client-certificate', (string) json_encode($sslOptions));
        }

        \Composer\Util\ProcessExecutor::setTimeout($config->get('process-timeout'));
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
