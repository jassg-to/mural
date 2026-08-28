{ ... }:

{
  networking.hostName = "sign";

  # PSK lives in /etc/mural/wifi.env (device-local, written once, never
  # committed — the repository is public). See docs/INSTALL.md's residual
  # manual state section.
  networking.wireless = {
    enable = true;
    secretsFile = "/etc/mural/wifi.env";
    networks."REDACTED".pskRaw = "ext:mural_wifi_psk";
  };

  time.timeZone = "America/Toronto";
  i18n.defaultLocale = "en_CA.UTF-8";

  system.stateVersion = "26.05";

  nix.settings.experimental-features = [
    "nix-command"
    "flakes"
  ];

  services.openssh = {
    enable = true;
    settings = {
      PasswordAuthentication = false;
      PermitRootLogin = "no";
    };
  };

  users.users.admin = {
    isNormalUser = true;
    extraGroups = [ "wheel" ];
    openssh.authorizedKeys.keys = [
      "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHKKxhFT2wu5gueaEMzP6J9UL76/UcOxAYesQglL9XaG kiva+2026@piranga"
    ];
  };

  services.mural = {
    enable = true;
    share.enable = true;
  };
}
