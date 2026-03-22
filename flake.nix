{
  description = "exocortex";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      nixpkgs,
      flake-utils,
      ...
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        # Import nixpkgs for the specific system
        pkgs = import nixpkgs { inherit system; };
        lib = pkgs.lib;
      in
      {
        packages.exo = pkgs.buildGoModule {
          name = "exo";
          version = "1.1.0";

          src = lib.cleanSource ./.;

          subPackages = [ "cmd/exo" ];

          doCheck = false;

          vendorHash = "sha256-Kx80kQ5xSFBuaG3ey0n3oX3g5gOvphtuLPNP+qRROCQ=";
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
