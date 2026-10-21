{
  perSystem = {
    lib,
    pkgs,
    ...
  }: let
    # The Go toolchain, pinned to the version go.mod declares: with
    # GOTOOLCHAIN=local a module asking for a newer Go fails instead of
    # downloading one. The package and every Go tool in the dev shell are
    # built with it, so gopls and golangci-lint parse what the compiler
    # accepts.
    go = pkgs.go_1_27;
    buildGoModule = pkgs.buildGoModule.override {inherit go;};
  in {
    _module.args = {
      inherit go buildGoModule;
      # Rebuilds a Go tool with `go`, whichever buildGo*Module it takes.
      withGo = pkg: let
        builders = lib.filterAttrs (name: _: lib.match "buildGo.*Module" name != null) (lib.functionArgs pkg.override);
      in
        if builders == {}
        then throw "withGo: ${pkg.pname} takes no buildGo*Module argument to rebuild it with ${go.name}"
        else pkg.override (lib.mapAttrs (_: _: buildGoModule) builders);
    };
  };
}
