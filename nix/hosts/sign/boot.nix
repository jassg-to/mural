{ ... }:

{
  # Deliberately NOT importing nixos-hardware's raspberry-pi-3 profile: its
  # kernel is not in the Hydra binary cache, forcing a full on-device kernel
  # compile that can exhaust the board's RAM (spike-findings.md). The stock
  # cached mainline kernel is used instead.

  boot.loader.grub.enable = false;
  boot.loader.generic-extlinux-compatible.enable = true;
  boot.loader.generic-extlinux-compatible.configurationLimit = 10;

  hardware.enableRedistributableFirmware = true;

  # Matches the stock upstream NixOS aarch64 SD image's own partition layout
  # and labels (confirmed on the live board via lsblk -f).
  fileSystems."/" = {
    device = "/dev/disk/by-label/NIXOS_SD";
    fsType = "ext4";
    options = [ "x-initrd.mount" ];
  };

  fileSystems."/boot/firmware" = {
    device = "/dev/disk/by-label/FIRMWARE";
    fsType = "vfat";
    options = [
      "nofail"
      "noauto"
    ];
  };
}
