{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs?ref=nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, flake-utils, nixpkgs }: flake-utils.lib.eachDefaultSystem (system:
    let pkgs = import nixpkgs { inherit system; }; in rec
    {
      packages.golexirc = pkgs.buildGoModule {
        pname = "golexirc";
        version = "local";
        src = ./.;
        vendorHash = "sha256-6rFtnR/vKkhEhxgt/L1iQwyeFMA+t7XiTNucRfTfziY=";
      };
      packages.default = packages.golexirc;
      devShell = pkgs.mkShell {
        buildInputs = with pkgs; [ go gopls ];
      };
    });
}
