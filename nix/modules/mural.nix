{
  config,
  lib,
  pkgs,
  ...
}:

with lib;

let
  cfg = config.services.mural;
in
{
  options.services.mural = {
    enable = mkEnableOption "Mural digital signage player";

    package = mkOption {
      type = types.package;
      default = pkgs.mural;
      description = "The Mural derivation to run.";
    };

    user = mkOption {
      type = types.str;
      default = "mural";
      description = "Unprivileged kiosk user Mural runs as.";
    };

    contentDir = mkOption {
      type = types.path;
      default = "/var/lib/mural/content";
      description = "Directory holding images and config.toml. Runtime state.";
    };

    seedConfig = mkOption {
      type = types.nullOr types.path;
      default = ./../config.sample.toml;
      description = ''
        Config file copied to `contentDir/config.toml` only if absent.
        Set to `null` to disable seeding.
      '';
    };

    session = mkOption {
      type = types.enum [
        "x11"
        "none"
      ];
      default = "x11";
      description = "Which presentation layer drives the display.";
    };

    restartDelay = mkOption {
      type = types.int;
      default = 30;
      description = "Seconds to pause before relaunching a crashed session.";
    };

    cec.enable = mkOption {
      type = types.bool;
      default = true;
      description = "Put cec-client on the session PATH and grant CEC device access.";
    };

    share = {
      enable = mkOption {
        type = types.bool;
        default = false;
        description = "Samba share of contentDir.";
      };

      name = mkOption {
        type = types.str;
        default = "content";
        description = "Samba share name.";
      };

      validUsers = mkOption {
        type = types.listOf types.str;
        default = [ cfg.user ];
        description = "Users permitted to access the share.";
      };

      openFirewall = mkOption {
        type = types.bool;
        default = true;
        description = "Open the SMB ports when the share is enabled.";
      };
    };
  };

  config = mkIf cfg.enable {
    users.users.${cfg.user} = {
      isNormalUser = true;
      hashedPassword = null;
      extraGroups = [ "video" ];
    };

    systemd.tmpfiles.rules =
      [
        "d ${cfg.contentDir} 0755 ${cfg.user} users -"
      ]
      ++ lib.optional (
        cfg.seedConfig != null
      ) "C ${cfg.contentDir}/config.toml 0644 ${cfg.user} users - ${cfg.seedConfig}";

    environment.systemPackages = mkIf cfg.cec.enable [ pkgs.libcec ];

    services.udev.extraRules = mkIf cfg.cec.enable ''
      KERNEL=="cec[0-9]*", GROUP="video", MODE="0660"
    '';

    hardware.graphics.enable = mkDefault true;

    assertions = [
      {
        assertion = config.hardware.graphics.enable;
        message = ''
          services.mural requires hardware.graphics.enable = true. Mesa
          hardcodes /run/opengl-driver/lib, which only exists when this flag is
          set; without it Mural fails with a misleading "failed to open dri"
          error rather than a clear message about the missing flag.
        '';
      }
    ];

    services.samba = mkIf cfg.share.enable {
      enable = true;
      openFirewall = cfg.share.openFirewall;
      nmbd.enable = true;
      settings.${cfg.share.name} = {
        path = cfg.contentDir;
        browseable = "yes";
        "read only" = "no";
        "guest ok" = "no";
        "map to guest" = "never";
        "force user" = cfg.user;
        "valid users" = cfg.share.validUsers;
      };
    };
  };
}
