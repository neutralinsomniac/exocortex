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
          version = "0.5.0";

          src = lib.cleanSource ./.;

          subPackages = [ "cmd/exotui" ];

          doCheck = false;

          vendorHash = "sha256-27sDcfEKar2BnnWD6ZwgcNS/d8/gKocSv34ijVcQ824=";
        };

        packages.exobt = pkgs.buildGoModule {
          name = "exobt";
          version = "1.0.6";

          src = lib.cleanSource ./.;

          subPackages = [ "cmd/exobt" ];

          doCheck = false;

          vendorHash = "sha256-27sDcfEKar2BnnWD6ZwgcNS/d8/gKocSv34ijVcQ824=";
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
