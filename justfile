# maestro dev tasks.
#
# vet, lint, deadcode, tidy-check, test, build, check, test-race, e2e and
# shell run in the dev container (compose.yaml), so they depend on nothing
# of this machine and leave nothing on it; extra arguments go to the
# command, e.g. `just test ./internal/config -run TestFactory`.

# The container runs as you, so what it writes to the checkout is yours.
export MAESTRO_UID := `id -u`
export MAESTRO_GID := `id -g`

compose := "docker compose run --rm --build --quiet-build"

# List all recipes with their descriptions.
default:
    @just --list

# Run every release gate in order: vet, lint, deadcode, tidy-check, test, build.
check:
    {{ compose }} check

# Static analysis of every package with go vet.
[positional-arguments]
vet *args:
    {{ compose }} lint go vet "${@:-./...}"

# Lint every package; settings live in .golangci.yml.
[positional-arguments]
lint *args:
    {{ compose }} lint golangci-lint run "$@"

# Fail on functions nothing reaches, not even a test (tools/deadcode).
deadcode:
    {{ compose }} lint tools/deadcode/check.sh

# Fail when `go mod tidy` would change go.mod or go.sum (tools/tidycheck).
tidy-check:
    {{ compose }} lint tools/tidycheck/check.sh

# Run the test suite for every package, the php-driven tests included.
[positional-arguments]
test *args:
    {{ compose }} test go test "${@:-./...}"

# Compile-check every package; the output is discarded.
build:
    {{ compose }} test go build -o /dev/null ./...

# Run the test suite with the race detector, including the php-driven tests.
[positional-arguments]
test-race *args:
    {{ compose }} race go test -race "${@:-./...}"

# Compare maestro with the real Composer 2.10.3 phar end to end (network, slow).
e2e:
    {{ compose }} e2e

# Open a shell in the dev container.
shell:
    {{ compose }} shell

# Format every Go source in place with gofmt.
fmt:
    gofmt -w .

# Build the current tree and run it in the directory you call just from,
# e.g. `just dev install -v` inside a PHP project. The binary is ./maestro
# (gitignored).
[no-cd]
[positional-arguments]
dev *args:
    @go build -C "{{ justfile_directory() }}" -o "{{ justfile_directory() }}/maestro" ./cmd/maestro
    @"{{ justfile_directory() }}/maestro" "$@"

# Recreate .ref/, the Composer sources the port follows (see docs/PORTING.md).
ref:
    ref-sync

# CI does this on every dependency change (see .github/workflows/flake.yml);
# run it locally when you want `nix build` to work before pushing.
# Recompute package.nix's vendorHash from go.mod/go.sum.
nix-vendor-hash:
    #!/usr/bin/env bash
    set -euo pipefail
    FAKE="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
    CUR="$(grep -oP 'vendorHash = "\K[^"]+' package.nix)"
    sed -i "s#vendorHash = \"${CUR}\"#vendorHash = \"${FAKE}\"#" package.nix
    GOT="$(nix build .#default --no-link 2>&1 | grep -oP 'got:\s+\K(sha256-\S+)' | head -1 || true)"
    [ -z "$GOT" ] && GOT="$CUR"
    sed -i "s#vendorHash = \"${FAKE}\"#vendorHash = \"${GOT}\"#" package.nix
    echo "vendorHash = ${GOT}"

# Show what the next patch, minor and major tags would be.
release-preview:
    #!/usr/bin/env bash
    set -euo pipefail
    v="$(git describe --tags --abbrev=0 --match 'v*' 2>/dev/null || echo v0.0.0)"
    IFS=. read -r maj min pat <<<"${v#v}"
    echo "current: $v"
    echo "patch:   v$maj.$min.$((pat + 1))"
    echo "minor:   v$maj.$((min + 1)).0"
    echo "major:   v$((maj + 1)).0.0"

release-patch: (release "patch")
release-minor: (release "minor")
release-major: (release "major")

# The version lives in the git tag: publish.yml stamps it into the binary.
# Nix can't see tags, so the release commit also records the version in
# release.json for flake.nix. Syncs the flake vendorHash, runs the gates,
# commits, tags, and pushes -- the tag push triggers
# .github/workflows/publish.yml.
# Tag a release and push it.
release level:
    #!/usr/bin/env bash
    set -euo pipefail
    if ! git diff --quiet || ! git diff --cached --quiet; then
        echo "working tree is dirty — commit or stash first" >&2
        exit 1
    fi
    git fetch --tags --quiet
    v="$(git describe --tags --abbrev=0 --match 'v*' 2>/dev/null || echo v0.0.0)"
    IFS=. read -r maj min pat <<<"${v#v}"
    case "{{ level }}" in
        patch) new="v$maj.$min.$((pat + 1))" ;;
        minor) new="v$maj.$((min + 1)).0" ;;
        major) new="v$((maj + 1)).0.0" ;;
        *) echo "unknown level: {{ level }}" >&2; exit 1 ;;
    esac
    if git rev-parse -q --verify "refs/tags/$new" >/dev/null; then
        echo "tag $new already exists" >&2
        exit 1
    fi
    echo "releasing $v -> $new"
    just nix-vendor-hash
    just check
    # nix-vendor-hash rewrites package.nix when dependencies moved; that has to
    # land before the tag so the tagged tree builds under Nix.
    if ! git diff --quiet package.nix; then
        git add package.nix
        git commit -m "chore(nix): update vendorHash for $new"
    fi
    # flake.nix reports the bare version only for the commit whose commit
    # time equals release.json's commitTime, so pin both dates to it. The
    # tag always sits on this commit, which also keeps it off the "[skip ci]"
    # vendorHash commit .github/workflows/flake.yml pushes (GitHub honours
    # [skip ci] on a tag push too, and the release would quietly never build).
    now="$(date +%s)"
    printf '{\n  "version": "%s",\n  "commitTime": %s\n}\n' "${new#v}" "$now" > release.json
    git add release.json
    GIT_AUTHOR_DATE="@$now +0000" GIT_COMMITTER_DATE="@$now +0000" \
        git commit -m "chore: release $new"
    # The flake workflow can push its own vendorHash commit to main while
    # this recipe runs; both commits carry the same content, so rebasing
    # onto theirs drops ours as empty and both sides converge instead of
    # a push being rejected. --committer-date-is-author-date keeps the
    # release commit's pinned commit time through the rebase. Re-tagging
    # every iteration keeps the tag on the head that actually lands.
    for _ in 1 2 3 4 5; do
        git fetch origin main --quiet
        git rebase --committer-date-is-author-date origin/main
        git tag -f --annotate -m "$new" "$new"
        if git push origin HEAD && git push origin "$new"; then
            echo "released $new"
            exit 0
        fi
    done
    echo "could not push $new after 5 attempts" >&2
    exit 1
