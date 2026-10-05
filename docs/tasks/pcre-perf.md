# Task: make internal/php's regex engine fast on large recursive patterns

The engine evaluation (see internal/php/doc.go) kept the in-house PCRE2 engine: exact on every golden and fastest on small patterns. But on JsonManipulator's recursive patterns over a 66 KB composer.json it takes 95 ms and allocates 88 MB where PHP takes ~1 ms; patterns that hit the backtrack limit take 4.2 s vs 0.68 s for PCRE2; compiling the corpus without cache costs 17.6 ms / 14.8 MB vs 5.1 ms / 0.3 MB.

Close those gaps without losing exactness:
- Profile (pprof CPU + alloc) the JsonManipulator workload and the limit-hitting set. The evaluation harness and a ccgo-transpiled PCRE2 10.48 reference are in the worktree .claude/worktrees/pcre-eval (branch pcre-eval; ext_*_eval.go, ext_bench_eval_test.go, go.ccgo.mod) and /tmp/pcre-eval/ if still present — reuse them as the speed reference.
- Study what PCRE2 does that the VM doesn't: start-of-match optimisations (first code unit / required code unit "req char" checks, minimum subject length), auto-possessification, recursion frame handling without per-call heap allocation (frame vector reuse), compiled character class bitmaps, avoiding capture-vector copies on recursion entry/exit, memoising nothing that changes semantics.
- Match-limit accounting must keep giving the same PHP-visible results (which patterns hit the limit) on every golden; if PCRE2's own optimisations change which patterns hit the limit, mirror PCRE2's behaviour since that's what PHP does.
- Reduce compile-time allocations (the parser/compiler), and keep the Compile cache.
Target: within ~2x of the transpiled PCRE2 on every workload in the evaluation's table, without regressing the small-pattern workloads where the engine already wins. All goldens (internal/php/testdata/preg, engine goldens) must stay exact; add the JsonManipulator 66 KB workload (with PHP's results) as a committed golden + benchmark.

Scope: internal/php engine files (pcre_*.go) and their tests/benchmarks only; public API unchanged. Other agents are working in other packages concurrently and calling internal/php.
Report: before/after table, what changed, test/lint status.
