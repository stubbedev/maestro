#!/usr/bin/env bash
# Regenerates internal/platform/testdata/oracle/*.json with several php
# builds (see runtime.php). The builds come from the devenv shell's
# nixpkgs pin; run from the repository root, in the devenv shell.
set -euo pipefail

nixpkgs="https://github.com/cachix/devenv-nixpkgs/archive/c2f38fe7f9e04d9aadd354d380f2bd40531d9737.tar.gz"
build() { # attribute expression
  nix-build --no-out-link -E "let pkgs = import (builtins.fetchTarball \"$nixpkgs\") {}; in $1"
}

php82=$(build 'pkgs.php82')/bin/php
php83x=$(build 'pkgs.php83.withExtensions ({ enabled, all }: enabled ++ [ all.xdebug all.imagick all.mongodb all.yaml all.redis ])')/bin/php
php84min=$(build 'pkgs.php84.withExtensions ({ enabled, all }: [ all.mbstring all.ctype all.tokenizer all.filter all.openssl ])')/bin/php
php84=$(command -v php)

script=tools/oracle/platform/runtime.php
"$php84" "$script" php84
"$php82" "$script" php82
"$php83x" "$script" php83-xdebug
XDEBUG_MODE=off "$php83x" "$script" php83-xdebug-off
"$php84min" "$script" php84-minimal
PHPRC=tools/oracle/platform/ini "$php84" "$script" php84-custom-ini
