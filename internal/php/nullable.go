// PHP's nullable types (?T).

package php

// Nullable is a PHP ?T: null, or a T. An empty T is not null: for a slice,
// Some([]T{}) is PHP's [] and Null() is null, a distinction Go's nil slice
// cannot carry, since the usual Go idioms (append onto a nil slice, a
// filter that keeps nothing, slices.Collect) turn an empty list into nil
// and back. Use it where PHP gives null and [] different meanings, such as
// whether composer.lock has a "packages-dev" list. The zero value is
// null.
type Nullable[T any] struct {
	v   T
	set bool
}

// Null returns a null ?T.
func Null[T any]() Nullable[T] { return Nullable[T]{} }

// Some returns the ?T holding v, which is not null even when v is empty or
// nil.
func Some[T any](v T) Nullable[T] { return Nullable[T]{v: v, set: true} }

// Get returns the value and whether there is one (false for null, with
// T's zero value).
func (n Nullable[T]) Get() (T, bool) { return n.v, n.set }

// IsNull reports whether n is null.
func (n Nullable[T]) IsNull() bool { return !n.set }

// OrElse returns the value, or def for null (PHP's $n ?? $def).
func (n Nullable[T]) OrElse(def T) T {
	if !n.set {
		return def
	}

	return n.v
}
