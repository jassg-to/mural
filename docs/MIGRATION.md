## If your sign was set up the old way

Mural's supported install method changed to NixOS. If your sign was set up
with the old `install.sh` script on Raspberry Pi OS, here's what that means
for you:

- **It keeps running.** Nothing reaches out to it or breaks it. You don't
  need to do anything right now.
- **It won't get updates.** There is no installer and no update procedure for
  the old setup anymore. If a future version of Mural fixes a bug you care
  about, the only way to get it is to move the sign to NixOS.
- **If the SD card dies, the sign is re-provisioned on NixOS, not rebuilt as
  it was.** There's no tooling to restore an old-style install — see
  [INSTALL.md](INSTALL.md) for the current setup process.

There's no in-place upgrade path from the old setup to NixOS. Moving a sign
means re-imaging its SD card and following [INSTALL.md](INSTALL.md) from the
start.
