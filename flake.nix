{
  description = "Mural — digital signage image slideshow player";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-26.05";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      pkgsFor = system: import nixpkgs { inherit system; };
      pkgsForChecks = system: import nixpkgs { inherit system; overlays = [ self.overlays.default ]; };
    in
    {
      overlays.default = final: prev: {
        mural = final.callPackage ./nix/package.nix { };
      };

      packages = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
          mural = pkgs.callPackage ./nix/package.nix { };
        in
        {
          inherit mural;
          default = mural;
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go
              gcc
              pkg-config
              libGL
              xorg.libX11
              xorg.libXrandr
              xorg.libXinerama
              xorg.libXcursor
              xorg.libXi
              xorg.libXxf86vm
            ];
          };
        }
      );

      nixosModules.mural = ./nix/modules/mural.nix;
      nixosModules.kiosk-x11 = ./nix/modules/kiosk-x11.nix;

      nixosConfigurations.sign = nixpkgs.lib.nixosSystem {
        system = "aarch64-linux";
        modules = [
          { nixpkgs.overlays = [ self.overlays.default ]; }
          self.nixosModules.mural
          self.nixosModules.kiosk-x11
          ./nix/hosts/sign/default.nix
          ./nix/hosts/sign/boot.nix
        ];
      };

      # Module logic only, verified on x86_64 — per Analyst.md this does not
      # discharge the aarch64/Raspberry Pi hardware claim (see Layer H).
      checks.x86_64-linux =
        let
          pkgs = pkgsForChecks "x86_64-linux";
        in
        {
          kiosk = import ./nix/tests/kiosk.nix { inherit pkgs; };
          content = import ./nix/tests/content.nix { inherit pkgs; };
          share = import ./nix/tests/share.nix { inherit pkgs; };
        };
    };
}
