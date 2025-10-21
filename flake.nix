{
  description = "DankMaterialShell Command Line Interface";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    gomod2nix = {
      url = "github:nix-community/gomod2nix/v1.7.0";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    { self, nixpkgs, gomod2nix }:
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
          dms-cliVersion = "0.1.17";
        in
        {
          dms-cli = gomod2nix.legacyPackages.${system}.buildGoApplication {
            pname = "dms-cli";
            version = dms-cliVersion;
            src = ./.;
            modules = ./gomod2nix.toml;

            subPackages = [ "cmd/dms" ];

            ldflags = [
              "-s"
              "-w"
              "-X main.Version=${dms-cliVersion}"
            ];

            meta = {
              description = "DankMaterialShell Command Line Interface";
              homepage = "https://github.com/AvengeMedia/danklinux";
              mainProgram = "dms";
              license = lib.licenses.mit;
              platforms = lib.platforms.unix;
            };
          };

          default = self.packages.${system}.dms-cli;
        }
      );
    };
}
