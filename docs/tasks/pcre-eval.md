# Task: choose the regex engine behind internal/php

internal/php currently ships its own PCRE2-compatible engine (pcre_*.go). The user prefers a maintained library if one is at least as correct and fast: "whichever is the newest and most performant implementation of the regex spec we require".

Requirements: pure Go (CGO_ENABLED=0 builds, static binaries); behaviour identical to PHP 8.4's PCRE2 10.48 on everything Composer uses — including recursion/subroutines (`(?&json)`, `(?R)`, `(?1)`), conditionals, lookbehind, possessive/atomic, byte mode without /u vs UTF-8 mode with /u, PREG_UNMATCHED_AS_NULL group semantics, offsets in bytes, match-limit/backtrack-limit errors as PHP reports them.

Do:
1. Find candidates: at least github.com/KarpelesLab/gopcre2, go.arsenm.dev/pcre (ccgo-transpiled PCRE2), github.com/dlclark/regexp2, and search for anything newer/faster (pure Go PCRE2 ports, wazero/wasm-compiled PCRE2 run in pure Go, transpiled PCRE2 versions closer to 10.48). Record version, last activity, license, maintenance signals.
2. For each, write an adapter behind internal/php's existing Regexp/Preg API (in a scratch git worktree or build-tagged files — don't break the main tree; other agents are working in it) and run the existing goldens: internal/php/testdata/preg (596 Composer patterns, 6,974 subjects × 8 ops) and the engine-feature goldens (304 patterns). Count exact passes/failures per category; investigate failures (adapter bug vs engine divergence).
3. Benchmark each passing candidate against the current engine on: the Composer pattern corpus overall, semver/constraint-like patterns, JsonManipulator's recursive patterns on a 66 KB composer.json, classmap-style scanning over large PHP files, compile time and allocations.
4. Recommend. If a library wins (fully golden-exact, and at least as fast or close with clear maintenance benefits), switch internal/php to it behind the same public API (keeping the PHP-delimiter/modifier parsing and Preg semantics layer), delete the in-house engine, and make sure everything in the repo still passes. If no library qualifies, keep the in-house engine and document why in internal/php/doc.go, with the numbers. Upstream-able fixes to a library are fine to note but don't vendor-fork without saying so.

Scope: internal/php (engine files only; the public API stays), go.mod via `go get`. Other agents are adding new packages concurrently and calling internal/php's API; it must keep building throughout (do experiments outside the main tree).
Report: the comparison table (correctness and speed), the decision and why, and test/lint status.
