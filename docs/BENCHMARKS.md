# Benchmarks: maestro vs Composer 2.10.3

## Warm install and dump-autoload (issue #12, 2026-10-06)

Machine: Linux 6.18 x86-64, 12 cores, php 8.4.25 of the dev shell, real
Packagist network. Measured on tmpfs (/dev/shm: the store imports by
hardlink) and on btrfs with zstd (/tmp: by reflink clone). Other agents'
builds and test suites ran on the same machine (load 3 to 16), so each
maestro row interleaves the two binaries run by run.

Method (scripts kept out of the repo): the laravel and symfony projects of
the e2e suite (laravel/laravel v13.10.1 locked; symfony/skeleton 7.4 plus
symfony/webapp-pack), every command with `--no-plugins --no-scripts -q`,
per tool and binary its own COMPOSER_HOME, COMPOSER_CACHE_DIR and
MAESTRO_CACHE_DIR, COMPOSER_TEST_SUITE=1. Each maestro binary starts from
a fresh maestro cache (Composer's files cache copied in), one install per
project to fill the store, a second warm install and three dumps; then
*dump -o* (`dump-autoload -o` in the installed project, median of 15) and
*warm install* (a fresh copy of the project without vendor/, `install`,
median of 11), laravel before symfony. Composer: mean of 5 hyperfine runs
after a warm-up. "before" is b01f0e2, "after" this change.

| fs | Project | Command | Composer | maestro before | | maestro after | |
|---|---|---|---:|---:|---:|---:|---:|
| tmpfs | laravel | warm install | 2.46s | 299 ms | 8.2x | 291 ms | 8.4x |
| tmpfs | laravel | dump-autoload -o | 1.02s | 137 ms | 7.4x | 127 ms | 8.0x |
| tmpfs | laravel | dump -o after 22 warm installs | 1.02s | 163 ms | 6.2x | 122 ms | 8.3x |
| tmpfs | symfony | warm install | 2.19s | 302 ms | 7.2x | 290 ms | 7.5x |
| tmpfs | symfony | dump-autoload -o | 1.15s | 183 ms | 6.3x | 143 ms | 8.0x |
| btrfs | laravel | warm install | 2.50s | 510 ms | 4.9x | 477 ms | 5.2x |
| btrfs | laravel | dump-autoload -o | 1.07s | 149 ms | 7.2x | 143 ms | 7.4x |
| btrfs | symfony | warm install | 2.14s | 547 ms | 3.9x | 452 ms | 4.7x |
| btrfs | symfony | dump-autoload -o | 1.19s | 184 ms | 6.5x | 150 ms | 7.9x |

CPU time (user + system, medians) falls further than wall time: warm
install 479 → 439 ms (laravel) and 428 → 361 ms (symfony) on tmpfs,
1288 → 1162 ms and 1345 → 1101 ms on btrfs. A quieter btrfs run of the
laravel install measured 540 → 418 ms.

What changed:

- The parse cache file (`classmap/v1.bin`) recorded the content hash of
  every file a dump read or a store import created, by inode. A warm
  install into a new worktree creates new inodes, so each one added an
  entry per PHP file, and every dump loads the whole file first: before
  grew to 8 MB after 22 warm installs in the runs above (24 MB after some
  thirty earlier), and its dump slowed from 137 to 163 ms. Store imports
  now give clones and copies their object's stamp as modification time,
  as hardlinks always had, so the dump recognises a package file by one
  stat (size and stamp of the release file at that path) without any
  record, and keeps the parse results with the release in the store
  (`derived/`), loaded only for the releases a project uses and shared
  by every project: the cache file stays at a few KB (the root package's
  files), and a new worktree's dump reads and parses none of the package
  files an earlier dump saw. Store imports no longer stat each PHP file
  to report it to the dump.
- Imports ran up to GOMAXPROCS goroutines each, a hundred packages at a
  time: on btrfs creating files gets slower with more threads (a
  standalone test cloned laravel's 8,861 files in 230 to 350 ms with one
  thread, 115 to 165 ms with 4 to 8, 125 to 170 ms with 16), on tmpfs no
  faster beyond 8.
  All imports now share at most eight, and a large package takes on
  helpers as others finish.
- The dump after an install no longer decodes the store indexes again,
  indexes decode without a copy per path, release lookups run while the
  scan walks the directories, and each exclude-from-classmap regex is
  compiled once per dump instead of once per autoload directory.

Not done: io_uring. On tmpfs the store imports (hardlinks: some 14 ms of
syscalls over eight threads for laravel) finish while the installer is
still issuing the downloads; on btrfs the time is the filesystem's own
work creating and cloning inodes, which batching syscalls does not
reduce, and FICLONE has no io_uring operation. Copies already use
copy_file_range (Go's io.Copy between files), clones FICLONE or
clonefile.

Where a warm install's time goes now (execution trace, laravel on tmpfs,
about 290 ms): the main goroutine waits 76 ms on the filter list's
conditional request (`verifyLock`), 28 ms on the php probe and 20 ms on
root version guessing, all before the first package; GC mark workers use
58 ms of CPU. In this change's areas: the imports overlap the download
loop (about 50 ms for 109 packages, mostly store lookups and the cached
archives' opens), the 109 install operations take 35 ms one after the
other (each resolves the vendor dir's realpath several times), and the
dump 45 ms (walk and stat of 9,000 files, the rules' scans, writing
autoload_static.php). A standalone `dump-autoload -o` spends some 60 ms
before the dump starts (process start, the php probe, version guessing).
The ≥20x warm install target needs those fixed costs gone first.

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
