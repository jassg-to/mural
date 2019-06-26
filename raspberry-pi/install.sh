#!/bin/bash

sudo apt update
sudo apt upgrade -y
sudo apt install -y firefox-esr xserver-xorg xinit git xdotool cec-utils

cd
git clone https://github.com/jassg-to/mural-digital.git
