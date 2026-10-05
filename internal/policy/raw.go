// Helpers reading the raw config arrays the way the PHP of
// src/Composer/Policy does.

package policy

import (
	"iter"

	"github.com/stubbedev/maestro/internal/php"
)

// coalesce is $a[$key] ?? null: the value when the key is set and not
// null.
func coalesce(a *php.Array, key string) (any, bool) {
	if a == nil {
		return nil, false
	}
	v, ok := a.Get(key)

	return v, ok && v != nil
}

// boolOr is (bool) ($a[$key] ?? $def).
func boolOr(a *php.Array, key string, def bool) bool {
	if v, ok := coalesce(a, key); ok {
		return php.ToBool(v)
	}

	return def
}

// stringOr is $a[$key] ?? $def as a string.
func stringOr(a *php.Array, key, def string) string {
	if v, ok := coalesce(a, key); ok {
		return php.ToString(v)
	}

	return def
}

// reasonOf is $a['reason'] ?? null as a ?string.
func reasonOf(a *php.Array) *string {
	if v, ok := coalesce(a, "reason"); ok {
		s := php.ToString(v)

		return &s
	}

	return nil
}

// arrayOf is $a[$key] ?? [] as an array: nil (empty) unless it is one.
func arrayOf(a *php.Array, key string) *php.Array {
	v, _ := coalesce(a, key)
	arr, _ := v.(*php.Array)

	return arr
}

// each iterates over a, which may be nil.
func each(a *php.Array) iter.Seq2[php.Key, any] {
	if a == nil {
		return func(func(php.Key, any) bool) {}
	}

	return a.All()
}

// isEmpty is $a === [] for an array that may be nil.
func isEmpty(a *php.Array) bool { return a == nil || a.Len() == 0 }
