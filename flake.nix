{
  description = "exocortex";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
    flake-utils.url = "github:numtide/flake-utils";
    build-go-cache.url = "github:numtide/build-go-cache";
    build-go-cache.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      build-go-cache,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        lib = pkgs.lib;
        buildGoCache = build-go-cache.legacyPackages.${system}.buildGoCache;
        vendorHash = "sha256-yQuQ7/jDmKev/WxorsclgJOupPM3hHvgj3RPBU1sPQw=";
        src = lib.cleanSource ./.;

        goCache = buildGoCache {
          importPackagesFile = ./imported-packages;
          inherit src vendorHash;
        };
      in
      {
        packages.exo = pkgs.buildGoModule {
          name = "exo";
          version = "1.1.0";

          inherit src;

          subPackages = [ "cmd/exo" ];

          doCheck = false;

          buildInputs = [ goCache ];

          inherit vendorHash;
        };

        devShell = pkgs.mkShell {
          packages = with pkgs; [
            pkg-config
            gcc
          ];
        };
      }
    );
}
