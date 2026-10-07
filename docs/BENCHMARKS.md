# Benchmarks: maestro vs Composer 2.10.3

## Probe cache keyed on the variables php reads (#87, 2026-10-07)

The platform probe cache used to key each entry on the whole environment,
so any variable that differed from the last run (a terminal's WINDOWID,
direnv's DIRENV_DIFF, the COMPOSER_* variables Composer exports to
scripts) ran php again, ~30 ms on every command. hyperfine sets
HYPERFINE_RANDOMIZED_ENVIRONMENT_OFFSET to a new value on every run, so
hyperfine numbers in the sections below include that probe on every run
(a ~45 ms floor for `--version`, `list` and `show`); the interleaved
wait4 series do not. Entries are now keyed on the variables php can read
(internal/platform/probecache_linux.go says which and why) and the cache
keeps at most 64 entries.

`show --no-plugins --no-scripts` in the laravel project, each binary with
an empty MAESTRO_CACHE_DIR, 3 warm-ups then medians of 30 runs; load
average 0.4 to 0.8. "before" is d053ec3.

| run | before | after |
|---|---:|---:|
| same environment every run | 17.9 ms | 17.9 ms |
| FOO_RANDOM changing every run | 47.6 ms | 17.9 ms |
| cache entries after the series | 31 (5.5 MB) | 1 (180 KB) |

## No-op install: start-up and the work after the revalidation (#29, 2026-10-07)

"before" is cd6b2a7, "after" is f78ff7a (the four commits below). Same
projects and method as the section below: `install --no-plugins
--no-scripts -q`, COMPOSER_TEST_SUITE=1, Composer 2.10.3 (phar) and the
two binaries interleaved run by run (order reversed every other round),
2 untimed warm-ups, medians of wall time, CPU (user + system) and peak
RSS from wait4; each binary with its own copy of MAESTRO_CACHE_DIR
(after's probe cache entries have a new format). CPU profile
`performance`; the 1-minute load average was 0.7 to 1.3 during the
series.

| Project | Command | Composer | before | after | speed-up before → after |
|---|---|---:|---:|---:|---:|
| laravel | no-op install (20 rounds) | 1.27 s | 102.6 ms | 90.6 ms | 12.4x → 14.0x |
| symfony | no-op install (20) | 623 ms | 98.9 ms | 91.1 ms | 6.3x → 6.8x |
| laravel | warm install (12) | | 211.0 ms | 207.3 ms | |
| symfony | warm install (12) | | 235.9 ms | 231.5 ms | |

CPU: laravel's no-op 130 → 124 ms, symfony's 92 → 91 ms; peak RSS
within 1.5 MB. Vendor trees and verbose output are identical between
the two binaries on laravel and symfony no-op installs, also after
removing or changing autoload_static.php, autoload_classmap.php,
autoload_psr4.php (touched), installed.json, installed.php,
vendor/autoload.php or all of vendor/composer; a `-race` build ran both
no-op installs without a report.

Timing marks (not committed; laravel, ms since exec): main 2.0 (was
4.5), revalidation responses at ~84, then before: local repository
write ~3.5, dump ~8.3 (of which class map analysis ~1.2, class map files
~4.4, reading files to compare ~1.4), exit ~97; after: repository write
~0.4, dump ~1.4, exit ~87.7.

- **Lazy package-level regexes** (1e57436): `php.MustCompile` compiles
  on first use (through the regex cache, one *Regexp per pattern);
  `GODEBUG=inittrace=1` sums package init at 3.5 → 0.9 ms of clock,
  the last init finishing at ~1.6 instead of ~4.2 ms. A test compiles
  every registered pattern, since a bad one now panics on first use.
  What is left of init is mostly third-party (klauspost/compress's
  snapref table 0.15 ms, xz, brotli, go-runewidth, 0.05 ms each).
- **Binary probe cache** (41021cb): the cached platform probe result is
  stored decoded, in php's binary form of decoded JSON; reading it and
  building the snapshot take ~0.8 instead of ~1.7 ms (benchmark, hot).
  A test checks it reads back === to the JSON and gives the same
  snapshot.
- **Dump built during the wait** (3a346b9): the speculation also
  analyses its scan (warnings kept and printed by the dump at the same
  point), builds the class map files, and reads the files the dump
  writes; the dump takes the built files when its paths match, and
  skips writing a file whose contents are those read while its
  description (file, size, mode, mtime) is unchanged.
- **Local repository write built ahead** (f78ff7a): under the same
  conditions as the speculation, installed.json and installed.php are
  built while the lock is verified; the write uses them when the same
  package objects, dev mode, dev package names, root package and
  directory are given and the install paths (looked up again) match,
  with the same unchanged-file skip. Both paths share the builder.

Left: the ~73 ms of TCP, TLS and the two conditional requests; before
the dial, the application and configuration (~3 ms) and the probe
snapshot (~0.8 ms, could be decoded lazily per section, but the
platform check reads most sections); after the responses ~3.5 ms (pool
and solver ~2, the package map and autoload rules ~0.7 to 1.2, the
static file ~0.4, exit ~0.6).

## No-op and warm install: store imports during the revalidation (#29, 2026-10-07)

"before" is 84b0087, "after" is 4a4f0cf (the four commits below). Same
projects and method as the sections below: `install --no-plugins
--no-scripts -q`, COMPOSER_TEST_SUITE=1, Composer 2.10.3 (phar) and the
two binaries interleaved run by run (order reversed every other round),
2 untimed warm-ups, medians of wall time, CPU (user + system) and peak
RSS from wait4. *no-op*: vendor/ installed, 20 rounds; *warm*: a fresh
copy of the project without vendor/, warm caches and store, 15 rounds.
Each maestro binary had **its own copy of MAESTRO_CACHE_DIR** (the
platform probe cache and the class map records are per binary, so
sharing one makes every other run miss them); COMPOSER_CACHE_DIR was
shared. CPU profile `performance` (intel_pstate powersave governor,
energy preference "performance"), as in the section below; the 1-minute
load average was 0.4 to 1.1 at the start of each series (2.5 at the end
of the last).

| Project | Command | Composer | before | after | speed-up before → after |
|---|---|---:|---:|---:|---:|
| laravel | no-op install | 1.28 s | 108.3 ms | 103.4 ms | 11.8x → 12.3x |
| symfony | no-op install | 623 ms | 106.1 ms | 99.6 ms | 5.9x → 6.3x |
| laravel | warm install | 2.12 s | 306.8 ms | 213.9 ms | 6.9x → 9.9x |
| symfony | warm install | 1.65 s | 323.0 ms | 237.3 ms | 5.1x → 6.9x |

CPU and memory are unchanged (warm: 982 → 964 ms and 936 → 937 ms of
CPU; peak RSS within 2 MB). Composer is much faster in this profile than
in #28's power-saver one (laravel's warm install 3.25 → 2.12 s, its no-op
1.79 → 1.28 s), so the ratios here are lower than #28's for the same
maestro times; compare within this table.

Where a run's time goes, from timing marks (not committed; time since
exec, medians of a few runs):

- **No-op, before**: main at 4.5 ms (runtime and package init ~4 ms), the
  platform probe's cached result decoded by 8 ms, the configuration read
  by 11 ms, the dial to repo.packagist.org started at 14.7 ms (the
  transport's TLS configuration, CA bundle parsed, was built first). TCP
  handshake, TLS handshake and the two conditional requests (sent
  together on the HTTP/2 connection) are three round trips of ~24 ms:
  the 304s arrive at ~89 ms. Then pool and solver ~2 ms, the local
  repository's write (installed.json and installed.php compared with
  the files) ~2.6 ms, the dump 6 (symfony) to 10 ms (laravel), exit at
  ~105 ms.
- **Warm, before** (laravel): the 304s at ~88 ms, the store import of the
  109 packages from ~89 to ~220 ms, the dump (class map record misses on
  a fresh copy; the parse cache answers) until ~261 ms, and the install
  notification (POST to packagist.org/downloads/, which Composer sends
  and waits for too) answered at ~299 ms: its connection had been opened
  only when the first package was installed (~190 ms), so the POST,
  sent at ~220 ms, waited for the handshakes.

What changed (vendor trees, modes and output identical to before on
laravel, symfony with plugins and scripts, and a lock that fails to
verify; the e2e install-from-lock, install-no-dev, warm-worktree,
store-heal, autoload, laravel, symfony, scripts, prefer-source,
plugin-flex, plugin-discovery and plugin-runtime scenarios pass):

- **Packages materialized while the lock is verified** (4272f6b). The
  install from a lock already started the dist transfers the files cache
  lacks during the verification (#20). A dist it holds whose release the
  store has is now materialized from the store then, into a directory of
  vendor/composer of its own; the download still checks the files cache
  and prints what it printed, and takes that tree with one rename when
  it is of the same release with the same import options, which is what
  materializing it then would build. What no download took is removed
  once the operations ran or failed, or the lock did not verify, with
  vendor/composer and vendor when they were made for it (a lock that
  fails to verify leaves no vendor dir). laravel's store import now runs
  from ~25 to ~150 ms, mostly during the revalidation.
- **The notification's connection opens during the verification**
  (4272f6b), when the operations are known, instead of at the first
  install: the POST no longer waits for TCP and TLS handshakes (~25 ms).
- **Raw descriptors for clones** (719316a): each FICLONE import went
  through os.File, which registers with the poller on open and switches
  back to blocking mode on every Fd() call: strace counted 113,000 fcntl,
  28,000 failing epoll_ctl and 17,000 fstat calls for laravel's 8,800
  files. Now seven syscalls per file instead of some twenty-three; ~4 ms
  of the import (3%).
- **The preconnect dials while its transport is set up** (7258edc): the
  dial starts at ~11.4 ms instead of ~14.7 ms; the TLS handshake waits
  for the TCP one anyway.
- **The no-op dump takes installed.json's dev mode from the speculation**
  (4a4f0cf) when the file is still the one the speculation read (same
  file, size, mode and modification time): it decoded the 405 KB file
  again for one key (~2 to 3 ms on symfony).

Tried and not kept: handing the import's files out to its goroutines in
chunks of 8 or 32 consecutive files (fewer goroutines in one directory
at a time) gave 127 vs 130 ms on laravel, within the noise.

Not reachable, and what is left (#29):

- **No-op ≥20x is below the network floor.** 20x of Composer's no-op is
  64 ms (laravel) and 31 ms (symfony) in this profile; the three round
  trips alone take ~73 ms here, and Composer makes the same two
  conditional requests (#14). What remains around them, ~27 ms: before
  the dial ~11 ms (Go runtime and package init ~4 ms, of which an estimated 2 ms are
  some 190 package-level regex compiles across the packages, which could
  be made lazy in php.MustCompile; decoding the platform probe's cached
  161 KB of JSON ~2.6 ms, mostly its constants (79 KB) and functions
  (39 KB); the application and configuration ~3 ms); after the 304s
  ~15 ms (pool and solver ~2 ms, which need the filter list; the local
  repository's write ~2.6 ms and the dump's file contents ~4 to 8 ms,
  both computable during the wait from what the speculation already
  holds, since a no-op's 304s change nothing they depend on, but that
  means running the dump's whole file generation ahead and replaying its
  warnings, not done).
- **Warm ≥20x** would be 106 ms (laravel) and 82 ms (symfony). After this
  change a laravel run is ~25 ms of start-up and verification set-up,
  ~125 ms of store import (now from ~25 ms, overlapping the ~90 ms of
  revalidation), then the notification POST's ~60 ms (one round trip
  and ~35 ms of server time; the dump, ~40 ms on a fresh copy, runs
  meanwhile). The POST is sent only once the operations ran and waited
  for, as Composer does, so the floor is the import plus that POST: the
  import (btrfs file creation and FICLONE, ~70 µs of kernel CPU per file
  over 8 slots) is what is left to cut.

## symfony's update: the optimizer, re-reads and double decodes (#29, 2026-10-07)

"before" is caec675, "after" is b4140ab (the five commits listed below).
Same projects, flags and method as #28 below: `update --dry-run
--no-plugins --no-scripts -q`, COMPOSER_TEST_SUITE=1, each tool with its
own COMPOSER_HOME, COMPOSER_CACHE_DIR and MAESTRO_CACHE_DIR, binaries
interleaved run by run (order reversed every other round), 2 untimed
warm-ups, medians of wall time, CPU (user + system) and peak RSS from
wait4. "offline" is COMPOSER_DISABLE_NETWORK=1 (warm caches, no
requests). Nothing else heavy ran; the 1-minute load average was 0.4 to 1.0
at the start of each series (up to 2.5 at the end of the offline ones).
**The CPU was in the `performance` profile this time** (intel_pstate
energy preference "performance"; #28 measured in "power"): absolute
times are lower than #28's for both tools, so compare the ratios within
this section, not with #28's.

| Project | Cache | Composer | before | after | speed-up before → after | CPU before → after |
|---|---|---:|---:|---:|---:|---:|
| symfony | warm (12 rounds) | 6.18 s | 572 ms | 441 ms | 10.8x → 14.0x | 2150 → 1777 ms |
| symfony | offline (15) | | 508 ms | 365 ms | | 2188 → 1737 ms |
| laravel | warm (12) | 2.12 s | 284 ms | 255 ms | 7.5x → 8.3x | 834 → 652 ms |
| laravel | offline (15) | | 189 ms | 151 ms | | 819 → 615 ms |

Peak RSS: symfony 259 → 216 MB, laravel about the same (138 → 141 MB
warm, 153 → 134 MB offline). Installs are unchanged (no-op install
symfony 107.9 → 107.6 ms, laravel 108.5 → 108.9 ms; warm install within
2%). Interleaving two different maestro binaries on one
MAESTRO_CACHE_DIR makes no-op installs alternate between ~108 and
~215 ms: the platform cache is per binary, so each run of the other
binary misses it. The no-op numbers above come from one binary at a
time.

Where symfony's offline update spent its time, from timing marks (not
committed) after the loads, each filter, each optimizer step and the
solver, medians of 8-11 runs in this profile: before, the security
advisory filter took ~60 ms, the filter list filter ~12, the optimizer
~175 (prepare 16, package hashes 50, filing them 46, preferred packages
47, keeping them 10, applying 6) and the rules ~47; after, ~35, ~11, ~75
and ~30. The loads (~150-200 ms, CPU-bound) are about the same. Under
-vvv in the power-saver profile the optimizer alone had taken 0.15 to
0.47 s.

What changed (the -vvv pool statistics, the rule count, normal and -v
output and the locks of laravel and symfony, updated with and without a
lock, offline, are identical before and after; the e2e update,
require-remove, laravel, symfony, security, outdated, commands,
plugin-download-events, plugin-merge and plugin-flex scenarios pass):

- **Optimizer hashes by id** (2ac867c). symfony's pool gave 287,726
  group hashes (42 MB of strings, 3,711 distinct) filed in three levels
  of string maps. Each distinct hash string now gets an id once, built
  in a reused buffer, and packages are filed by name index and ids, in
  the same insertion order. The links the loaders share between versions
  are extracted once in prepare.
- **Small chunks** (2ac867c). The optimizer's package hashes, preferred
  packages and the security filter's matching gave each goroutine one
  fixed range of the pool; a pool keeps a name's versions together and
  their costs differ widely, so most goroutines waited for one. They now
  take 64 items at a time.
- **WhatProvides** (2ac867c) matches a name's own candidates with the
  constraint's compiled checker, looked up once, instead of building
  CompilingMatcher::match's cache key per candidate (rules ~35 → ~26 ms).
- **Version comparisons** (6092520): a compiled constraint canonicalizes
  its version once; plain versions ("6.4.12.0") are copied rather than
  canonicalized byte by byte (version_compare of two plain versions
  takes ~85 ns, of two with suffixes ~210 ns as before).
- **Cache re-reads** (27e2b0d): each metadata file was read up to six
  times per update (look-ahead, loads, advisories and filter lists of
  the pool and of the audit), 24 MB each time for symfony. A read of a
  file whose identity (device, inode, size, mtime, ctime) is unchanged
  returns the contents read before (Linux): ~30 ms and 12% CPU less
  offline, and less garbage.
- **No double decodes** (93d9e32): ~50 of symfony's ~230 loads decoded a
  cached file the speculation was decoding too (the first wave's large
  root requirements among them). A load now waits for the speculation's
  handover of a file it is to offer: ~17 ms less offline.
- **Advisory lookups** (b4140ab): PackageVersionsConstraintMap, built
  three times per update over the whole pool, uses one presized set; the
  advisories of each file are created and matched in parallel, taken in
  order.

Online, the loads end ~90 ms later than offline: the root file's
conditional request waits for the TLS connection (connect + TLS to
repo.packagist.org take ~50 ms here, the response ~75 ms), and the first
wave waits ~35 ms more for its prefetched responses, so CPU savings
before ~130 ms do not show in warm runs.

Not done (#29):

- symfony under the power-saver profile was not measured again (the
  machine had been switched to `performance`); before, it was 6.9x and
  7.7x there. The CPU cuts above (17-21%) and the warm wall time (-23%)
  suggest ≥8x, not shown.
- The loads stay CPU-bound offline: decoding the decoded-cache slots
  (~200 ms CPU per run), the speculation's prebuild (~190 ms), the
  collector (~18% of CPU). A load waits for all of a wave's responses
  (HttpDownloader.Wait) before building any of its files; building
  each file as its response arrives would overlap the first wave's
  ~30 ms of building with the round trips online, but changes how the
  downloader is driven.
- The optimizer's preferred packages (~30 ms wall, ~250 ms CPU, the
  policy's sorting and comparisons) and keepPackageInGroup (~10 ms).
- No-op install: not looked at.

## main vs Composer on a quiet machine; GOGC decided (#28, 2026-10-07)

main at bca07d6 against composer.phar 2.10.3, the laravel and symfony
projects of the sections below (private-app was not available), on
btrfs, real Packagist and GitHub network. Every command with
`--no-plugins --no-scripts -q`, COMPOSER_TEST_SUITE=1, each tool its own
COMPOSER_HOME, COMPOSER_CACHE_DIR and MAESTRO_CACHE_DIR. The two tools
ran interleaved run by run (the order reversed every other round), 2
untimed warm-ups for the warm rows, medians; wall, CPU (user + system)
and peak RSS from wait4. *cold install*: caches, store and vendor/
removed before each run (5 rounds); *warm install*: a fresh copy of the
project without vendor/, warm caches and store (15); *no-op install*:
vendor/ in place (20); *update --dry-run* warm (15, two series) and cold
(caches removed, 5-6); *dump-autoload -o* in the installed project (20).

No other agent ran. The 1-minute load average was 0.6 to 1.2 at the
start of each series (its own runs take it to 1-2; the end of the cold
symfony series saw 3.3 from the desktop's own builds). The CPU ran in
the power-saver profile (intel_pstate powersave, energy preference
"power"), as the machine was set: both tools' CPU times are higher than
in the sections below (symfony's offline update takes ~4 s of CPU here,
~1.9 s in #26), so compare the ratios, not the absolute times.

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

The #12 targets:

- **update ≥8x: met on laravel** (10.0x and 8.8x in two series), **not on
  symfony** (6.9x and 7.7x). symfony's run is CPU-bound and spread wide
  (0.6 to 1.3 s for the same command, 3.7 to 4.2 s of CPU over 12
  cores): offline it takes as long as online (~1 s), so what is left is
  decoding, building and resolving, not round trips.
- **no-op install ≥20x: not met** (laravel 16x, symfony 10x). Both take
  ~110 ms, of which the conditional requests revalidating packages.json
  and the filter list (Composer's cache semantics, #14) and process
  startup are most; Composer's symfony no-op takes 1.1 s, so 20x would be
  55 ms.
- **warm install ≥20x: not met** (9-10x). The laravel runs fall in two
  groups, ~312 and ~362 ms, whatever the GC setting: the 50 ms between
  them is a network round trip in the revalidation, not maestro's work.
  The rest is the store import on btrfs and the dump.
- **cold install ≥4x: not met** (2.8-2.9x), bound by the dist downloads
  (GitHub codeload). A cold update reaches 6.3-6.4x.
- dump-autoload -o, not a #12 target of its own, is now 33-35x.

**GOGC: Go's default kept.** Same method, maestro against itself with
GOGC unset, GOGC=200 (96dab1b's default), GOGC=400 with
GOMEMLIMIT=320MiB, and GOGC=off with GOMEMLIMIT=512MiB (wall median,
CPU median, peak RSS median):

| Project | Command | default | GOGC=200 | 400 + 320MiB | off + 512MiB |
|---|---|---:|---:|---:|---:|
| laravel | update, warm (20) | 349 ms, 986 ms, 140 MB | 358 ms, 865 ms, 188 MB | 377 ms, 820 ms, 234 MB | 457 ms, 881 ms, 383 MB |
| laravel | update, offline (20) | 269 ms, 1020 ms, 155 MB | 303 ms, 975 ms, 203 MB | | |
| symfony | update, warm (12) | 907 ms, 3798 ms, 259 MB | 825 ms, 2963 ms, 323 MB | 1049 ms, 3466 ms, 299 MB | 974 ms, 2875 ms, 501 MB |
| symfony | update, warm (20) | 957 ms, 3922 ms, 259 MB | 1020 ms, 3569 ms, 342 MB | | |
| symfony | update, offline (20) | 987 ms, 4172 ms, 272 MB | 927 ms, 3469 ms, 353 MB | | |
| laravel | warm install (15) | 362 ms, 1058 ms, 64 MB | 363 ms, 1017 ms, 69 MB | 316 ms, 961 ms, 78 MB | 358 ms, 951 ms, 115 MB |
| symfony | warm install (15) | 376 ms, 1040 ms, 48 MB | 384 ms, 1006 ms, 50 MB | 367 ms, 931 ms, 57 MB | 354 ms, 936 ms, 92 MB |
| laravel | no-op install (20) | 111 ms, 133 ms, 48 MB | 112 ms, 113 ms, 51 MB | 112 ms, 103 ms, 55 MB | 112 ms, 97 ms, 70 MB |
| symfony | no-op install (20) | 108 ms, 97 ms, 37 MB | 107 ms, 79 ms, 41 MB | 107 ms, 73 ms, 47 MB | 111 ms, 66 ms, 56 MB |

GOGC=200 cuts CPU by 3 to 22% and raises peak memory by 4 to 34%, but
the wall time moves both ways by about as much as the series' own spread:
symfony's update 9% and 6% faster in two series, 6.5% slower in a third;
laravel's update 3% and 13% slower; installs within ±2%. The 316 ms of
"400 + 320MiB" on laravel's warm install is the network grouping above
(its fastest run, 310 ms, is the default's 311 ms). GOGC=off with a limit
is slower on laravel's update (the larger heap's page faults) and
doubles peak memory. None improves wall time beyond the noise, so main
keeps Go's default; an explicit GOGC or GOMEMLIMIT works as for any Go
program.

Not done: the CPU items of #28 (the 39 loads that decode files the
look-ahead has not handed over, slots for fresh 200 responses, the first
wave's wait, the pool optimizer, a scan of `require` names). The GC
series show 12% less CPU giving no measurable wall time on laravel,
whose update meets its target; symfony's spread is wider than any of
those items' estimated gains (~50 ms CPU each), so none could be shown
to help here. A slot for a fresh 200 response must hold the array that
decoding the re-encoded file (with `last-modified` added) gives, and be
written before the load expands it in place, on the load's path; it
saves one decode in the next run only.

## update --dry-run: versions expanded once, GOGC=200 (#26, 2026-10-07)

Same machine, projects and command as #22 below (`update --dry-run
--no-plugins --no-scripts -q`, COMPOSER_TEST_SUITE=1, binaries
interleaved round by round, medians). "before" is 68c361d, "after" is
96dab1b; "-gc" is a3c7a1c (without the GOGC change). Other agents' work
kept the load average at 4 to 14; it is given per row. Not a quiet
machine, so the wall times are the quietest available, not quiet ones.

The GOGC=200 default was reverted when merging (see the follow-up issue):
main ships the "-gc" column's behaviour.

| Project | Cache | Composer | before | after (-gc) | after | CPU before → -gc → after |
|---|---|---:|---:|---:|---:|---:|
| laravel | offline (20 rounds, load 4-9) | | 234 ms | 214 ms | 198 ms | 980 → 832 → 712 ms |
| laravel | warm (12 rounds, load 5-13) | 3.21 s | 584 ms | | 398 ms | 1034 → 742 ms |
| laravel | warm (12 rounds, load 7-11) | 2.45 s | 345 ms | | 308 ms | 994 → 706 ms |
| symfony | offline (10 rounds, load 12-14) | | 704 ms | | 588 ms | 2579 → 1934 ms |
| symfony | warm (8 rounds, load 12) | | 661 ms | | 624 ms | 2499 → 1891 ms |

The laravel target (≥8x Composer, warm): 8.1x and 7.9x by median in the
two rounds (Composer itself moved from 3.2 to 2.45 s between them), 7.3x
to 7.7x by the fastest runs; against #22's Composer median (2.8-2.9 s)
308 ms is 9.2x. Still not a quiet-machine number.

What changed (normal and -v output identical, offline and online, on
both projects; the e2e update, require-remove, laravel, symfony,
security, outdated, commands, plugin-download-events, plugin-merge and
plugin-flex scenarios pass):

- **Versions expanded once.** The speculation's version scan now tells
  whether what it read of a minified list is exactly what the load reads
  (every version has a usable version_normalized, branch aliases read
  without error); those versions go to the load with the packages built
  ahead, and the load decides from them which versions it builds,
  expanding the list only up to the last one not built ahead (in this
  run, 131 of 170 loads took this path; the other 39 decoded files the
  speculation had not handed over yet). The prebuild expands only up to
  the last version accepted and loads each version in place (the
  notification-url set only for the time of the load) instead of copying
  it, and copies of arrays without holes copy the index instead of
  building it. `expandEach` and its callbacks went from ~205 to ~125 ms
  CPU per run, the prebuild from ~170 to ~70 ms.
- **GOGC=200 unless GOGC is set.** A run builds most of what it keeps
  early, so the default collector ran 18 times on laravel. Peak RSS:
  laravel ~160 → ~185 MB, symfony ~280 → ~320 MB (GOGC=400 saved
  another ~85 ms CPU but grew symfony's peak to ~510 MB).
- **Cache housekeeping.** `clear-cache` removes maestro's decoded p2
  directory when it clears cache-repo-dir, and `--gc` removes slots not
  written for cache-ttl, silently. The slots stay twice the JSON's size:
  dropping the JSON copy means hashing on every read, which costs more
  than the copy saves (#22).

Not done: the 39 loads above still decode (from the decoded cache)
files the speculation has not handed over (~50 ms CPU); fresh 200
responses are decoded without storing a slot; the pool builder's first
wave still waits for laravel/framework's file; the pool optimizer
(~35 ms wall); a fast scan of `require` names before a full decode.

## After a class map record hit (issue #27, 2026-10-07)

Machine, projects and method as in the #21 section below (btrfs,
laravel and symfony, `--no-plugins --no-scripts -q`, warm caches, store
and class map records). hyperfine, 30 runs (no-op install 25) after 2
warm-ups, the two maestro binaries and Composer in one run; other
agents' builds and test suites shared the machine (load 3 to 11).
"before" is 0ce4604, "after" this change.

| Project | Command | Composer | maestro before | | maestro after | |
|---|---|---:|---:|---:|---:|---:|
| laravel | dump-autoload -o | 1.02 s | 68.4 ms | 14.9x | 63.5 ms | 16.0x |
| symfony | dump-autoload -o | 1.43 s | 74.2 ms | 19.3x | 69.4 ms | 20.6x |
| laravel | no-op install | 1.56 s | 150 ms | 10.4x | 142 ms | 11.0x |
| symfony | no-op install | 0.88 s | 136 ms | 6.5x | 134 ms | 6.6x |

What changed:

- **autoload_classmap.php and autoload_static.php's class map are built
  in parallel chunks** of 512 classes (each chunk's paths, its lines of
  autoload_classmap.php and its lines of the static class map), then
  joined. The static class map's var_export, strtr and re-indent all
  work line by line and each entry is one line, so the chunks joined are
  the whole's bytes; when a replaced dir holds a newline (a strtr match
  could span entries) it is exported whole as before
  (`TestClassmapChunks`). The entries are written without building a
  PHP array first. Timing marks on laravel: building both took about
  10 ms on the critical path (5 ms in `classmap`, then 5 ms waiting for
  the background static export), now about 3 to 4.5 ms; garbage
  collection during the chunks takes 1 to 2 ms of that.
- **git's version is kept across runs** (`vcs.UseVersionCache`, in
  maestro's cache dir under `git-version`): one of the four git
  processes a dump-autoload starts for the root version guess, about
  2 ms ahead of the others. The entry is keyed on the git binary the
  process executor would start (path found, file it resolves to, its
  device, inode, mode, size, modification and change times), used for a
  day at most, and only when that file is named git and is not a script;
  Linux only (macOS's /usr/bin/git and Windows' shims pick the git they
  run). Only a real `util.ProcessExecutor` uses it, never a test's mock,
  and not at -vvv, where the log shows `git --version` run.
- **Store-imported files no longer make records miss when another
  project links them.** A hard-link import changes the shared inode's
  change time, so every other project's record missed once (rescan
  ~130 ms on tmpfs). A file the scan took for a release file by its
  stamp (size and the hash-derived modification time, what the scan
  itself trusts for its content) is now recorded without its change
  time; its device, inode, size and stamp must still match. Two laravel
  projects on tmpfs with `MAESTRO_PACKAGE_IMPORT_METHOD=hardlink`: after
  reinstalling the second, the first's dump-autoload -o missed (132 ms)
  before and hits (41 ms) after, with the same files as a fresh scan.

Checks: dumps from a record are byte for byte (files and output) a fresh
scan's by the "before" binary with the records removed, on laravel and
symfony (`dump-autoload -o`, plain, `-a --no-dev`, `install`) and on the
autoload e2e fixture (all six dump variants of #21, warnings included).

Looked at and not changed:

- Glob classmap paths are still never recorded. Recording them exactly
  needs the identity of every directory the glob expansion read or
  checked a name in, including the targets of symlinked components,
  whose changes do not show in the parent's times; a slip there gives a
  wrong class map, while not recording only costs the scan. The only
  exclusion matcher of the dump's scans without a pattern text is the
  one standing for an exclude-from-classmap pattern that fails to
  compile, which is left to the scan and its error.
- Preparing the files during the no-op install's network wait: after
  the chunking the files take 3 to 4 ms, and the dump runs after the
  wait.

## Git mirrors read in Go (issue #23, 2026-10-06)

Machine as below, git 2.55.0, the project of #18 (psr/log, psr/container
and symfony/console v7.3.4, 10 packages), warm mirror cache, files cache
and package store. `install --prefer-source --no-plugins --no-scripts
-q` with vendor/ removed before each run, hyperfine, 15 runs after 2
warm-ups, two rounds; other agents' work ran meanwhile (load 7 to 13).
"before" is a314d83.

| Command | maestro before (median) | maestro after (median) |
|---|---:|---:|
| `install --prefer-source` | 311 / 359 ms | 285 / 284 ms |

The "Syncing … into cache" step ran four git commands on the mirror per
package (`rev-parse --git-dir`, `rev-parse --verify <sha>^{commit}`,
`branch`, `tag`): 54 git processes in this install before, 19 after (36
of those 40 gone, the first mirror still asking git once, and one `git
config --list` added). The answers now come from
HEAD, packed-refs, the loose refs, the pack indexes and the loose
objects, only where they are certainly git's; anything else runs git as
before, and each kind of answer is compared with git's on the first
mirror of a run.

Also measured: the dist prefetch of #20 (ddcc28a against its parent
0121f74), cold `install --no-plugins --no-scripts -vvv --profile` of
the laravel and symfony projects of #20, caches and vendor/ removed
before every run, binaries interleaved, 12 runs each. The machine was
never quiet: load 5 to 30 (other agents' test suites), so these are the
quietest numbers available, not quiet ones.

| Project | 0121f74 wall (median) | ddcc28a wall (median) | 0121f74 first dist 200 | ddcc28a first dist 200 |
|---|---:|---:|---:|---:|
| laravel | 3.41 s | 3.20 s | 0.41 s | 0.37 s |
| symfony | 4.01 s | 3.71 s | 0.39 s | 0.36 s |

The first dist response again comes some 25 to 40 ms earlier. The wall
times moved 6 to 7 % in the prefetch's favour, but single runs ranged
from 2.5 to 10 s, so the difference is within the noise, as in #20.

## Dist downloads overlap lock verification (issue #20, 2026-10-06)

Machine as below, real Packagist and GitHub network. Projects: laravel
(laravel/laravel v13.10.1, 109 packages) and symfony (symfony/skeleton
7.4 plus symfony/webapp-pack), both locked. Cold `install --no-plugins
--no-scripts -q`: vendor/, COMPOSER_CACHE_DIR and MAESTRO_CACHE_DIR
removed before every run, the two binaries interleaved run by run, 14
runs each. Other agents' test suites ran meanwhile (load 14 to 53), so
the wall times are mostly network and load noise. "before" is 0121f74.

| Project | Composer (1 run) | maestro before (median) | maestro after (median) |
|---|---:|---:|---:|
| laravel | 6.60 s | 2.93 s | 3.06 s |
| symfony | 6.19 s | 2.66 s | 2.55 s |

The overlap is bounded by what precedes the first download in an install
from a lock: the lock verification, which fetches packages.json and the
filter list summary (some 90 to 120 ms here). With `-vvv --profile` the
first dist response arrives at 0.37 to 0.44 s before and 0.32 to 0.37 s
after (3 runs per project and binary), so the head start is there; the
total stays within the noise of a 2.5 s network-bound install.

What changed: while the lock is verified, the installer computes the
transaction it will run and starts, without output, the dist transfer of
every package it will install or update from a dist URL the files cache
does not hold (`DownloadManager.Prefetch`). The body goes to an unlinked
spool file; only the installer's own request for the same exchange (URL,
headers, options, field for field) takes it and copies it into its
temporary file, so it still prints "Downloading", fills the files cache
and fires its events as before. Skipped when a PRE_FILE_DOWNLOAD
listener might change the request, credentials are stored for the host,
or on Windows. An update has no such window (the downloads start within
10 ms of the solver result), so it is unchanged.

## update --dry-run: p2 files kept decoded between runs (#22, 2026-10-06)

Same machine and projects as #17 below (`update --dry-run --no-plugins
--no-scripts -q`, COMPOSER_TEST_SUITE=1, the binaries run interleaved
round by round, medians). Other agents' test suites kept the load at
5-35 for the whole session, so the wall times are noisy; the CPU times
(user + system) are steadier. "before" is f39b3ea, "after" this change.
*offline*: warm caches with COMPOSER_DISABLE_NETWORK=1.

| Project | Cache | Composer | maestro before | maestro after | CPU before → after |
|---|---|---:|---:|---:|---:|
| laravel | offline (20 rounds, load 5-8) | | 276 ms | 253 ms | 1167 → 989 ms |
| laravel | offline (20 rounds, load 15) | | 381 ms | 319 ms | 1169 → 968 ms |
| laravel | warm (15 rounds, load 22-29) | 2.79 s | 448-622 ms | 428-600 ms | 1221-1255 → 1026-1062 ms |
| laravel | cold (10 rounds, load 15) | | 609 ms | 606 ms | 1443 → 1433 ms |
| symfony | offline (12 rounds, load 16) | | 846 ms | 848 ms | 3753 → 3597 ms |
| symfony | warm (10 rounds, load 22) | 7.70 s | 731 ms | 712 ms | 2897 → 2556 ms |
| symfony | cold (6 rounds, load 15) | | 932 ms | 968 ms | 3371 → 3483 ms |

What changed (deliberate deviation 3; normal output identical, checked
offline and online on both projects, and the e2e update, require-remove,
laravel, symfony, security, outdated, plugin-download-events,
plugin-merge and plugin-flex scenarios pass):

- **Decoded p2 files are cached.** A profile of the offline laravel run
  put `json_decode` of p2 files at ~330 ms of ~1.18 s CPU. The cached
  metadata files are now also kept decoded, in maestro's cache directory
  (`p2/v1`, one slot per repository and file name), in a binary form
  (`php.AppendBinary`): each string is stored and allocated once, arrays
  are built at their final size and indexed once. Reading laravel/
  framework's file back takes 3.1 ms against 7.5 ms for `json_decode`
  (5x fewer allocations). A slot holds a copy of the JSON it was decoded
  from and is read back only for that exact JSON: comparing the copy costs
  ~0.1 ms per MB, where SHA-256 cost ~70 ms per run on this CPU (no SHA
  extensions). The result is the array decoding gives, internal state
  included (`TestDecodedCacheEqualsDecoding` over the p2 fixtures and all
  cached Packagist files). A miss decodes the JSON and stores the slot
  after the speculation sent the next level's requests; storing it
  before them made the cold laravel run ~40 ms slower. The slots take
  about twice the JSON's size (17 MB for laravel).
- **The IO authentication store has a lock.** It was only safe while
  every write happened under the downloader's lock; the prefetches read
  it from other goroutines while a plugin or a credentials prompt may
  write it on the main one. `BaseIO` now guards it with a read-write lock.

Not done: the profile's other large items. Expanding minified versions
runs three times per file (the speculation's version scan, its prebuild,
the load) for ~190 ms CPU; the loads still decode (now from the cache)
files the speculation has not handed over yet (~50 ms); GC is ~20%. The
pool builder's first wave waits ~45 ms for laravel/framework's file.

## Class map record (issue #21, 2026-10-07)

Machine, projects and method as in the #14 section below (btrfs, laravel
and symfony with `--no-plugins --no-scripts -q`, warm caches and store,
separate COMPOSER_HOME, COMPOSER_CACHE_DIR and MAESTRO_CACHE_DIR per
tool). hyperfine, 30 runs after 2 warm-ups, the two maestro binaries one
after the other; *warm install* removes vendor/ before each run (not
timed), 25 runs. "before" is db1459b, "after" this change. Other agents'
builds shared the machine (load 4 to 12). Composer: median of 20 to 25
runs.

| Project | Command | Composer | maestro before | | maestro after | |
|---|---|---:|---:|---:|---:|---:|
| laravel | dump-autoload -o | 1.43 s | 98 ms | 14.6x | 72 ms | 19.9x |
| symfony | dump-autoload -o | 1.27 s | 100 ms | 12.7x | 75 ms | 16.9x |
| laravel | no-op install | 1.43 s | 150 ms | 9.5x | 145 ms | 9.9x |
| symfony | no-op install | 0.78 s | 139 ms | 5.6x | 137 ms | 5.7x |
| laravel | warm install | | 333 ms | | 332 ms | |
| symfony | warm install | | 343 ms | | 343 ms | |

CPU time (user + system, medians): dump-autoload -o 236 → 123 ms
(laravel) and 259 → 135 ms (symfony); no-op install 280 → 168 ms and
184 → 134 ms. The no-op install's scan was already hidden behind the
network wait, so it gains CPU rather than wall time.

What changed:

- **The class map is kept per project** (`classmap.Record`, in
  maestro's cache dir under `classmap/records`, at most 64 records).
  It is keyed by the scans (paths, exclusion patterns, autoload types,
  namespaces), the parser, the binary, the working directory and the
  project and vendor realpaths, and holds, besides the class map with
  its ambiguous classes and PSR violations, the identity (device,
  inode, size, modification and change times) of every directory the
  Finder listed, of the scan roots' ancestors below the project and
  vendor dirs, and of every file with a scanned extension. A dump with
  the same scans stats those (some 10,000 on laravel, in parallel, about
  2 ms) instead of scanning, and takes the class map when none changed.
  Identities not safely older than the record (3 s, git's racily clean
  entries) are not trusted, so a record is neither written nor used
  right after an install changed the files: the next dump writes it. The
  warnings are printed from the record at the same point as from a scan
  (checked against the scans on the autoload e2e fixture, all six dump
  variants, and on laravel and symfony). Store releases are only looked
  up when a scan runs. On laravel the dump's scan went from about 13 ms
  of wall time (95 ms of CPU) to about 5 ms.
- **autoload_static.php's class map is exported in the background**
  (var_export, the strtr of the absolute dirs, the re-indent) while
  autoload_classmap.php and the files before it are built and written:
  about 2 ms.
- `Generator.Warm` (-vvv only) now parses the root package's
  autoload-dev rules too; it read them from a dev mode not set yet.

Looked at and not changed:

- The vendor dir's realpath on every install step (item 4 of #21) was
  already resolved once per install by 17d6eed; a warm install's
  install steps now spend their time in the store import.
- `git --version` caching (item 5): one exec of the four git commands a
  dump-autoload runs for root version guessing; not done.
- After a record hit, a laravel dump-autoload -o spends about 25 ms in
  the process before the dump (startup, the php probe, version
  guessing), 5 ms reading and checking the record, and about 20 ms
  after it, mostly building autoload_classmap.php and
  autoload_static.php (1 MB each) and comparing them with the files on
  disk.

## Store-backed git sources (issue #18, 2026-10-06)

Machine as below, btrfs, git 2.55.0, the user's git configuration (index
v4 with index.skipHash). Project of #18: psr/log 1.1.4, psr/container
1.1.2 and symfony/console v7.3.4 (10 packages), locked, warm mirror cache
and files cache. `install --no-plugins --no-scripts -q` with vendor/
removed before each run, hyperfine, median of 7 after 2 warm-ups. Other
agents' builds ran meanwhile (load 5 to 6): two rounds each, "before" is
06657e5.

| Command | maestro before | maestro after |
|---|---:|---:|
| `install --prefer-source` | 1025 / 913 ms | 311 / 440 ms |
| `install --prefer-dist` | 254 / 265 ms | 238 / 249 ms |
| `install --prefer-source`, empty store (every checkout a miss) | 895 ms | 1244 ms |

A checkout cloned from the mirror cache is now stored in the package
store, `.git` included, under a key over the mirror's refs, the package's
URLs, reference and version, the push-URL settings, git's version and
binary, its configuration and environment. The next install of the same
key imports it unshared (clone or copy, never hardlink) and rewrites only
what differs between two clones: the reflog times, and the stat data in
`.git/index` (in Go for index v2 to v4 with SHA-1, else `git update-index
--refresh`). Until #24 the empty untracked cache that feature.manyFiles
(core.untrackedCache) adds, whose ident names the work tree, sent every
hit to `git update-index`: 20 git processes for this project's install,
10 after (none per package; the rest is root-package and lock checks). No git process runs on a hit, except `git config --list`
once per process. A miss pays for storing the checkout once (some 35 ms a
package here, mostly hashing and writing the packfile). The trees are
identical to git's but for those two files (diff -r, find -printf %M),
and `git diff-files` and `git status` are clean in every checkout.

Download overlap (Work 2 of #18) followed in #20; see its section above.

## No-op install (issue #14, 2026-10-06)

Machine and projects as in the next section (btrfs, real Packagist,
load 5 to 6 from other agents' work). Each command is
`install --no-plugins --no-scripts` in a project whose vendor/ is
already installed, with warm caches. Each maestro binary had 30 hyperfine
runs after 3 warm-ups, the two binaries one after the other; Composer
had 20 runs. "before" is 06657e5, "after" is this change.

| Project | Composer (mean) | maestro before (median) | maestro after (median) | | after (min) |
|---|---:|---:|---:|---:|---:|
| laravel | 1.56 s | 212 ms | 164 ms | 9.5x | 146 ms |
| symfony | 0.75 s | 153 ms | 141 ms | 5.3x | 135 ms |

Where the time goes (timing marks, laravel): the process reaches
`Installer.Run` at about 24 ms. In `verifyLock`, everything before
`CreatePool` takes about 1.5 ms (`createPlatformRepo`, `createPolicy`,
`createRepositorySet`, `createRequest`, `IsFresh`,
`MissingRequirementInfo`, `createFilterListPoolFilter`). `CreatePool`
then waits about 60 ms for the filter list, and `Solve` takes about
1.5 ms. The filter list request is already sent when `Run` starts
(`prefetchFilterSummaries`), on the connection the factory opened ahead.
With the cached packages.json older than 600 s, its revalidation goes
on that same HTTP/2 connection. So the wait is the network itself:
TCP and TLS handshakes plus one request, about three round trips of
24 ms from a connection started at about 15 ms. Starting the request any
earlier gains nothing.

What changed:

- **The dump's class map scan runs during that wait.** When the lock file
  asks for no package operation, and no script or plugin listens to
  pre-pool-create, pre-operations-exec or pre-autoload-dump, the
  installer starts the class map scan in the background before
  `verifyLock` (`autoload.Generator.Speculate`). The dump takes that
  result only if its autoload rules, paths, parser and PSR flag are the
  same as the ones scanned. The ambiguous-class and PSR warnings are
  still printed from it at the same point, after "Generating autoload
  files". The scan is dropped if operations run or the lock check fails.
  On laravel (optimized autoloader) this takes 25 to 30 ms off the run.
  Symfony's dump scans only its few classmap rules, so it saves less.
- **Startup.** The two e-mail validation regexps (jsonschema and the
  package loader's filter_var port) were compiled at package init on
  every run, 4.5 ms before `main`. They are now compiled on first use.

Looked at and not changed:

- The "~20 ms gap on symfony" between "Nothing to install" and
  "Generating autoload files" did not show up: 5 to 6 ms on both
  projects (installed.json/installed.php write 4 to 5 ms, abandoned check
  0.5 ms).
- GC tuning: GOGC=400 or GOGC=off with GOMEMLIMIT cut CPU time by 25 to
  40%, but no-op wall time stayed the same or got worse (it waits on the
  network), and the warm install did not get faster within the noise.
  Not kept.
- `git --version` caching: root version guessing runs after the factory
  opens the connection, so it overlaps the network wait and saving that
  one exec does not shorten a no-op.
- A persistent class map record (issue item 2) was not built. The scan
  is now hidden behind the network wait on a no-op install, but it still
  costs CPU (about 95 ms over the worker threads on laravel) and still
  costs wall time in `dump-autoload`.

## update --dry-run: one Packagist connection, parallel cache reads (#17, 2026-10-06)

Same machine and projects as "update --dry-run: metadata loading" below
(`update --dry-run --no-plugins --no-scripts -q`, per tool its own
COMPOSER_HOME/COMPOSER_CACHE_DIR/MAESTRO_CACHE_DIR, COMPOSER_TEST_SUITE=1),
the tools run interleaved round by round, medians. Other agents' test
suites kept the load at 4-18, so the times are noisy. "before" is 14eb58f,
"after" this change. *offline*: warm caches with COMPOSER_DISABLE_NETWORK=1,
which shows the CPU floor.

| Project | Cache | Composer | maestro before | | maestro after | |
|---|---|---:|---:|---:|---:|---:|
| laravel | warm (25 rounds, load 3-6) | 2.89s | 376 ms | 7.7x | 379 ms | 7.6x |
| laravel | cold (12 rounds, load 6-13) | 2.99s | 805 ms | 3.7x | 769 ms | 3.9x |
| laravel | offline (15 rounds) | | 306 ms | | 282 ms | |
| symfony | warm (15 rounds, load 8-17) | 6.77s | 758 ms | 8.9x | 745 ms | 9.1x |
| symfony | cold (8 rounds, load 6-12) | | 1007 ms | | 1043 ms | |

CPU time (user + system, medians) goes down a little: 3066 → 2933 ms on
symfony warm, 1184 → 1172 ms on laravel warm (an earlier set of runs
measured 1261 → 1202 ms). The wall times change by less than the noise.

What changed (deliberate deviation 3; frozen output unchanged, -vvv prints
the same lines in the same order):

- **One connection per host.** A trace showed 8 TLS connections to
  repo.packagist.org per run: the speculation's prefetches start about
  20 ms in, before the connection opened ahead (Preconnect) finished,
  and net/http dials one connection per waiting request, up to
  MaxConnsPerHost (8). Each costs TCP and TLS handshakes, and the root
  file's conditional request could end up on one of them and wait for its
  handshake. Now the transfers to an https address wait while the first
  transfer to it is still connecting, as curl's CURLOPT_PIPEWAIT does, and
  then share its HTTP/2 connection, sending their requests in the order
  they were made, so packages.json goes out ahead of the prefetches
  (`internal/util/http/firstconn.go`). Over HTTP/1, or when the first
  transfer fails, they go ahead together as before.
- **Advisory and filter cache files read in parallel.** The advisory and
  filter list loads read ~136 cached p2 files one after the other on the
  main goroutine, then took their slim copies. The reads, slim copies and
  decoding now run on all cores (`Cache.ReadAll`), and the "Reading … from
  cache" lines are still printed in the order of the files, before any
  error. The advisory load's file reading went from 6-7 ms to ~2 ms on
  laravel; the filter list load reads the same files.

Decided against: opening the Packagist connection at process start.
6c60af1 already opens it when the project's Composer is created, which is
9-12 ms after start. The handshake then takes 50-65 ms (Go's TLS 1.3 here:
DNS ~1 ms from the cache, TCP ~24 ms, TLS 26-37 ms), so starting at 0 would
save at most ~10 ms. It would also need the TLS settings, proxy and
repositories before the configuration is read.

Why laravel is still under 8x (2.9 s / 8 = 360 ms): the run is CPU-bound,
not network-bound. Offline, with every request skipped, a warm run still
takes ~280 ms, and the pool is built only ~170 ms in (with -vvv). The CPU
profile of a warm online run (about 1.2 s of CPU over 0.38 s of wall time)
breaks down like this:

- the speculation's decoding of p2 JSON into php.Arrays, ~250 ms;
- building the packages ahead (prebuild, expanding minified versions),
  ~200 ms;
- GC mark work, ~260 ms;
- the loads decoding files the speculation had not offered yet,
  ~70 ms.

The speculation finds the deeper levels of the graph only after decoding
the large files. laravel/framework.json is 1 MB and phpunit/phpunit.json
0.7 MB. Prefetches for names such as phpunit/php-invoker therefore start
150-220 ms in and complete ~50 ms later, and the pool builder waits for
them. GOGC 200, 400 and off made no difference offline. Raising the
prefetch limit from 64 to 192 gave about 5-10 ms. The next step for laravel
is less CPU per p2 file. One option is a decoded or prebuilt form kept
across runs, keyed by the cached file's content, so that a warm run does
not decode the same megabytes again. Typed p2 decoding (the option set
aside in #12) is another.

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

## No-op install and fixed per-run costs (#12, 2026-10-06)

Same machine and projects as below (laravel, symfony: the e2e projects'
composer.json and composer.lock, `--no-plugins --no-scripts`, separate
COMPOSER_HOME/COMPOSER_CACHE_DIR/MAESTRO_CACHE_DIR per tool,
COMPOSER_TEST_SUITE=1). Other agents' builds and test suites shared the
machine (load 3–8), so runs were repeated until stable. *no-op install*:
`install` with vendor/ in place, `hyperfine -N`, 3 warm-ups, median of
30 runs; *warm install*: vendor/ removed before each run, mean of 10.
"before" is b01f0e2, "after" this change; both with warm caches, store and
(after) probe cache.

| Project | Command | Composer | maestro before | | maestro after | |
|---|---|---:|---:|---:|---:|---:|
| laravel | no-op install | 1.45s | 241ms | 6.0x | 213ms | 6.8x |
| laravel | warm install | 2.52s | 479ms | 5.3x | 457ms | 5.5x |
| symfony | no-op install | 717ms | 194ms | 3.7x | 157ms | 4.6x |
| symfony | warm install | 2.06s | 692ms | 3.0x | 588ms | 3.5x |

Processes a no-op install starts (outside a VCS checkout): before 11 (php
for the probe; git 4 times, hg, fossil twice and svn, each through
`/bin/sh -c exec`, ~4.5 ms per shell here; `lsmod`; `sh -c command -v
hhvm`), after 4 (git, run directly, three of them at once).

What changed (all deliberate deviation 3; frozen output is unchanged, the
e2e suites pass, -vvv logs the same commands in the same order):

- **php probe cached across runs** (internal/platform/probecache_linux.go):
  the probe's output is kept in maestro's cache dir, keyed by the php
  binary (found and resolved, its stat), probe.php and the environment
  (but PWD, OLDPWD, SHLVL, _ and MAESTRO_*), and used only while every
  file php mapped (/proc/self/maps: its executable, libraries and
  extensions), the ini files it read, the places it looks for more
  (PHPRC, the binaries' directories, PHP_CONFIG_FILE_PATH, the scan
  directories) and uname are unchanged, for at most 24 hours. Scripts
  (version managers' shims) and non-php* binaries are never cached; a
  wrapper (the executable php runs is another file) is cached per working
  directory. Linux only. ~20–30 ms per run.
- **Root version guessing**: VCS tools (git, hg, svn, fossil, p4) found in
  an absolute PATH directory start without `/bin/sh -c exec` (the shell
  still runs them otherwise, so a missing tool gives the shell's 127 and
  message). Once `git branch` fails, the fallbacks (git describe, git
  rev-list, hg, fossil, svn) start at once (ProcessExecutor.Prefetch) and
  are taken in Composer's order; hg, fossil and svn are not started at all
  when absent from PATH (below -vvv, where Composer's log shows them).
  ~35 ms → ~8 ms here.
- **Connections opened early**: install, update, require, remove,
  reinstall, outdated and audit open TLS connections to the first https
  composer repositories while the project loads, so the request
  revalidating Packagist's filter list (every install from a lock file)
  finds one open.
- `lsmod` is read from /proc/modules; ExecutableFinder's `command -v`
  fallback is skipped when the shell could only answer "not found" (no
  executable of that name in the working directory, no "~" in PATH);
  LibraryInstaller resolves the vendor dir again only when it is no longer
  the directory resolved before; Locker::isFresh does not decode the lock
  file LockData already decoded when its content is unchanged (~6 ms on
  laravel's lock).

Tried and dropped: parsing the autoload files while the lock file is
verified (Generator.Warm before doInstall) gave nothing measurable and,
running alongside installs, broke the store-heal scenario; raising GOGC
cut ~30% of the CPU time but no wall time.

Why the ≥20x no-op target is not met: what is left is not fixed cost. A
no-op install revalidates Packagist's filter list (and packages.json when
older than 600 s) with conditional requests, which Composer's cache
semantics require: one to two round trips after the connection
(~25 ms each here), now overlapped with loading. The autoload dump, which
Composer runs on every install, is the rest (laravel ~90 ms of ~200 ms,
symfony ~40 ms); caching it is a separate part of #12. Composer's no-op
symfony install takes 0.72 s, so 20x would be 36 ms.

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
