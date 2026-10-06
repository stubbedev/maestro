<?php

/*
 * maestro's plugin shim (docs/PLUGINS.md §5.1). Not part of Composer.
 * Written for PHP 7.2.5 to 8.5.
 */

namespace Maestro\Shim;

use Composer\Package\Link;
use Composer\Semver\Constraint\Constraint;
use Composer\Semver\Constraint\ConstraintInterface;
use Composer\Semver\Constraint\MatchAllConstraint;
use Composer\Semver\Constraint\MatchNoneConstraint;
use Composer\Semver\Constraint\MultiConstraint;

/**
 * Links and constraints cross the channel as values (docs/PLUGINS.md
 * §6.4): a "\0c" tag with their structure, rebuilt with the vendored
 * composer/semver constructors, never by parsing.
 *
 *   {"\0c":"constraint","op":">=","v":"1.0.0.0-dev","p":">=1.0"}
 *   {"\0c":"multi","and":true,"cs":[...],"p":"^1.0"}
 *   {"\0c":"all","p":"*"}
 *   {"\0c":"none","p":null}
 *   {"\0c":"link","src":"a/b","tgt":"c/d","c":<constraint>,"d":"requires","pc":"^1.0"}
 *   {"\0c":"response","url":..,"code":200,"headers":[..],"body":..}   (a Composer\Util\Http\Response, maestro to PHP)
 *   {"\0c":"date","v":"2024-01-02T03:04:05.000000+00:00"}   (a DateTimeInterface, PHP to maestro)
 */
final class Values
{
    const TAG = "\0c";

    public static function register(): void
    {
        Codec::registerTag(self::TAG, [self::class, 'decode']);
        Codec::registerEncoder(Link::class, [self::class, 'encodeLink']);
        Codec::registerEncoder(ConstraintInterface::class, [self::class, 'encodeConstraint']);
        Codec::registerEncoder(\DateTimeInterface::class, [self::class, 'encodeDate']);
    }

    /**
     * @param array<string, mixed> $tag
     * @return Link|ConstraintInterface|\Composer\Util\Http\Response
     */
    public static function decode(array $tag)
    {
        switch ($tag[self::TAG]) {
            case 'link':
                $constraint = self::decode($tag['c']);
                $link = new Link((string) $tag['src'], (string) $tag['tgt'], $constraint, (string) $tag['d'], isset($tag['pc']) ? (string) $tag['pc'] : null);
                // The description is already Composer's (getDescription()).
                Remote::fill($link, Link::class, ['description' => (string) $tag['d']]);

                return $link;
            case 'response':
                return new \Composer\Util\Http\Response(['url' => Codec::decode($tag['url'])], $tag['code'], Codec::decode($tag['headers']), Codec::decode($tag['body']));
            case 'constraint':
                $c = new Constraint((string) $tag['op'], (string) $tag['v']);
                break;
            case 'multi':
                $cs = [];
                foreach ($tag['cs'] as $item) {
                    $cs[] = self::decode($item);
                }
                $c = new MultiConstraint($cs, (bool) $tag['and']);
                break;
            case 'all':
                $c = new MatchAllConstraint();
                break;
            case 'none':
                $c = new MatchNoneConstraint();
                break;
            default:
                throw new ProtocolException('maestro shim: unknown value '.json_encode($tag[self::TAG]));
        }
        if (isset($tag['p'])) {
            $c->setPrettyString((string) $tag['p']);
        }

        return $c;
    }

    /**
     * @return array<string, string>
     */
    public static function encodeDate(\DateTimeInterface $date): array
    {
        return [self::TAG => 'date', 'v' => $date->format('Y-m-d\TH:i:s.uP')];
    }

    /**
     * @return array<string, mixed>
     */
    public static function encodeLink(Link $link): array
    {
        $pretty = Remote::read($link, Link::class, ['prettyConstraint'])['prettyConstraint'];

        return [
            self::TAG => 'link',
            'src' => $link->getSource(),
            'tgt' => $link->getTarget(),
            'c' => self::encodeConstraint($link->getConstraint()),
            'd' => $link->getDescription(),
            'pc' => $pretty,
        ];
    }

    /**
     * @return array<string, mixed>
     */
    public static function encodeConstraint(ConstraintInterface $c): array
    {
        if ($c instanceof Constraint) {
            $tag = [self::TAG => 'constraint', 'op' => $c->getOperator(), 'v' => $c->getVersion()];
        } elseif ($c instanceof MultiConstraint) {
            $cs = [];
            foreach ($c->getConstraints() as $item) {
                $cs[] = self::encodeConstraint($item);
            }
            $tag = [self::TAG => 'multi', 'and' => $c->isConjunctive(), 'cs' => $cs];
        } elseif ($c instanceof MatchAllConstraint) {
            $tag = [self::TAG => 'all'];
        } elseif ($c instanceof MatchNoneConstraint) {
            $tag = [self::TAG => 'none'];
        } else {
            throw new \InvalidArgumentException('maestro shim: a '.get_class($c).' constraint cannot cross to maestro');
        }
        $tag['p'] = $c->getPrettyString();

        return $tag;
    }
}
