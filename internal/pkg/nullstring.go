package pkg

// NullString is a PHP ?string: S when Valid, else null. The zero value is
// null.
type NullString struct {
	S     string
	Valid bool
}

// Str returns the non-null string s.
func Str(s string) NullString { return NullString{S: s, Valid: true} }

// NonEmpty returns s, or null when s is "" (PHP's `$s === ” ? null : $s`).
func NonEmpty(s string) NullString { return NullString{S: s, Valid: s != ""} }

// Value returns the string as a PHP value: the string, or nil for null.
func (n NullString) Value() any {
	if !n.Valid {
		return nil
	}

	return n.S
}
