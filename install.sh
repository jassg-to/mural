#!/usr/bin/env bash
set -euo pipefail

REPO="jassg-to/mural"
INSTALL_DIR="$HOME/mural"
CONTENT_DIR="$HOME/mural/content"
CURRENT_USER=$(id -un)

# ── 1. System packages ────────────────────────────────────────────────────────
echo "Installing system packages..."
sudo apt update
sudo apt install -y cec-utils

# Mural talks to DRM/KMS and evdev directly, as an unprivileged user — no
# root, no setuid, no elevated capabilities. That requires membership in the
# groups that own /dev/dri/card* and /dev/input/event* (conventionally
# 'video' and 'input'). Takes effect on next login/reboot.
echo "Adding ${CURRENT_USER} to the video and input groups..."
sudo usermod -aG video,input "$CURRENT_USER"

# ── 2. Binary from GitHub Releases ───────────────────────────────────────────
ARCH=$(uname -m)
case "$ARCH" in
  aarch64) ARCH_TAG="arm64" ;;
  armv7l)  ARCH_TAG="arm"   ;;
  x86_64)  ARCH_TAG="amd64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

BINARY_URL="https://github.com/$REPO/releases/latest/download/mural_linux_$ARCH_TAG"
echo "Downloading mural ($ARCH_TAG)..."
mkdir -p "$INSTALL_DIR"
curl -fsSL "$BINARY_URL" -o "$INSTALL_DIR/mural"
chmod +x "$INSTALL_DIR/mural"

# ── 3. Content directory + sample schedule ────────────────────────────────────
mkdir -p "$CONTENT_DIR"

if [ ! -f "$CONTENT_DIR/config.toml" ]; then
  cat > "$CONTENT_DIR/config.toml" <<'EOF'
[slideshow]
interval = "30s"    # time between slides (e.g. "30s", "1m", "2m30s")
thumb_width = 80    # thumbnail width in pixels for keyboard navigation

[schedule]
reload_time = "01:00"  # reload this file daily at this time (HH:MM)

[schedule.monday]
all = [ "08:00-12:00", "13:30-22:00" ]

[schedule.tuesday]
all = [ "08:00-12:00", "13:30-22:00" ]

[schedule.wednesday]
all = [ "08:00-12:00", "13:30-22:00" ]

[schedule.thursday]
all = [ "08:00-12:00", "13:30-22:00" ]
second = [ "07:00-08:00" ]

[schedule.friday]
all = [ "08:00-12:00", "13:30-22:00" ]

[schedule.saturday]
all = [ "10:00-18:00" ]
last = [ "18:00-22:00"]

# sunday: off all day (no section needed)
EOF
fi

# ── 4. USB stick automount (udev rule) ────────────────────────────────────────
echo "Setting up USB stick automount..."

# Raspberry Pi OS Lite ships systemd and should always have this — this
# guards a non-Debian or minimal target, not an expected path. Warn rather
# than silently installing a rule that can never actually mount anything:
# that failure mode ("feature appears installed and does nothing") is
# exactly what this whole design keeps trying to avoid.
if ! command -v systemd-mount &>/dev/null; then
  echo "WARNING: systemd-mount not found on this system. The USB automount rule will be installed, but it will never actually mount a stick until systemd-mount is available. USB hotplug content updates will silently do nothing." >&2
fi

# Must match mural's -media-dir default (see main.go) — the two are
# independent literals that have to be kept in sync by hand.
MEDIA_ROOT="/media/mural"
sudo mkdir -p "$MEDIA_ROOT"
sudo chmod 0755 "$MEDIA_ROOT"

CURRENT_UID=$(id -u "$CURRENT_USER")
CURRENT_GID=$(id -g "$CURRENT_USER")

# uid=/gid=/umask= are FAT/exFAT/NTFS driver options, not generic VFS
# options: ext4/btrfs/xfs reject unknown mount options and fail the mount
# entirely, so a stick formatted with one of those would never appear in
# /proc/self/mountinfo at all if this rule applied those options
# unconditionally — silence, not a diagnosable failure. So those options
# are confined to their own rule, keyed on ID_FS_TYPE; the other rule
# carries only the four options that are safe on every filesystem.
#
# A USB-boot Pi mounting its own root filesystem a second time under this
# directory was meant to be excluded by ATTRS{removable}=="1" — dropped
# after on-hardware testing showed it can never work. udev requires
# SUBSYSTEMS=="usb" and an ATTRS{} match to be satisfied by the *same*
# ancestor device, but the numeric removable flag (0/1) lives only on the
# block-layer disk device (subsystem "block"), while the actual "usb"
# ancestor's own "removable" attribute is a separate string field
# ("fixed"/"removable"/"unknown") — and many USB mass-storage bridge
# chips report "fixed" there regardless of being genuinely removable
# flash media. No ancestor can satisfy both conditions at once, so a rule
# built that way silently never matches any USB stick, on any hardware.
# docs/INSTALL.md's only supported deployment boots from the SD card, so
# a USB-boot Pi getting its root double-mounted under /media/mural is an
# accepted residual risk for an unsupported configuration, not something
# this rule tries to guard against.
#
# The 99- prefix is required, not cosmetic: ID_FS_USAGE is set by the
# blkid builtin invoked from Debian's 60-persistent-storage.rules, and a
# rule sorting before that one sees the variable unset and never matches.
sudo tee /etc/udev/rules.d/99-mural-usb.rules > /dev/null <<UDEVEOF
# Mural USB stick automount. Mounts read-only under ${MEDIA_ROOT} — must
# match mural's -media-dir flag default (see main.go). See install.sh for
# why this does not (and structurally cannot) guard against a USB-boot Pi.
ACTION=="add", SUBSYSTEM=="block", SUBSYSTEMS=="usb", ENV{ID_FS_USAGE}=="filesystem", ENV{ID_FS_TYPE}=="vfat|exfat|ntfs", RUN{program}+="/usr/bin/systemd-mount --no-block --collect -o ro,nosuid,nodev,noexec,uid=${CURRENT_UID},gid=${CURRENT_GID},umask=0077 \$devnode ${MEDIA_ROOT}/%k"
ACTION=="add", SUBSYSTEM=="block", SUBSYSTEMS=="usb", ENV{ID_FS_USAGE}=="filesystem", ENV{ID_FS_TYPE}!="vfat|exfat|ntfs", RUN{program}+="/usr/bin/systemd-mount --no-block --collect -o ro,nosuid,nodev,noexec \$devnode ${MEDIA_ROOT}/%k"
UDEVEOF

sudo udevadm control --reload-rules
sudo udevadm trigger

# ── 5. Done ───────────────────────────────────────────────────────────────────
echo ""
echo "mural installed successfully."
echo ""
echo "Next steps:"
echo "  1. Copy images (JPG/PNG) into $CONTENT_DIR"
echo "  2. Edit $CONTENT_DIR/config.toml to set your display hours and slideshow settings"
echo "  3. Log out and back in (or reboot) for the video/input group membership to take effect"
echo "  4. Run '$INSTALL_DIR/mural $CONTENT_DIR' from a text console to launch manually,"
echo "     or configure automatic startup below"
echo "  5. To update content later, plug in a USB stick (FAT32/exFAT) carrying images"
echo "     and a top-level config.toml (copy the sample from $CONTENT_DIR) — the sign"
echo "     picks it up automatically. A stick with no config.toml is ignored entirely."
echo ""

# ── 6. Offer full system setup ────────────────────────────────────────────────
printf "Configure automatic startup (autologin + auto-launch on boot)? [Y/n] "
read -r response </dev/tty
case "${response,,}" in
  ""|y|yes)
    # Write tty1-guard.sh
    cat > "$INSTALL_DIR/tty1-guard.sh" <<'GUARDEOF'
#!/usr/bin/env bash
[ "$(tty)" != "/dev/tty1" ] && exit 1

cd ~/mural
./mural

cat <<'BANNER'
    ************************************************************
    ***  Waiting 30 seconds before restarting...             ***
    ***  Press Ctrl+C to enter the system shell.             ***
    ************************************************************
BANNER
sleep 30
GUARDEOF
    chmod +x "$INSTALL_DIR/tty1-guard.sh"

    # Add tty1-guard hook to .bashrc (idempotent)
    if ! grep -q "tty1-guard.sh" "$HOME/.bashrc" 2>/dev/null; then
      cat >> "$HOME/.bashrc" <<BASHRCEOF

if bash "${INSTALL_DIR}/tty1-guard.sh"; then
  exit 1
fi
BASHRCEOF
    fi

    # Configure console autologin via systemd drop-in
    DROPIN=/etc/systemd/system/getty@tty1.service.d/autologin.conf
    sudo mkdir -p "$(dirname "$DROPIN")"
    sudo tee "$DROPIN" > /dev/null <<DROPINEOF
[Service]
ExecStart=
ExecStart=-/sbin/agetty --autologin ${CURRENT_USER} --noclear %I
DROPINEOF
    sudo systemctl daemon-reload

    echo ""
    echo "Autologin configured for user '${CURRENT_USER}'."
    echo "Run 'sudo reboot' to start automatically on next boot."
    ;;
  *)
    echo "Skipped. Re-run this script any time to set it up."
    ;;
esac

# ── 7. Offer Samba shared folder ───────────────────────────────────────────
printf "Set up Samba file sharing (access content folder from your network)? [Y/n] "
read -r response </dev/tty
case "${response,,}" in
  ""|y|yes)
    if ! command -v smbd &>/dev/null; then
      echo "Installing Samba..."
      sudo apt install -y samba
    fi

    # Set/reset the Samba password for the current user
    echo ""
    echo "Set a Samba password for user '${CURRENT_USER}'."
    echo "You'll use this when connecting from Windows/Mac."
    sudo smbpasswd -a "${CURRENT_USER}"
    sudo smbpasswd -e "${CURRENT_USER}"

    SAMBA_CONF="/etc/samba/smb.conf"

    if grep -q '^\[content\]' "$SAMBA_CONF" 2>/dev/null; then
      echo "Samba [content] share already exists — skipping."
    else
      echo "Adding [content] share to $SAMBA_CONF..."
      sudo tee -a "$SAMBA_CONF" > /dev/null <<SAMBAEOF

[content]
   path = ${CONTENT_DIR}
   browseable = yes
   read only = no
   guest ok = no
   force user = ${CURRENT_USER}
   valid users = ${CURRENT_USER}
SAMBAEOF
    fi

    sudo systemctl restart smbd nmbd

    echo ""
    echo "Samba share ready. Access from your computer:"
    echo "  \\\\$(hostname -I | awk '{print $1}')\\content"
    echo "  Username: ${CURRENT_USER}"
    ;;
  *)
    echo "Skipped Samba setup."
    ;;
esac
