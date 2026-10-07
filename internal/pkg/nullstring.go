package pkg

// Null is a PHP ?string of a string type T: S when Valid, else null. The
// zero value is null.
type Null[T ~string] struct {
	S     T
	Valid bool
}

// NullString is a PHP ?string.
type NullString = Null[string]

// Some returns the non-null v.
func Some[T ~string](v T) Null[T] { return Null[T]{S: v, Valid: true} }

// Str returns the non-null string s.
func Str(s string) NullString { return Some(s) }

// NullAs is n as a ?T: where a string read from PHP or JSON becomes a T.
func NullAs[T ~string](n NullString) Null[T] { return Null[T]{S: T(n.S), Valid: n.Valid} }

// NonEmpty returns s, or null when s is "" (PHP's `$s === ” ? null : $s`).
func NonEmpty(s string) NullString { return NullString{S: s, Valid: s != ""} }

// Is reports whether n is the non-null v (PHP's `$n === $v`).
func (n Null[T]) Is(v T) bool { return n.Valid && n.S == v }

// Value returns the string as a PHP value: the string, or nil for null.
func (n Null[T]) Value() any {
	if !n.Valid {
		return nil
	}

	return string(n.S)
}

// ptr is n as a pointer: nil for null.
func (n Null[T]) ptr() *T {
	if !n.Valid {
		return nil
	}

	return &n.S
}
