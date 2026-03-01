{
  description = "PicoClaw — ultra-lightweight personal AI assistant";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
      ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
      pkgsFor = system: import nixpkgs { inherit system; };
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
          version = self.shortRev or self.dirtyShortRev or "dev";
          ldflagsPrefix = "github.com/sipeed/picoclaw/cmd/picoclaw/internal";
        in
        {
          default = pkgs.buildGoModule {
            pname = "picoclaw";
            inherit version;
            src = self;
            # Only update this hash when go.mod / vendor dependencies change.
            vendorHash = "sha256-EJAYrVMDgAXssqRcCdjrYbuKsKSp7tIG5xvLeY0xZeY=";
            subPackages = [ "cmd/picoclaw" ];
            tags = [ "stdjson" ];
            env.CGO_ENABLED = 0;
            doCheck = false;
            # Embed default workspace templates into the binary for `picoclaw onboard`.
            preBuild = ''
              cp -r workspace cmd/picoclaw/internal/onboard/workspace
            '';
            ldflags = [
              "-s"
              "-w"
              "-X ${ldflagsPrefix}.version=${version}"
              "-X ${ldflagsPrefix}.gitCommit=${version}"
            ];
            meta = {
              description = "Ultra-lightweight personal AI assistant in Go";
              homepage = "https://github.com/arutkowski00/picoclaw";
              license = pkgs.lib.licenses.mit;
              mainProgram = "picoclaw";
            };
          };
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = pkgsFor system;
        in
        {
          default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go
              gopls
              gotools
              go-tools
            ];
          };
        }
      );
    };
}
