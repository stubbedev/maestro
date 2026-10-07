#!/usr/bin/env bash
# Fails when `go mod tidy` would change go.mod or go.sum, printing the
# diff; it rewrites nothing. CI's lint job, `just tidy-check` and
# `just check` all run this script, so they cannot disagree. To fix a
# reported diff, run `go mod tidy` on main and commit the result
# (docs/PORTING.md, "Working alongside other ports").
#
# Usage (from the repo root; `just tidy-check` runs it in the dev container):
#   tools/tidycheck/check.sh
set -euo pipefail

go mod tidy -diff
