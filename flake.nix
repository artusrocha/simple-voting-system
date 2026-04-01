{
  description = "Voting Platform - K8s development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in {
        devShells.default = pkgs.mkShell {
          packages = with pkgs; [
            k3d
            kustomize
            go # This provides the Go compiler and tools
            gopls # The Go language server
            gotools # Additional Go tools like goimports
          ];

          shellHook = ''            
          '';
        };
      });
}
