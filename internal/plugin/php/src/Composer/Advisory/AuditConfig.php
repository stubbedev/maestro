<?php

/*
 * maestro's plugin shim: Composer\Advisory\AuditConfig, Composer 2.10.3's
 * value object. maestro's (an Installer's) crosses as a mirror of its two
 * properties; one created in PHP (BaseCommand::createAuditConfig()) is
 * adopted by maestro when it first crosses, and what PHP writes into its
 * properties reaches maestro (Maestro\Shim\Adapter\AuditConfigAdapter).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Advisory;

class AuditConfig
{
    public $audit;

    public $auditFormat;

    public function __construct(bool $audit = true, string $auditFormat = \Composer\Advisory\Auditor::FORMAT_SUMMARY)
    {
        $this->audit = $audit;
        $this->auditFormat = $auditFormat;
    }
}
