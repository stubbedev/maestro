# Benchmarks: maestro vs Composer 2.10.3

How fast maestro is against the official composer.phar 2.10.3 on real
projects, how that is measured, and where the time goes.

## Method

Projects: the real-world projects of the end-to-end suite.

- **laravel**: laravel/laravel v13.10.1, locked (109 packages, about 8,800
  files).
- **symfony**: symfony/skeleton 7.4 plus symfony/webapp-pack, locked.
- **private-app**: a large private Laravel application (75k files to
  autoload). It is not in the repository; point `MAESTRO_E2E_PRIVATE_APP`
  at a checkout with its composer.lock to run its rows and its e2e
  scenario.

Setup, for every number below unless a table says otherwise:

- Linux x86-64, php 8.4 of the dev shell, the real Packagist and GitHub
  network (about 24 ms round trip), projects on btrfs (the store imports
  by reflink clone) or tmpfs (by hardlink).
- Every command with `--no-plugins --no-scripts -q` and
  `COMPOSER_TEST_SUITE=1`; each tool, and each maestro binary, has its own
  COMPOSER_HOME, COMPOSER_CACHE_DIR and MAESTRO_CACHE_DIR. maestro's
  platform probe cache and class map records are per binary, so two
  binaries sharing one MAESTRO_CACHE_DIR miss them on alternate runs.
- The tools run interleaved run by run, the order reversed every other
  round, after 2 untimed warm-ups; wall time, CPU (user + system) and peak
  RSS are medians taken from wait4. Single-tool series use `hyperfine -N`.
  Compare ratios within one table: the CPU's power profile (intel_pstate
  "power" vs "performance") moves both tools' absolute times by up to a
  third.

Scenarios:

- *cold install*: caches, store and vendor/ removed before each run.
- *warm install*: a fresh copy of the project without vendor/, warm caches
  and store; this is also the "new git worktree" case.
- *no-op install*: `install` with vendor/ in place.
- *update --dry-run*: *warm* has the metadata cache filled (every p2 file
  still gets its conditional request, as Composer's); *offline* adds
  `COMPOSER_DISABLE_NETWORK=1`, which shows the CPU floor; *cold* starts
  from empty caches.
- *dump-autoload -o*: in the installed project.

The end-to-end suite times every scenario for both tools;
`MAESTRO_E2E_REPORT=<file>` writes its table (see "End-to-end suite").

Profiles come from a build with `go build -tags maestro_profile
./cmd/maestro`: `MAESTRO_CPUPROFILE=<file>` writes a CPU profile,
`MAESTRO_MEMPROFILE=<file>` an allocation profile and
`MAESTRO_TRACE=<file>` an execution trace (docs/PORTING.md, "Profiling").

## Results

### Every command, power-saver profile

Quiet machine, CPU in the power-saver profile; cold install 5 rounds,
warm install 15, no-op 20, update warm 15 (two series), update cold 5-6,
dump-autoload 20.

| Project | Command | Composer | maestro | speed-up |
|---|---|---:|---:|---:|
| laravel | cold install | 7.22 s | 2.47 s | 2.9x |
| laravel | warm install | 3.25 s | 360 ms | 9.0x |
| laravel | no-op install | 1.79 s | 112 ms | 16.0x |
| laravel | update --dry-run, warm | 3.51 s / 3.42 s | 351 ms / 387 ms | 10.0x / 8.8x |
| laravel | update --dry-run, cold | 3.59 s | 567 ms | 6.3x |
| laravel | dump-autoload -o | 1.29 s | 37.3 ms | 34.7x |
| symfony | cold install | 7.71 s | 2.72 s | 2.8x |
| symfony | warm install | 3.74 s | 381 ms | 9.8x |
| symfony | no-op install | 1.11 s | 108 ms | 10.2x |
| symfony | update --dry-run, warm | 7.77 s / 7.47 s | 1.12 s / 965 ms | 6.9x / 7.7x |
| symfony | update --dry-run, cold | 7.45 s | 1.15 s | 6.4x |
| symfony | dump-autoload -o | 1.38 s | 41.6 ms | 33.2x |

### Installs and updates, performance profile

CPU in the `performance` profile, where Composer is markedly faster
(laravel's warm install 2.12 s instead of 3.25 s), so the ratios are lower
than above for the same work. No-op 20 rounds, warm install 12-15, update
warm 12, offline 15.

| Project | Command | Composer | maestro | speed-up | maestro CPU | maestro peak RSS |
|---|---|---:|---:|---:|---:|---:|
| laravel | no-op install | 1.27 s | 90.6 ms | 14.0x | 124 ms | |
| symfony | no-op install | 623 ms | 91.1 ms | 6.8x | 91 ms | |
| laravel | warm install | 2.12 s | 207 ms | 10.2x | | |
| symfony | warm install | 1.65 s | 232 ms | 7.1x | | |
| laravel | update --dry-run, warm | 2.12 s | 255 ms | 8.3x | 652 ms | 141 MB |
| laravel | update --dry-run, offline | | 151 ms | | 615 ms | 134 MB |
| symfony | update --dry-run, warm | 6.18 s | 441 ms | 14.0x | 1777 ms | 216 MB |
| symfony | update --dry-run, offline | | 365 ms | | 1737 ms | |

### Source installs

psr/log 1.1.4, psr/container 1.1.2 and symfony/console v7.3.4 (10
packages), locked, warm mirror cache, files cache and store, vendor/
removed before each run; hyperfine, 15 runs.

| Command | maestro |
|---|---:|
| `install --prefer-source` | 285 ms |
| `install --prefer-dist` | 238-249 ms |

A source install from the store starts no git process per package.

### Per command, with private-app

The only series that includes private-app: 8 cores, tmpfs (hardlink
imports), a busy machine, the three tools back to back per row; warm rows
are medians of 5 hyperfine runs, cold rows one run. These numbers come
from an earlier build than the tables above, which supersede them for
laravel and symfony.

| Project | Command | Composer | maestro | speed-up |
|---|---|---:|---:|---:|
| private-app | cold install | 18.15 s | 8.88 s | 2.0x |
| private-app | warm install | 8.69 s | 1.16 s | 7.5x |
| private-app | no-op install | 5.18 s | 0.72 s | 7.2x |
| private-app | dump-autoload -o | 3.91 s | 0.65 s | 6.0x |
| private-app | update --dry-run | 5.62 s | 1.61 s | 3.5x |

In private-app's warm install, ~0.3 s is one package installed from
source (git clone, checkout and reset, which Composer runs too) and
~0.2 s the conditional requests every install makes; the rest is the
autoload dump of 75k files.

### End-to-end suite

Wall time of the end-to-end suite (`cmd/maestro`,
`MAESTRO_E2E=1 go test -run TestE2E`), summed over each scenario's steps.
Each scenario runs with composer.phar 2.10.3 and with maestro from cold
caches (empty COMPOSER_CACHE_DIR and store), then warm (a fresh project on
the caches and store the cold run left); both tools use the same php,
environment and network, and their results are compared byte for byte.
Recorded with an earlier build than the tables above; rerun with
`MAESTRO_E2E_REPORT=<file>` for current numbers.

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

Commands that mostly wait on Packagist metadata (`commands`, `verbosity`:
show, outdated, audit, search) gain the least.

## What makes it fast

Everything here keeps frozen output identical (deliberate deviations 1
and 3 in docs/PORTING.md); docs/PORTING.md's "maestro's own caches"
describes the caches and why a stale entry cannot change a result.

Installing:

- **The package store.** Dists are extracted once into a content-addressed
  store and imported into vendor/ by reflink clone, hardlink or copy. All
  imports share at most eight goroutines (creating files on btrfs gets
  slower with more threads), each clone takes seven syscalls on raw
  descriptors, and source checkouts are kept in the store too, `.git`
  included, so a source install from it starts no git process per package.
- **Work during the lock verification.** While an install from a lock
  revalidates packages.json and the filter list, maestro computes the
  transaction, starts the dist transfers the files cache lacks, imports
  from the store the packages it already holds (into a directory of their
  own, taken by the install with one rename), opens the install
  notification's connection, scans the class map, builds the autoload
  files and installed.json/installed.php, and reads the files the dump
  will compare. Each is used only when it is exactly what the step would
  build; a file whose contents and description are unchanged is not
  written again.
- **GitHub dists from codeload.github.com directly**, skipping the
  api.github.com redirect.
- **Up to 48 https requests at once** below -vvv; installed.json and
  installed.php written once per batch of maestro's own installers instead
  of after every operation.

Metadata and resolution:

- **Speculative loading.** The pool builder lets the repository walk the
  dependency graph ahead on other goroutines: it requests, decodes and
  builds the versions the constraints seen accept, and the loads take
  those for the exact same JSON. Minified versions are expanded once,
  only as far as needed.
- **Skeleton packages.** A version read back from the decoded metadata
  cache is built with what the solver reads only, from the cache's index
  of the file; its other properties are loaded the first time they are
  used, from the version decoded alone. Of the ~30,000 versions a
  symfony update builds, about 120 are ever loaded in full.
- **Shared connections**: transfers to an https host wait for the
  first one's connection and share its HTTP/2 connection, sending in
  request order, as curl's CURLOPT_PIPEWAIT does. On Linux the requests
  released together leave in full TCP segments (TCP_CORK until the last
  is written), as curl writes the frames of all its requests at once: a
  new connection sends about ten segments in its first round trip, which
  one segment per request would spend on the first eight requests.
  Connections to the first https repositories open while the project
  loads. Metadata requests started ahead run 100 at once per HTTP/2
  connection; those beyond open a second connection to the host as soon
  as they are asked for, so a symfony update's 201 revalidations leave
  in one round trip instead of two.
- **Decoded caches.** p2 metadata files and installed.json are kept
  decoded in a binary form, read back only for byte-identical JSON, which
  a metadata file's unchanged identity tells without a copy of the JSON
  to compare; a metadata file whose identity is unchanged is not read
  again within a run. A metadata file's versions are decoded only when
  they are read, one by one.
- **Parallel CPU work**: the pool optimizer (packages filed by interned
  hash ids, 64 items per work chunk), the advisory filter, cache reads and
  their slim copies, and autoload_classmap.php and autoload_static.php
  built in chunks of 512 classes.

Autoload:

- **Class map records**: a project's class map is kept with the identity
  of every file and directory its scans depended on; a dump with the same
  scans stats those (about 10,000 on laravel, 2 ms) instead of scanning.
- **Parse results** are kept by content hash, and with each release in the
  store, so a new worktree's dump parses none of the package files an
  earlier dump saw. The files of the classmap (and, for an optimized dump,
  PSR) paths of a package the store inserts are parsed as they are
  inserted, from the extracted bytes and by the store's hash, while other
  downloads run: the dump of a cold install reads, hashes and parses none
  of them.

Fixed per-run costs:

- **The php platform probe is cached** across runs (Linux), keyed on
  what can change its result, and stored decoded (~0.8 ms to read).
- **git's version is cached** across runs (Linux); root version guessing
  starts VCS tools directly, not through `/bin/sh -c`, and starts the
  fallbacks at once once `git branch` fails. A no-op install starts 4
  processes.
- **Package-level regexes compile on first use**: package init takes
  ~0.9 ms.
- **The schema validation of an unchanged composer.json is remembered.**

GC: the collector stays off until the run's memory reaches 64 MiB, then
runs with Go's defaults (docs/PORTING.md, "Go runtime settings"). Small
commands (`--version`, validate, a no-op install, dump-autoload) collect
at most once instead of 3 to 12 times, using up to a third less CPU, for
at most 64 MiB more memory; long runs are as before. GOGC=200, GOGC=400
with GOMEMLIMIT=320MiB and GOGC=off with GOMEMLIMIT=512MiB for the whole
run cut CPU by up to 22% but moved wall time by no more than the series'
own spread, and raised peak memory by up to 2x. An explicit GOGC or
GOMEMLIMIT works as for any Go program.

## Where the time goes

- **No-op install** (laravel, performance profile, ~88 ms): main at
  ~2 ms, the dial to repo.packagist.org at ~11 ms; TCP, TLS and the two
  conditional requests Composer's cache semantics require are three round
  trips, the 304s arriving at ~84 ms; then ~3.5 ms (pool and solver ~2,
  package map and autoload rules ~1, the static file ~0.4) and exit. The
  network is ~73 ms of it, so 20x Composer (64 ms on laravel) is below
  the floor.
- **Warm install** (laravel): ~25 ms of start-up, ~125 ms of store import
  overlapping the ~90 ms revalidation, then the install notification's
  POST (~60 ms, one round trip and server time; Composer waits for it
  too) while the dump runs. The store import (btrfs file creation and
  FICLONE, ~70 µs of kernel CPU per file over 8 threads) is the largest
  part left.
- **update --dry-run** is CPU-bound offline: decoding cached slots
  (~200 ms CPU), the speculation's prebuild (~190 ms), the collector
  (~18% of CPU), the optimizer's preferred packages (~30 ms wall).
  Online the loads end ~90 ms later, waiting for the TLS connection and
  the first wave's responses.
- **Cold install** is bound by the dist downloads from GitHub.
- **dump-autoload -o** after a record hit (laravel): ~25 ms before the
  dump (start-up, the php probe, version guessing), ~5 ms checking the
  record, then building and comparing the 1 MB autoload_classmap.php and
  autoload_static.php (3 to 4.5 ms in chunks).
