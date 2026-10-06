# Task: end-to-end comparison suite against real Composer 2.10.3

The final gate for the drop-in promise (PORTING.md, Tests §3). Rebuild cmd/maestro's e2e test into a proper suite gated by MAESTRO_E2E=1:

- Real Composer: download the official composer.phar 2.10.3 into the test cache (checksum-pinned against getcomposer.org's published sha256), never shipped. Both tools run with the devenv php, separate COMPOSER_HOME/COMPOSER_CACHE_DIR per run, same env, COMPOSER_NO_INTERACTION=1 unless a scenario tests prompts.
- Compare per step: exit code, stdout and stderr (normalise only what is inherently variable: timings, absolute temp paths, and "Downloading"/"Loading from cache" wording only where the two tools' cache states legitimately differ — document every normalisation), composer.lock byte-for-byte, the whole vendor/ tree (paths, types, modes, symlink targets, file contents; mtimes excluded per deviation 1), and any files scripts/plugins write.
- Scenarios (each a fixture project under cmd/maestro/testdata/e2e): install from lock; update; require/remove with version selection and composer.json editing; partial updates (with-dependencies, with-all-dependencies); --no-dev; --prefer-source (git clones); path repositories (symlink and mirror); vcs repositories; artifact repo; platform overrides and ignore-platform-reqs; unsatisfiable requirements (Problem output); scripts of every kind (shell, @php, @composer, @putenv, PHP-callable); create-project; show/outdated/why/why-not/licenses/fund/suggests/audit/validate/status/check-platform-reqs/dump-autoload -o/-a/--apcu; config read/write; init non-interactive; second worktree from a warm store (no network for dists, identical result); and the plugins from docs/PLUGINS.md §9.3 that the plugin phases have landed so far (start with none; add as they land).
- Real-world projects: laravel/laravel, symfony/skeleton + webapp, and a large private application (read-only: copy composer.json/lock into a temp dir; private repositories that need credentials may be dropped as the resolver oracle did — document).
- Speed report: wall time per scenario for composer vs maestro (cold and warm store), printed by the test and summarised in docs/BENCHMARKS.md.

Every mismatch found is a bug in maestro: fix it in the owning package (with a unit test there) — unless it is one of PORTING.md's deliberate deviations. Keep a list of fixes in your report.

Scope: cmd/maestro e2e tests + testdata, docs/BENCHMARKS.md, and fixes anywhere (note cross-package changes in HANDOFF.md).
