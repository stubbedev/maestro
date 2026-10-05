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
// ((*VERB)), callouts, (?C), and \p{...} by script extension (scripts
// match by their Script property only). Compilation errors carry PCRE2's
// message texts, but their offsets may differ.
//
// The match limit (pcre.backtrack_limit, 1000000) is counted as PHP
// counts it, so the same subjects fail with PREG_BACKTRACK_LIMIT_ERROR:
// as PCRE2's JIT does (at iterator backtracks, group exits, recursion
// entries, with its early-fail, character-position and repeat
// optimisations), and as pcre2_match does (one per backtracking frame)
// for the anchored retry after an empty match, which PHP runs without the
// JIT. Each start position is counted from zero. Counts may differ by a
// few at start positions that PCRE2 skips with heuristics of its own
// (testdata/preg/counts.json holds PHP's counts). Recursion that loops or
// nests past 100000 calls fails like PHP's JIT running out of stack, with
// PREG_JIT_STACKLIMIT_ERROR.
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
// Composer spends its time (testdata/preg/jsonmanipulator holds the
// JsonManipulator workload: its patterns over a 66 KB composer.json, with
// PHP's results); time per run on amd64 (pcre_bench_test.go):
//
//	                                    in-house  ccgo 10.48
//	whole corpus (match, all, repl)       233 ms      221 ms
//	semver/VersionParser patterns       0.53 ms     0.93 ms
//	one version match                     438 ns      827 ns
//	class-map patterns, Composer source  0.045 ms    0.155 ms
//	class-map patterns, 700 KB PHP        5.9 ms       47 ms
//	JsonManipulator, completing           5.1 ms      9.0 ms
//	JsonManipulator, backtrack limit      549 ms      488 ms
//	compiling the corpus, uncached        6.8 ms      3.8 ms
//
// Runs that end in the backtrack limit (most of the corpus time is one
// such pattern) cost about what PCRE2's JIT spends reaching the limit,
// which this engine counts the same way. Compilation, which the Compile
// cache does once per pattern, is where it trails; adopting a transpiled
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
