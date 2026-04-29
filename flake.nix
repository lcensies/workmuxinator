{
  description = "Minimal Archon-like DAG workflow runner in Go";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        workmuxinator = pkgs.buildGoModule {
          pname = "workmuxinator";
          version = "0.3.0";
          src = ./.;
          vendorHash = pkgs.lib.fakeHash;

          subPackages = [ "cmd/workmuxinator" ];

          meta = with pkgs.lib; {
            description = "Minimal Archon-like DAG workflow runner in Go";
            license = licenses.mit;
            platforms = platforms.all;
            mainProgram = "workmuxinator";
          };
        };
      in {
        packages = {
          inherit workmuxinator;
          default = workmuxinator;
        };

        apps.default = flake-utils.lib.mkApp {
          drv = workmuxinator;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = [
            pkgs.go
            pkgs.gopls
          ];
        };
      }
    );
}
