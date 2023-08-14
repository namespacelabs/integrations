{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    flake-utils.url = "github:numtide/flake-utils";
    flake-compat = {
      url = "github:edolstra/flake-compat";
      flake = false;
    };
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
    ...
  }:
    flake-utils.lib.eachDefaultSystem (system: let
      pkgs = nixpkgs.legacyPackages.${system};
    in {
      devShell = pkgs.mkShell {
        buildInputs = with pkgs;
          [
            buf
            go_1_20
            gopls
            go-outline
            go-tools
            git
            pre-commit
            goreleaser

            git
            nodejs
            crane
            kubectl
            awscli2
            jq
            aws-iam-authenticator
            google-cloud-sdk

            postgresql_15
          ];
      };
    });
}
