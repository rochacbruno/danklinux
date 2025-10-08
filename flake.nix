{
  description = "DankMaterialShell Command Line Interface";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
      ];

      forAllSystems =
        f:
        builtins.listToAttrs (
          map (system: {
            name = system;
            value = f system;
          }) supportedSystems
        );

    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          lib = pkgs.lib;
        in
        {
          dms-cli = pkgs.buildGoModule (finalAttrs: {
            pname = "dms-cli";
            version = "0.1.2";
            src = ./.;
            vendorHash = "sha256-8EIcLCJuv7EYSHGkMh8WpDw2ATSfXBftGnWxfUTxkoc=";

            subPackages = [ "cmd/dms" ];

            ldflags = [
              "-s"
              "-w"
              "-X main.Version=${finalAttrs.version}"
            ];

            meta = {
              description = "DankMaterialShell Command Line Interface";
              homepage = "https://github.com/AvengeMedia/danklinux";
              mainProgram = "dms";
              license = lib.licenses.mit;
              platforms = lib.platforms.unix;
            };
          });

          default = self.packages.${system}.dms-cli;
        }
      );
    };
}
