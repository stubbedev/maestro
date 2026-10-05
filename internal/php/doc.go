// Package php reproduces the PHP 8.4 runtime semantics the Composer port
// depends on, so Go code ported from Composer behaves byte for byte like
// the original. It ports parts of php-src (Zend/zend_hash.c,
// Zend/zend_operators.c, Zend/zend_sort.c, ext/standard/array.c,
// ext/standard/string.c, ext/standard/strnatcmp.c, ext/standard/var.c,
// ext/json, ext/pcre), the matching semantics of PCRE2 10.48 and
// composer/pcre.
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
// during the loop is visible. Concurrent reads (All, Get) of an array that
// nobody modifies are safe.
//
// # Regular expressions
//
// Compile accepts PHP pattern literals verbatim, delimiters and modifiers
// included, and Regexp and the Preg* functions reproduce preg_* and
// Composer\Pcre\Preg. Patterns run on a backtracking engine written for
// this package rather than a translation to another regex library: PCRE2
// semantics that matter to Composer (recursion and subroutine calls as in
// its JSON and SPDX grammars, group numbering, captures set when a group
// closes, empty-iteration loop termination, the retry after empty matches
// in preg_match_all/preg_replace/preg_split) only come out exact that way.
//
// Without the u modifier a pattern works on bytes: . matches one byte,
// \d \s \w \b, POSIX classes and caseless matching are ASCII-only (the C
// locale tables PHP uses), and \p{...} looks at the byte value as a code
// point. With u, the pattern and subject are UTF-8, offsets stay byte
// offsets, and, as PHP sets PCRE2_UCP, the character types follow Unicode
// properties (\w is \p{L}\p{N}\p{Mn}\p{Pc}, as in PCRE2 10.43+).
//
// Deliberately not reproduced: \X, \C, backtracking control verbs
// ((*VERB)), callouts, (?C), \p{...} by script extension (scripts match
// by their Script property only), and PCRE2's exact match-limit
// accounting: the limit (pcre.backtrack_limit, 1000000) counts
// resumptions after backtracking, and recursion that loops or nests past
// 100000 calls fails like PHP's default JIT does, with
// PREG_JIT_STACKLIMIT_ERROR. Compilation errors carry PCRE2's message
// texts, but their offsets may differ.
//
// # Errors
//
// PHP warnings that Composer never relies on are not reproduced. Errors
// that PHP reports through return values or exceptions (json_last_error,
// preg_last_error, JsonException, Composer\Pcre\PcreException) are Go
// errors carrying PHP's exact message text.
package php
