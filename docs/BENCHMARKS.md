# Benchmarks: maestro vs Composer 2.10.3

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
| kontainer | 29.00s | 24.18s | 1.2x | 12.84s | 5.29s | 2.4x |
| **total** | 288.64s | 134.00s | 2.2x | 143.73s | 66.40s | 2.2x |

Notes:

- Cold runs of real projects (laravel, symfony, kontainer) are bound by
  downloading the dists: both tools use at most 12 parallel HTTP transfers
  (COMPOSER_MAX_PARALLEL_HTTP), so the gain is mostly in resolution,
  extraction (native, into the store, while other downloads run) and
  autoload dumping.
- Warm runs show the store (deviation 1): packages are materialized from the
  extracted store (reflink or copy) instead of unzipping cached archives.
- Commands that mostly wait on Packagist metadata (`commands`, `verbosity`:
  show/outdated/audit/search) gain the least.
- The real-world scenarios run with `--no-plugins --no-scripts` until the
  plugin phases land (see e2e_realworld_test.go); kontainer installs from its
  lock without its private packages.
