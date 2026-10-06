<?php

namespace MaestroTest\InternalsPlugin;

use Composer\Config;
use Composer\IO\IOInterface;
use Composer\Package\CompletePackage;
use Composer\Repository\ArrayRepository;

/** A repository type registered with setRepositoryClass(). */
class CustomRepository extends ArrayRepository
{
    /** @var array<string, mixed> */
    private $repoConfig;

    public function __construct(array $repoConfig, IOInterface $io, Config $config)
    {
        parent::__construct();
        $this->repoConfig = $repoConfig;
        $this->addPackage(new CompletePackage($repoConfig['package'], '3.0.0.0', '3.0.0'));
    }

    public function getRepoName(): string
    {
        return 'custom repo '.$this->repoConfig['package'];
    }
}
