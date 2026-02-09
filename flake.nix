{
  description = "msi-gpu-switcher - MSI GPU MUX switcher";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [ "x86_64-linux" ];
    in
    {
      packages = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.buildGoModule {
            pname = "msi-gpu-switcher";
            version = "0.1.2"; # x-release-please-version

            src = self;
            subPackages = [ "." ];

            vendorHash = "sha256-loaEr1mX4T1MwfuNiQYByxeSa7qEmaH7EZ2nCdD0AY8=";
          };
        });

      apps = forAllSystems (system: {
        default = {
          type = "app";
          program = "${self.packages.${system}.default}/bin/msi-gpu-switcher";
        };
      });

      devShells = forAllSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.go
            ];
          };
        });
    };
}
