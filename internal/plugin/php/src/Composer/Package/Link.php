<?php

/*
 * maestro's plugin shim: Composer\Package\Link, reimplemented with
 * Composer 2.10.3's behaviour (docs/PLUGINS.md §4.5). A value class: links
 * cross the channel as values, not by handle (Maestro\Shim\Values).
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Composer\Package;

use Composer\Semver\Constraint\ConstraintInterface;

class Link implements \Stringable
{
    public const TYPE_REQUIRE = 'requires';
    public const TYPE_DEV_REQUIRE = 'devRequires';
    public const TYPE_PROVIDE = 'provides';
    public const TYPE_CONFLICT = 'conflicts';
    public const TYPE_REPLACE = 'replaces';
    public const TYPE_DOES_NOT_REQUIRE = 'does not require';

    private const TYPE_UNKNOWN = 'relates to';

    public static $TYPES = ['requires', 'devRequires', 'provides', 'conflicts', 'replaces'];

    protected $source;
    protected $target;
    protected $constraint;
    protected $description;
    protected $prettyConstraint;

    public function __construct(string $source, string $target, ConstraintInterface $constraint, $description = self::TYPE_UNKNOWN, ?string $prettyConstraint = null)
    {
        $this->source = strtolower($source);
        $this->target = strtolower($target);
        $this->constraint = $constraint;
        $this->description = self::TYPE_DEV_REQUIRE === $description ? 'requires (for development)' : $description;
        $this->prettyConstraint = $prettyConstraint;
    }

    public function getDescription(): string
    {
        return $this->description;
    }

    public function getSource(): string
    {
        return $this->source;
    }

    public function getTarget(): string
    {
        return $this->target;
    }

    public function getConstraint(): ConstraintInterface
    {
        return $this->constraint;
    }

    public function getPrettyConstraint(): string
    {
        if (null === $this->prettyConstraint) {
            throw new \UnexpectedValueException(sprintf('Link %s has been misconfigured and had no prettyConstraint given.', $this));
        }

        return $this->prettyConstraint;
    }

    public function __toString(): string
    {
        return $this->source.' '.$this->description.' '.$this->target.' ('.$this->constraint.')';
    }

    public function getPrettyString(PackageInterface $sourcePackage): string
    {
        return $sourcePackage->getPrettyString().' '.$this->description.' '.$this->target.' '.$this->constraint->getPrettyString();
    }
}
