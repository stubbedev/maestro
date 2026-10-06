# maestro dev tasks.

# List all recipes with their descriptions.
default:
    @just --list

# Run every release gate in order: vet, lint, test, build.
check: vet lint test build

# Static analysis of every package with go vet.
vet:
    go vet ./...

# Lint every package; settings live in .golangci.yml.
lint:
    golangci-lint run

# Run the test suite for every package.
test:
    go test ./...

# Compile-check every package; the output is discarded.
build:
    go build -o /dev/null ./...

# Format every Go source in place with gofmt.
fmt:
    gofmt -w .

# Run the test suite with the race detector, including the php-driven tests.
test-race:
    CGO_ENABLED=1 MAESTRO_PHP_TESTS=1 go test -race ./...

# Compare maestro with the real Composer 2.10.3 phar end to end (network, slow).
e2e:
    MAESTRO_E2E=1 MAESTRO_PHP_TESTS=1 go test -count=1 -timeout 4h -run 'TestE2E|TestAllFunctional' ./cmd/maestro

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

# The version lives in the git tag: publish.yml stamps it into the binary, so
# there is no version file to bump. Syncs the flake vendorHash, runs the gates,
# tags, and pushes -- the tag push triggers .github/workflows/publish.yml.
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
    # GitHub honours [skip ci] on a tag push too, so a tag landing on the
    # vendorHash commit that .github/workflows/flake.yml pushes ("chore(nix):
    # update vendorHash [skip ci]") creates the tag and the release quietly
    # never builds. Put an empty commit under the tag in that case.
    # The flake workflow can push its own vendorHash commit to main while
    # this recipe runs; both commits carry the same content, so rebasing
    # onto theirs drops ours as empty and both sides converge instead of
    # a push being rejected. Re-checking the guard and re-tagging every
    # iteration keeps the tag on the head that actually lands.
    for _ in 1 2 3 4 5; do
        git fetch origin main --quiet
        git rebase origin/main
        if git log -1 --format=%B | grep -qiE '\[(skip ci|ci skip)\]'; then
            git commit --allow-empty -m "chore: release $new"
        fi
        git tag -f --annotate -m "$new" "$new"
        if git push origin HEAD && git push origin "$new"; then
            echo "released $new"
            exit 0
        fi
    done
    echo "could not push $new after 5 attempts" >&2
    exit 1
