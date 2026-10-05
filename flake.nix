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
        # Nix has no version to read from the source tree, so builds from a
        # checkout are stamped with the commit date and hash.
        rev = self.shortRev or self.dirtyShortRev or "unknown";
        version = "0-unstable-${self.lastModifiedDate or "0"}";
      in {
        packages = rec {
          maestro = pkgs.callPackage ./package.nix {inherit rev version;};
          default = maestro;
        };

        apps = rec {
          maestro = flake-utils.lib.mkApp {drv = self.packages.${system}.maestro;};
          default = maestro;
        };

        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            # Go toolchain. Pinned to the version go.mod declares: the
            # default `go` attribute lags behind, and with GOTOOLCHAIN=auto
            # it would silently download its own toolchain instead of
            # failing. gopls and golangci-lint are built with the same
            # nixpkgs Go, so they parse what the compiler accepts.
            go_1_27

            # Development tools
            gopls # Go language server
            golangci-lint # Linter behind `just lint`, config in .golangci.yml
            delve # Go debugger
            just # Task runner

            # Oracle for the port: generates golden fixtures by running
            # Composer's own PHP (in .ref/) and runs the e2e comparisons.
            php84
            unzip

            # Development workflow
            git # Version control
            gh # GitHub CLI
          ];

          shellHook = ''
            # Keep accidental cgo out, and keep the compiler the flake
            # provides: a module asking for a newer Go fails instead of
            # downloading one at first use.
            export CGO_ENABLED=0 GOTOOLCHAIN=local
          '';
        };

        formatter = pkgs.alejandra;
      }
    );
}
