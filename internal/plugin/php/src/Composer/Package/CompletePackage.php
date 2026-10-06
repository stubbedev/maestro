<?php

/*
 * maestro's plugin shim: Composer\Package\CompletePackage, reimplemented with
 * Composer 2.10.3's behaviour (docs/PLUGINS.md §4.5). See BasePackage for
 * how a Go-owned package mirrors maestro's.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Package;

use Maestro\Shim\Remote;
use Maestro\Shim\Rpc;

class CompletePackage extends Package implements CompletePackageInterface
{
    protected $repositories = [];
    protected $license = [];
    protected $keywords = [];
    protected $authors = [];
    protected $description;
    protected $homepage;
    protected $scripts = [];
    protected $support = [];
    protected $funding = [];
    protected $abandoned = false;
    protected $archiveName;
    protected $archiveExcludes = [];

    public function setScripts(array $scripts): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setScripts', [$this, $scripts]);

            return;
        }
        $this->scripts = $scripts;
    }

    public function setRepositories(array $repositories): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setRepositories', [$this, $repositories]);

            return;
        }
        $this->repositories = $repositories;
    }

    public function setLicense(array $license): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setLicense', [$this, $license]);

            return;
        }
        $this->license = $license;
    }

    public function setKeywords(array $keywords): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setKeywords', [$this, $keywords]);

            return;
        }
        $this->keywords = $keywords;
    }

    public function setAuthors(array $authors): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setAuthors', [$this, $authors]);

            return;
        }
        $this->authors = $authors;
    }

    public function setDescription(?string $description): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setDescription', [$this, $description]);

            return;
        }
        $this->description = $description;
    }

    public function setHomepage(?string $homepage): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setHomepage', [$this, $homepage]);

            return;
        }
        $this->homepage = $homepage;
    }

    public function setSupport(array $support): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setSupport', [$this, $support]);

            return;
        }
        $this->support = $support;
    }

    public function setFunding(array $funding): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setFunding', [$this, $funding]);

            return;
        }
        $this->funding = $funding;
    }

    public function setAbandoned($abandoned): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setAbandoned', [$this, $abandoned]);

            return;
        }
        $this->abandoned = $abandoned;
    }

    public function setArchiveName(?string $name): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setArchiveName', [$this, $name]);

            return;
        }
        $this->archiveName = $name;
    }

    public function setArchiveExcludes(array $excludes): void
    {
        if (Remote::owned($this)) {
            Rpc::call('pkg.setArchiveExcludes', [$this, $excludes]);

            return;
        }
        $this->archiveExcludes = $excludes;
    }

    public function getScripts(): array
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->scripts;
    }

    public function getRepositories(): array
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->repositories;
    }

    public function getLicense(): array
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->license;
    }

    public function getKeywords(): array
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->keywords;
    }

    public function getAuthors(): array
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->authors;
    }

    public function getDescription(): ?string
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->description;
    }

    public function getHomepage(): ?string
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->homepage;
    }

    public function getSupport(): array
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->support;
    }

    public function getFunding(): array
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->funding;
    }

    public function isAbandoned(): bool
    {
        \Maestro\Shim\LazyPackages::load($this);
        return (bool) $this->abandoned;
    }

    public function getReplacementPackage(): ?string
    {
        \Maestro\Shim\LazyPackages::load($this);
        return \is_string($this->abandoned) ? $this->abandoned : null;
    }

    public function getArchiveName(): ?string
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->archiveName;
    }

    public function getArchiveExcludes(): array
    {
        \Maestro\Shim\LazyPackages::load($this);
        return $this->archiveExcludes;
    }
}
