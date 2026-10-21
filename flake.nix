{
  description = "maestro - fast Composer-compatible PHP package manager";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }:
    flake-utils.lib.eachDefaultSystem (
      system: let
        pkgs = nixpkgs.legacyPackages.${system};
        inherit (pkgs) lib;

        # The Go toolchain, pinned to the version go.mod declares: with
        # GOTOOLCHAIN=local a module asking for a newer Go fails instead of
        # downloading one. The package and every Go tool in the dev shell are
        # built with it, so gopls and golangci-lint parse what the compiler
        # accepts.
        go = pkgs.go_1_27;
        buildGoModule = pkgs.buildGoModule.override {inherit go;};
        # Rebuilds a Go tool with `go`, whichever buildGo*Module it takes.
        withGo = pkg: let
          builders = lib.filterAttrs (name: _: lib.match "buildGo.*Module" name != null) (lib.functionArgs pkg.override);
        in
          if builders == {}
          then throw "withGo: ${pkg.pname} takes no buildGo*Module argument to rebuild it with ${go.name}"
          else pkg.override (lib.mapAttrs (_: _: buildGoModule) builders);

        # Oracle for the port: generates golden fixtures by running Composer's
        # own PHP from .ref/ (see docs/PORTING.md) and runs the e2e comparisons
        # against real Composer.
        php = pkgs.php84;
        composer = php.packages.composer; # only to install .ref/composer's dependencies

        # Recreates .ref/: the exact sources being ported, with their tests: the
        # Composer release internal/upstream names and the libraries at the
        # versions its composer.lock pins. A checkout at another tag is replaced.
        ref-sync = pkgs.writeShellApplication {
          name = "ref-sync";
          runtimeInputs = [pkgs.git php composer];
          text = ''
            cd "$(git rev-parse --show-toplevel)"
            mkdir -p .ref
            clone() { # repo tag dir
              if [ -d ".ref/$3" ] && [ "$(git -C ".ref/$3" describe --tags --exact-match 2>/dev/null)" != "$2" ]; then
                rm -rf ".ref/$3"
              fi
              [ -d ".ref/$3" ] || git -c advice.detachedHead=false clone -q --depth 1 --branch "$2" "https://github.com/$1" ".ref/$3"
            }
            locked() { # package: its version in Composer's composer.lock
              # shellcheck disable=SC2016
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
        };

        # Flakes can't see git tags, so `just release` commits release.json
        # with the release version and the release commit's own commit time
        # (it pins the commit date to make that knowable). A clean build of
        # exactly that commit reports the plain version, like the release
        # binaries; anything else is stamped with the date and hash.
        release = builtins.fromJSON (builtins.readFile ./release.json);
        isRelease = self ? rev && (self.lastModified or 0) == release.commitTime;
        rev =
          if isRelease
          then null
          else self.shortRev or self.dirtyShortRev or "unknown";
        version =
          if isRelease
          then release.version
          else "${release.version}-unstable-${self.lastModifiedDate or "0"}";
      in {
        packages = rec {
          maestro = pkgs.callPackage ./package.nix {inherit buildGoModule rev version;};
          default = maestro;
        };

        apps = rec {
          maestro = flake-utils.lib.mkApp {drv = self.packages.${system}.maestro;};
          default = maestro;
        };

        devShells.default = pkgs.mkShell {
          packages =
            [go]
            ++ map withGo (with pkgs; [
              gopls # language server
              delve # debugger
              golangci-lint # linter behind `just lint`, config in .golangci.yml
              # what editor Go integrations (vscode-go, vim-go) install
              gotools
              go-tools
              gomodifytags
              impl
              gotests
              iferr
            ])
            ++ [
              pkgs.just # task runner
              php
              composer
              pkgs.unzip # what Composer extracts with; the archive tests compare against it
              ref-sync

              # Workflow
              pkgs.git
              pkgs.gh # GitHub CLI: releases, CI
            ];

          # Keep accidental cgo out (the race detector needs CGO_ENABLED=1 per
          # command), and keep the compiler the shell provides.
          CGO_ENABLED = "0";
          GOTOOLCHAIN = "local";
        };

        formatter = pkgs.alejandra;
      }
    );
}
