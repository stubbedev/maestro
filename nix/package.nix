{self, ...}: {
  perSystem = {
    buildGoModule,
    lib,
    self',
    ...
  }: let
    # Flakes can't see git tags, so `just release` commits release.json
    # with the release version and the release commit's own commit time
    # (it pins the commit date to make that knowable). A clean build of
    # exactly that commit reports the plain version, like the release
    # binaries; anything else is stamped with the date and hash.
    release = lib.importJSON ../release.json;
    isRelease = self ? rev && (self.lastModified or 0) == release.commitTime;
    # null for a release build, which reports the bare version like the
    # release binaries.
    rev =
      if isRelease
      then null
      else self.shortRev or self.dirtyShortRev or "unknown";
    version =
      if isRelease
      then release.version
      else "${release.version}-unstable-${self.lastModifiedDate or "0"}";
  in {
    packages = {
      maestro = buildGoModule {
        pname = "maestro";
        inherit version;

        src = lib.fileset.toSource {
          root = ../.;
          fileset = lib.fileset.difference (lib.fileset.gitTracked ../.) (
            lib.fileset.unions [
              ../flake.nix
              ../flake.lock
              ../nix
            ]
          );
        };

        # Hash of the go.mod/go.sum module set. .github/workflows/flake.yml
        # recomputes it on every dependency change; `just nix-vendor-hash` does it
        # locally.
        vendorHash = "sha256-JCE+GAAglBC2X++S0uUggU2Z1g1CrFZvpaC4Xi9xwYE=";

        subPackages = ["cmd/maestro"];

        env.CGO_ENABLED = 0;

        ldflags = [
          "-s"
          "-w"
          "-X main.version=${version}${lib.optionalString (rev != null) "+${rev}"}"
        ];

        # Every test fakes the registry or works on temp dirs; none reach the network.
        doCheck = true;

        # maestro is a drop-in replacement: installing it provides `composer` too.
        postInstall = ''
          ln -s maestro $out/bin/composer
        '';

        meta = {
          description = "Composer, natively: a fast drop-in replacement for the composer command";
          homepage = "https://github.com/stubbedev/maestro";
          license = lib.licenses.mit;
          mainProgram = "maestro";
          platforms = lib.platforms.unix;
        };
      };
      default = self'.packages.maestro;
    };

    apps = {
      maestro = {
        type = "app";
        program = lib.getExe self'.packages.maestro;
      };
      default = self'.apps.maestro;
    };
  };
}
