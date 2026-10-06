// PHP array write semantics the JsonConfigSource fallbacks rely on:
// auto-vivification of nested arrays through $a[..][..] = and references,
// and unset() of nested offsets. The errors are PHP 8.4's, raised at the
// line of src/Composer/Config/JsonConfigSource.php doing the write.

package config

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// writable is $v used as the array of a write $v[...] =
// (php.WritableArray): the array itself, or a new one (replace true) for
// null and false, the latter after the deprecation notice Composer's
// ErrorHandler prints before the write goes on.
func writable(v any) (arr *php.Array, replace bool, err error) {
	arr, replace, deprecated, e := php.WritableArray(v)
	if e != nil {
		return nil, false, e
	}
	if deprecated {
		util.RaiseDeprecation(php.FalseToArrayDeprecation)
	}

	return arr, replace, nil
}

// child is &$a[$key] used as an array: the array stored at key,
// created when the key is missing or holds null or false
// (auto-vivification).
func child(a *php.Array, key any) (*php.Array, error) {
	v, _ := a.Get(key)
	c, replace, err := writable(v)
	if err != nil {
		return nil, err
	}
	if replace {
		a.Set(key, c)
	}

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

// unsetIn is unset($a[$k1][$k2]...[$kn]): a missing or null level
// makes it a no-op, as false does after the deprecation notice; other
// scalars fail as in PHP.
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
		case bool:
			if c {
				return &php.EngineError{Class: "Error", Message: "Cannot unset offset in a non-array variable"}
			}
			util.RaiseDeprecation(php.FalseToArrayDeprecation)

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
