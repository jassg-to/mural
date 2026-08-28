{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.mural;

  sessionScript = pkgs.writeShellScript "mural-kiosk-session" ''
    export MURAL_BIN="${cfg.package}/bin/mural"
    export MURAL_CONTENT_DIR="${cfg.contentDir}"
    export MURAL_RATPOISONRC="${../session/ratpoisonrc}"

    ${pkgs.xorg.xinit}/bin/xinit ${../session/xinitrc.sh} -- :0 vt1 -nolisten tcp

    cat <<BANNER
        ************************************************************
        ***  Mural kiosk session ended.                          ***
        ***  Restarting in ${toString cfg.restartDelay}s. Switch to VT2
        ***  (Ctrl+Alt+F2) for a system shell.
        ************************************************************
    BANNER
    sleep ${toString cfg.restartDelay}
  '';
in
lib.mkIf (cfg.enable && cfg.session == "x11") {
  environment.systemPackages = [
    pkgs.xorg.xinit
    pkgs.xorg.xorgserver
    pkgs.xorg.xset
    pkgs.ratpoison
    pkgs.unclutter
  ];

  services.greetd = {
    enable = true;
    settings = {
      initial_session = {
        command = "${sessionScript}";
        user = cfg.user;
      };
      default_session = {
        command = "${sessionScript}";
        user = cfg.user;
      };
    };
  };

  systemd.services.greetd.serviceConfig.StartLimitIntervalSec = 0;

  # greetd hardcodes its terminal to VT1 upstream (services.greetd.vt was
  # removed; nixpkgs now sets services.greetd.settings.terminal.vt = 1
  # unconditionally). The operator console therefore lives on tty2, not tty1
  # — an operator physically at the sign reaches it with Ctrl+Alt+F2, which
  # starts getty@tty2 on demand.
  services.getty.autologinUser = cfg.user;
}
