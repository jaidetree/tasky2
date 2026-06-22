{
  description = "Tasky";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            postgresql_14
            pgcli
            postgresql_14.lib
            go
            goose
            nodejs_22
          ];

          # Shell hook for additional environment setup
          shellHook = ''
            echo "Postgres version: $(postgres --version)"
            echo "Go version: $(go version)"
            echo "Node version: $(node --version)"
          '';
        };
      }
    );
}
