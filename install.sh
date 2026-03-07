#!/usr/bin/env bash
set -euo pipefail

REPO="jassg-to/mural-digital"
INSTALL_DIR="$HOME/.local/bin"
CONTENT_DIR="$HOME/mural-digital/content"

# ── 1. System packages ────────────────────────────────────────────────────────
echo "Installing system packages..."
sudo apt update
sudo apt install -y \
  xinit ratpoison cec-utils \
  libgl1 \
  libx11-6 libxrandr2 libxinerama1 libxcursor1 libxi6 libxxf86vm1

# ── 2. Binary from GitHub Releases ───────────────────────────────────────────
ARCH=$(uname -m)
case "$ARCH" in
  aarch64) ARCH_TAG="arm64" ;;
  armv7l)  ARCH_TAG="arm"   ;;
  x86_64)  ARCH_TAG="amd64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

BINARY_URL="https://github.com/$REPO/releases/latest/download/mural-digital_linux_$ARCH_TAG"
echo "Downloading mural-digital ($ARCH_TAG)..."
mkdir -p "$INSTALL_DIR"
curl -fsSL "$BINARY_URL" -o "$INSTALL_DIR/mural-digital"
chmod +x "$INSTALL_DIR/mural-digital"

# Ensure ~/.local/bin is on PATH in future shells
if ! grep -q 'local/bin' "$HOME/.bashrc" 2>/dev/null; then
  echo 'export PATH="$HOME/.local/bin:$PATH"' >> "$HOME/.bashrc"
fi
export PATH="$HOME/.local/bin:$PATH"

# ── 3. Dotfiles ───────────────────────────────────────────────────────────────
echo "Writing dotfiles..."

cat > "$HOME/.ratpoisonrc" <<'EOF'
set border 0
EOF

cat > "$HOME/.xinitrc" <<'EOF'
ratpoison &
cd ~/mural-digital
exec mural-digital
EOF

# ── 4. Content directory + sample schedule ────────────────────────────────────
mkdir -p "$CONTENT_DIR"

if [ ! -f "$CONTENT_DIR/schedule.toml" ]; then
  cat > "$CONTENT_DIR/schedule.toml" <<'EOF'
reload_time = "01:00"  # reload this file daily at this time (HH:MM)

[weekday]
monday    = [{ on = "08:00", off = "12:00" }, { on = "13:30", off = "22:00" }]
tuesday   = [{ on = "08:00", off = "12:00" }, { on = "13:30", off = "22:00" }]
wednesday = [{ on = "08:00", off = "12:00" }, { on = "13:30", off = "22:00" }]
thursday  = [{ on = "08:00", off = "12:00" }, { on = "13:30", off = "22:00" }]
friday    = [{ on = "08:00", off = "12:00" }, { on = "13:30", off = "22:00" }]
saturday  = [{ on = "10:00", off = "18:00" }]
sunday    = []   # off all day
EOF
fi

# ── 5. Done ───────────────────────────────────────────────────────────────────
echo ""
echo "mural-digital installed successfully."
echo ""
echo "Next steps:"
echo "  1. Copy images (JPG/PNG) into $CONTENT_DIR"
echo "  2. Edit $CONTENT_DIR/schedule.toml to set your display hours"
echo "  3. Type 'startx' to launch"
echo ""

# ── 6. Offer full system setup ────────────────────────────────────────────────
echo "Detected: running on tty1 with admin access."
echo ""
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
    ***  Waiting 10 seconds before restarting...             ***
    ***  Press Ctrl+C to enter the system shell.             ***
    ************************************************************
BANNER
sleep 10
GUARDEOF
    chmod +x "$INSTALL_DIR/tty1-guard.sh"

    # Add tty1-guard hook to .bashrc (idempotent)
    if ! grep -q "tty1-guard.sh" "$HOME/.bashrc" 2>/dev/null; then
      cat >> "$HOME/.bashrc" <<'BASHRCEOF'

if bash "$HOME/.local/bin/tty1-guard.sh"; then
  exit 1
fi
BASHRCEOF
    fi

    # Configure console autologin via systemd drop-in
    CURRENT_USER=$(id -un)
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
    echo "Skipped. Re-run this script on tty1 any time to set it up."
    ;;
esac
