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
