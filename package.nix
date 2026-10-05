{
  lib,
  buildGo127Module,
  # Set by the flake from `self.shortRev or self.dirtyShortRev`; falls back to
  # the placeholder used when nothing was stamped in.
  rev ? "unknown",
  version ? "0-unstable",
}:
buildGo127Module {
  pname = "maestro";
  inherit version;

  src = lib.cleanSource ./.;

  # Hash of the go.mod/go.sum module set. Changes with every dependency bump:
  # run `nix build` and put the reported got: hash here.
  vendorHash = lib.fakeHash;

  subPackages = ["cmd/maestro"];

  env.CGO_ENABLED = 0;

  ldflags = [
    "-s"
    "-w"
    "-X main.version=${version}+${rev}"
  ];

  # Every test fakes the registry or works on temp dirs; none reach the network.
  doCheck = true;

  meta = {
    description = "Fast Composer-compatible PHP package manager";
    homepage = "https://github.com/stubbedev/maestro";
    license = lib.licenses.mit;
    mainProgram = "maestro";
    platforms = lib.platforms.unix;
  };
}
