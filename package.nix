{
  lib,
  buildGo127Module,
  # Set by the flake from `self.shortRev or self.dirtyShortRev`, or null for
  # a release build, which reports the bare version like the release binaries.
  rev ? "unknown",
  version ? "0-unstable",
}:
buildGo127Module {
  pname = "maestro";
  inherit version;

  src = lib.cleanSource ./.;

  # Hash of the go.mod/go.sum module set. .github/workflows/flake.yml
  # recomputes it on every dependency change; `just nix-vendor-hash` does it
  # locally.
  vendorHash = "sha256-zdIg5vK02YNuPFv+YVyb4KgM6LE5JfW1CjxJr5vEJdE=";

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
}
