# Task: consolidate PHP helpers into internal/php (DRY)

Wave 1 ports were written in parallel and duplicated PHP helpers. Consolidate without changing any behaviour or public API (all existing tests and oracle goldens must keep passing unchanged):
- internal/semver: private helpers (numeric strings, loose ==, float to string (`phpcompat.go`), sort) → use internal/php equivalents where they are exactly equivalent; keep semver's hand-written matchers.
- internal/util/pcre.go: replace with internal/php's Regexp/Preg API.
- internal/console: levenshtein, strip_tags, escapeshellarg, stripcslashes and its sprintf → move into internal/php as exported functions with oracle-golden tests (tools/oracle/php), and make console use them. Check whether internal/util's ProcessExecutor escaping duplicates escapeshellarg.
- Look for any other duplication across internal/{php,semver,classmap,console,io,util,spdx,archive,store,cache} (e.g. path helpers, natural sort, error types) and consolidate where it is a true duplicate.
Run benchmarks before and after for anything on a hot path (semver matching, classmap parsing) and don't regress them.

Other agents are concurrently creating new packages (internal/json, internal/config, internal/policy, internal/pkg, internal/util/http, and new files in internal/util and internal/cache). Don't touch those; keep your internal/util changes limited to pcre.go and its callers.
Report: what was consolidated, benchmark before/after, test/lint status.
