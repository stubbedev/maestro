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

        formatter = pkgs.alejandra;
      }
    );
}
