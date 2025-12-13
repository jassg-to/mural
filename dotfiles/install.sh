#!/usr/bin/env bash

sudo apt install -y xinit ratpoison git cec-utils
curl -LsSf https://astral.sh/uv/install.sh | sh

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

ln -sf "$SCRIPT_DIR/.ratpoisonrc" ~/
ln -sf "$SCRIPT_DIR/.xinitrc" ~/
cat >> ~/.bashrc <<EOF

if bash "$SCRIPT_DIR/tty1-guard.sh"; then
  exit 1
fi

EOF
