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
        pkgs = import nixpkgs {
          inherit system;
          config.allowUnfree = true;
        };
        lib = pkgs.lib;
        buildGoCache = build-go-cache.legacyPackages.${system}.buildGoCache;
        vendorHash = "sha256-yQuQ7/jDmKev/WxorsclgJOupPM3hHvgj3RPBU1sPQw=";
        src = lib.fileset.toSource {
          root = ./.;
          fileset = lib.fileset.unions [
            ./cmd
            ./db
            ./go.mod
            ./go.sum
            ./imported-packages
          ];
        };

        goCache = buildGoCache {
          importPackagesFile = ./imported-packages;
          inherit src vendorHash;
        };

        buildMyGoModule = (
          cmd:
          pkgs.buildGoModule {
            inherit src;
            inherit vendorHash;

            buildInputs = [ goCache ];

            name = cmd;
            version = "1.2.0";
            subPackages = [ "cmd/${cmd}" ];

            doCheck = false;
          }
        );
      in
      rec {
        packages.default = packages.exo;

        packages.exo = buildMyGoModule "exo";
        packages.exo-server = buildMyGoModule "exo-server";

        devShell = pkgs.mkShell {
          packages = with pkgs; [
            pkg-config
            gcc
            vscode
            platformio
          ];
        };
      }
    );
}
