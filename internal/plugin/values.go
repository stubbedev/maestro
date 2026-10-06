// Links and constraints crossing the channel as values (docs/PLUGINS.md
// §6.4): a "\x00c" tag holding their structure, which the shim rebuilds
// with the vendored composer/semver constructors (Maestro\Shim\Values).
//
//	{"\u0000c":"constraint","op":">=","v":"1.0.0.0-dev","p":">=1.0"}
//	{"\u0000c":"multi","and":true,"cs":[...],"p":"^1.0"}
//	{"\u0000c":"all","p":"*"}
//	{"\u0000c":"none","p":null}
//	{"\u0000c":"link","src":"a/b","tgt":"c/d","c":<constraint>,"d":"requires","pc":"^1.0"}
//	{"\u0000c":"date","v":"2024-01-02T03:04:05.000000+00:00"}   (a DateTimeInterface from PHP)

package plugin

import (
	"fmt"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
	"github.com/stubbedev/maestro/internal/semver"
)

// valueTag is the key of link and constraint tags.
const valueTag = "\x00c"

func encodeConstraint(e *rpc.Encoder, c semver.ConstraintInterface) (*php.Array, error) {
	var t *php.Array
	switch c := c.(type) {
	case *semver.Constraint:
		t = rpc.Tag(valueTag, "constraint", "op", c.Operator(), "v", c.Version())
	case *semver.MultiConstraint:
		cs := php.NewArrayCap(len(c.Constraints()))
		for _, sub := range c.Constraints() {
			st, err := encodeConstraint(e, sub)
			if err != nil {
				return nil, err
			}
			cs.Append(st)
		}
		t = rpc.Tag(valueTag, "multi", "and", c.IsConjunctive(), "cs", cs)
	case *semver.MatchAllConstraint:
		t = rpc.Tag(valueTag, "all")
	case *semver.MatchNoneConstraint:
		t = rpc.Tag(valueTag, "none")
	default:
		return nil, fmt.Errorf("plugin: a %T constraint cannot cross to PHP", c)
	}
	p, err := e.Value(c.PrettyString())
	if err != nil {
		return nil, err
	}
	t.Set("p", p)

	return t, nil
}

// linkValue is a link crossing the channel. As a php.Opaque value it can
// sit in a *php.Array (links from PHP decode to it too).
type linkValue struct{ l *pkg.Link }

// constraintValue is a constraint from PHP, as a php.Opaque value.
type constraintValue struct{ c semver.ConstraintInterface }

// PHPOpaque implements php.Opaque.
func (constraintValue) PHPOpaque() {}

// EncodeRPC implements rpc.ValueEncoder: a constraint maestro hands to PHP.
func (v constraintValue) EncodeRPC(e *rpc.Encoder) (*php.Array, error) {
	return encodeConstraint(e, v.c)
}

// dateValue is a DateTimeInterface from PHP, as a php.Opaque value.
type dateValue struct{ t time.Time }

// PHPOpaque implements php.Opaque.
func (dateValue) PHPOpaque() {}

// PHPOpaque implements php.Opaque.
func (linkValue) PHPOpaque() {}

// EncodeRPC implements rpc.ValueEncoder.
func (v linkValue) EncodeRPC(e *rpc.Encoder) (*php.Array, error) {
	c, err := encodeConstraint(e, v.l.Constraint())
	if err != nil {
		return nil, err
	}
	var pc any
	if raw := v.l.RawPrettyConstraint(); raw.Valid {
		pc = raw.S
	}
	t := rpc.Tag(valueTag, "link", "src", v.l.Source(), "tgt", v.l.Target(), "c", c, "d", v.l.Description(), "pc", pc)
	for _, k := range []string{"src", "tgt", "d", "pc"} {
		raw, _ := t.Get(k)
		enc, err := e.Value(raw)
		if err != nil {
			return nil, err
		}
		t.Set(k, enc)
	}

	return t, nil
}

// linksValue is a map of links (getRequires() and friends) as PHP holds
// it: key => Link.
func linksValue(links pkg.Links) *php.Array {
	a := php.NewArrayCap(links.Len())
	for k, l := range links.All() {
		a.Set(k, linkValue{l})
	}

	return a
}

// decodeValue decodes a "\x00c" tag from PHP: a linkValue, a
// constraintValue or a dateValue.
func decodeValue(tag *php.Array, d *rpc.Decoder) (any, error) {
	kind, _ := tag.GetString(valueTag)
	if kind == "date" {
		v, _ := tag.GetString("v")
		t, err := time.Parse(releaseDateLayout, v)
		if err != nil {
			return nil, &rpc.ProtocolError{Message: "invalid date " + v}
		}

		return dateValue{t}, nil
	}
	if kind == "link" {
		raw, _ := tag.Get("c")
		cv, err := d.Value(raw)
		if err != nil {
			return nil, err
		}
		c, ok := cv.(constraintValue)
		if !ok {
			return nil, &rpc.ProtocolError{Message: "a link without a constraint"}
		}
		src, _ := tag.GetString("src")
		tgt, _ := tag.GetString("tgt")
		desc, _ := tag.GetString("d")
		var pretty pkg.NullString
		if pc, ok := tag.Get("pc"); ok && pc != nil {
			pretty = pkg.Str(php.ToString(pc))
		}

		return linkValue{pkg.NewLink(src, tgt, c.c, desc, pretty)}, nil
	}

	c, err := decodeConstraint(tag)
	if err != nil {
		return nil, err
	}

	return constraintValue{c}, nil
}

func decodeConstraint(tag *php.Array) (semver.ConstraintInterface, error) {
	kind, _ := tag.GetString(valueTag)

	var c semver.ConstraintInterface
	switch kind {
	case "constraint":
		op, _ := tag.GetString("op")
		v, _ := tag.GetString("v")
		cc, err := semver.NewConstraint(op, v)
		if err != nil {
			return nil, err
		}
		c = cc
	case "multi":
		list, _ := tag.GetArray("cs")
		var cs []semver.ConstraintInterface
		if list != nil {
			for _, item := range list.Values() {
				sub, ok := item.(*php.Array)
				if !ok {
					return nil, &rpc.ProtocolError{Message: "invalid multi constraint"}
				}
				sc, err := decodeConstraint(sub)
				if err != nil {
					return nil, err
				}
				cs = append(cs, sc)
			}
		}
		and, _ := tag.Get("and")
		mc, err := semver.NewMultiConstraint(cs, and == true)
		if err != nil {
			return nil, err
		}
		c = mc
	case "all":
		c = semver.NewMatchAllConstraint()
	case "none":
		c = semver.NewMatchNoneConstraint()
	default:
		return nil, &rpc.ProtocolError{Message: "unknown value tag " + kind}
	}
	if p, ok := tag.Get("p"); ok && p != nil {
		c.SetPrettyString(php.ToString(p))
	}

	return c, nil
}
