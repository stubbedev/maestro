#!/bin/sh
# Installs maestro from its GitHub releases:
#
#   curl -fsSL https://raw.githubusercontent.com/stubbedev/maestro/main/install.sh | sh
#
# The binary is chosen from the machine itself, not from the shell's or a
# package manager's idea of it: on Apple Silicon an x86_64 shell (Rosetta,
# an Intel Homebrew) still gets the arm64 build. It is verified against
# the release's checksums.txt, as self-update verifies its downloads.
#
# MAESTRO_VERSION    a release tag (v1.0.3); the latest release by default
# MAESTRO_INSTALL_DIR where to put maestro; ~/.local/bin by default

set -eu

repo="stubbedev/maestro"
dir="${MAESTRO_INSTALL_DIR:-$HOME/.local/bin}"

fail() {
	echo "maestro install: $*" >&2
	exit 1
}

case "$(uname -s)" in
Linux) os=linux ;;
Darwin) os=darwin ;;
FreeBSD) os=freebsd ;;
*) fail "no release build for $(uname -s); see https://github.com/$repo/releases" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
i386 | i686) arch=386 ;;
armv6l | armv7l) arch=arm ;;
*) fail "no release build for $(uname -m); see https://github.com/$repo/releases" ;;
esac
# an x86_64 process on Apple Silicon runs under Rosetta; the hardware is arm64
if [ "$os" = darwin ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
	arch=arm64
fi

asset="maestro_${os}_${arch}"
case "$os/$arch" in
darwin/amd64 | darwin/arm64 | freebsd/amd64 | freebsd/arm64 | linux/*) ;;
*) fail "no release build $asset; see https://github.com/$repo/releases" ;;
esac

if [ -n "${MAESTRO_VERSION:-}" ]; then
	base="https://github.com/$repo/releases/download/$MAESTRO_VERSION"
else
	base="https://github.com/$repo/releases/latest/download"
fi

if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$2" "$1"; }
else
	fail "curl or wget is needed"
fi

if command -v sha256sum >/dev/null 2>&1; then
	checksum() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum >/dev/null 2>&1; then
	checksum() { shasum -a 256 "$1" | cut -d' ' -f1; }
elif command -v sha256 >/dev/null 2>&1; then
	checksum() { sha256 -q "$1"; }
else
	fail "sha256sum, shasum or sha256 is needed to verify the download"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading $asset from $base"
fetch "$base/$asset" "$tmp/$asset" || fail "could not download $base/$asset"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || fail "could not download $base/checksums.txt"

want="$(awk -v a="$asset" '$2 == a { print $1 }' "$tmp/checksums.txt")"
[ -n "$want" ] || fail "checksums.txt lists no $asset"
[ "$(checksum "$tmp/$asset")" = "$want" ] || fail "$asset does not match its checksum in checksums.txt"

mkdir -p "$dir"
chmod 0755 "$tmp/$asset"
mv "$tmp/$asset" "$dir/maestro"
echo "Installed $("$dir/maestro" --version 2>&1 | tail -n 1) to $dir/maestro"

case ":$PATH:" in
*":$dir:"*) ;;
*) echo "Add $dir to your PATH to run maestro." ;;
esac
if ! command -v php >/dev/null 2>&1; then
	echo "maestro runs PHP code (platform detection, plugins, scripts) with the php first on PATH; none was found, so install PHP."
fi
echo "To use maestro as composer, symlink it: ln -s \"$dir/maestro\" \"$dir/composer\""
