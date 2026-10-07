{
  pkgs,
  lib,
  config,
  ...
}: {
  # Go toolchain. Pinned to the version go.mod declares: with GOTOOLCHAIN=local
  # a module asking for a newer Go fails instead of downloading one, and
  # gopls/golangci-lint are built with the same nixpkgs Go, so they parse
  # what the compiler accepts.
  languages.go = {
    enable = true;
    package = pkgs.go_1_27;
  };

  packages = with pkgs; [
    # Go development
    gopls # language server
    golangci-lint # linter behind `just lint`, config in .golangci.yml
    delve # debugger
    just # task runner

    # Oracle for the port: generates golden fixtures by running Composer's
    # own PHP from .ref/ (see docs/PORTING.md) and runs the e2e comparisons
    # against real Composer.
    php84
    php84Packages.composer # only to install .ref/composer's dependencies
    unzip # what Composer extracts with; the archive tests compare against it

    # Workflow
    git
    gh # GitHub CLI: releases, CI
  ];

  env = {
    # Keep accidental cgo out (the race detector needs CGO_ENABLED=1 per
    # command), and keep the compiler devenv provides.
    CGO_ENABLED = "0";
    GOTOOLCHAIN = "local";
  };

  # Recreates .ref/: the exact sources being ported, with their tests: the
  # Composer release internal/upstream names and the libraries at the
  # versions its composer.lock pins. A checkout at another tag is replaced.
  scripts.ref-sync.exec = ''
    set -euo pipefail
    cd "$DEVENV_ROOT"
    mkdir -p .ref
    clone() { # repo tag dir
      if [ -d ".ref/$3" ] && [ "$(git -C ".ref/$3" describe --tags --exact-match 2>/dev/null)" != "$2" ]; then
        rm -rf ".ref/$3"
      fi
      [ -d ".ref/$3" ] || git -c advice.detachedHead=false clone -q --depth 1 --branch "$2" "https://github.com/$1" ".ref/$3"
    }
    locked() { # package: its version in Composer's composer.lock
      php -r 'foreach (json_decode(file_get_contents(".ref/composer/composer.lock"), true)["packages"] as $p) { if ($p["name"] === $argv[1]) { echo $p["version"]; exit(0); } } exit(1);' "$1"
    }
    clone composer/composer "$(tools/upstream/composer-version.sh)" composer
    clone composer/semver "$(locked composer/semver)" semver
    clone composer/class-map-generator "$(locked composer/class-map-generator)" class-map-generator
    clone composer/spdx-licenses "$(locked composer/spdx-licenses)" spdx-licenses
    clone composer/metadata-minifier "$(locked composer/metadata-minifier)" metadata-minifier
    clone Seldaek/jsonlint "$(locked seld/jsonlint)" jsonlint
    [ -d .ref/composer/vendor ] || (cd .ref/composer && composer install --no-dev --no-scripts -q)
  '';

  enterTest = ''
    just check
  '';
}
