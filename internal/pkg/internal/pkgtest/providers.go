// Package pkgtest reads the PHPUnit data providers tools/oracle/pkg/
// providers.php extracts into internal/pkg/testdata/providers.json, for
// the tests of internal/pkg and its subpackages.
package pkgtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

// Arr is a decoded PHP array whose values may be objects (links,
// constraints, dates) that *php.Array cannot hold.
type Arr struct {
	Keys []php.Key
	Vals []any
}

// Len returns count($a).
func (a *Arr) Len() int { return len(a.Vals) }

// At returns the i-th value.
func (a *Arr) At(i int) any { return a.Vals[i] }

// Get returns $a[$k].
func (a *Arr) Get(k any) (any, bool) {
	key := php.ToKey(k)
	for i, kk := range a.Keys {
		if kk == key {
			return a.Vals[i], true
		}
	}

	return nil, false
}

// String returns the i-th value as a string.
func (a *Arr) String(i int) string {
	s, _ := a.Vals[i].(string)

	return s
}

// PHP converts a to a *php.Array; every value must be a PHP value.
func (a *Arr) PHP() *php.Array {
	out := php.NewArrayCap(len(a.Vals))
	for i, k := range a.Keys {
		v := a.Vals[i]
		if sub, ok := v.(*Arr); ok {
			v = sub.PHP()
		}

		out.SetKey(k, v)
	}

	return out
}

// Strings returns the values as strings.
func (a *Arr) Strings() []string {
	out := make([]string, len(a.Vals))
	for i, v := range a.Vals {
		out[i], _ = v.(string)
	}

	return out
}

// Links converts an array of links to Links, keys kept.
func (a *Arr) Links() pkg.Links {
	var b pkg.LinksBuilder
	for i, k := range a.Keys {
		link, _ := a.Vals[i].(*pkg.Link)
		b.Set(k.String(), link)
	}

	return b.Build()
}

// Case is one data provider case: its key and arguments.
type Case struct {
	Name string
	Args *Arr
}

// Providers returns the cases of each provider, by "Class::method".
func Providers(t testing.TB) map[string][]Case {
	t.Helper()

	_, file, _, _ := runtime.Caller(0)

	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "providers.json"))
	if err != nil {
		t.Fatal(err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	out := make(map[string][]Case, len(raw))

	for name, r := range raw {
		v, err := decode(r)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}

		list, _ := v.(*Arr)
		cases := make([]Case, list.Len())

		for i := range cases {
			args, _ := list.Vals[i].(*Arr)
			cases[i] = Case{Name: list.Keys[i].String(), Args: args}
		}

		out[name] = cases
	}

	return out
}

func decode(raw json.RawMessage) (any, error) {
	if len(raw) == 0 || raw[0] != '{' {
		v, err := php.JSONDecode(string(raw), true)

		return v, err
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}

	switch {
	case obj["a"] != nil:
		var pairs [][2]json.RawMessage
		if err := json.Unmarshal(obj["a"], &pairs); err != nil {
			return nil, err
		}

		a := &Arr{}

		for _, p := range pairs {
			k, err := php.JSONDecode(string(p[0]), true)
			if err != nil {
				return nil, err
			}

			v, err := decode(p[1])
			if err != nil {
				return nil, err
			}

			a.Keys = append(a.Keys, php.ToKey(k))
			a.Vals = append(a.Vals, v)
		}

		return a, nil
	case obj["c"] != nil:
		var c [2]string
		if err := json.Unmarshal(obj["c"], &c); err != nil {
			return nil, err
		}

		return semver.NewConstraint(c[0], c[1])
	case obj["link"] != nil:
		var l []json.RawMessage
		if err := json.Unmarshal(obj["link"], &l); err != nil {
			return nil, err
		}

		var source, target, description, pretty string

		_ = json.Unmarshal(l[0], &source)
		_ = json.Unmarshal(l[1], &target)
		_ = json.Unmarshal(l[3], &description)
		_ = json.Unmarshal(l[4], &pretty)

		c, err := decode(l[2])
		if err != nil {
			return nil, err
		}

		constraint, _ := c.(semver.ConstraintInterface)

		return pkg.NewLink(source, target, constraint, description, pkg.Str(pretty)), nil
	case obj["date"] != nil:
		var s string
		_ = json.Unmarshal(obj["date"], &s)

		return time.Parse(time.RFC3339, s)
	}

	return nil, fmt.Errorf("unknown value %s", raw)
}
