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

        devShell = pkgs.mkShell {
          packages = with pkgs; [
            libxcb
            libxkbcommon
            xorg.libX11
            xorg.libX11.dev
            xorg.xorgproto # Crucial: Includes X11/X.h and other headers
            xorg.libXext
            xorg.libXft
            xorg.libXinerama
            xorg.libXcursor
            xorg.libXi
            xorg.libXrender
            xorg.libXrandr
            xorg.libXfixes

            wayland
            wayland.dev

            libGL.dev
            pkg-config
            xorg.libXxf86vm
            gcc
          ];
        };
      }
    );
}
