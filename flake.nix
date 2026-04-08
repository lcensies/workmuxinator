{
  description = "Launch workmux worktrees for all tmuxinator projects";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        workmuxinator = pkgs.stdenv.mkDerivation {
          pname = "workmuxinator";
          version = "0.1.0";
          src = ./.;

          nativeBuildInputs = [ pkgs.makeWrapper ];

          # Pure bash script – no build step needed
          dontBuild = true;

          installPhase = ''
            install -Dm755 bin/workmuxinator $out/bin/workmuxinator
          '';

          # Wrap to ensure runtime deps are on PATH
          postInstall = ''
            wrapProgram $out/bin/workmuxinator \
              --prefix PATH : ${pkgs.lib.makeBinPath [
                pkgs.bash
                pkgs.tmux
                pkgs.yq-go
                # workmux and tmuxinator are expected to be installed by the user
              ]}
          '';

          meta = with pkgs.lib; {
            description = "Launch workmux worktrees for all tmuxinator projects";
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
            pkgs.bash
            pkgs.tmux
            pkgs.yq-go
            pkgs.shellcheck
          ];
        };
      }
    );
}
