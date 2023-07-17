#!/usr/bin/env bash

sudo apt install -y xinit ratpoison git python3-tk python3-pil.imagetk python3-yaml cec-utils

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )

ln -sf "$SCRIPT_DIR/.ratpoisonrc" ~/
ln -sf "$SCRIPT_DIR/.xinitrc" ~/
cat >> ~/.bashrc <<EOF

if PYTHONPATH="$SCRIPT_DIR/.." bash "$SCRIPT_DIR/tty1-guard.sh"; then
  exit 1
fi

EOF
