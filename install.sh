#!/usr/bin/env bash
set -euo pipefail

REPO="jassg-to/mural"
INSTALL_DIR="$HOME/mural"
CONTENT_DIR="$HOME/mural/content"
CURRENT_USER=$(id -un)

# ── 1. System packages ────────────────────────────────────────────────────────
echo "Installing system packages..."
sudo apt update
sudo apt install -y xinit ratpoison cec-utils libgl1 unclutter x11-xserver-utils

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

# ── 3. Dotfiles ───────────────────────────────────────────────────────────────
echo "Writing dotfiles..."

cat > "$HOME/.ratpoisonrc" <<'EOF'
set border 0
EOF

cat > "$HOME/.xinitrc" <<'EOF'
xset s off
xset -dpms
xset s noblank

ratpoison &
unclutter -idle 0 -root &
cd ~/mural
exec ./mural
EOF

# ── 4. Content directory + sample schedule ────────────────────────────────────
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

# ── 5. Done ───────────────────────────────────────────────────────────────────
echo ""
echo "mural installed successfully."
echo ""
echo "Next steps:"
echo "  1. Copy images (JPG/PNG) into $CONTENT_DIR"
echo "  2. Edit $CONTENT_DIR/config.toml to set your display hours and slideshow settings"
echo "  3. Type 'startx' to launch"
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

startx

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
