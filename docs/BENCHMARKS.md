# Benchmarks: maestro vs Composer 2.10.3

## update --dry-run: metadata loading (#12, 2026-10-06)

Same machine, projects and flags as below (`update --dry-run --no-plugins
--no-scripts -q`, per tool its own COMPOSER_HOME/COMPOSER_CACHE_DIR/
MAESTRO_CACHE_DIR, COMPOSER_TEST_SUITE=1). The three tools ran
interleaved round by round (medians of 5-7 rounds) because other agents
kept the machine at a load average of 5-26; absolute times are noisy,
the ratios less so. "before" is b01f0e2, "after" this change. *warm*:
metadata cache filled by earlier runs (every p2 file still gets its
conditional request, as Composer's); *cold*: empty caches each run.

| Project | Cache | Composer | maestro before | | maestro after | |
|---|---|---:|---:|---:|---:|---:|
| laravel | warm (quiet run, load ~5) | 2.51s | 0.74s | 3.4x | 0.42s | 6.0x |
| laravel | warm (load 3-15) | 2.62s | 0.80s | 3.3x | 0.48s | 5.4x |
| laravel | cold | 2.60s | 1.04s | 2.5x | 0.61s | 4.3x |
| symfony | warm (quiet run, load ~4) | 7.27s | 1.73s | 4.2x | 0.85s | 8.5x |
| symfony | warm (load up to 26) | 8.85s | 1.72s | 5.2x | 0.90s | 9.9x |
| symfony | cold | 6.89s | 1.95s | 3.5x | 1.31s | 5.3x |

What the profiles showed and what changed:

- The waves: the installer prefetched the locked packages' p2 files, but
  every package outside the lock (old versions' requirements, replaced
  names such as illuminate/*) cost a round trip per level, and every wave
  decoded, expanded and built its files on the critical path. The pool
  builder now lets the repository speculate (`composerrepo/speculate.go`):
  it walks the dependency graph ahead on other goroutines, going on with
  cached copies at once, requests (prefetches) each file, decodes it and
  builds the packages of the versions the constraints seen accept; the
  loads take decoded files and packages for the exact same JSON, once,
  and otherwise behave, print and request as before. Without a cached
  root file it starts once the root file is loaded.
- Each p2 file was decoded three times (packages, advisories, filter
  list): the advisory and filter loads now use a slim copy of the files
  already decoded. Minified versions are expanded in place, copying only
  the versions loaded.
- The pool optimizer (0.5 s for symfony on one core) and the advisory
  filter's matching run on all cores (0.19 s).
- Prefetches start in request order, the speculation's first, and a
  request taking a still-queued prefetch makes it at once; the
  installer's prefetch now includes the root requirements (the fixed root
  package it read has none). Packagist already answers over one HTTP/2
  connection (128 streams); 64 vs 128 parallel prefetches made no
  measurable difference.

Not done, and why: conditional requests cannot be skipped (Composer
revalidates every p2 file on update, so a skipped request could miss a
change); typed p2 decoding was not needed once decoding left the
critical path. laravel's remaining ~0.4 s is ~40 ms of startup before the
installer, ~70-130 ms for the TLS connection and the root file's
conditional request, Packagist's latency for ~150 conditional requests,
then advisory/filter/optimizer/solver (~70 ms).

## Per command, real-world projects (perf task, 2026-10-06)

Machine: Linux 7.0 x86-64, 8 cores, 30 GB RAM, /tmp on tmpfs (so the
store imports by hardlink), php 8.4.25 of the dev shell (58 extensions),
real Packagist/GitHub network (~24 ms round trip). Other agents' test
suites were running on the same machine, so absolute times are noisy; each
row ran the three tools back to back under the same conditions.

Method (scripts kept out of the repo): the laravel, symfony and private-app
(a large private Laravel application) projects of the e2e suite (composer.json + composer.lock after its
create-project/require steps), each command with `--no-plugins
--no-scripts` (private-app also `--ignore-platform-reqs`), per tool its own
COMPOSER_HOME/COMPOSER_CACHE_DIR/MAESTRO_CACHE_DIR and
COMPOSER_TEST_SUITE=1. *cold*: empty caches and store, `install` (one
run). *warm install*: caches and store warm, a fresh copy of the project
(no vendor/), `install`; that is also the "new worktree" case.
*no-op install*: `install` with vendor/ in place. *dump -o*:
`dump-autoload -o`. *update --dry-run*: warm metadata cache. Warm rows are
the median of 5 hyperfine runs after a warm-up. "before" is f33729d (the tree
the perf task started from), "after" is the perf task's tree.

| Project | Command | Composer | maestro before | | maestro after | |
|---|---|---:|---:|---:|---:|---:|
| laravel | cold install | 7.01s | 4.28s | 1.6x | 2.52s | 2.8x |
| laravel | warm install | 3.12s | 0.58s | 5.3x | 0.27s | 11.4x |
| laravel | no-op install | 1.71s | 0.27s | 6.4x | 0.22s | 7.8x |
| laravel | dump-autoload -o | 0.58s | 0.18s | 3.3x | 0.12s | 5.0x |
| laravel | update --dry-run | 2.46s | 0.89s | 2.8x | 0.71s | 3.4x |
| symfony | cold install | 7.52s | 3.50s | 2.1x | 2.74s | 2.7x |
| symfony | warm install | 2.83s | 0.60s | 4.7x | 0.29s | 9.8x |
| symfony | no-op install | 1.30s | 0.18s | 7.2x | 0.18s | 7.2x |
| symfony | dump-autoload -o | 0.66s | 0.17s | 3.9x | 0.12s | 5.7x |
| symfony | update --dry-run | 5.41s | 1.58s | 3.4x | 1.35s | 4.0x |
| private-app | cold install | 18.15s | 13.54s | 1.3x | 8.88s | 2.0x |
| private-app | warm install | 8.69s | 2.86s | 3.0x | 1.16s | 7.5x |
| private-app | no-op install | 5.18s | 0.97s | 5.4x | 0.72s | 7.2x |
| private-app | dump-autoload -o | 3.91s | 0.86s | 4.6x | 0.65s | 6.0x |
| private-app | update --dry-run | 5.62s | 2.09s | 2.7x | 1.61s | 3.5x |
| basic (e2e fixture) | warm-worktree `install`, offline | 0.34s | 0.19s | 1.8x | 0.14s | 2.4x |

What the profiles (`go build -tags maestro_profile`: MAESTRO_CPUPROFILE,
MAESTRO_TRACE, MAESTRO_MEMPROFILE) showed, and what changed:

- Warm install was dominated by InstallationManager writing installed.json
  and installed.php after every operation, each with every package
  (O(n²): 1.8 s of private-app's 5 s). Batches handled only by maestro's own
  installers now write once, as the last write would have.
- The autoload dump scanned each autoload rule separately (parallel only
  within one rule, ~3 cores busy): all files are now walked and parsed in
  one parallel pass first, then the rules are applied in Composer's order.
  Parse results are kept by content hash in maestro's cache dir, so
  unchanged files are read and hashed, not tokenized (-40% CPU); strtr()
  of autoload_static.php and the class-keyword pre-scan got faster.
- Cold installs waited on the 12-request limit (completions are taken in
  start order, so a slow download held its slot's successors): below -vvv
  up to 48 https requests now run at once.
- The install notification (a POST to packagist.org) is no longer waited
  for before the autoload dump (below -vvv, where nothing about it is
  printed), and its TLS connection is opened while packages install.

Targets not met, and why:

- Warm install ≥5x: met (7.5–11x). Cold: bound by the network (GitHub
  zipball generation and redirects); 2–2.8x.
- Warm worktree ≥10x: laravel 11x, symfony ~10x, private-app 7.5x. In
  private-app, ~0.3 s is one package installed from source (git clone,
  checkout and reset, which Composer runs as well) and ~0.2 s the
  conditional requests every install makes (packages.json and the filter
  list); the rest is the autoload dump of 75k files. The e2e warm-worktree
  fixture (offline, 52 packages) is 2.4x: its floor is processes Composer
  runs too: the php probe (~20 ms, needed before anything prints), the
  root version guessing (git/hg/fossil/svn, ~15 ms) and two `@php`
  scripts (~55 ms), against Composer's 0.34 s.
- update/require: the metadata loads in waves (one round trip per level of
  the dependency graph), as Composer's; 3.4–4x here (the 5–6x of the
  resolver alone is unchanged).

## e2e suite (before the perf task)

Wall time of the end-to-end suite (`cmd/maestro`, `MAESTRO_E2E=1 go test -run TestE2E`),
summed over each scenario's steps. Every scenario runs with the official
composer.phar 2.10.3 and with maestro, from cold caches (empty
COMPOSER_CACHE_DIR, empty package store) and then warm (a fresh project with
the caches and store the cold run left: a second worktree). Both tools use the
same php (8.4.25), environment and network; steps are the same commands and
their results are compared byte for byte (see cmd/maestro/e2e_test.go).

Measured 2026-10-05 on Linux (x86-64), scenarios run one at a time, real
Packagist/GitHub network. `MAESTRO_E2E_REPORT=<file>` writes this table.

| Scenario | Composer cold | maestro cold | speed-up | Composer warm | maestro warm | speed-up |
|---|---:|---:|---:|---:|---:|---:|
| install-from-lock | 4.98s | 4.68s | 1.1x | 1.46s | 0.58s | 2.5x |
| commands | 20.48s | 11.72s | 1.7x | 16.35s | 8.56s | 1.9x |
| update | 20.70s | 5.82s | 3.6x | 7.81s | 3.07s | 2.5x |
| require-remove | 22.98s | 6.75s | 3.4x | 10.09s | 4.12s | 2.4x |
| path-repositories | 5.96s | 1.97s | 3.0x | 3.23s | 1.44s | 2.2x |
| vcs-repositories | 7.68s | 2.84s | 2.7x | 4.69s | 2.30s | 2.0x |
| prefer-source | 6.27s | 2.81s | 2.2x | 2.54s | 1.06s | 2.4x |
| artifact-repository | 2.14s | 0.95s | 2.2x | 2.07s | 0.84s | 2.5x |
| platform | 11.43s | 3.29s | 3.5x | 5.79s | 2.39s | 2.4x |
| unsatisfiable | 9.20s | 2.61s | 3.5x | 3.96s | 1.65s | 2.4x |
| scripts | 6.54s | 2.38s | 2.7x | 3.82s | 1.90s | 2.0x |
| scripts-php-callable | 3.54s | 0.99s | 3.6x | 1.04s | 0.46s | 2.2x |
| create-project | 13.20s | 4.48s | 2.9x | 4.85s | 2.24s | 2.2x |
| config | 2.90s | 1.52s | 1.9x | 2.88s | 1.49s | 1.9x |
| init | 4.66s | 0.91s | 5.1x | 1.02s | 0.43s | 2.4x |
| warm-worktree | 5.21s | 3.23s | 1.6x | 1.21s | 0.55s | 2.2x |
| verbosity | 12.01s | 7.96s | 1.5x | 11.82s | 8.04s | 1.5x |
| security | 9.72s | 3.63s | 2.7x | 6.95s | 2.88s | 2.4x |
| global | 4.35s | 1.20s | 3.6x | 1.85s | 0.72s | 2.6x |
| validate | 0.41s | 0.26s | 1.6x | 0.43s | 0.24s | 1.8x |
| prompts | 10.44s | 3.41s | 3.1x | 4.58s | 1.84s | 2.5x |
| autoload | 7.16s | 2.28s | 3.1x | 4.19s | 1.80s | 2.3x |
| outdated | 15.28s | 5.19s | 2.9x | 7.58s | 3.44s | 2.2x |
| dists | 2.25s | 0.88s | 2.6x | 1.75s | 0.72s | 2.4x |
| install-no-dev | 4.74s | 1.67s | 2.8x | 0.80s | 0.31s | 2.5x |
| install-lock-to-lock | 7.45s | 2.19s | 3.4x | 1.50s | 0.55s | 2.7x |
| laravel | 16.72s | 12.72s | 1.3x | 7.33s | 3.35s | 2.2x |
| symfony | 21.22s | 11.49s | 1.8x | 9.30s | 4.11s | 2.3x |
| private-app | 29.00s | 24.18s | 1.2x | 12.84s | 5.29s | 2.4x |
| **total** | 288.64s | 134.00s | 2.2x | 143.73s | 66.40s | 2.2x |

Notes:

- Cold runs of real projects (laravel, symfony, private-app) are bound by
  downloading the dists: both tools use at most 12 parallel HTTP transfers
  (COMPOSER_MAX_PARALLEL_HTTP), so the gain is mostly in resolution,
  extraction (native, into the store, while other downloads run) and
  autoload dumping.
- Warm runs show the store (deviation 1): packages are materialized from the
  extracted store (reflink or copy) instead of unzipping cached archives.
- Commands that mostly wait on Packagist metadata (`commands`, `verbosity`:
  show/outdated/audit/search) gain the least.
- The real-world scenarios run with `--no-plugins --no-scripts` until the
  plugin phases land (see e2e_realworld_test.go); private-app installs from its
  lock without its private packages.
