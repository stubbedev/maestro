// PHP array write semantics the JsonConfigSource fallbacks rely on:
// auto-vivification of nested arrays through $a[..][..] = and references,
// and unset() of nested offsets.

package config

import "github.com/stubbedev/maestro/internal/php"

var errScalarAsArray = &php.EngineError{Class: "Error", Message: "Cannot use a scalar value as an array"}

// child is &$a[$key] used as an array: the array stored at key, created
// when the key is missing or holds null or false (auto-vivification).
func child(a *php.Array, key any) (*php.Array, error) {
	v, _ := a.Get(key)
	switch c := v.(type) {
	case *php.Array:
		return c, nil
	case nil:
	case bool:
		if c {
			return nil, errScalarAsArray
		}
	case string:
		// PHP would write a byte at a numeric offset; configuration
		// keys never are one
		return nil, &php.EngineError{Class: "TypeError", Message: "Cannot access offset of type string on string"}
	default:
		return nil, errScalarAsArray
	}
	c := php.NewArray()
	a.Set(key, c)

	return c, nil
}

// setIn is $a[$k1][$k2]...[$kn] = $value.
func setIn(a *php.Array, value any, keys ...any) error {
	for _, k := range keys[:len(keys)-1] {
		var err error
		if a, err = child(a, k); err != nil {
			return err
		}
	}
	a.Set(keys[len(keys)-1], value)

	return nil
}

// unsetIn is unset($a[$k1][$k2]...[$kn]): a missing or null level makes it
// a no-op; other scalars fail as in PHP.
func unsetIn(a *php.Array, keys ...any) error {
	for i, k := range keys {
		if i == len(keys)-1 {
			a.Delete(k)

			return nil
		}
		v, _ := a.Get(k)
		switch c := v.(type) {
		case *php.Array:
			a = c
		case nil:
			return nil
		case string:
			return &php.EngineError{Class: "Error", Message: "Cannot unset string offsets"}
		default:
			return &php.EngineError{Class: "Error", Message: "Cannot unset offset in a non-array variable"}
		}
	}

	return nil
}

// getIn is $a[$k1]...[$kn] ?? null, looking through arrays only.
func getIn(a *php.Array, keys ...any) any {
	var v any = a
	for _, k := range keys {
		c, ok := v.(*php.Array)
		if !ok {
			return nil
		}
		v, _ = c.Get(k)
	}

	return v
}

// isEmptyArray is $v === [].
func isEmptyArray(v any) bool {
	a, ok := v.(*php.Array)

	return ok && a.Len() == 0
}

// arrayIsList is array_is_list($v), with its TypeError for non-arrays.
func arrayIsList(v any) (bool, error) {
	a, ok := v.(*php.Array)
	if !ok {
		return false, &php.EngineError{Class: "TypeError", Message: "array_is_list(): Argument #1 ($array) must be of type array, " + zvalName(v) + " given"}
	}

	return a.IsList(), nil
}
