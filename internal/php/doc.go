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
// # Why not a regex library
//
// The engine was compared (October 2026) with every pure-Go option, each
// adapted behind this package's API and run against the goldens in
// testdata/preg: 596 Composer patterns (55,792 checks) and 304 engine
// feature patterns (17,752 checks). Exact patterns, Composer / feature:
//
//	in-house engine                             596/596  304/304
//	PCRE2 10.48 transpiled with ccgo v4 (local) 596/596  304/304
//	go.elara.ws/pcre (ccgo PCRE2 10.40, 2023)   596/596  297/304
//	github.com/KarpelesLab/gopcre2 v0.1.0       564/596  181/304
//	github.com/dlclark/regexp2 v1.12.0          491/596  205/304
//
// gopcre2 has no byte mode, start offset or PCRE2_NOTEMPTY_ATSTART, hangs
// on lookbehinds and panics on \g{-1}. regexp2 has .NET semantics with no
// recursion and no possessive quantifiers. Elara's PCRE2 10.40 predates
// syntax and \w changes up to 10.48, is unmaintained, and builds only for
// linux and darwin. Also rejected without a full run:
// github.com/bobby-stripe/go-pcre (PCRE2 10.38 in a naive wasm
// interpreter, no start offset, about 2500 times slower: 1.9 ms for one
// version match) and github.com/dwisiswant0/pcregexp (loads a shared
// libpcre2 at run time, so it cannot build a static binary).
//
// Only a PCRE2 10.48 transpiled to Go is exact as well. It is slower where
// Composer spends its time: time per run relative to this engine, on
// amd64.
//
//	                                in-house  ccgo 10.48
//	whole corpus (match, all, repl)   237 ms       +27%
//	semver/VersionParser patterns    0.67 ms      2.15x
//	one version match                 764 ns      1.45x
//	class-map patterns, 700 KB PHP    34 ms       1.8x
//	JsonManipulator, 66 KB json       95 ms     0.13x
//	compiling the corpus, uncached    18 ms     0.29x
//
// Both give PHP's results on the JsonManipulator run, including its 18
// backtrack-limit errors. That run (and compilation) is where this engine
// trails PCRE2, so it is the place to optimise; adopting a transpiled
// PCRE2 would also mean maintaining about 4 MB of generated Go per
// platform. This engine stays.
//
// # Errors
//
// PHP warnings that Composer never relies on are not reproduced. Errors
// that PHP reports through return values or exceptions (json_last_error,
// preg_last_error, JsonException, Composer\Pcre\PcreException) are Go
// errors carrying PHP's exact message text.
package php
