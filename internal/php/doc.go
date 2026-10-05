// Package php reproduces the PHP 8.4 runtime semantics the Composer port
// depends on, so Go code ported from Composer behaves byte for byte like
// the original. It ports parts of php-src (Zend/zend_hash.c,
// Zend/zend_operators.c, Zend/zend_sort.c, ext/standard/array.c,
// ext/standard/string.c, ext/standard/strnatcmp.c, ext/standard/var.c,
// ext/json, ext/pcre) and composer/pcre.
//
// # Values
//
// A PHP value is a Go any holding exactly one of:
//
//	nil      null
//	bool     bool
//	int64    int
//	float64  float
//	string   string (a byte string, not necessarily UTF-8)
//	*Array   array
//	*Object  stdClass
//
// Functions taking values also accept a Go int and store it as int64; any
// other dynamic type panics, because it can only be a programming error.
//
// # Arrays are references
//
// PHP arrays are values with copy-on-assignment semantics; *Array is a
// reference. Where PHP copies an array ($b = $a, passing to a function,
// returning a property) and either side is modified afterwards, call
// Clone. Functions in this package never modify their arguments unless
// documented (the sort functions and the mutating methods).
//
// Iterating with All while modifying the array behaves like PHP's foreach
// by value: the loop sees the elements as they were when it started. Values
// that are themselves *Array are shared, so modifying a nested array
// during the loop is visible.
//
// # Errors
//
// PHP warnings that Composer never relies on are not reproduced. Errors
// that PHP reports through return values or exceptions (json_last_error,
// preg_last_error, JsonException, Composer\Pcre\PcreException) are Go
// errors carrying PHP's exact message text.
package php
