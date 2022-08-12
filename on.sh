#!/bin/bash

DISPLAY=:0 xdotool key 'ctrl+F5'
echo 'on 0' | cec-client -s
