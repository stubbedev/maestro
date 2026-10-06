# Task: make CI green on Linux, macOS and Windows

The first CI run (.github/workflows/ci.yml) on GitHub failed (run on commit 63ebd85; see `gh run list -w CI`, `gh run view <id> --log-failed`):
- ubuntu: internal/classmap TestOracleGenerate/tree-classmap-dedupe — the golden encodes one filesystem's readdir order (which file wins when the same class is reachable via a symlink and a real path). The job used -failfast, so later failures are hidden: run the whole suite.
- macos: also internal/archive TestDifferentialZip (macOS's unzip is not Info-ZIP 6.0 as on Linux — the differential test must compare against the reference extractor the port targets, not whatever `unzip` is on PATH), internal/autoload TestOracle_Dump/s234, and whatever else.
- windows: `go vet ./...` fails.

Fix every failure at its root:
- Where Composer itself is order-dependent (readdir order), maestro must do exactly what Composer would do on the same filesystem; tests must not encode one machine's order — make goldens order-independent where the behaviour legitimately varies, or construct fixtures whose order is fixed, documenting why.
- Platform differences in reference tools: pin or detect the reference (e.g. require Info-ZIP unzip for the differential test, skip with a clear message otherwise; on macOS CI install Info-ZIP via brew in the workflow if that's the right call).
- Windows: make `go vet ./...` and `go build ./...` pass (build tags, Windows-specific code paths); the test suite need not run on Windows (the workflow says so), but vet must.
- Drop -failfast from the workflow's test step so all failures surface.

You may push to a branch named `ci/green` on origin (github.com/stubbedev/maestro) to run CI and iterate (`gh run watch`, `gh run view --log-failed`); never push to main, never force-push anything but ci/green, and don't commit to the local main branch — work in a separate git worktree on branch ci/green created from main. Other agents (performance, plugin phase 6) are editing the main working tree concurrently: avoid their areas (internal/plugin; perf touches hot paths across packages — coordinate via HANDOFF.md in the main tree).
Report: each failure's root cause and fix, the final green CI run URL, and the branch's commits for me to merge.
