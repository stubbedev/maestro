{
  perSystem = {
    inputs',
    lib,
    pkgs,
    ...
  }: let
    go = inputs'.devtools.packages.go;
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
