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
        packages.exotui = pkgs.buildGoModule {
          name = "exotui";
          version = "1.0.0";

          src = lib.cleanSource ./.;

          subPackages = [ "cmd/exotui" ];

          vendorHash = "sha256-+QZfW6WyhCsXGBBewjrtB8WfH14m/J5kxSytOOq/5xc=";
        };
      }
    );
}
