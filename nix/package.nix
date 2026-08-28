{
  lib,
  buildGoModule,
  pkg-config,
  libGL,
  xorg,
}:

buildGoModule {
  pname = "mural";
  version = "0.1.0";

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      (lib.fileset.fileFilter (file: file.hasExt "go") ../.)
      ../go.mod
      ../go.sum
    ];
  };

  vendorHash = "sha256-NplI36b1zZwFxTaUJs5/zCSUA/tHLO+yEUR4paeZQZQ=";

  nativeBuildInputs = [ pkg-config ];

  buildInputs = [
    libGL
    xorg.libX11
    xorg.libXrandr
    xorg.libXinerama
    xorg.libXcursor
    xorg.libXi
    xorg.libXxf86vm
  ];

  meta = {
    description = "Digital signage image slideshow player";
    homepage = "https://github.com/jassg-to/mural";
    license = lib.licenses.mit;
    mainProgram = "mural";
  };
}
