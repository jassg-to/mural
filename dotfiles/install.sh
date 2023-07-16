#!/usr/bin/env bash

sudo apt install -y xinit ratpoison git python3-tk python3-pil.imagetk cec-utils

cd
PATH=$(find -name mural-digital | head 1)

ln -sf "$PATH/dotfiles/.ratpoisonrc" ~/
ln -sf "$PATH/dotfiles/.xinitrc" ~/
cat >> ~/.bashrc <<EOF

if PYTHONPATH="$PATH" bash "$PATH/dotfiles/tty1-guard.sh"; then
  exit 1
fi

EOF
